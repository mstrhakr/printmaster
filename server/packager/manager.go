package packager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"printmaster/common/logger"
	"printmaster/server/storage"
)

var safeSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

type ManagerOptions struct {
	CacheDir          string
	DefaultTTL        time.Duration
	Builders          []Builder
	Now               func() time.Time
	EncryptionKeyPath string
}

type BuildRequest struct {
	TenantID     string
	Component    string
	Version      string
	Platform     string
	Arch         string
	Format       string
	OverlayFiles []OverlayFile
	Metadata     map[string]interface{}
	TTL          time.Duration
}

type OverlayFile struct {
	Path string
	Mode fs.FileMode
	Data []byte
}

type Builder interface {
	Format() string
	Build(ctx context.Context, input BuildInput) (*BuildResult, error)
}

type BuildInput struct {
	TenantID     string
	Component    string
	Version      string
	Platform     string
	Arch         string
	Format       string
	Artifact     *storage.ReleaseArtifact
	ArtifactPath string
	OutputDir    string
	OverlayFiles []OverlayFile
	Metadata     map[string]interface{}
	ConfigHash   string
}

type BuildResult struct {
	BundlePath string
	SizeBytes  int64
	Metadata   map[string]interface{}
}

type Manager struct {
	store      storage.Store
	log        *logger.Logger
	cacheDir   string
	defaultTTL time.Duration
	builders   map[string]Builder
	now        func() time.Time
	encryptor  *bundleEncryptor
}

func NewManager(store storage.Store, log *logger.Logger, opts ManagerOptions) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("storage store is required")
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = defaultCacheDir()
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to prepare cache dir: %w", err)
	}
	ttl := opts.DefaultTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	builders := map[string]Builder{}
	for _, b := range opts.Builders {
		if b == nil {
			continue
		}
		format := strings.TrimSpace(strings.ToLower(b.Format()))
		if format == "" {
			return nil, fmt.Errorf("builder missing format identifier")
		}
		if _, exists := builders[format]; exists {
			return nil, fmt.Errorf("duplicate builder for format %s", format)
		}
		builders[format] = b
	}
	if len(builders) == 0 {
		return nil, fmt.Errorf("at least one builder must be registered")
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	keyPath := strings.TrimSpace(opts.EncryptionKeyPath)
	if keyPath == "" {
		return nil, fmt.Errorf("encryption key path is required")
	}
	encryptor, err := newBundleEncryptor(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bundle encryption: %w", err)
	}
	return &Manager{
		store:      store,
		log:        log,
		cacheDir:   cacheDir,
		defaultTTL: ttl,
		builders:   builders,
		now:        nowFn,
		encryptor:  encryptor,
	}, nil
}

