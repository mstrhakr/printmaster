package storage

import (
	"context"
	"testing"
	"time"
)

func TestReleaseArtifactsCRUD(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	publishedAt := time.Now().UTC().Add(-2 * time.Hour)
	downloadedAt := time.Now().UTC()

	art := &ReleaseArtifact{
		Component:    "server",
		Version:      "0.9.16",
		Platform:     "windows",
		Arch:         "amd64",
		Channel:      "stable",
		SourceURL:    "https://example.com/server.exe",
		CachePath:    "C:/temp/printmaster-server.exe",
		SHA256:       "deadbeef",
		SizeBytes:    1024,
		ReleaseNotes: "Initial server test release",
		PublishedAt:  publishedAt,
		DownloadedAt: downloadedAt,
	}

	if err := store.UpsertReleaseArtifact(ctx, art); err != nil {
		t.Fatalf("failed to upsert artifact: %v", err)
	}

	fetched, err := store.GetReleaseArtifact(ctx, "server", "0.9.16", "windows", "amd64")
	if err != nil {
		t.Fatalf("failed to fetch artifact: %v", err)
	}
	if fetched.CachePath != art.CachePath {
		t.Fatalf("expected cache path %s, got %s", art.CachePath, fetched.CachePath)
	}
	if fetched.ReleaseNotes != art.ReleaseNotes {
		t.Fatalf("release notes mismatch")
	}

	// Update existing artifact metadata
	art.CachePath = "C:/temp/new-location/server.exe"
	art.SHA256 = "feedcafe"
	art.SizeBytes = 2048
	if err := store.UpsertReleaseArtifact(ctx, art); err != nil {
		t.Fatalf("failed to update artifact: %v", err)
	}

	updated, err := store.GetReleaseArtifact(ctx, "server", "0.9.16", "windows", "amd64")
	if err != nil {
		t.Fatalf("failed to fetch updated artifact: %v", err)
	}
	if updated.CachePath != art.CachePath {
		t.Fatalf("expected updated cache path %s, got %s", art.CachePath, updated.CachePath)
	}
	if updated.SHA256 != art.SHA256 {
		t.Fatalf("expected sha %s, got %s", art.SHA256, updated.SHA256)
	}

	// Insert a second artifact to validate listing and component filtering
	artAgent := &ReleaseArtifact{
		Component: "agent",
		Version:   "0.9.12",
		Platform:  "linux",
		Arch:      "amd64",
		SourceURL: "https://example.com/agent",
		CachePath: "/tmp/agent",
		SHA256:    "abcd",
		SizeBytes: 512,
	}
	if err := store.UpsertReleaseArtifact(ctx, artAgent); err != nil {
		t.Fatalf("failed to upsert agent artifact: %v", err)
	}

	serverArtifacts, err := store.ListReleaseArtifacts(ctx, "server", 10)
	if err != nil {
		t.Fatalf("failed to list server artifacts: %v", err)
	}
	if len(serverArtifacts) != 1 {
		t.Fatalf("expected 1 server artifact, got %d", len(serverArtifacts))
	}

	allArtifacts, err := store.ListReleaseArtifacts(ctx, "", 0)
	if err != nil {
		t.Fatalf("failed to list all artifacts: %v", err)
	}
	if len(allArtifacts) != 2 {
		t.Fatalf("expected 2 artifacts total, got %d", len(allArtifacts))
	}
}

func TestSigningKeysAndManifests(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	key := &SigningKey{
		ID:         "key-test",
		Algorithm:  "ed25519",
		PublicKey:  "pub",
		PrivateKey: "priv",
		Notes:      "initial",
	}
	if err := store.CreateSigningKey(ctx, key); err != nil {
		t.Fatalf("failed to create signing key: %v", err)
	}
	if err := store.SetSigningKeyActive(ctx, key.ID); err != nil {
		t.Fatalf("failed to activate key: %v", err)
	}
	active, err := store.GetActiveSigningKey(ctx)
	if err != nil {
		t.Fatalf("failed to get active key: %v", err)
	}
	if active == nil || active.ID != key.ID {
		t.Fatalf("active key mismatch")
	}
	keys, err := store.ListSigningKeys(ctx, 10)
	if err != nil {
		t.Fatalf("failed to list keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	manifest := &ReleaseManifest{
		Component:       "agent",
		Version:         "0.9.1",
		Platform:        "windows",
		Arch:            "amd64",
		Channel:         "stable",
		ManifestVersion: "1.0",
		ManifestJSON:    `{"version":"0.9.1"}`,
		Signature:       "sig",
		SigningKeyID:    key.ID,
		GeneratedAt:     time.Now().UTC(),
	}
	if err := store.UpsertReleaseManifest(ctx, manifest); err != nil {
		t.Fatalf("failed to upsert manifest: %v", err)
	}
	fetched, err := store.GetReleaseManifest(ctx, "agent", "0.9.1", "windows", "amd64")
	if err != nil {
		t.Fatalf("failed to fetch manifest: %v", err)
	}
	if fetched.Signature != manifest.Signature {
		t.Fatalf("manifest signature mismatch")
	}
	manifest.Signature = "sig-new"
	manifest.ManifestJSON = `{"version":"0.9.1","channel":"stable"}`
	if err := store.UpsertReleaseManifest(ctx, manifest); err != nil {
		t.Fatalf("failed to update manifest: %v", err)
	}
	list, err := store.ListReleaseManifests(ctx, "", 0)
	if err != nil {
		t.Fatalf("failed to list manifests: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(list))
	}
}
