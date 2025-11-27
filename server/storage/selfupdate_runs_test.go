package storage

import (
	"context"
	"testing"
	"time"
)

func TestSelfUpdateRunLifecycle(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	run := &SelfUpdateRun{
		Status:         SelfUpdateStatusChecking,
		CurrentVersion: "0.9.4",
		TargetVersion:  "0.9.5",
		Channel:        "stable",
		Platform:       "windows",
		Arch:           "amd64",
		RequestedBy:    "unit-test",
		Metadata: map[string]any{
			"reason": "unit",
		},
	}

	if err := store.CreateSelfUpdateRun(ctx, run); err != nil {
		t.Fatalf("CreateSelfUpdateRun: %v", err)
	}
	if run.ID == 0 {
		t.Fatalf("expected run ID to be assigned")
	}
	if run.RequestedAt.IsZero() {
		t.Fatalf("expected requested_at to be set")
	}

	run.Status = SelfUpdateStatusSucceeded
	run.StartedAt = run.RequestedAt
	run.CompletedAt = run.RequestedAt.Add(2 * time.Minute)
	run.Metadata["result"] = "ok"

	if err := store.UpdateSelfUpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateSelfUpdateRun: %v", err)
	}

	reloaded, err := store.GetSelfUpdateRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetSelfUpdateRun: %v", err)
	}
	if reloaded == nil || reloaded.ID != run.ID {
		t.Fatalf("expected run %d, got %#v", run.ID, reloaded)
	}

	runs, err := store.ListSelfUpdateRuns(ctx, 10)
	if err != nil {
		t.Fatalf("ListSelfUpdateRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly one run, got %d", len(runs))
	}

	got := runs[0]
	if got.Status != SelfUpdateStatusSucceeded {
		t.Fatalf("unexpected status: %s", got.Status)
	}
	if got.CompletedAt.IsZero() {
		t.Fatalf("expected CompletedAt to be set")
	}
	if got.Metadata["result"] != "ok" {
		t.Fatalf("expected metadata to persist, got %#v", got.Metadata)
	}
	if got.RequestedBy != "unit-test" {
		t.Fatalf("unexpected requested_by: %s", got.RequestedBy)
	}
}
