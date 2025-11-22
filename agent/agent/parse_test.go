package agent

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestParsePDUs_NonUTF8SerialAndHexCounter(t *testing.T) {
	t.Parallel()
	t.Run("non-utf8-serial-sanitized", func(t *testing.T) {
		t.Parallel()
		// serial contains a NUL byte which should be stripped by DecodeOctetString
		vars := []gosnmp.SnmpPDU{
			// Use prtGeneralSerialNumber (17.1) for serial, not 16.1 (description)
			{Name: "1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte{'S', 'N', 0x00, 'A', 'B', 'C'}},
		}
		pi, ok := ParsePDUs("10.0.0.1", vars, nil, nil)
		if !ok {
			t.Fatalf("expected device to be detected as printer due to serial, got not-printer")
		}
		if pi.Serial != "SNABC" {
			t.Fatalf("expected sanitized serial 'SNABC', got '%s'", pi.Serial)
		}
	})

	t.Run("hex-counter-parsed", func(t *testing.T) {
		t.Parallel()
		// marker life count provided as hex string should parse to integer
		vars := []gosnmp.SnmpPDU{
			{Name: "1.3.6.1.2.1.43.10.2.1.4.1.1", Type: gosnmp.OctetString, Value: []byte("0xb3e7")},
		}
		pi, ok := ParsePDUs("10.0.0.2", vars, nil, nil)
		if !ok {
			t.Fatalf("expected device to be detected as printer due to marker count, got not-printer")
		}
		if pi.PageCount != 0xb3e7 {
			t.Fatalf("expected PageCount %d got %d", 0xb3e7, pi.PageCount)
		}
		if pi.MonoImpressions != 0xb3e7 {
			t.Fatalf("expected MonoImpressions %d got %d", 0xb3e7, pi.MonoImpressions)
		}
	})
}

func TestParsePDUs_OID16_UUID_DoesNotSetSerial(t *testing.T) {
	t.Parallel()
	// Ensure UUID-like value at 1.3.6.1.2.1.43.5.1.1.16.1 does not become serial
	uuid := "5995673a-a33d-49d2-a45e-1852a977"
	vars := []gosnmp.SnmpPDU{
		{Name: "1.3.6.1.2.1.43.5.1.1.16.1", Type: gosnmp.OctetString, Value: []byte(uuid)},
		// Provide a marker count so the device is detected as a printer
		{Name: "1.3.6.1.2.1.43.10.2.1.4.1.1", Type: gosnmp.OctetString, Value: []byte("1234")},
	}
	pi, ok := ParsePDUs("10.0.0.3", vars, nil, nil)
	if !ok {
		t.Fatalf("expected device to be detected as printer due to marker count")
	}
	if pi.Serial != "" {
		t.Fatalf("expected serial to remain empty when only OID 16.1 UUID present; got '%s'", pi.Serial)
	}
	if pi.Description != uuid {
		t.Fatalf("expected description to capture UUID '%s', got '%s'", uuid, pi.Description)
	}
}

func TestParsePDUs_VendorDeviceIDSetsModelAndSerial(t *testing.T) {
	t.Parallel()

	vars := []gosnmp.SnmpPDU{
		{Name: "1.3.6.1.4.1.11.2.3.9.1.1.7.0", Type: gosnmp.OctetString, Value: []byte("MFG:HP;MDL:LaserJet 400;SN:CN123456;DES:Workgroup printer;")},
		{Name: "1.3.6.1.2.1.43.10.2.1.4.1.1", Type: gosnmp.OctetString, Value: []byte("1500")},
	}

	pi, ok := ParsePDUs("10.0.0.4", vars, nil, nil)
	if !ok {
		t.Fatalf("expected vendor device ID to still mark as printer via marker count")
	}
	if pi.Model != "LaserJet 400" {
		t.Fatalf("expected model LaserJet 400, got %q", pi.Model)
	}
	if pi.Serial != "CN123456" {
		t.Fatalf("expected serial CN123456, got %q", pi.Serial)
	}
	if pi.Description != "Workgroup printer" {
		t.Fatalf("expected description from DES field, got %q", pi.Description)
	}
}
