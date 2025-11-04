package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func loadWalkFile(t *testing.T, path string) []gosnmp.SnmpPDU {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read walk file %s: %v", path, err)
	}
	var doc struct {
		IP      string `json:"ip"`
		Entries []struct {
			Oid   string      `json:"oid"`
			Type  string      `json:"type"`
			Value interface{} `json:"value"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid json in %s: %v", path, err)
	}
	vars := []gosnmp.SnmpPDU{}
	for _, e := range doc.Entries {
		p := gosnmp.SnmpPDU{Name: e.Oid}
		// coerce typical types
		switch e.Type {
		case "OctetString":
			if s, ok := e.Value.(string); ok {
				p.Type = gosnmp.OctetString
				p.Value = []byte(s)
			} else {
				p.Type = gosnmp.OctetString
				p.Value = []byte{}
			}
		case "Integer":
			// JSON numbers decode to float64
			if f, ok := e.Value.(float64); ok {
				p.Type = gosnmp.Integer
				p.Value = int(f)
			} else {
				p.Type = gosnmp.Integer
			}
		case "Counter32", "Gauge32":
			if f, ok := e.Value.(float64); ok {
				p.Type = gosnmp.Counter32
				p.Value = uint(f)
			} else {
				p.Type = gosnmp.Counter32
			}
		case "Null":
			p.Type = gosnmp.Null
			p.Value = nil
		default:
			// best-effort: treat as printable
			if s, ok := e.Value.(string); ok {
				p.Type = gosnmp.OctetString
				p.Value = []byte(s)
			} else {
				p.Type = gosnmp.OctetString
				p.Value = []byte{}
			}
		}
		vars = append(vars, p)
	}
	return vars
}

func findWalkFiles() []string {
	candidates := []string{}
	// search a few likely locations relative to the package
	patterns := []string{"../logs/mib_walk_*.json", "logs/mib_walk_*.json", "./logs/mib_walk_*.json"}
	for _, pat := range patterns {
		if matches, err := filepath.Glob(pat); err == nil {
			for _, m := range matches {
				// skip summary files or other non-walk aggregates
				if strings.Contains(strings.ToLower(filepath.Base(m)), "summary") {
					continue
				}
				candidates = append(candidates, m)
			}
		}
	}
	return candidates
}

func TestReplayMIBWalks_ParsePDUs(t *testing.T) {
	t.Parallel()
	files := findWalkFiles()
	if len(files) == 0 {
		t.Skip("no recorded mib_walk_*.json files found in logs/")
	}
	for _, path := range files {
		path := path // capture for parallel subtest
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			t.Logf("replaying %s", path)
			vars := loadWalkFile(t, path)
			// skip empty walk captures (some logs may contain roots only or be empty)
			if len(vars) == 0 {
				t.Skipf("skipping empty walk %s", path)
			}
			pi, isPrinter := ParsePDUs("replay", vars, nil, func(s string) {})
			if !isPrinter {
				t.Fatalf("expected isPrinter=true for %s; got false; parsed: %+v", path, pi)
			}
			if pi.Manufacturer == "" && pi.Model == "" && pi.Serial == "" {
				t.Fatalf("expected at least one of Manufacturer/Model/Serial to be set for %s; got empty; parsed: %+v", path, pi)
			}
			t.Logf("replay %s -> manufacturer=%q model=%q serial=%q", path, pi.Manufacturer, pi.Model, pi.Serial)
		})
	}
}
