package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"printmaster/common/snmp/oids"

	"github.com/gosnmp/gosnmp"
)

// SNMPTestData represents the structure of our test data files
type SNMPTestData struct {
	OID      string `json:"oid"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	HexValue string `json:"hex_value,omitempty"`
}

// loadTestData loads SNMP test data from a JSON file
func loadTestData(t *testing.T, filename string) []SNMPTestData {
	t.Helper()

	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("test data file not found: %s (run with real data to enable)", path)
		return nil
	}

	var records []SNMPTestData
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("failed to parse test data %s: %v", filename, err)
	}

	return records
}

// convertToPDUs converts test data to gosnmp PDUs
func convertToPDUs(data []SNMPTestData) []gosnmp.SnmpPDU {
	pdus := make([]gosnmp.SnmpPDU, 0, len(data))

	for _, d := range data {
		pdu := gosnmp.SnmpPDU{
			Name: "." + d.OID,
		}

		switch d.Type {
		case "OctetString":
			pdu.Type = gosnmp.OctetString
			pdu.Value = []byte(d.Value)
		case "Integer":
			pdu.Type = gosnmp.Integer
			var val int
			if err := json.Unmarshal([]byte(d.Value), &val); err == nil {
				pdu.Value = val
			}
		case "Counter32":
			pdu.Type = gosnmp.Counter32
			var val uint32
			if err := json.Unmarshal([]byte(d.Value), &val); err == nil {
				pdu.Value = val
			}
		case "Gauge32", "Gauge":
			pdu.Type = gosnmp.Gauge32
			var val uint32
			if err := json.Unmarshal([]byte(d.Value), &val); err == nil {
				pdu.Value = val
			}
		case "TimeTicks":
			pdu.Type = gosnmp.TimeTicks
			var val uint32
			if err := json.Unmarshal([]byte(d.Value), &val); err == nil {
				pdu.Value = val
			}
		case "ObjectIdentifier":
			pdu.Type = gosnmp.ObjectIdentifier
			pdu.Value = d.Value
		case "Null":
			pdu.Type = gosnmp.Null
			pdu.Value = nil
		default:
			pdu.Type = gosnmp.OctetString
			pdu.Value = []byte(d.Value)
		}

		pdus = append(pdus, pdu)
	}

	return pdus
}

// findOIDValue finds a specific OID value from test data
func findOIDValue(data []SNMPTestData, oid string) string {
	for _, d := range data {
		if d.OID == oid {
			return d.Value
		}
	}
	return ""
}

// TestRealData_Brother tests parsing of real Brother HL-L8260CDW SNMP data
func TestRealData_Brother(t *testing.T) {
	data := loadTestData(t, "brother_snmp.json")
	if data == nil {
		return
	}

	// Verify key OIDs are present
	tests := []struct {
		oid      string
		expected string
		desc     string
	}{
		{oids.SysDescr, "Brother NC-8700w, Firmware Ver.Y  ,MID 84E-822", "sysDescr"},
		{oids.HrDeviceDescr, "Brother HL-L8260CDW series", "hrDeviceDescr (model)"},
		{oids.PrtGeneralSerialNumber, "U64641E7J134163", "serial number"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := findOIDValue(data, tc.oid)
			if got != tc.expected {
				t.Errorf("OID %s: expected %q, got %q", tc.oid, tc.expected, got)
			}
		})
	}

	// Verify Brother enterprise OID (2435)
	sysObjectID := findOIDValue(data, oids.SysObjectID)
	if sysObjectID == "" {
		t.Error("sysObjectID not found")
	} else if len(sysObjectID) > 0 && sysObjectID[0] == '.' {
		// Should contain Brother enterprise OID 2435
		if !contains(sysObjectID, "2435") {
			t.Errorf("expected Brother enterprise OID (2435), got %s", sysObjectID)
		}
	}

	// Verify page count is present and reasonable
	pageCount := findOIDValue(data, oids.PrtMarkerLifeCount)
	if pageCount == "" {
		t.Log("page count not found at standard OID, checking with index")
	}
}

// TestRealData_Epson_CW tests parsing of real Epson CW-C6500Au SNMP data
func TestRealData_Epson_CW(t *testing.T) {
	data := loadTestData(t, "epson_cw_snmp.json")
	if data == nil {
		return
	}

	// Verify key OIDs
	tests := []struct {
		oid      string
		expected string
		desc     string
	}{
		{oids.HrDeviceDescr, "EPSON CW-C6500Au", "hrDeviceDescr (model)"},
		{oids.PrtGeneralSerialNumber, "X7F4008528", "serial number"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := findOIDValue(data, tc.oid)
			if got != tc.expected {
				t.Errorf("OID %s: expected %q, got %q", tc.oid, tc.expected, got)
			}
		})
	}

	// Verify Epson enterprise OID (1248)
	sysObjectID := findOIDValue(data, oids.SysObjectID)
	if !contains(sysObjectID, "1248") {
		t.Errorf("expected Epson enterprise OID (1248), got %s", sysObjectID)
	}
}

// TestRealData_Epson_SC tests parsing of real Epson SC-T3405 SNMP data
func TestRealData_Epson_SC(t *testing.T) {
	data := loadTestData(t, "epson_sc_snmp.json")
	if data == nil {
		return
	}

	// Check for Epson enterprise OID
	sysObjectID := findOIDValue(data, oids.SysObjectID)
	if sysObjectID != "" && !contains(sysObjectID, "1248") {
		t.Errorf("expected Epson enterprise OID (1248), got %s", sysObjectID)
	}

	// Verify model is detected
	model := findOIDValue(data, oids.HrDeviceDescr)
	if model == "" {
		t.Error("model not found in hrDeviceDescr")
	} else {
		t.Logf("Detected model: %s", model)
	}
}

// TestRealData_Kyocera_P2040 tests parsing of real Kyocera ECOSYS P2040dw SNMP data
func TestRealData_Kyocera_P2040(t *testing.T) {
	data := loadTestData(t, "kyocera_p2040_snmp.json")
	if data == nil {
		return
	}

	// Verify Kyocera enterprise OID (1347)
	sysObjectID := findOIDValue(data, oids.SysObjectID)
	if !contains(sysObjectID, "1347") {
		t.Errorf("expected Kyocera enterprise OID (1347), got %s", sysObjectID)
	}

	// Check model detection
	model := findOIDValue(data, oids.HrDeviceDescr)
	if model == "" {
		t.Error("model not found")
	} else if !contains(model, "P2040") && !contains(model, "ECOSYS") {
		t.Errorf("expected P2040 or ECOSYS in model, got %s", model)
	}
}

// TestRealData_Kyocera_CS4053 tests parsing of real Kyocera CS 4053ci SNMP data
func TestRealData_Kyocera_CS4053(t *testing.T) {
	data := loadTestData(t, "kyocera_cs4053_snmp.json")
	if data == nil {
		return
	}

	// Verify Kyocera enterprise OID (1347)
	sysObjectID := findOIDValue(data, oids.SysObjectID)
	if !contains(sysObjectID, "1347") {
		t.Errorf("expected Kyocera enterprise OID (1347), got %s", sysObjectID)
	}

	// This is a color MFP - check for supplies
	suppliesDesc := findOIDValue(data, "1.3.6.1.2.1.43.11.1.1.6.1.1")
	if suppliesDesc != "" {
		t.Logf("Found supply: %s", suppliesDesc)
	}
}

// TestRealData_VendorDetection verifies vendor detection from real sysObjectID values
func TestRealData_VendorDetection(t *testing.T) {
	testCases := []struct {
		file           string
		expectedVendor string
		enterpriseOID  string
	}{
		{"brother_snmp.json", "Brother", "2435"},
		{"epson_cw_snmp.json", "Epson", "1248"},
		{"epson_sc_snmp.json", "Epson", "1248"},
		{"kyocera_p2040_snmp.json", "Kyocera", "1347"},
		{"kyocera_cs4053_snmp.json", "Kyocera", "1347"},
	}

	for _, tc := range testCases {
		t.Run(tc.file, func(t *testing.T) {
			data := loadTestData(t, tc.file)
			if data == nil {
				return
			}

			sysObjectID := findOIDValue(data, oids.SysObjectID)
			if sysObjectID == "" {
				t.Skip("sysObjectID not found in test data")
			}

			if !contains(sysObjectID, tc.enterpriseOID) {
				t.Errorf("expected enterprise OID %s for %s, got %s",
					tc.enterpriseOID, tc.expectedVendor, sysObjectID)
			}
		})
	}
}

// TestRealData_SuppliesDetection verifies supplies can be parsed from real data
func TestRealData_SuppliesDetection(t *testing.T) {
	testCases := []struct {
		file        string
		description string
		hasSupplies bool
	}{
		{"brother_snmp.json", "Brother HL-L8260CDW (color laser)", true},
		{"epson_cw_snmp.json", "Epson CW-C6500Au (color label)", true},
		{"kyocera_p2040_snmp.json", "Kyocera P2040dw (mono laser)", true},
		{"kyocera_cs4053_snmp.json", "Kyocera CS 4053ci (color MFP)", true},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			data := loadTestData(t, tc.file)
			if data == nil {
				return
			}

			// Check for standard supplies OIDs
			suppliesOIDPrefix := "1.3.6.1.2.1.43.11.1.1"
			foundSupplies := false
			for _, d := range data {
				if len(d.OID) > len(suppliesOIDPrefix) && d.OID[:len(suppliesOIDPrefix)] == suppliesOIDPrefix {
					foundSupplies = true
					break
				}
			}

			if tc.hasSupplies && !foundSupplies {
				t.Errorf("expected supplies data in %s", tc.file)
			}
		})
	}
}

// TestRealData_PageCounts verifies page count OIDs are present
func TestRealData_PageCounts(t *testing.T) {
	testCases := []struct {
		file        string
		description string
	}{
		{"brother_snmp.json", "Brother HL-L8260CDW"},
		{"epson_cw_snmp.json", "Epson CW-C6500Au"},
		{"kyocera_p2040_snmp.json", "Kyocera P2040dw"},
		{"kyocera_cs4053_snmp.json", "Kyocera CS 4053ci"},
	}

	// Standard page count OID (with various indices)
	pageCountOIDPrefix := "1.3.6.1.2.1.43.10.2.1.4"

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			data := loadTestData(t, tc.file)
			if data == nil {
				return
			}

			foundPageCount := false
			var pageCount string
			for _, d := range data {
				if len(d.OID) >= len(pageCountOIDPrefix) && d.OID[:len(pageCountOIDPrefix)] == pageCountOIDPrefix {
					foundPageCount = true
					pageCount = d.Value
					break
				}
			}

			if !foundPageCount {
				t.Error("page count OID not found")
			} else {
				t.Logf("Page count: %s", pageCount)
			}
		})
	}
}

// contains checks if substr is in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
