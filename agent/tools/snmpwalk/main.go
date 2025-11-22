package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

//go:embed printer-mib-complete.json
var printerMIBData []byte

//go:embed hp-mib.json
var hpMIBData []byte

// MIBEntry represents a single OID and its value
type MIBEntry struct {
	OID         string      `json:"oid"`
	Name        string      `json:"name,omitempty"`
	Type        string      `json:"type"`
	Value       interface{} `json:"value"`
	Description string      `json:"description,omitempty"`
}

// OIDInfo represents OID metadata from the MIB
type OIDInfo struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Type        string             `json:"type,omitempty"`
	Children    map[string]OIDInfo `json:"children,omitempty"`
}

// PrinterMIB represents the complete MIB structure
type PrinterMIB struct {
	Module      string             `json:"module"`
	Description string             `json:"description"`
	BaseOID     string             `json:"base_oid"`
	OIDs        map[string]OIDInfo `json:"oids"`
}

var printerMIB PrinterMIB
var oidLookup map[string]OIDInfo

// WalkResult contains all MIB data from a walk
type WalkResult struct {
	Target    string     `json:"target"`
	Timestamp time.Time  `json:"timestamp"`
	Community string     `json:"community"`
	Version   string     `json:"version"`
	Entries   []MIBEntry `json:"entries"`
}

func init() {
	// Load embedded standard MIB data
	if err := json.Unmarshal(printerMIBData, &printerMIB); err != nil {
		log.Fatalf("Failed to parse embedded Printer MIB data: %v", err)
	}

	// Load embedded HP MIB data
	var hpMIB PrinterMIB
	if err := json.Unmarshal(hpMIBData, &hpMIB); err != nil {
		log.Fatalf("Failed to parse embedded HP MIB data: %v", err)
	}

	// Build flat OID lookup map for fast searching
	oidLookup = make(map[string]OIDInfo)

	// Add standard MIB OIDs
	for oid, info := range printerMIB.OIDs {
		oidLookup[oid] = info
		// Also add all children
		for childOID, childInfo := range info.Children {
			oidLookup[childOID] = childInfo
		}
	}

	// Add HP MIB OIDs
	for oid, info := range hpMIB.OIDs {
		oidLookup[oid] = info
		// Also add all children
		for childOID, childInfo := range info.Children {
			oidLookup[childOID] = childInfo
		}
	}
}

// findBestLabel finds the most specific label for an OID
// Handles table entries by matching prefixes (e.g., 1.3.6.1.2.1.43.11.1.1.9.1 matches 1.3.6.1.2.1.43.11.1.1.9)
func findBestLabel(oid string) (OIDInfo, bool) {
	// Remove leading dot if present
	cleanOID := strings.TrimPrefix(oid, ".")

	// Try exact match first
	if info, found := oidLookup[cleanOID]; found {
		return info, true
	}

	// Try prefix matches (for table entries with indices)
	// Start from longest possible match and work backwards
	parts := strings.Split(cleanOID, ".")
	for i := len(parts) - 1; i > 0; i-- {
		prefix := strings.Join(parts[:i], ".")
		if info, found := oidLookup[prefix]; found {
			// Found a parent OID - use it with indication this is a table entry
			return info, true
		}
	}

	return OIDInfo{}, false
}

func main() {
	// Command line flags
	target := flag.String("target", "", "Target IP address (required)")
	community := flag.String("community", "public", "SNMP community string")
	port := flag.Uint("port", 161, "SNMP port")
	timeout := flag.Duration("timeout", 5*time.Second, "SNMP timeout")
	retries := flag.Int("retries", 2, "Number of retries")
	version := flag.String("version", "2c", "SNMP version (1, 2c, 3)")
	rootOID := flag.String("oid", "1.3.6.1", "Root OID to walk (default: full MIB tree)")
	output := flag.String("output", "", "Output file (JSON format, default: stdout)")
	verbose := flag.Bool("v", false, "Verbose output")

	flag.Parse()

	if *target == "" {
		fmt.Fprintf(os.Stderr, "Error: -target is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Parse SNMP version
	var snmpVersion gosnmp.SnmpVersion
	switch *version {
	case "1":
		snmpVersion = gosnmp.Version1
	case "2c":
		snmpVersion = gosnmp.Version2c
	case "3":
		snmpVersion = gosnmp.Version3
	default:
		log.Fatalf("Invalid SNMP version: %s (use 1, 2c, or 3)", *version)
	}

	// Setup SNMP connection
	snmp := &gosnmp.GoSNMP{
		Target:    *target,
		Port:      uint16(*port),
		Community: *community,
		Version:   snmpVersion,
		Timeout:   *timeout,
		Retries:   *retries,
	}

	err := snmp.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", *target, err)
	}
	defer snmp.Conn.Close()

	if *verbose {
		log.Printf("Connected to %s:%d (community: %s, version: %s)", *target, *port, *community, *version)
		log.Printf("Starting walk from OID: %s", *rootOID)
	}

	// Perform walk
	result := WalkResult{
		Target:    *target,
		Timestamp: time.Now(),
		Community: *community,
		Version:   *version,
		Entries:   []MIBEntry{},
	}

	walkCount := 0
	walkFunc := func(pdu gosnmp.SnmpPDU) error {
		walkCount++
		if *verbose && walkCount%100 == 0 {
			log.Printf("Processed %d OIDs...", walkCount)
		}

		entry := MIBEntry{
			OID:   pdu.Name,
			Type:  pdu.Type.String(),
			Value: pdu.Value,
		}

		// Try to find a label for this OID
		if label, found := findBestLabel(pdu.Name); found {
			entry.Name = label.Name
			entry.Description = label.Description
		}

		result.Entries = append(result.Entries, entry)
		return nil
	}

	// Use BulkWalk for SNMPv2c/v3, regular Walk for v1
	if snmpVersion == gosnmp.Version1 {
		err = snmp.Walk(*rootOID, walkFunc)
	} else {
		err = snmp.BulkWalk(*rootOID, walkFunc)
	}

	if err != nil {
		log.Fatalf("Walk failed: %v", err)
	}

	if *verbose {
		log.Printf("Walk complete: %d OIDs retrieved", len(result.Entries))
	}

	// Output results
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}

	if *output != "" {
		err = os.WriteFile(*output, jsonData, 0644)
		if err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}
		if *verbose {
			log.Printf("Results written to: %s", *output)
		}
	} else {
		fmt.Println(string(jsonData))
	}
}
