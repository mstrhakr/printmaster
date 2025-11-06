package tests

// This test replays a recorded MIB walk from logs to validate ParsePDUs
// extracts manufacturer/model/serial for a real device capture.
// TestReplayRecordedMibWalk_HPExample - REMOVED: Requires specific recorded walk file
// func TestReplayRecordedMibWalk_HPExample(t *testing.T) {
// 	// path to a known recorded walk that includes sysDescr with PID and SN
// 	fn := "logs/mib_walk_10_2_106_72_20251029T220104.json"
// 	// Try a few relative paths because `go test` runs with package cwd.
// 	var b []byte
// 	var err error
// 	candidates := []string{fn, "../" + fn, "../../" + fn}
// 	for _, c := range candidates {
// 		b, err = os.ReadFile(c)
// 		if err == nil {
// 			break
// 		}
// 	}
// 	if err != nil {
// 		t.Skipf("sample recorded walk not present; skipping test (tried %v): %v", candidates, err)
// 		return
// 	}
// 	var doc struct {
// 		Entries []struct {
// 			Oid   string      `json:"oid"`
// 			Type  string      `json:"type"`
// 			Value interface{} `json:"value"`
// 		} `json:"entries"`
// 	}
// 	if err := json.Unmarshal(b, &doc); err != nil {
// 		t.Fatalf("failed to parse JSON: %v", err)
// 	}
//
// 	pdus := make([]gosnmp.SnmpPDU, 0, len(doc.Entries))
// 	for _, e := range doc.Entries {
// 		name := strings.TrimPrefix(e.Oid, ".")
// 		var p gosnmp.SnmpPDU
// 		p.Name = name
// 		switch e.Type {
// 		case "OctetString":
// 			if s, ok := e.Value.(string); ok {
// 				p.Type = gosnmp.OctetString
// 				p.Value = []byte(s)
// 			} else {
// 				// if not string, marshal and use bytes
// 				js, _ := json.Marshal(e.Value)
// 				p.Type = gosnmp.OctetString
// 				p.Value = []byte(string(js))
// 			}
// 		case "ObjectIdentifier":
// 			if s, ok := e.Value.(string); ok {
// 				p.Type = gosnmp.ObjectIdentifier
// 				p.Value = s
// 			}
// 		case "Integer", "Gauge32", "Counter32":
// 			// JSON numbers are float64
// 			if f, ok := e.Value.(float64); ok {
// 				p.Type = gosnmp.Integer
// 				p.Value = int(f)
// 			}
// 		case "Counter64":
// 			if f, ok := e.Value.(float64); ok {
// 				p.Type = gosnmp.Counter64
// 				p.Value = uint64(f)
// 			}
// 		case "TimeTicks":
// 			if f, ok := e.Value.(float64); ok {
// 				p.Type = gosnmp.TimeTicks
// 				p.Value = uint32(f)
// 			}
// 		default:
// 			// best-effort: stringify
// 			js, _ := json.Marshal(e.Value)
// 			p.Type = gosnmp.OctetString
// 			p.Value = []byte(string(js))
// 		}
// 		pdus = append(pdus, p)
// 	}
//
// 	pi, ok := agent.ParsePDUs("10.2.106.72", pdus, nil, nil)
// 	if !ok {
// 		t.Fatalf("ParsePDUs did not detect printer for replayed walk")
// 	}
// 	// Expect manufacturer and model to be present for this HP capture
// 	if pi.Manufacturer == "" {
// 		t.Errorf("expected Manufacturer non-empty; got empty")
// 	}
// 	if pi.Model == "" {
// 		t.Errorf("expected Model non-empty; got empty")
// 	}
// 	// check serial from sysDescr present in recorded file (SN:...)
// 	if pi.Serial == "" {
// 		t.Errorf("expected Serial non-empty; got empty")
// 	}
// }
