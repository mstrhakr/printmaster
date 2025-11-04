//go:build ignore

// This file was moved to tools/mibwalkscan. Keep here only for reference and
// to avoid accidental rebuild; the build tag above excludes it from 'go' builds.
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type WalkEntry struct {
	OID   string      `json:"oid"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

type WalkFile struct {
	Count   int         `json:"count"`
	Entries []WalkEntry `json:"entries"`
}

type Found struct {
	Field string `json:"field"`
	OID   string `json:"oid"`
	Value string `json:"value"`
}

type Summary struct {
	File  string  `json:"file"`
	IP    string  `json:"ip"`
	Found []Found `json:"found"`
}

func main() {
	logDir := "./logs"
	files, err := filepath.Glob(filepath.Join(logDir, "mib_walk_*.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "glob error:", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no mib_walk files found in logs/")
		os.Exit(1)
	}

	mfgRe := regexp.MustCompile(`(?i)\b(hp|hewlett[-\s]?packard|canon|brother|epson|lexmark|kyocera|konica|xerox|ricoh|sharp|okidata|dell|toshiba|samsung)\b`)
	pidRe := regexp.MustCompile(`(?i)(?:pid|product|product id|model(?: name)?|model)[:=\s]*([A-Za-z0-9\-\s]{2,80})`)
	snRe := regexp.MustCompile(`(?i)(?:sn|s/n|serial(?:number)?|serial)[:=\s]*([A-Za-z0-9\-]{4,40})`)
	modelKeywords := []string{"laserjet", "mfp", "printer", "series", "deskjet", "workcentre", "imageclass", "clx", "ml-", "color", "mono", "imageclass"}

	summaries := []Summary{}

	for _, f := range files {
		raw, err := ioutil.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", f, err)
			continue
		}
		var w WalkFile
		if err := json.Unmarshal(raw, &w); err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse %s: %v\n", f, err)
			continue
		}
		var s Summary
		s.File = filepath.Base(f)
		// extract IP from filename like mib_walk_10_2_106_72_20251029T221657.json
		parts := strings.Split(s.File, "_")
		if len(parts) >= 3 {
			s.IP = strings.ReplaceAll(parts[2], "-", ".")
		}
		found := []Found{}
		for _, e := range w.Entries {
			val := ""
			switch v := e.Value.(type) {
			case string:
				val = v
			default:
				val = fmt.Sprintf("%v", v)
			}
			ls := strings.ToLower(val)
			// manufacturer
			if m := mfgRe.FindStringSubmatch(val); len(m) > 1 {
				found = append(found, Found{Field: "manufacturer", OID: e.OID, Value: strings.TrimSpace(m[1])})
				continue
			}
			// pid/model
			if m := pidRe.FindStringSubmatch(val); len(m) > 1 {
				found = append(found, Found{Field: "model", OID: e.OID, Value: strings.TrimSpace(m[1])})
				continue
			}
			// serial
			if m := snRe.FindStringSubmatch(val); len(m) > 1 {
				found = append(found, Found{Field: "serial", OID: e.OID, Value: strings.TrimSpace(m[1])})
				continue
			}
			// model-like keywords
			for _, kw := range modelKeywords {
				if strings.Contains(ls, kw) && len(val) > 3 && len(val) < 200 {
					found = append(found, Found{Field: "model-like", OID: e.OID, Value: strings.TrimSpace(val)})
					break
				}
			}
		}
		s.Found = found
		summaries = append(summaries, s)
		// print a short summary for this file
		if len(found) > 0 {
			fmt.Printf("%s (%s): %d matches\n", s.File, s.IP, len(found))
			for _, ff := range found {
				fmt.Printf("  %s: %s -> %s\n", ff.Field, ff.OID, ff.Value)
			}
		} else {
			fmt.Printf("%s (%s): no matches\n", s.File, s.IP)
		}
	}

	out := filepath.Join("./logs", "mib_walk_summary.json")
	b, _ := json.MarshalIndent(summaries, "", "  ")
	_ = ioutil.WriteFile(out, b, 0644)
	fmt.Printf("\nWrote summary to %s\n", out)
}
