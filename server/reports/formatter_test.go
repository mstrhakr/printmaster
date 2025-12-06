package reports

import (
	"strings"
	"testing"
)

func TestFormatter_FormatCSV(t *testing.T) {
	t.Parallel()

	f := NewFormatter()

	result := &GenerateResult{
		Columns: []string{"serial", "model", "ip"},
		Rows: []map[string]any{
			{"serial": "SN001", "model": "HP LaserJet", "ip": "192.168.1.10"},
			{"serial": "SN002", "model": "Canon", "ip": "192.168.1.11"},
		},
		RowCount: 2,
	}

	csvBytes, err := f.FormatCSV(result)
	if err != nil {
		t.Fatalf("FormatCSV failed: %v", err)
	}

	csv := string(csvBytes)

	// Check header
	if !strings.Contains(csv, "serial") {
		t.Error("CSV should contain column headers")
	}

	// Check data rows
	if !strings.Contains(csv, "SN001") {
		t.Error("CSV should contain SN001")
	}
	if !strings.Contains(csv, "HP LaserJet") {
		t.Error("CSV should contain HP LaserJet")
	}

	// Check row count
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 3 { // header + 2 data rows
		t.Errorf("expected 3 lines (header + 2 rows), got %d", len(lines))
	}
}

func TestFormatter_FormatCSV_EmptyResult(t *testing.T) {
	t.Parallel()

	f := NewFormatter()

	result := &GenerateResult{
		Columns:  []string{"serial", "model"},
		Rows:     []map[string]any{},
		RowCount: 0,
		Summary:  map[string]any{"total": 0},
	}

	csvBytes, err := f.FormatCSV(result)
	if err != nil {
		t.Fatalf("FormatCSV failed: %v", err)
	}

	csv := string(csvBytes)
	// Should have at least some output (summary converted to row)
	if len(csv) == 0 {
		t.Error("CSV should have some output")
	}
}

func TestFormatter_FormatCSV_SpecialCharacters(t *testing.T) {
	t.Parallel()

	f := NewFormatter()

	result := &GenerateResult{
		Columns: []string{"name", "description"},
		Rows: []map[string]any{
			{"name": "Test, Printer", "description": "Has \"quotes\" and commas"},
		},
		RowCount: 1,
	}

	csvBytes, err := f.FormatCSV(result)
	if err != nil {
		t.Fatalf("FormatCSV failed: %v", err)
	}

	csv := string(csvBytes)
	// CSV should properly escape quotes and commas
	if !strings.Contains(csv, "\"Test, Printer\"") {
		t.Error("CSV should quote fields containing commas")
	}
}

func TestFormatter_FormatJSON(t *testing.T) {
	t.Parallel()

	f := NewFormatter()

	result := &GenerateResult{
		Columns: []string{"serial", "model"},
		Rows: []map[string]any{
			{"serial": "SN001", "model": "HP LaserJet"},
		},
		RowCount: 1,
		Metadata: map[string]string{
			"report_type": "inventory.devices",
		},
	}

	jsonBytes, err := f.FormatJSON(result, false)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	jsonData := string(jsonBytes)
	// Check for expected content
	if !strings.Contains(jsonData, "SN001") {
		t.Error("JSON should contain SN001")
	}
	if !strings.Contains(jsonData, "row_count") {
		t.Error("JSON should contain row_count")
	}
	if !strings.Contains(jsonData, "inventory.devices") {
		t.Error("JSON should contain report type in metadata")
	}
}

func TestFormatter_FormatJSON_EmptyResult(t *testing.T) {
	t.Parallel()

	f := NewFormatter()

	result := &GenerateResult{
		Columns:  []string{"serial"},
		Rows:     []map[string]any{},
		RowCount: 0,
	}

	jsonBytes, err := f.FormatJSON(result, false)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	jsonData := string(jsonBytes)
	if !strings.Contains(jsonData, "\"row_count\":0") {
		t.Error("JSON should have row_count: 0")
	}
}

func TestFormatter_FormatJSON_Pretty(t *testing.T) {
	t.Parallel()

	f := NewFormatter()

	result := &GenerateResult{
		Columns: []string{"serial"},
		Rows: []map[string]any{
			{"serial": "SN001"},
		},
		RowCount: 1,
	}

	jsonBytes, err := f.FormatJSON(result, true)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	jsonData := string(jsonBytes)
	// Pretty format should have indentation
	if !strings.Contains(jsonData, "  ") {
		t.Error("Pretty JSON should have indentation")
	}
}
