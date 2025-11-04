package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Suggestion struct {
	MIBFile    string `json:"mib_file"`
	ObjectName string `json:"object_name"`
	Line       int    `json:"line"`
	MatchType  string `json:"match_type"`
	Context    string `json:"context"`
	Desc       string `json:"description"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: import_suggestions_to_candidates <source_dir> <target_profiles_dir>")
		os.Exit(2)
	}
	src := os.Args[1]
	tgt := os.Args[2]
	// ensure target directories exist
	_ = os.MkdirAll(tgt, 0o755)
	_ = os.MkdirAll(filepath.Join("agent", "logs"), 0o755)

	entries, err := ioutil.ReadDir(src)
	if err != nil {
		fmt.Printf("read dir %s: %v\n", src, err)
		os.Exit(1)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".suggest.json") {
			continue
		}
		path := filepath.Join(src, name)
		b, err := ioutil.ReadFile(path)
		if err != nil {
			fmt.Printf("read %s: %v\n", path, err)
			continue
		}
		var arr []Suggestion
		if err := json.Unmarshal(b, &arr); err != nil {
			fmt.Printf("unmarshal %s: %v\n", path, err)
			continue
		}
		probes := []string{}
		for _, s := range arr {
			if s.ObjectName != "" {
				probes = append(probes, s.ObjectName)
			}
		}
		base := strings.TrimSuffix(name, ".suggest.json")
		lower := strings.ToLower(base)
		vendor := strings.TrimSuffix(lower, ".mib")
		if vendor == "" {
			vendor = lower
		}
		vendor = strings.ReplaceAll(vendor, " ", "_")

		candidatePath := filepath.Join(tgt, vendor+".candidate.json")
		// merge if exists
		var existing struct {
			Vendor string   `json:"vendor"`
			Probes []string `json:"probes"`
		}
		if data, err := ioutil.ReadFile(candidatePath); err == nil {
			_ = json.Unmarshal(data, &existing)
		}
		probeSet := map[string]struct{}{}
		for _, p := range existing.Probes {
			probeSet[p] = struct{}{}
		}
		for _, p := range probes {
			probeSet[p] = struct{}{}
		}
		merged := []string{}
		for p := range probeSet {
			merged = append(merged, p)
		}

		prof := map[string]interface{}{
			"vendor":         vendor,
			"enterprise_oid": "",
			"probes":         merged,
		}
		jb, _ := json.MarshalIndent(prof, "", "  ")
		if err := ioutil.WriteFile(candidatePath, jb, 0o644); err != nil {
			fmt.Printf("write candidate %s failed: %v\n", candidatePath, err)
			continue
		}
		fmt.Printf("Wrote candidate %s probes=%d\n", filepath.Base(candidatePath), len(merged))

		// append audit to agent/logs/mib_profile_actions.log
		audit := fmt.Sprintf("%s IMPORT vendor=%s file=%s selected=%d\n", time.Now().Format(time.RFC3339), vendor, name, len(probes))
		alog := filepath.Join("agent", "logs", "mib_profile_actions.log")
		f, err := os.OpenFile(alog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			f.WriteString(audit)
			f.Close()
		}
		// append suggestion UI log
		slog := filepath.Join("agent", "logs", "mib_suggestions_ui.log")
		entry := fmt.Sprintf("%s IMPORT file=%s vendor=%s probes=%d\n", time.Now().Format(time.RFC3339), name, vendor, len(probes))
		f2, err2 := os.OpenFile(slog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err2 == nil {
			f2.WriteString(entry)
			f2.Close()
		}
	}
}
