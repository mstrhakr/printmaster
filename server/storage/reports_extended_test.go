package storage

import (
	"context"
	"testing"
)

func TestSeedBuiltInReports(t *testing.T) {
	t.Parallel()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// First call should seed reports
	if err := store.SeedBuiltInReports(ctx); err != nil {
		t.Fatalf("SeedBuiltInReports() first call error: %v", err)
	}

	// Verify reports were created
	reports, err := store.ListReports(ctx, ReportFilter{})
	if err != nil {
		t.Fatalf("ListReports() error: %v", err)
	}

	// Should have all built-in report types
	defs := getBuiltInReportDefs()
	if len(reports) != len(defs) {
		t.Errorf("Expected %d built-in reports, got %d", len(defs), len(reports))
	}

	// All should be marked as built-in
	for _, r := range reports {
		if !r.IsBuiltIn {
			t.Errorf("Report %q should be marked as built-in", r.Name)
		}
	}

	// Second call should be a no-op (idempotent)
	if err := store.SeedBuiltInReports(ctx); err != nil {
		t.Fatalf("SeedBuiltInReports() second call error: %v", err)
	}

	// Count should remain the same
	reports2, err := store.ListReports(ctx, ReportFilter{})
	if err != nil {
		t.Fatalf("ListReports() after second seed error: %v", err)
	}
	if len(reports2) != len(reports) {
		t.Errorf("SeedBuiltInReports not idempotent: had %d reports, now have %d", len(reports), len(reports2))
	}
}

func TestGetBuiltInReportDefs(t *testing.T) {
	t.Parallel()
	defs := getBuiltInReportDefs()

	// Should have expected number of report types
	if len(defs) == 0 {
		t.Error("getBuiltInReportDefs() returned empty list")
	}

	// Verify structure of each
	for i, def := range defs {
		if def.Name == "" {
			t.Errorf("defs[%d].Name is empty", i)
		}
		if def.Description == "" {
			t.Errorf("defs[%d].Description is empty", i)
		}
		if def.Type == "" {
			t.Errorf("defs[%d].Type is empty", i)
		}
	}

	// Check specific known types are present
	typeSet := make(map[string]bool)
	for _, def := range defs {
		typeSet[def.Type] = true
	}

	expectedTypes := []string{
		string(ReportTypeDeviceInventory),
		string(ReportTypeUsageSummary),
		string(ReportTypeSuppliesStatus),
		string(ReportTypeAlertHistory),
		string(ReportTypeAgentStatus),
		string(ReportTypeFleetHealth),
	}

	for _, typ := range expectedTypes {
		if !typeSet[typ] {
			t.Errorf("getBuiltInReportDefs() missing expected type: %s", typ)
		}
	}
}
