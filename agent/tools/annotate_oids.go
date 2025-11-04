package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// conservative MIB parser + annotator

var objIdRe = regexp.MustCompile(`(?i)^(\S+)\s+OBJECT\s+IDENTIFIER\s+::=\s+\{\s*(.+)\s*\}`)
var objTypeRe = regexp.MustCompile(`(?i)^(\S+)\s+OBJECT-TYPE`) // block header
var assignRe = regexp.MustCompile(`::=\s+\{\s*(.+)\s*\}`)

// mapping numeric prefix -> symbol name
type MibMap map[string]string

func parseMibFile(path string) (MibMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// store intermediate name -> token list (strings that may be numeric or name(num) or name)
	nameTokens := map[string][]string{}
	// also capture OBJECT-TYPE blocks where the ::= { parent idx } appears later
	objTypeBody := map[string]string{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if m := objIdRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			body := m[2]
			toks := tokenizeBody(body)
			nameTokens[name] = toks
			continue
		}
		if m := objTypeRe.FindStringSubmatch(line); m != nil {
			// read forward to find ::= { ... }
			name := m[1]
			// scan subsequent lines to find assignment
			for scanner.Scan() {
				l2 := strings.TrimSpace(scanner.Text())
				if l2 == "" || strings.HasPrefix(l2, "--") {
					continue
				}
				if am := assignRe.FindStringSubmatch(l2); am != nil {
					body := am[1]
					objTypeBody[name] = body
					break
				}
			}
		}
	}
	// merge objTypeBody into nameTokens
	for n, b := range objTypeBody {
		nameTokens[n] = tokenizeBody(b)
	}

	// resolve nameTokens into numeric sequences
	resolved := map[string][]string{}
	// helper to attempt resolve one name
	var resolveOne func(string) ([]string, bool)
	resolveOne = func(name string) ([]string, bool) {
		if parts, ok := resolved[name]; ok {
			return parts, true
		}
		toks, ok := nameTokens[name]
		if !ok {
			return nil, false
		}
		parts := []string{}
		for _, t := range toks {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			// token like iso(1) or org(3)
			if strings.Contains(t, "(") && strings.Contains(t, ")") {
				// extract number inside parentheses
				idx := strings.Index(t, "(")
				j := strings.Index(t, ")")
				if idx >= 0 && j > idx {
					num := t[idx+1 : j]
					parts = append(parts, num)
					continue
				}
			}
			// pure number
			if _, err := strconv.Atoi(t); err == nil {
				parts = append(parts, t)
				continue
			}
			// named reference: attempt to resolve recursively
			if ref, ok := resolveOne(t); ok {
				parts = append(parts, ref...)
				continue
			}
			// unknown token -> give up on this name
			return nil, false
		}
		resolved[name] = parts
		return parts, true
	}

	// attempt resolve all names
	for name := range nameTokens {
		resolveOne(name)
	}

	// build numeric prefix -> symbol mapping
	mm := MibMap{}
	for name, parts := range resolved {
		if len(parts) == 0 {
			continue
		}
		prefix := "." + strings.Join(parts, ".")
		mm[prefix] = name
	}
	return mm, nil
}

func tokenizeBody(body string) []string {
	// split on whitespace and commas
	body = strings.ReplaceAll(body, "{", " ")
	body = strings.ReplaceAll(body, "}", " ")
	body = strings.ReplaceAll(body, ",", " ")
	fields := strings.Fields(body)
	return fields
}

