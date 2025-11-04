package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ParseDebug holds structured debugging information for a parsed IP
type ParseDebug struct {
	IP                string                 `json:"ip"`
	Timestamp         string                 `json:"timestamp"`
	RawPDUs           []RawPDU               `json:"raw_pdus"`
	Steps             []string               `json:"steps"`
	ManufacturerHints []string               `json:"manufacturer_hints"`
	FinalManufacturer string                 `json:"final_manufacturer"`
	Model             string                 `json:"model"`
	Serial            string                 `json:"serial"`
	IsPrinter         bool                   `json:"is_printer"`
	DetectionReasons  []string               `json:"detection_reasons"`
	Extra             map[string]interface{} `json:"extra,omitempty"`
}

// RawPDU is a JSON-serializable representation of a gosnmp.SnmpPDU
type RawPDU struct {
	OID      string `json:"oid"`
	Type     string `json:"type"`
	StrValue string `json:"str_value,omitempty"`
	HexValue string `json:"hex_value,omitempty"`
	RawValue string `json:"raw_value,omitempty"`
}

var (
	diagMu      sync.Mutex
	parseDebugs = map[string]ParseDebug{}
	// DumpParseDebugEnabled controls whether parse debug snapshots are persisted to disk.
	DumpParseDebugEnabled = true
)

// RecordParseDebug stores the debug in-memory and persists it to logs/parse_debug_<ip>.json
func RecordParseDebug(ip string, d ParseDebug) error {
	// ensure timestamp
	if d.Timestamp == "" {
		d.Timestamp = time.Now().Format(time.RFC3339)
	}
	diagMu.Lock()
	parseDebugs[ip] = d
	diagMu.Unlock()

	// persist to logs (only if enabled)
	if DumpParseDebugEnabled {
		logDir := ensureLogDir()
		fname := fmt.Sprintf("parse_debug_%s.json", ipToFileName(ip))
		fpath := filepath.Join(logDir, fname)
		data, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(fpath, data, 0o644)
	}
	return nil
}

// SetDumpParseDebug toggles whether parse debug snapshots are written to disk.
func SetDumpParseDebug(v bool) {
	DumpParseDebugEnabled = v
}

// GetParseDebug returns the last recorded ParseDebug for the given IP, if any
func GetParseDebug(ip string) (ParseDebug, bool) {
	diagMu.Lock()
	defer diagMu.Unlock()
	d, ok := parseDebugs[ip]
	return d, ok
}

// ipToFileName converts 1.2.3.4 into safe filename part
func ipToFileName(ip string) string {
	fn := filepath.Base(ip)
	fn = strings.ReplaceAll(fn, ".", "_")
	return fn
}
