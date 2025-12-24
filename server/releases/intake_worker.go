package releases

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"printmaster/common/logger"
	"printmaster/server/storage"
)

const (
	defaultRepoOwner    = "mstrhakr"
	defaultRepoName     = "printmaster"
	defaultPollInterval = 4 * time.Hour
	defaultMaxReleases  = 6
)

var safeSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Options control IntakeWorker behavior.
type Options struct {
	CacheDir          string
	PollInterval      time.Duration
	RepoOwner         string
	RepoName          string
	BaseAPIURL        string
	GitHubToken       string
	HTTPClient        *http.Client
	MaxReleases       int
	RetentionVersions int // 0 = disabled (keep all), N = keep N versions per component
	UserAgent         string
	ManifestManager   *Manager
}

type IntakeWorker struct {
	store             storage.Store
	log               *logger.Logger
	cacheDir          string
	pollInterval      time.Duration
	repoOwner         string
	repoName          string
	baseAPIURL        string
	client            *http.Client
	maxReleases       int
	retentionVersions int
	token             string
	userAgent         string
	manifests         *Manager
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string    `json:"name"`
	BrowserDownloadURL string    `json:"browser_download_url"`
	Size               int64     `json:"size"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type artifactDescriptor struct {
	component string
	version   string
	platform  string
	arch      string
	fileName  string
}

// NewIntakeWorker wires release intake with sane defaults.
func NewIntakeWorker(store storage.Store, log *logger.Logger, opts Options) (*IntakeWorker, error) {
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "printmaster", "release-cache")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}

	poll := opts.PollInterval
	if poll <= 0 {
		poll = defaultPollInterval
	}

	repoOwner := opts.RepoOwner
	if repoOwner == "" {
		repoOwner = defaultRepoOwner
	}
	repoName := opts.RepoName
	if repoName == "" {
		repoName = defaultRepoName
	}

	baseAPI := strings.TrimRight(opts.BaseAPIURL, "/")
	if baseAPI == "" {
		baseAPI = "https://api.github.com"
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	maxReleases := opts.MaxReleases
	if maxReleases <= 0 {
		maxReleases = defaultMaxReleases
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = "printmaster-release-intake"
	}

	return &IntakeWorker{
		store:             store,
		log:               log,
		cacheDir:          cacheDir,
		pollInterval:      poll,
		repoOwner:         repoOwner,
		repoName:          repoName,
		baseAPIURL:        baseAPI,
		client:            client,
		maxReleases:       maxReleases,
		retentionVersions: opts.RetentionVersions,
		token:             strings.TrimSpace(opts.GitHubToken),
		userAgent:         userAgent,
		manifests:         opts.ManifestManager,
	}, nil
}

// Run starts the periodic release intake loop.
func (w *IntakeWorker) Run(ctx context.Context) {
	w.logInfo("release intake worker started", "cache_dir", w.cacheDir, "interval", w.pollInterval.String())
	if err := w.runOnce(ctx); err != nil {
		w.logWarn("initial release intake failed", "error", err)
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logInfo("release intake worker stopping")
			return
		case <-ticker.C:
			if err := w.runOnce(ctx); err != nil {
				w.logWarn("release intake iteration failed", "error", err)
			}
		}
	}
}

// RunOnce executes a single fetch cycle (exported for tests).
func (w *IntakeWorker) RunOnce(ctx context.Context) error {
	return w.runOnce(ctx)
}

func (w *IntakeWorker) runOnce(ctx context.Context) error {
	releases, err := w.fetchReleases(ctx)
	if err != nil {
		return err
	}

	processed := map[string]int{}
	for _, rel := range releases {
		component, version := parseTag(rel.TagName)
		if component == "" || version == "" || rel.Draft || rel.Prerelease {
			continue
		}
		if processed[component] >= w.maxReleases {
			continue
		}
		if err := w.processRelease(ctx, component, version, rel); err != nil {
			w.logWarn("release processing failed", "component", component, "version", version, "error", err)
			continue
		}
		processed[component]++
	}

	// Prune old artifacts if retention is configured
	if w.retentionVersions > 0 {
		for _, comp := range []string{"agent", "server"} {
			if err := w.pruneOldArtifacts(ctx, comp); err != nil {
				w.logWarn("artifact pruning failed", "component", comp, "error", err)
			}
		}
	}

	return nil
}

func (w *IntakeWorker) fetchReleases(ctx context.Context) ([]ghRelease, error) {
	perPage := w.maxReleases * 3
	if perPage < 10 {
		perPage = 10
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", w.baseAPIURL, w.repoOwner, w.repoName, perPage)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", w.userAgent)
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("github api error: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var releases []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (w *IntakeWorker) processRelease(ctx context.Context, component, version string, rel ghRelease) error {
	for _, asset := range rel.Assets {
		if asset.BrowserDownloadURL == "" {
			continue
		}
		desc, ok := buildDescriptor(component, version, asset.Name)
		if !ok {
			continue
		}
		if err := w.ensureArtifact(ctx, desc, rel, asset); err != nil {
			w.logWarn("artifact processing failed", "component", component, "version", version, "asset", asset.Name, "error", err)
		}
	}
	return nil
}

func (w *IntakeWorker) ensureArtifact(ctx context.Context, desc artifactDescriptor, rel ghRelease, asset ghAsset) error {
	existing, err := w.store.GetReleaseArtifact(ctx, desc.component, desc.version, desc.platform, desc.arch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existing != nil && fileExists(existing.CachePath) && existing.SHA256 != "" {
		needsMetadataUpdate := existing.SourceURL != asset.BrowserDownloadURL || existing.ReleaseNotes != rel.Body || existing.SizeBytes != asset.Size
		if needsMetadataUpdate {
			existing.SourceURL = asset.BrowserDownloadURL
			existing.ReleaseNotes = rel.Body
			existing.SizeBytes = asset.Size
			existing.PublishedAt = rel.PublishedAt
			if err := w.store.UpsertReleaseArtifact(ctx, existing); err != nil {
				return err
			}
		}
		w.ensureManifest(ctx, existing)
		return nil
	}

	cachePath, sha, size, err := w.downloadArtifact(ctx, desc, asset.BrowserDownloadURL)
	if err != nil {
		return err
	}

	record := &storage.ReleaseArtifact{
		Component:    desc.component,
		Version:      desc.version,
		Platform:     desc.platform,
		Arch:         desc.arch,
		Channel:      "stable",
		SourceURL:    asset.BrowserDownloadURL,
		CachePath:    cachePath,
		SHA256:       sha,
		SizeBytes:    size,
		ReleaseNotes: rel.Body,
		PublishedAt:  rel.PublishedAt,
		DownloadedAt: time.Now().UTC(),
	}
	if err := w.store.UpsertReleaseArtifact(ctx, record); err != nil {
		return err
	}
	w.logInfo("cached release artifact", "component", desc.component, "version", desc.version, "platform", desc.platform, "arch", desc.arch)
	w.ensureManifest(ctx, record)
	return nil
}

func (w *IntakeWorker) ensureManifest(ctx context.Context, artifact *storage.ReleaseArtifact) {
	if w.manifests == nil || artifact == nil {
		return
	}
	if _, err := w.manifests.EnsureManifestForArtifact(ctx, artifact); err != nil {
		w.logWarn("manifest generation failed", "component", artifact.Component, "version", artifact.Version, "platform", artifact.Platform, "arch", artifact.Arch, "error", err)
	}
}

func (w *IntakeWorker) downloadArtifact(ctx context.Context, desc artifactDescriptor, downloadURL string) (string, string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("User-Agent", w.userAgent)
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("download failed: %s", resp.Status)
	}

	componentDir, err := buildCacheDir(w.cacheDir, desc)
	if err != nil {
		return "", "", 0, err
	}
	if err := os.MkdirAll(componentDir, 0o755); err != nil {
		return "", "", 0, err
	}

	tempFile, err := os.CreateTemp(componentDir, "download-*.tmp")
	if err != nil {
		return "", "", 0, err
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempFile.Name())
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tempFile, hasher)
	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		return "", "", 0, err
	}
	if err := tempFile.Sync(); err != nil {
		return "", "", 0, err
	}
	if err := tempFile.Close(); err != nil {
		return "", "", 0, err
	}

	finalPath := filepath.Join(componentDir, desc.fileName)
	_ = os.Remove(finalPath)
	if err := os.Rename(tempFile.Name(), finalPath); err != nil {
		return "", "", 0, err
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	return finalPath, checksum, written, nil
}

func buildCacheDir(root string, desc artifactDescriptor) (string, error) {
	for _, part := range []string{desc.component, "v" + desc.version, desc.platform + "-" + desc.arch} {
		if !safeSegmentPattern.MatchString(part) {
			return "", fmt.Errorf("unsafe path segment: %s", part)
		}
	}
	return filepath.Join(root, desc.component, "v"+desc.version, desc.platform+"-"+desc.arch), nil
}

func parseTag(tag string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(tag), "-", 2)
	if len(parts) != 2 {
		return "", ""
	}
	component := strings.ToLower(parts[0])
	version := strings.TrimPrefix(parts[1], "v")
	if version == "" {
		return "", ""
	}
	if component != "agent" && component != "server" {
		return "", ""
	}
	return component, version
}

func buildDescriptor(component, version, assetName string) (artifactDescriptor, bool) {
	prefix := fmt.Sprintf("printmaster-%s-v%s-", component, version)
	if !strings.HasPrefix(assetName, prefix) {
		return artifactDescriptor{}, false
	}
	remainder := strings.TrimPrefix(assetName, prefix)
	core := remainder
	if idx := strings.Index(core, "."); idx != -1 {
		core = core[:idx]
	}
	parts := strings.Split(core, "-")
	if len(parts) < 2 {
		return artifactDescriptor{}, false
	}
	platform := strings.ToLower(parts[0])
	arch := strings.ToLower(parts[1])
	if !safeSegmentPattern.MatchString(platform) || !safeSegmentPattern.MatchString(arch) {
		return artifactDescriptor{}, false
	}
	return artifactDescriptor{
		component: component,
		version:   version,
		platform:  platform,
		arch:      arch,
		fileName:  assetName,
	}, true
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// pruneOldArtifacts removes artifacts for versions beyond retention threshold
func (w *IntakeWorker) pruneOldArtifacts(ctx context.Context, component string) error {
	artifacts, err := w.store.ListArtifactsForPruning(ctx, component, w.retentionVersions)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return nil
	}

	var deletedCount int
	var freedBytes int64
	for _, art := range artifacts {
		// Remove cached file from disk
		if art.CachePath != "" {
			if rmErr := os.Remove(art.CachePath); rmErr != nil && !os.IsNotExist(rmErr) {
				w.logWarn("failed to remove cached artifact file", "path", art.CachePath, "error", rmErr)
			}
		}
		// Remove from database
		if err := w.store.DeleteReleaseArtifact(ctx, art.ID); err != nil {
			w.logWarn("failed to delete artifact record", "id", art.ID, "component", art.Component, "version", art.Version, "error", err)
			continue
		}
		deletedCount++
		freedBytes += art.SizeBytes
	}

	if deletedCount > 0 {
		w.logInfo("pruned old release artifacts",
			"component", component,
			"deleted", deletedCount,
			"freed_bytes", freedBytes,
			"retention", w.retentionVersions)
	}
	return nil
}

func (w *IntakeWorker) logInfo(msg string, kv ...interface{}) {
	if w.log != nil {
		w.log.Info(msg, kv...)
	}
}

func (w *IntakeWorker) logWarn(msg string, kv ...interface{}) {
	if w.log != nil {
		w.log.Warn(msg, kv...)
	}
}
