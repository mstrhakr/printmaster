package agent

import (
	"context"
	"fmt"

	"printmaster/agent/scanner"

	"github.com/gosnmp/gosnmp"
)

// MakeDefaultDetectFunc returns a DetectFunc suitable for ScannerConfig.DetectFunc.
// It performs a compact SNMP GET of a few quick OIDs and calls ParsePDUs to
// decide whether the host is a printer. timeoutSeconds controls the SNMP
// client timeout used for detection probes.
func MakeDefaultDetectFunc(cfg *SNMPConfig, timeoutSeconds int) func(ctx context.Context, job scanner.ScanJob, openPorts []int) (interface{}, bool, error) {
	return func(ctx context.Context, job scanner.ScanJob, openPorts []int) (interface{}, bool, error) {
		// build ScanMeta from available info
		var meta *ScanMeta
		if job.Meta != nil {
			if m, ok := job.Meta.(*ScanMeta); ok {
				meta = m
			}
		}
		if meta == nil {
			meta = &ScanMeta{OpenPorts: openPorts}
		} else if len(openPorts) > 0 && len(meta.OpenPorts) == 0 {
			meta.OpenPorts = openPorts
		}

		// If TCP evidence alone suggests a printer, we can short-circuit for speed.
		for _, p := range meta.OpenPorts {
			switch p {
			case 9100, 515, 631, 80, 443:
				// Proceed to SNMP as primary detection; we still try SNMP to collect evidence
			}
		}

		// prepare quick probe OIDs (compact set)
		oids := []string{
			"1.3.6.1.2.1.1.2.0",           // sysObjectID
			"1.3.6.1.2.1.1.1.0",           // sysDescr
			"1.3.6.1.2.1.43.5.1.1.16.1",   // prtGeneral entry
			"1.3.6.1.2.1.43.10.2.1.4.1.1", // prtMarkerLifeCount marker 1
		}

		client, err := NewSNMPClient(cfg, job.IP, timeoutSeconds)
		if err != nil {
			// SNMP connect failed; return non-fatal (not-detected) so upstream
			// pipeline can continue. Propagate error for observability.
			IncDetectionErrors()
			return nil, false, fmt.Errorf("SNMP connect error: %w", err)
		}
		defer client.Close()

		// Try a multi-GET first
		if res, err := client.Get(oids); err == nil && res != nil && len(res.Variables) > 0 {
			pi, isPrinter := ParsePDUs(job.IP, res.Variables, meta, nil)
			return pi, isPrinter, nil
		}

		// Fallback: try single OID GETs and collect any successful PDUs
		collected := []gosnmp.SnmpPDU{}
		for _, o := range oids {
			if r2, e2 := client.Get([]string{o}); e2 == nil && r2 != nil && len(r2.Variables) > 0 {
				collected = append(collected, r2.Variables...)
			}
		}
		if len(collected) > 0 {
			pi, isPrinter := ParsePDUs(job.IP, collected, meta, nil)
			return pi, isPrinter, nil
		}

		// As last resort, run a very small diagnostic walk around Printer-MIB
		roots := []string{"1.3.6.1.2.1.43"}
		cols := diagnosticWalk(client, nil, roots, 200, []string{"pid", "model", "serial", "prtGeneral", "prtMarker", "supply", "toner"})
		if len(cols) > 0 {
			pi, isPrinter := ParsePDUs(job.IP, cols, meta, nil)
			return pi, isPrinter, nil
		}

		return nil, false, nil
	}
}
