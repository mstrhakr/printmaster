package storage

import (
	"context"
	"testing"
	"time"
)

func TestInstallerBundlesCRUD(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	artifact := &ReleaseArtifact{
		Component: "agent",
		Version:   "1.2.3",
		Platform:  "windows",
		Arch:      "amd64",
		Channel:   "stable",
		SourceURL: "https://example.com/agent.msi",
	}
	if err := store.UpsertReleaseArtifact(ctx, artifact); err != nil {
		t.Fatalf("failed to upsert artifact: %v", err)
	}
	recorded, err := store.GetReleaseArtifact(ctx, artifact.Component, artifact.Version, artifact.Platform, artifact.Arch)
	if err != nil {
		t.Fatalf("failed to fetch artifact: %v", err)
	}

	expires := time.Now().UTC().Add(24 * time.Hour)
	bundle := &InstallerBundle{
		TenantID:         "tenant-123",
		Component:        artifact.Component,
		Version:          artifact.Version,
		Platform:         artifact.Platform,
		Arch:             artifact.Arch,
		Format:           "msi",
		SourceArtifactID: recorded.ID,
		ConfigHash:       "hash-one",
		BundlePath:       "C:/cache/installers/tenant-123/agent-1.2.3.msi",
		SizeBytes:        2048,
		Encrypted:        true,
		EncryptionKeyID:  "unit-key",
		MetadataJSON:     `{"notes":"initial"}`,
		ExpiresAt:        expires,
	}
	if err := store.CreateInstallerBundle(ctx, bundle); err != nil {
		t.Fatalf("failed to create installer bundle: %v", err)
	}

	saved, err := store.FindInstallerBundle(ctx, bundle.TenantID, bundle.Component, bundle.Version, bundle.Platform, bundle.Arch, bundle.Format, bundle.ConfigHash)
	if err != nil {
		t.Fatalf("failed to find installer bundle: %v", err)
	}
	if saved.BundlePath != bundle.BundlePath {
		t.Fatalf("expected bundle path %s, got %s", bundle.BundlePath, saved.BundlePath)
	}

	bundle.BundlePath = "C:/cache/installers/tenant-123/new-agent.msi"
	bundle.SizeBytes = 4096
	bundle.MetadataJSON = `{"notes":"updated"}`
	if err := store.CreateInstallerBundle(ctx, bundle); err != nil {
		t.Fatalf("failed to update installer bundle: %v", err)
	}

	reloaded, err := store.GetInstallerBundle(ctx, saved.ID)
	if err != nil {
		t.Fatalf("failed to load bundle by id: %v", err)
	}
	if reloaded.SizeBytes != 4096 {
		t.Fatalf("expected updated size, got %d", reloaded.SizeBytes)
	}
	if reloaded.BundlePath != bundle.BundlePath {
		t.Fatalf("bundle path not updated")
	}
	if !reloaded.Encrypted {
		t.Fatalf("expected encrypted flag to persist")
	}
	if reloaded.EncryptionKeyID != "unit-key" {
		t.Fatalf("expected encryption key id to persist, got %s", reloaded.EncryptionKeyID)
	}

	list, err := store.ListInstallerBundles(ctx, bundle.TenantID, 10)
	if err != nil {
		t.Fatalf("failed to list bundles: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(list))
	}

	// Expire bundle and ensure cleanup works.
	bundle.ExpiresAt = time.Now().UTC().Add(-1 * time.Hour)
	if err := store.CreateInstallerBundle(ctx, bundle); err != nil {
		t.Fatalf("failed to set expired bundle: %v", err)
	}
	deleted, err := store.DeleteExpiredInstallerBundles(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to delete expired bundles: %v", err)
	}
	if deleted == 0 {
		t.Fatalf("expected expired bundle to be deleted")
	}

	// Ensure targeted delete works (no-op if already gone)
	if err := store.DeleteInstallerBundle(ctx, saved.ID); err != nil {
		t.Fatalf("failed to delete bundle by id: %v", err)
	}
}
