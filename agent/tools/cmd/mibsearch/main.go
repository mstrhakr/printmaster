package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var keywords = []string{
	"marker",
	"life count",
	"lifecount",
	"life_count",
	"lifeCount",
	"pages",
	"page",
	"impression",
	"impressions",
	"serial",
	"supply",
	"supplies",
	"prtMarker",
	"prtGeneralSerialNumber",
	"A4Equivalent",
	"print-meter",
}

var objTypeRe = regexp.MustCompile(`^(\S+)\s+OBJECT-TYPE`) // captures symbol
var assignRe = regexp.MustCompile(`::=\s+\{\s*(.+)\s*\}`)
var numericOidRe = regexp.MustCompile(`(\d+(?:\.\d+){3,})`)

type Match struct {
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Keyword     string   `json:"keyword"`
	Snippet     string   `json:"snippet"`
	ObjType     string   `json:"obj_type,omitempty"`
	AssignLine  string   `json:"assign_line,omitempty"`
	NumericOids []string `json:"numeric_oids,omitempty"`
}

func main() {
	var mibDir string
	flag.StringVar(&mibDir, "mibdir", "./mib_profiles/mibs", "directory containing MIB files (relative to agent)")
	flag.Parse()

	out := []Match{}

	_ = filepath.WalkDir(mibDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".mib") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		lines := []string{}
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		// lower-cased copy for keyword scanning
		lower := make([]string, len(lines))
		for i, l := range lines {
			lower[i] = strings.ToLower(l)
		}

		for i, l := range lower {
			for _, kw := range keywords {
				if strings.Contains(l, strings.ToLower(kw)) {
					// collect context
					start := i - 6
					if start < 0 {
						start = 0
					}
					end := i + 3
					if end >= len(lines) {
						end = len(lines) - 1
					}
					snippet := strings.Join(lines[start:end+1], "\n")
					m := Match{File: path, Line: i + 1, Keyword: kw, Snippet: snippet}
					// search backwards for OBJECT-TYPE
					for j := i; j >= start; j-- {
						if objTypeRe.MatchString(lines[j]) {
							m.ObjType = strings.TrimSpace(objTypeRe.FindStringSubmatch(lines[j])[1])
							break
						}
						if assignRe.MatchString(lines[j]) {
							m.AssignLine = strings.TrimSpace(lines[j])
							break
						}
					}
					// search forwards a little for assign line or numeric oids
					nums := map[string]bool{}
					for j := i; j <= end; j++ {
						if assignRe.MatchString(lines[j]) {
							m.AssignLine = strings.TrimSpace(lines[j])
						}
						for _, sub := range numericOidRe.FindAllString(lines[j], -1) {
							nums[sub] = true
						}
					}
					if len(nums) > 0 {
						for k := range nums {
							m.NumericOids = append(m.NumericOids, k)
						}
					}
					out = append(out, m)
					break
				}
			}
		}
		return nil
	})

	if len(out) == 0 {
		fmt.Println("No matches found")
		return
	}

	// write JSON report
	logDir := "./logs"
	_ = os.MkdirAll(logDir, 0755)
	outPath := filepath.Join(logDir, "mib_keyword_report.json")
	b, _ := json.MarshalIndent(out, "", "  ")
	_ = os.WriteFile(outPath, b, 0644)
	fmt.Printf("Wrote %d matches to %s\n", len(out), outPath)
}
