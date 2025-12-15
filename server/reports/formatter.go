package reports

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const tonerLevelsColumn = "toner_levels"

// Formatter formats report results into various output formats.
type Formatter struct{}

// NewFormatter creates a new formatter.
func NewFormatter() *Formatter {
	return &Formatter{}
}

// FormatJSON formats the result as JSON.
func (f *Formatter) FormatJSON(result *GenerateResult, pretty bool) ([]byte, error) {
	output := map[string]interface{}{
		"metadata":  result.Metadata,
		"summary":   result.Summary,
		"row_count": result.RowCount,
	}

	if result.Rows != nil {
		output["columns"] = result.Columns
		output["rows"] = result.Rows
	} else if result.Data != nil {
		output["data"] = result.Data
	}

	if pretty {
		return json.MarshalIndent(output, "", "  ")
	}
	return json.Marshal(output)
}

// FormatCSV formats the result as CSV.
func (f *Formatter) FormatCSV(result *GenerateResult) ([]byte, error) {
	if len(result.Rows) == 0 {
		// For summary-only reports, convert summary to single row
		if result.Summary != nil {
			return f.formatSummaryAsCSV(result.Summary)
		}
		return nil, fmt.Errorf("no data to format")
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Expand certain structured columns (e.g., toner_levels) into native CSV columns.
	rows := result.Rows
	columns := result.Columns
	rows, columns = expandMapColumn(rows, columns, tonerLevelsColumn, "toner_")

	// Write header
	if len(columns) == 0 && len(rows) > 0 {
		// Extract columns from first row
		for key := range rows[0] {
			columns = append(columns, key)
		}
		sort.Strings(columns) // Consistent ordering

		// If columns were inferred, re-expand after inference as well.
		rows, columns = expandMapColumn(rows, columns, tonerLevelsColumn, "toner_")
	}

	if err := writer.Write(columns); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}

	// Write data rows
	for _, row := range rows {
		record := make([]string, len(columns))
		for i, col := range columns {
			record[i] = formatValue(row[col])
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("write row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}

	return buf.Bytes(), nil
}

func expandMapColumn(rows []map[string]any, columns []string, columnName string, prefix string) ([]map[string]any, []string) {
	if len(rows) == 0 {
		return rows, columns
	}

	// Check if the column exists (either in explicit columns or in row data)
	colIdx := -1
	for i, c := range columns {
		if c == columnName {
			colIdx = i
			break
		}
	}
	if colIdx == -1 {
		// If columns are empty (or don't include it), see if any row has the field
		found := false
		for _, r := range rows {
			if r != nil {
				if _, ok := r[columnName]; ok {
					found = true
					break
				}
			}
		}
		if !found {
			return rows, columns
		}
	}

	// Collect union of keys across rows.
	keySet := map[string]struct{}{}
	for _, r := range rows {
		if r == nil {
			continue
		}
		m, ok := r[columnName].(map[string]interface{})
		if !ok || len(m) == 0 {
			continue
		}
		for k := range m {
			if k == "" {
				continue
			}
			keySet[k] = struct{}{}
		}
	}
	if len(keySet) == 0 {
		return rows, columns
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build new columns: replace the map column with expanded columns.
	expandedCols := make([]string, 0, len(columns)+len(keys))
	if len(columns) > 0 {
		for _, c := range columns {
			if c == columnName {
				for _, k := range keys {
					expandedCols = append(expandedCols, prefix+k)
				}
				continue
			}
			expandedCols = append(expandedCols, c)
		}
	} else {
		// If columns are not provided, keep them empty and only mutate row maps;
		// caller will infer columns later.
		expandedCols = columns
	}

	// Expand row data.
	for _, r := range rows {
		if r == nil {
			continue
		}
		m, ok := r[columnName].(map[string]interface{})
		if ok {
			for _, k := range keys {
				r[prefix+k] = m[k]
			}
			delete(r, columnName)
		}
	}

	return rows, expandedCols
}

// formatSummaryAsCSV formats a summary map as a simple CSV.
func (f *Formatter) formatSummaryAsCSV(summary map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Get sorted keys for consistent output
	var keys []string
	for k := range summary {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Write header
	if err := writer.Write(keys); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}

	// Write values
	values := make([]string, len(keys))
	for i, k := range keys {
		values[i] = formatValue(summary[k])
	}
	if err := writer.Write(values); err != nil {
		return nil, fmt.Errorf("write values: %w", err)
	}

	writer.Flush()
	return buf.Bytes(), writer.Error()
}

// FormatHTML formats the result as an HTML table.
func (f *Formatter) FormatHTML(result *GenerateResult, title string) ([]byte, error) {
	var buf bytes.Buffer

	// HTML header
	buf.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>` + escapeHTML(title) + `</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 20px; }
h1 { color: #333; }
table { border-collapse: collapse; width: 100%; margin-top: 20px; }
th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
th { background-color: #4a5568; color: white; }
tr:nth-child(even) { background-color: #f9f9f9; }
tr:hover { background-color: #f1f1f1; }
.summary { background: #f3f4f6; padding: 15px; border-radius: 8px; margin-bottom: 20px; }
.summary-item { display: inline-block; margin-right: 30px; }
.summary-label { font-weight: bold; color: #666; }
.summary-value { font-size: 1.2em; color: #333; }
.metadata { color: #888; font-size: 0.9em; margin-bottom: 10px; }
</style>
</head>
<body>
`)

	// Title
	buf.WriteString("<h1>" + escapeHTML(title) + "</h1>\n")

	// Metadata
	if result.Metadata != nil {
		buf.WriteString(`<div class="metadata">`)
		if gen, ok := result.Metadata["generated"]; ok {
			buf.WriteString("Generated: " + escapeHTML(gen))
		}
		buf.WriteString("</div>\n")
	}

	// Summary section
	if len(result.Summary) > 0 {
		buf.WriteString(`<div class="summary">`)
		for k, v := range result.Summary {
			buf.WriteString(`<div class="summary-item">`)
			buf.WriteString(`<span class="summary-label">` + escapeHTML(formatLabel(k)) + `: </span>`)
			buf.WriteString(`<span class="summary-value">` + escapeHTML(formatValue(v)) + `</span>`)
			buf.WriteString(`</div>`)
		}
		buf.WriteString("</div>\n")
	}

	// Data table
	if len(result.Rows) > 0 {
		buf.WriteString("<table>\n<thead><tr>\n")

		columns := result.Columns
		if len(columns) == 0 && len(result.Rows) > 0 {
			for key := range result.Rows[0] {
				columns = append(columns, key)
			}
			sort.Strings(columns)
		}

		for _, col := range columns {
			buf.WriteString("<th>" + escapeHTML(formatLabel(col)) + "</th>")
		}
		buf.WriteString("\n</tr></thead>\n<tbody>\n")

		for _, row := range result.Rows {
			buf.WriteString("<tr>")
			for _, col := range columns {
				buf.WriteString("<td>" + escapeHTML(formatValue(row[col])) + "</td>")
			}
			buf.WriteString("</tr>\n")
		}

		buf.WriteString("</tbody></table>\n")
	}

	buf.WriteString("</body></html>")

	return buf.Bytes(), nil
}

// formatValue converts any value to a string for output.
func formatValue(v interface{}) string {
	if v == nil {
		return ""
	}

	switch val := v.(type) {
	case string:
		return val
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%.2f", val)
	case bool:
		if val {
			return "Yes"
		}
		return "No"
	case time.Time:
		if val.IsZero() {
			return ""
		}
		return val.Format("2006-01-02 15:04:05")
	case *time.Time:
		if val == nil || val.IsZero() {
			return ""
		}
		return val.Format("2006-01-02 15:04:05")
	case []string:
		if len(val) == 0 {
			return ""
		}
		return fmt.Sprintf("%v", val)
	case map[string]interface{}:
		data, _ := json.Marshal(val)
		return string(data)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatLabel converts a snake_case key to a readable label.
func formatLabel(s string) string {
	if s == "" {
		return ""
	}

	result := make([]byte, 0, len(s)+5)
	capitalize := true

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			result = append(result, ' ')
			capitalize = true
		} else if capitalize {
			if c >= 'a' && c <= 'z' {
				result = append(result, c-32) // to uppercase
			} else {
				result = append(result, c)
			}
			capitalize = false
		} else {
			result = append(result, c)
		}
	}

	return string(result)
}

// escapeHTML escapes HTML special characters.
func escapeHTML(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		switch r {
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '&':
			buf.WriteString("&amp;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&#39;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
