package packager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printmaster/server/storage"
)

func TestManagerBuildsAndCachesInstaller(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	artifact := seedArtifact(t, store)
	builder := &fakeBuilder{format: "msi"}
	manager := newTestManager(t, store, builder)

	req := BuildRequest{
		TenantID:  "tenant-123",
		Component: artifact.Component,
		Version:   artifact.Version,
		Platform:  artifact.Platform,
		Arch:      artifact.Arch,
		Format:    "msi",
		OverlayFiles: []OverlayFile{
			{Path: "config/bootstrap.toml", Data: []byte("tenant_id=tenant-123")},
		},
		Metadata: map[string]interface{}{"requested_by": "unit"},
		TTL:      2 * time.Hour,
	}

	ctx := context.Background()
	bundle, err := manager.BuildInstaller(ctx, req)
	if err != nil {
		t.Fatalf("BuildInstaller returned error: %v", err)
	}
	if bundle == nil {
		t.Fatalf("expected bundle, got nil")
	}
	if builder.calls != 1 {
		t.Fatalf("expected builder to run once, ran %d times", builder.calls)
	}
	if bundle.SourceArtifactID != artifact.ID {
		t.Fatalf("expected source artifact id %d, got %d", artifact.ID, bundle.SourceArtifactID)
	}
	if _, err := os.Stat(bundle.BundlePath); err != nil {
		t.Fatalf("bundle path missing: %v", err)
	}
	if bundle.MetadataJSON == "" {
		t.Fatalf("expected metadata json to be stored")
	}
	var metadata map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(bundle.MetadataJSON), &metadata); err != nil {
		t.Fatalf("invalid metadata json: %v", err)
	}
	if metadata["request"]["requested_by"] != "unit" {
		t.Fatalf("request metadata not preserved")
	}
	if metadata["builder"]["bundle"] == "" {
		t.Fatalf("builder metadata missing bundle detail")
	}

	cached, err := manager.BuildInstaller(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error retrieving cached bundle: %v", err)
	}
	if cached.ID != bundle.ID {
		t.Fatalf("expected cached bundle id %d, got %d", bundle.ID, cached.ID)
	}
	if builder.calls != 1 {
		t.Fatalf("builder should not rerun when cache hit: calls=%d", builder.calls)
	}
}

func TestManagerRebuildsWhenBundleMissing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	artifact := seedArtifact(t, store)
	builder := &fakeBuilder{format: "msi"}
	manager := newTestManager(t, store, builder)

	req := BuildRequest{
		TenantID:  "tenant-abc",
		Component: artifact.Component,
		Version:   artifact.Version,
		Platform:  artifact.Platform,
		Arch:      artifact.Arch,
		Format:    "msi",
	}

	ctx := context.Background()
	bundle, err := manager.BuildInstaller(ctx, req)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	if err := os.Remove(bundle.BundlePath); err != nil {
		t.Fatalf("failed to remove bundle: %v", err)
	}

	_, err = manager.BuildInstaller(ctx, req)
	if err != nil {
		t.Fatalf("expected rebuild without error, got %v", err)
	}
	if builder.calls != 2 {
		t.Fatalf("expected builder to execute twice, got %d", builder.calls)
	}
	if !bundle.Encrypted {
		t.Fatalf("expected bundles to be encrypted")
	}
	if bundle.EncryptionKeyID == "" {
		t.Fatalf("expected encryption key id to be recorded")
	}
}

func TestManagerOpenBundleDecrypts(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	artifact := seedArtifact(t, store)
	builder := &fakeBuilder{format: "zip"}
	manager := newTestManager(t, store, builder)
	req := BuildRequest{
		TenantID:  "bundle-open",
		Component: artifact.Component,
		Version:   artifact.Version,
		Platform:  artifact.Platform,
		Arch:      artifact.Arch,
		Format:    "zip",
	}
	ctx := context.Background()
	bundle, err := manager.BuildInstaller(ctx, req)
	if err != nil {
		t.Fatalf("BuildInstaller failed: %v", err)
	}
	sealed, err := os.ReadFile(bundle.BundlePath)
	if err != nil {
		t.Fatalf("failed to read sealed bundle: %v", err)
	}
	if bytes.Contains(sealed, []byte("payload")) {
		t.Fatalf("sealed bundle should not contain plaintext payload")
	}
	handle, err := manager.OpenBundle(ctx, bundle)
	if err != nil {
		t.Fatalf("OpenBundle failed: %v", err)
	}
	defer handle.Close()
	data, err := io.ReadAll(handle)
	if err != nil {
		t.Fatalf("failed to read decrypted bundle: %v", err)
	}
	if !bytes.Contains(data, []byte("payload:")) {
		t.Fatalf("decrypted payload unexpected: %q", data)
	}
	if handle.Size() != int64(len(data)) {
		t.Fatalf("size mismatch: handle reports %d, read %d", handle.Size(), len(data))
	}
}

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedArtifact(t *testing.T, store storage.Store) *storage.ReleaseArtifact {
	t.Helper()
	tmp := t.TempDir()
	artifactPath := filepath.Join(tmp, "printmaster-agent-v1.2.3-windows-amd64.msi")
	content := []byte("binary")
	if err := os.WriteFile(artifactPath, content, 0o644); err != nil {
		t.Fatalf("failed to write artifact: %v", err)
	}
	record := &storage.ReleaseArtifact{
		Component:    "agent",
		Version:      "1.2.3",
		Platform:     "windows",
		Arch:         "amd64",
		Channel:      "stable",
		SourceURL:    "https://example.com/agent.msi",
		CachePath:    artifactPath,
		SHA256:       "abc123",
		SizeBytes:    int64(len(content)),
		DownloadedAt: time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	ctx := context.Background()
	if err := store.UpsertReleaseArtifact(ctx, record); err != nil {
		t.Fatalf("failed to upsert artifact: %v", err)
	}
	saved, err := store.GetReleaseArtifact(ctx, record.Component, record.Version, record.Platform, record.Arch)
	if err != nil {
		t.Fatalf("failed to reload artifact: %v", err)
	}
	return saved
}

func newTestManager(t *testing.T, store storage.Store, builder Builder) *Manager {
	t.Helper()
	root := t.TempDir()
	cache := filepath.Join(root, "installers")
	keyPath := filepath.Join(root, "bundles.key")
	manager, err := NewManager(store, nil, ManagerOptions{
		CacheDir:          cache,
		DefaultTTL:        time.Hour,
		Builders:          []Builder{builder},
		EncryptionKeyPath: keyPath,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	return manager
}

type fakeBuilder struct {
	format string
	calls  int
}

func (f *fakeBuilder) Format() string {
	if f.format == "" {
		return "msi"
	}
	return f.format
}

func (f *fakeBuilder) Build(ctx context.Context, input BuildInput) (*BuildResult, error) {
	f.calls++
	if err := os.MkdirAll(input.OutputDir, 0o755); err != nil {
		return nil, err
	}
	fileName := fmt.Sprintf("%s-%s.%s", input.Component, input.ConfigHash[:8], input.Format)
	outPath := filepath.Join(input.OutputDir, fileName)
	payload := append([]byte("payload:"), []byte(input.ConfigHash)...)
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return nil, err
	}
	return &BuildResult{
		BundlePath: outPath,
		SizeBytes:  int64(len(payload)),
		Metadata: map[string]interface{}{
			"builder": f.format,
			"bundle":  filepath.Base(outPath),
		},
	}, nil
}
