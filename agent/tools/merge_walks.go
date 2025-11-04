//go:build mergewalks
// +build mergewalks

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"printmaster/agent/agent"

	"github.com/gosnmp/gosnmp"
)

func main() {
	walkDir := filepath.Join(".", "logs")
	_ = os.MkdirAll(filepath.Join(walkDir), 0o755)
	count := 0
	err := filepath.WalkDir(walkDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "mib_walk_") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		count++
		fmt.Printf("Processing %s\n", d.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf(" read failed: %v\n", err)
			return nil
		}
		var doc struct {
			IP      string `json:"ip"`
			Entries []struct {
				Oid   string      `json:"oid"`
				Type  string      `json:"type"`
				Value interface{} `json:"value"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			fmt.Printf(" unmarshal failed: %v\n", err)
			return nil
		}
		// build pdus
		pdus := []gosnmp.SnmpPDU{}
		for _, e := range doc.Entries {
			name := strings.TrimPrefix(e.Oid, ".")
			var p gosnmp.SnmpPDU
			p.Name = name
			switch e.Type {
			case "OctetString":
				if s, ok := e.Value.(string); ok {
					p.Type = gosnmp.OctetString
					p.Value = []byte(s)
				} else {
					js, _ := json.Marshal(e.Value)
					p.Type = gosnmp.OctetString
					p.Value = []byte(string(js))
				}
			case "ObjectIdentifier":
				if s, ok := e.Value.(string); ok {
					p.Type = gosnmp.ObjectIdentifier
					p.Value = s
				}
			case "Integer", "Gauge32", "Counter32":
				if f, ok := e.Value.(float64); ok {
					p.Type = gosnmp.Integer
					p.Value = int(f)
				}
			case "Counter64":
				if f, ok := e.Value.(float64); ok {
					p.Type = gosnmp.Counter64
					p.Value = uint64(f)
				}
			case "TimeTicks":
				if f, ok := e.Value.(float64); ok {
					p.Type = gosnmp.TimeTicks
					p.Value = uint32(f)
				}
			default:
				js, _ := json.Marshal(e.Value)
				p.Type = gosnmp.OctetString
				p.Value = []byte(string(js))
			}
			pdus = append(pdus, p)
		}
		pi, _ := agent.ParsePDUs(doc.IP, pdus, nil, func(s string) {})
		serial := strings.TrimSpace(pi.Serial)
		if serial == "" {
			fmt.Printf("  no serial detected, skipping\n")
			return nil
		}
		// perform merge similar to mergeDeviceProfile
		devDir := filepath.Join(".", "logs", "devices")
		_ = os.MkdirAll(devDir, 0o755)
		devPath := filepath.Join(devDir, serial+".json")
		var existing map[string]interface{}
		if b2, err := os.ReadFile(devPath); err == nil {
			_ = json.Unmarshal(b2, &existing)
		} else {
			existing = map[string]interface{}{}
		}
		now := time.Now().Format(time.RFC3339)
		if prev, ok := existing["printer_info"].(map[string]interface{}); ok {
			pj, _ := json.Marshal(prev)
			if string(pj) != "" {
				piList := []interface{}{}
				if arr, ok2 := existing["previous_info"].([]interface{}); ok2 {
					piList = arr
				}
				piList = append(piList, map[string]interface{}{"merged_at": now, "info": prev})
				existing["previous_info"] = piList
			}
		}
		piMap := map[string]interface{}{
			"ip":           pi.IP,
			"manufacturer": pi.Manufacturer,
			"model":        pi.Model,
			"serial":       pi.Serial,
			"hostname":     pi.Hostname,
			"firmware":     pi.Firmware,
			"last_seen":    now,
		}
		// toner levels union
		tl := map[string]int{}
		if existingPI, ok := existing["printer_info"].(map[string]interface{}); ok {
			if oldTL, ok2 := existingPI["toner_levels"].(map[string]interface{}); ok2 {
				for k, v := range oldTL {
					if vi, ok3 := v.(float64); ok3 {
						tl[k] = int(vi)
					}
				}
			}
		}
		if pi.TonerLevels != nil {
			for k, v := range pi.TonerLevels {
				tl[k] = v
			}
		}
		if len(tl) > 0 {
			piMap["toner_levels"] = tl
		}
		// consumables union
		consSet := map[string]struct{}{}
		if existingPI, ok := existing["printer_info"].(map[string]interface{}); ok {
			if oldCons, ok2 := existingPI["consumables"].([]interface{}); ok2 {
				for _, v := range oldCons {
					if s, ok3 := v.(string); ok3 {
						consSet[s] = struct{}{}
					}
				}
			}
		}
		for _, c := range pi.Consumables {
			consSet[c] = struct{}{}
		}
		if len(consSet) > 0 {
			out := []string{}
			for k := range consSet {
				out = append(out, k)
			}
			piMap["consumables"] = out
		}
		history := []interface{}{}
		if h, ok := existing["history"].([]interface{}); ok {
			history = h
		}
		history = append(history, map[string]interface{}{"walk": d.Name(), "ip": doc.IP, "merged_at": now})
		existing["serial"] = serial
		existing["history"] = history
		existing["printer_info"] = piMap
		outb, _ := json.MarshalIndent(existing, "", "  ")
		if err := os.WriteFile(devPath, outb, 0o644); err != nil {
			fmt.Printf(" write failed: %v\n", err)
			return nil
		}
		fmt.Printf("  merged -> %s\n", devPath)
		return nil
	})
	if err != nil {
		fmt.Printf("walk scan failed: %v\n", err)
	}
	fmt.Printf("processed %d walk files\n", count)
}
