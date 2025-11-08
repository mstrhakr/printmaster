package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type RawEntry struct {
	OID   string      `json:"oid"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

type OidStat struct {
	OID               string         `json:"oid"`
	TotalOccurrences  int            `json:"total_occurrences"`
	FilesPresentCount int            `json:"files_present_count"`
	Files             []string       `json:"files_present"`
	TypeCounts        map[string]int `json:"type_counts"`
	ExampleValues     []interface{}  `json:"example_values"`
	Confidence        float64        `json:"confidence"`
	// ManufacturerFiles maps vendor -> number of files where this OID appeared AND the vendor string was detected
	ManufacturerFiles map[string]int `json:"manufacturer_files,omitempty"`
	// PageCounterEvidenceCount counts in how many files this OID looked like a page/marker counter
	PageCounterEvidenceCount int `json:"page_counter_evidence_count,omitempty"`
}

func main() {
	logDir := "logs"
	pattern := filepath.Join(logDir, "mib_walk_*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glob error: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no mib_walk files found under %s\n", logDir)
		os.Exit(2)
	}

	totalFiles := len(files)
	// map oid -> stat
	stats := map[string]*OidStat{}

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", f, err)
			continue
		}
		var arr []RawEntry
		// Try to unmarshal into a slice first
		if err := json.Unmarshal(b, &arr); err != nil {
			// Fallback: unmarshal into a generic object and look for "entries" array
			var top map[string]interface{}
			if err2 := json.Unmarshal(b, &top); err2 != nil {
				fmt.Fprintf(os.Stderr, "json parse %s failed: %v\n", f, err)
				continue
			}
			// If there's an "entries" key that's an array, convert that
			if ent, ok := top["entries"]; ok {
				if arrIface, ok2 := ent.([]interface{}); ok2 {
					arr = make([]RawEntry, 0, len(arrIface))
					for _, item := range arrIface {
						if m, ok3 := item.(map[string]interface{}); ok3 {
							var e RawEntry
							if v, ok4 := m["oid"].(string); ok4 {
								e.OID = v
							}
							if v, ok4 := m["type"].(string); ok4 {
								e.Type = v
							}
							if vv, ok4 := m["value"]; ok4 {
								e.Value = vv
							}
							arr = append(arr, e)
						}
					}
				}
			} else {
				// If no entries key, try to interpret top as a map-of-objects fallback
				fmt.Fprintf(os.Stderr, "json parse %s: unexpected top-level structure\n", f)
				continue
			}
		}

		seenInThisFile := map[string]bool{}
		// track whether an OID looked like a page counter in this file
		pageEvidenceInThisFile := map[string]bool{}
		// detect manufacturers mentioned in this mib_walk file
		fileManufacturers := detectManufacturers(arr)
		for _, e := range arr {
			if e.OID == "" {
				continue
			}
			st, ok := stats[e.OID]
			if !ok {
				st = &OidStat{
					OID:           e.OID,
					TypeCounts:    map[string]int{},
					ExampleValues: []interface{}{},
					Files:         []string{},
				}
				stats[e.OID] = st
			}
			st.TotalOccurrences++
			if e.Type != "" {
				st.TypeCounts[e.Type]++
			}
			// capture up to 5 example values (avoid duplicates)
			if len(st.ExampleValues) < 5 {
				duplicate := false
				for _, ex := range st.ExampleValues {
					if fmt.Sprintf("%v", ex) == fmt.Sprintf("%v", e.Value) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					st.ExampleValues = append(st.ExampleValues, e.Value)
				}
			}
			if !seenInThisFile[e.OID] {
				seenInThisFile[e.OID] = true
				st.FilesPresentCount++
				st.Files = append(st.Files, filepath.Base(f))
			}
			// mark page-counter evidence for this OID in this file
			if !pageEvidenceInThisFile[e.OID] && isPageCounterCandidate(e.OID, e.Type, e.Value) {
				pageEvidenceInThisFile[e.OID] = true
			}
		}
		// attribute manufacturer presence and page-counter evidence per OID for this file
		for oid := range seenInThisFile {
			st := stats[oid]
			if st.ManufacturerFiles == nil {
				st.ManufacturerFiles = map[string]int{}
			}
			for _, m := range fileManufacturers {
				st.ManufacturerFiles[m]++
			}
			if pageEvidenceInThisFile[oid] {
				st.PageCounterEvidenceCount++
			}
		}
	}

	// compute confidence and build list
	outList := make([]*OidStat, 0, len(stats))
	for _, st := range stats {
		st.Confidence = float64(st.FilesPresentCount) / float64(totalFiles)
		outList = append(outList, st)
	}

	// sort by confidence desc, then occurrences desc
	sort.Slice(outList, func(i, j int) bool {
		if outList[i].Confidence == outList[j].Confidence {
			return outList[i].TotalOccurrences > outList[j].TotalOccurrences
		}
		return outList[i].Confidence > outList[j].Confidence
	})

	report := map[string]interface{}{
		"total_files": totalFiles,
		"files":       files,
		"oids":        outList,
	}

	outB, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal report: %v\n", err)
		os.Exit(1)
	}
	outPath := filepath.Join("logs", "oid_aggregate_report.json")
	if err := os.WriteFile(outPath, outB, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote aggregate report to %s (oids=%d total_files=%d)\n", outPath, len(outList), totalFiles)
}

// detectManufacturers inspects the entries in a single mib-walk file and returns
// a list of manufacturer tokens it thinks are present in the device info.
func detectManufacturers(arr []RawEntry) []string {
	// candidate keywords (lowercase)
	keywords := []string{"hp", "hewlett", "brother", "canon", "lexmark", "epson", "kyocera", "ricoh", "xerox", "konica", "minolta", "sharp", "toshiba"}
	found := map[string]bool{}
	for _, e := range arr {
		// check common sysDescr OID and any string values for vendor tokens
		if e.Value == nil {
			continue
		}
		var s string
		switch v := e.Value.(type) {
		case string:
			s = v
		default:
			// try stringify
			s = fmt.Sprintf("%v", v)
		}
		ls := strings.ToLower(s)
		for _, k := range keywords {
			if strings.Contains(ls, k) {
				found[k] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// isPageCounterCandidate applies simple heuristics to decide whether this OID/value
// looks like a page or marker life counter.
func isPageCounterCandidate(oid string, typ string, val interface{}) bool {
	// OID-based heuristics: Printer-MIB prtMarkerLifeCount lives under .1.3.6.1.2.1.43.10
	if strings.Contains(oid, ".1.3.6.1.2.1.43.10") || strings.Contains(oid, ".43.10") || strings.Contains(strings.ToLower(oid), "prtmarkerlifecount") {
		return true
	}
	// type-based heuristic
	if strings.Contains(strings.ToLower(typ), "counter") {
		return true
	}
	// numeric heuristics: large integer-like values are likely counters
	switch v := val.(type) {
	case float64:
		if v > 1000 {
			return true
		}
	case string:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			if n > 1000 {
				return true
			}
		}
	}
	return false
}