func (m *Manager) BuildInstaller(ctx context.Context, req BuildRequest) (*storage.InstallerBundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalizedReq, err := m.normalizeRequest(req)
	if err != nil {
		return nil, err
	}
	builder := m.builders[normalizedReq.Format]
	if builder == nil {
		return nil, fmt.Errorf("no builder registered for format %s", normalizedReq.Format)
	}
	configHash, err := computeConfigHash(normalizedReq.OverlayFiles, normalizedReq.Metadata)
	if err != nil {
		return nil, err
	}
	now := m.now()
	existing, err := m.store.FindInstallerBundle(ctx, normalizedReq.TenantID, normalizedReq.Component, normalizedReq.Version, normalizedReq.Platform, normalizedReq.Arch, normalizedReq.Format, configHash)
	if err == nil && bundleStillValid(existing, now) {
		if fileExists(existing.BundlePath) {
			m.logDebug("installer bundle cache hit", "tenant", normalizedReq.TenantID, "component", normalizedReq.Component, "version", normalizedReq.Version, "format", normalizedReq.Format, "bundle", existing.ID)
			return existing, nil
		}
	}
	artifact, err := m.store.GetReleaseArtifact(ctx, normalizedReq.Component, normalizedReq.Version, normalizedReq.Platform, normalizedReq.Arch)
	if err != nil {
		return nil, fmt.Errorf("release artifact missing: %w", err)
	}
	if artifact.CachePath == "" {
		return nil, fmt.Errorf("artifact %s/%s has no cache path", artifact.Component, artifact.Version)
	}
	if _, err := os.Stat(artifact.CachePath); err != nil {
		return nil, fmt.Errorf("artifact cache path invalid: %w", err)
	}
	outputDir, err := m.buildOutputDir(normalizedReq, configHash)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to prepare output dir: %w", err)
	}
	input := BuildInput{
		TenantID:     normalizedReq.TenantID,
		Component:    normalizedReq.Component,
		Version:      normalizedReq.Version,
		Platform:     normalizedReq.Platform,
		Arch:         normalizedReq.Arch,
		Format:       normalizedReq.Format,
		Artifact:     artifact,
		ArtifactPath: artifact.CachePath,
		OutputDir:    outputDir,
		OverlayFiles: normalizedReq.OverlayFiles,
		Metadata:     cloneMetadata(normalizedReq.Metadata),
		ConfigHash:   configHash,
	}
	result, err := builder.Build(ctx, input)
	if err != nil {
		return nil, err
	}
	if result == nil || result.BundlePath == "" {
		return nil, fmt.Errorf("builder %s did not return bundle path", normalizedReq.Format)
	}
	bundlePath, err := m.validateBundlePath(result.BundlePath)
	if err != nil {
		return nil, err
	}
	size := result.SizeBytes
	if size <= 0 {
		info, statErr := os.Stat(bundlePath)
		if statErr != nil {
			return nil, fmt.Errorf("failed to stat bundle: %w", statErr)
		}
		size = info.Size()
	}
	encrypted := false
	encryptionKeyID := ""
	if m.encryptor != nil {
		if _, err := m.encryptor.encryptFileInPlace(bundlePath); err != nil {
			return nil, fmt.Errorf("failed to encrypt installer bundle: %w", err)
		}
		encrypted = true
		encryptionKeyID = m.encryptor.keyIdentifier()
	}
	ttl := normalizedReq.TTL
	if ttl <= 0 {
		ttl = m.defaultTTL
	}
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	metadataJSON, err := encodeCombinedMetadata(normalizedReq.Metadata, result.Metadata)
	if err != nil {
		return nil, err
	}
	bundle := &storage.InstallerBundle{
		TenantID:         normalizedReq.TenantID,
		Component:        normalizedReq.Component,
		Version:          normalizedReq.Version,
		Platform:         normalizedReq.Platform,
		Arch:             normalizedReq.Arch,
		Format:           normalizedReq.Format,
		SourceArtifactID: artifact.ID,
		ConfigHash:       configHash,
		BundlePath:       bundlePath,
		SizeBytes:        size,
		Encrypted:        encrypted,
		EncryptionKeyID:  encryptionKeyID,
		MetadataJSON:     metadataJSON,
		ExpiresAt:        expiresAt,
	}
	if existing != nil {
		bundle.CreatedAt = existing.CreatedAt
	}
	if err := m.store.CreateInstallerBundle(ctx, bundle); err != nil {
		return nil, err
	}
	created, err := m.store.FindInstallerBundle(ctx, normalizedReq.TenantID, normalizedReq.Component, normalizedReq.Version, normalizedReq.Platform, normalizedReq.Arch, normalizedReq.Format, configHash)
	if err != nil {
		return nil, err
	}
	m.logInfo("installer bundle generated", "tenant", normalizedReq.TenantID, "component", normalizedReq.Component, "version", normalizedReq.Version, "format", normalizedReq.Format, "size_bytes", created.SizeBytes)
	return created, nil
}

// OpenBundle prepares an installer bundle for download, decrypting it on the fly when required.
func (m *Manager) OpenBundle(ctx context.Context, bundle *storage.InstallerBundle) (*BundleHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, fmt.Errorf("bundle reference required")
	}
	if strings.TrimSpace(bundle.BundlePath) == "" {
		return nil, fmt.Errorf("bundle path missing")
	}
	info, err := os.Stat(bundle.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("bundle unavailable: %w", err)
	}
	filename := filepath.Base(bundle.BundlePath)
	if !bundle.Encrypted {
		file, err := os.Open(bundle.BundlePath)
		if err != nil {
			return nil, err
		}
		return newBundleHandle(file, filename, info.ModTime(), bundle.SizeBytes, func() error {
			return file.Close()
		}), nil
	}
	if m.encryptor == nil {
		return nil, fmt.Errorf("bundle requires encryption but manager is not configured")
	}
	if bundle.EncryptionKeyID != "" && bundle.EncryptionKeyID != m.encryptor.keyIdentifier() {
		return nil, fmt.Errorf("bundle encrypted with unknown key id %s", bundle.EncryptionKeyID)
	}
	plain, err := m.encryptor.decryptFile(bundle.BundlePath)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(plain)
	return newBundleHandle(reader, filename, info.ModTime(), int64(len(plain)), func() error {
		zeroBytes(plain)
		return nil
	}), nil
}