// annotate report
// AnnotateAndGenerateHPCandidates parses MIBs and annotates the aggregate report and
// writes an HP candidate profile. This function can be called from tests or tooling.
func AnnotateAndGenerateHPCandidates() error {
	// load MIBs
	mibDir := filepath.Join(".", "mib_profiles", "mibs")
	mibFiles := []string{}
	_ = filepath.WalkDir(mibDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".mib") {
			mibFiles = append(mibFiles, path)
		}
		return nil
	})
	combined := MibMap{}
	for _, mf := range mibFiles {
		mm, err := parseMibFile(mf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", mf, err)
			continue
		}
		for k, v := range mm {
			combined[k] = v
		}
	}
	// build slice of prefixes sorted by length desc for longest-prefix match
	prefixes := []string{}
	for p := range combined {
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })

	// read aggregate report
	inPath := filepath.Join(".", "logs", "oid_aggregate_report.json")
	b, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read report: %v\n", err)
		os.Exit(1)
	}
	var report map[string]interface{}
	if err := json.Unmarshal(b, &report); err != nil {
		return err
	}
	oids, _ := report["oids"].([]interface{})
	for _, oi := range oids {
		m, ok := oi.(map[string]interface{})
		if !ok {
			continue
		}
		oidS, _ := m["oid"].(string)
		// try longest-prefix match
		found := ""
		for _, p := range prefixes {
			if strings.HasPrefix(oidS, p) {
				found = combined[p]
				break
			}
		}
		if found != "" {
			m["mib_symbol"] = found
		}
	}
	outPath := filepath.Join(".", "logs", "oid_aggregate_report.annotated.json")
	outB, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(outPath, outB, 0644); err != nil {
		return err
	}
	fmt.Printf("Wrote annotated report to %s (mibs=%d oids=%d)\n", outPath, len(mibFiles), len(oids))

	// --- generate HP candidate profile using annotated report + manufacturer evidence ---
	candidates := []map[string]interface{}{}
	for _, oi := range oids {
		m, ok := oi.(map[string]interface{})
		if !ok {
			continue
		}
		oidS, _ := m["oid"].(string)
		// require manufacturer evidence for HP
		hasHP := false
		if mf, ok := m["manufacturer_files"].(map[string]interface{}); ok && mf != nil {
			if _, ok := mf["hp"]; ok {
				hasHP = true
			}
			if _, ok := mf["hewlett"]; ok {
				hasHP = true
			}
		}
		if !hasHP {
			continue
		}

		sym, _ := m["mib_symbol"].(string)
		symL := strings.ToLower(sym)

		// heuristics: symbol name indicates marker/supply/serial/impression OR evidence counters
		matchName := false
		if symL != "" {
			if strings.Contains(symL, "prtmarker") || strings.Contains(symL, "marker") || strings.Contains(symL, "suppl") || strings.Contains(symL, "impress") || strings.Contains(symL, "a4equivalent") || strings.Contains(symL, "serial") {
				matchName = true
			}
		}

		pc := 0
		if v, ok := m["page_counter_evidence_count"].(float64); ok {
			pc = int(v)
		}
		isCounter := false
		if tc, ok := m["type_counts"].(map[string]interface{}); ok && tc != nil {
			if _, ok := tc["Counter32"]; ok {
				isCounter = true
			}
			if _, ok := tc["Counter64"]; ok {
				isCounter = true
			}
		}

		if !(matchName || pc > 0 || isCounter) {
			continue
		}

		cand := map[string]interface{}{
			"oid":        oidS,
			"mib_symbol": sym,
			"confidence": m["confidence"],
			"evidence": map[string]interface{}{
				"page_counter_evidence": pc,
				"is_counter_type":       isCounter,
			},
		}
		if ex, ok := m["example_values"]; ok {
			cand["example_values"] = ex
		}
		candidates = append(candidates, cand)
	}

	// write hp.candidate.json
	outDir := filepath.Join(".", "mib_profiles", "candidates")
	_ = os.MkdirAll(outDir, 0755)
	hpPath := filepath.Join(outDir, "hp.candidate.json")
	hpJson, _ := json.MarshalIndent(map[string]interface{}{"candidates": candidates}, "", "  ")
	if err := os.WriteFile(hpPath, hpJson, 0644); err != nil {
		return err
	}
	fmt.Printf("Wrote %d HP candidates to %s\n", len(candidates), hpPath)
	return nil
}

// Note: This file exposes AnnotateAndGenerateHPCandidates for tooling. It intentionally
// does not provide a second main() to avoid duplicate main declarations when other
// tool files are present in the same package.