func (m *Manager) normalizeRequest(req BuildRequest) (BuildRequest, error) {
	norm := BuildRequest{
		TenantID:  strings.TrimSpace(req.TenantID),
		Component: strings.ToLower(strings.TrimSpace(req.Component)),
		Version:   strings.TrimSpace(req.Version),
		Platform:  strings.ToLower(strings.TrimSpace(req.Platform)),
		Arch:      strings.ToLower(strings.TrimSpace(req.Arch)),
		Format:    strings.ToLower(strings.TrimSpace(req.Format)),
		TTL:       req.TTL,
	}
	if norm.TenantID == "" || norm.Component == "" || norm.Version == "" || norm.Platform == "" || norm.Arch == "" || norm.Format == "" {
		return BuildRequest{}, fmt.Errorf("tenant, component, version, platform, arch, and format are required")
	}
	var err error
	if norm.TenantID, err = sanitizeSegment(norm.TenantID); err != nil {
		return BuildRequest{}, err
	}
	if norm.Component, err = sanitizeSegment(norm.Component); err != nil {
		return BuildRequest{}, err
	}
	if norm.Version, err = sanitizeSegment(norm.Version); err != nil {
		return BuildRequest{}, err
	}
	if norm.Platform, err = sanitizeSegment(norm.Platform); err != nil {
		return BuildRequest{}, err
	}
	if norm.Arch, err = sanitizeSegment(norm.Arch); err != nil {
		return BuildRequest{}, err
	}
	if norm.Format, err = sanitizeSegment(norm.Format); err != nil {
		return BuildRequest{}, err
	}
	files, err := normalizeOverlayFiles(req.OverlayFiles)
	if err != nil {
		return BuildRequest{}, err
	}
	norm.OverlayFiles = files
	norm.Metadata = cloneMetadata(req.Metadata)
	return norm, nil
}

func (m *Manager) buildOutputDir(req BuildRequest, configHash string) (string, error) {
	formatSegment := req.Format
	if len(configHash) > 16 {
		configHash = configHash[:16]
	}
	versionSegment := "v" + req.Version
	platformArch := req.Platform + "-" + req.Arch
	dir := filepath.Join(m.cacheDir, req.TenantID, req.Component, versionSegment, platformArch, formatSegment, configHash)
	return dir, nil
}

func (m *Manager) validateBundlePath(path string) (string, error) {
	absBundle, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve bundle path: %w", err)
	}
	absCache, err := filepath.Abs(m.cacheDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absCache, absBundle)
	if err != nil {
		return "", fmt.Errorf("bundle path outside cache: %w", err)
	}
	rel = filepath.Clean(rel)
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("bundle path escapes cache directory")
	}
	return absBundle, nil
}

func bundleStillValid(bundle *storage.InstallerBundle, now time.Time) bool {
	if bundle == nil {
		return false
	}
	if !bundle.ExpiresAt.IsZero() && !bundle.ExpiresAt.After(now) {
		return false
	}
	return true
}

func computeConfigHash(files []OverlayFile, metadata map[string]interface{}) (string, error) {
	hasher := sha256.New()
	for _, file := range files {
		hasher.Write([]byte(file.Path))
		mode := file.Mode
		if mode == 0 {
			mode = 0o640
		}
		hasher.Write([]byte(fmt.Sprintf("%04o", mode)))
		hasher.Write(file.Data)
	}
	metaBytes, err := canonicalizeMetadata(metadata)
	if err != nil {
		return "", err
	}
	if len(metaBytes) > 0 {
		hasher.Write(metaBytes)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func canonicalizeMetadata(meta map[string]interface{}) ([]byte, error) {
	if len(meta) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	var generic interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	normalized, err := normalizeValue(generic)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

// BundleHandle provides read/seek/close semantics plus metadata for HTTP handlers.
type BundleHandle struct {
	reader  io.ReadSeeker
	closeFn func() error
	name    string
	modTime time.Time
	size    int64
}

func newBundleHandle(reader io.ReadSeeker, name string, mod time.Time, size int64, closeFn func() error) *BundleHandle {
	return &BundleHandle{reader: reader, name: name, modTime: mod, size: size, closeFn: closeFn}
}

// NewBundleHandle exposes bundle handles for custom packager implementations/testing.
func NewBundleHandle(reader io.ReadSeeker, name string, mod time.Time, size int64, closeFn func() error) *BundleHandle {
	return newBundleHandle(reader, name, mod, size, closeFn)
}

func (b *BundleHandle) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *BundleHandle) Seek(offset int64, whence int) (int64, error) {
	return b.reader.Seek(offset, whence)
}

func (b *BundleHandle) Close() error {
	if b.closeFn != nil {
		return b.closeFn()
	}
	return nil
}

// Name returns the suggested filename for download responses.
func (b *BundleHandle) Name() string {
	return b.name
}

// Size reports the plaintext byte length for the installer bundle.
func (b *BundleHandle) Size() int64 {
	return b.size
}

// ModTime captures the last-modified timestamp associated with the installer bundle.
func (b *BundleHandle) ModTime() time.Time {
	return b.modTime
}

type orderedMap struct {
	entries []orderedKV
}

type orderedKV struct {
	key   string
	value interface{}
}

func (o orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, entry := range o.entries {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, err := json.Marshal(entry.key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valueBytes, err := json.Marshal(entry.value)
		if err != nil {
			return nil, err
		}
		buf.Write(valueBytes)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func normalizeValue(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]orderedKV, 0, len(keys))
		for _, key := range keys {
			normalized, err := normalizeValue(v[key])
			if err != nil {
				return nil, err
			}
			entries = append(entries, orderedKV{key: key, value: normalized})
		}
		return orderedMap{entries: entries}, nil
	case []interface{}:
		out := make([]interface{}, len(v))
		for i := range v {
			normalized, err := normalizeValue(v[i])
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return v, nil
	}
}

func normalizeOverlayFiles(files []OverlayFile) ([]OverlayFile, error) {
	if len(files) == 0 {
		return nil, nil
	}
	normalized := make([]OverlayFile, len(files))
	seen := make(map[string]struct{}, len(files))
	for i, file := range files {
		cleaned := strings.ReplaceAll(file.Path, "\\", "/")
		cleaned = path.Clean(strings.TrimSpace(cleaned))
		if cleaned == "." || cleaned == "" {
			return nil, fmt.Errorf("overlay file path required")
		}
		if strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "..\\") || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, ":") {
			return nil, fmt.Errorf("overlay path %s is invalid", file.Path)
		}
		if _, exists := seen[cleaned]; exists {
			return nil, fmt.Errorf("duplicate overlay path %s", cleaned)
		}
		seen[cleaned] = struct{}{}
		normalized[i] = file
		normalized[i].Path = cleaned
		if normalized[i].Mode == 0 {
			normalized[i].Mode = 0o640
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Path < normalized[j].Path
	})
	return normalized, nil
}

func sanitizeSegment(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path segment cannot be empty")
	}
	if !safeSegmentPattern.MatchString(value) {
		return "", fmt.Errorf("invalid path segment %s", value)
	}
	return value, nil
}

func encodeCombinedMetadata(requestMeta, builderMeta map[string]interface{}) (string, error) {
	payload := map[string]interface{}{}
	if len(requestMeta) > 0 {
		payload["request"] = requestMeta
	}
	if len(builderMeta) > 0 {
		payload["builder"] = builderMeta
	}
	if len(payload) == 0 {
		return "", nil
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func cloneMetadata(meta map[string]interface{}) map[string]interface{} {
	if len(meta) == 0 {
		return nil
	}
	clone := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		clone[k] = v
	}
	return clone
}

func defaultCacheDir() string {
	return filepath.Join(os.TempDir(), "printmaster", "installers")
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (m *Manager) logInfo(msg string, args ...interface{}) {
	if m.log != nil {
		m.log.Info(msg, args...)
	}
}

func (m *Manager) logDebug(msg string, args ...interface{}) {
	if m.log != nil {
		m.log.Debug(msg, args...)
	}
}
