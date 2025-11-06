package scanner

import (
	"context"
	"errors"
	"printmaster/agent/scanner/vendor"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// mockSNMPClient implements SNMPClient for testing
type mockSNMPClient struct {
	connectErr error
	getResult  *gosnmp.SnmpPacket
	getErr     error
	walkPDUs   []gosnmp.SnmpPDU
	walkErr    error
}

func (m *mockSNMPClient) Connect() error {
	return m.connectErr
}

func (m *mockSNMPClient) Get(oids []string) (*gosnmp.SnmpPacket, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.getResult, nil
}

func (m *mockSNMPClient) Walk(rootOid string, walkFn gosnmp.WalkFunc) error {
	if m.walkErr != nil {
		return m.walkErr
	}
	for _, pdu := range m.walkPDUs {
		if err := walkFn(pdu); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockSNMPClient) Close() error {
	return nil
}

func TestQueryDevice_EmptyIP(t *testing.T) {
	t.Parallel()
	_, err := QueryDevice(context.Background(), "", QueryMinimal, "", 30)
	if err == nil {
		t.Fatal("expected error for empty IP")
	}
	if err.Error() != "ip address required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestQueryDevice_SNMPGetSuccess(t *testing.T) {
	t.Parallel()

	// Mock SNMP client with test PDUs (matching HP GetSerialOIDs)
	mockClient := &mockSNMPClient{
		getResult: &gosnmp.SnmpPacket{
			Variables: []gosnmp.SnmpPDU{
				{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("SN12345")},
				{Name: ".1.3.6.1.4.1.11.2.3.9.1.2.1", Type: gosnmp.OctetString, Value: []byte("SERIAL_HP")},
				{Name: ".1.3.6.1.4.1.11.2.3.9.1.2.2", Type: gosnmp.OctetString, Value: []byte("SERIAL_HP2")},
			},
		},
	}

	// Mock client factory
	mockFactory := func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		if target != "10.0.0.1" {
			t.Errorf("unexpected target: %s", target)
		}
		return mockClient, nil
	}

	result, err := queryDeviceWithCapabilitiesAndClient(context.Background(), "10.0.0.1", QueryMinimal, "hp", 30, nil, mockFactory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.IP != "10.0.0.1" {
		t.Errorf("expected IP 10.0.0.1, got %s", result.IP)
	}

	if len(result.PDUs) != 3 {
		t.Errorf("expected 3 PDUs, got %d", len(result.PDUs))
	}

	if result.Profile != QueryMinimal {
		t.Errorf("expected QueryMinimal profile")
	}
}

func TestQueryDevice_SNMPWalkSuccess(t *testing.T) {
	t.Parallel()

	// Mock SNMP client with walk PDUs
	mockClient := &mockSNMPClient{
		walkPDUs: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: []byte("HP LaserJet")},
			{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("printer-01")},
			{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("SN98765")},
		},
	}

	// Mock client factory
	mockFactory := func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		if target != "10.0.0.2" {
			t.Errorf("unexpected target: %s", target)
		}
		return mockClient, nil
	}

	result, err := queryDeviceWithCapabilitiesAndClient(context.Background(), "10.0.0.2", QueryFull, "hp", 30, nil, mockFactory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// QueryFull walks 3 root OIDs, mock returns 3 PDUs per walk = 9 total
	if len(result.PDUs) != 9 {
		t.Errorf("expected 9 PDUs (3 per walk x 3 roots), got %d", len(result.PDUs))
	}

	if result.Profile != QueryFull {
		t.Errorf("expected QueryFull profile")
	}
}

func TestQueryDevice_SNMPError(t *testing.T) {
	t.Parallel()

	// Mock SNMP client that returns error
	mockClient := &mockSNMPClient{
		getErr: errors.New("SNMP timeout"),
	}

	// Mock client factory
	mockFactory := func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		return mockClient, nil
	}

	_, err := queryDeviceWithCapabilitiesAndClient(context.Background(), "10.0.0.3", QueryEssential, "hp", 30, nil, mockFactory)
	if err == nil {
		t.Fatal("expected error from SNMP GET failure")
	}

	if err.Error() != "SNMP GET failed: SNMP timeout" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestQueryDevice_NoDataReceived(t *testing.T) {
	t.Parallel()

	// Mock SNMP client that returns empty result
	mockClient := &mockSNMPClient{
		getResult: &gosnmp.SnmpPacket{
			Variables: []gosnmp.SnmpPDU{},
		},
	}

	// Mock client factory
	mockFactory := func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		return mockClient, nil
	}

	_, err := queryDeviceWithCapabilitiesAndClient(context.Background(), "10.0.0.4", QueryMinimal, "hp", 30, nil, mockFactory)
	if err == nil {
		t.Fatal("expected error for no SNMP data")
	}

	if err.Error() != "no SNMP data received from 10.0.0.4" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestQueryDevice_AllProfiles(t *testing.T) {
	t.Parallel()

	profiles := []QueryProfile{QueryMinimal, QueryEssential, QueryFull, QueryMetrics}

	for _, profile := range profiles {
		profile := profile // capture range variable
		t.Run(profile.String(), func(t *testing.T) {
			t.Parallel()

			// Mock SNMP client
			mockClient := &mockSNMPClient{
				getResult: &gosnmp.SnmpPacket{
					Variables: []gosnmp.SnmpPDU{
						{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("SERIAL123")},
						{Name: ".1.3.6.1.2.1.43.10.2.1.4.1.1", Type: gosnmp.Integer, Value: 1000},
						{Name: ".1.3.6.1.2.1.43.11.1.1.6.1.1", Type: gosnmp.Integer, Value: 50},
					},
				},
				walkPDUs: []gosnmp.SnmpPDU{
					{Name: ".1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: []byte("Test Device")},
				},
			}

			mockFactory := func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
				return mockClient, nil
			}

			result, err := queryDeviceWithCapabilitiesAndClient(context.Background(), "10.0.0.5", profile, "generic", 30, nil, mockFactory)
			if err != nil {
				t.Fatalf("unexpected error for profile %s: %v", profile, err)
			}

			if result.Profile != profile {
				t.Errorf("expected profile %s, got %s", profile, result.Profile)
			}
		})
	}
}

func TestQueryDevice_AllVendors(t *testing.T) {
	t.Parallel()

	vendors := []string{"hp", "HP", "canon", "brother", "generic", "unknown"}

	for _, v := range vendors {
		v := v // capture range variable
		t.Run(v, func(t *testing.T) {
			t.Parallel()

			// Mock SNMP client
			mockClient := &mockSNMPClient{
				getResult: &gosnmp.SnmpPacket{
					Variables: []gosnmp.SnmpPDU{
						{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("TEST")},
						{Name: ".1.3.6.1.4.1.11.2.3.9.1.2.1", Type: gosnmp.OctetString, Value: []byte("VENDOR_SN")},
					},
				},
			}

			mockFactory := func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
				return mockClient, nil
			}

			result, err := queryDeviceWithCapabilitiesAndClient(context.Background(), "10.0.0.6", QueryMinimal, v, 30, nil, mockFactory)
			if err != nil {
				t.Fatalf("unexpected error for vendor %s: %v", v, err)
			}

			if result.VendorHint != v {
				t.Errorf("expected vendor hint %s, got %s", v, result.VendorHint)
			}

			// Verify vendor module is resolved
			vendorMod := vendor.GetVendor(v)
			if vendorMod == nil {
				t.Errorf("vendor module not found for %s", v)
			}
		})
	}
}

func TestBuildQueryOIDs_Minimal(t *testing.T) {
	t.Parallel()

	oids := buildQueryOIDs(QueryMinimal)

	if len(oids) == 0 {
		t.Error("expected non-empty OID list for QueryMinimal")
	}

	// QueryMinimal should include serial OID
	expectedSerial := "1.3.6.1.2.1.43.5.1.1.17.1"
	found := false
	for _, oid := range oids {
		if oid == expectedSerial {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected serial OID %s in minimal query", expectedSerial)
	}
}

func TestBuildQueryOIDs_Essential(t *testing.T) {
	t.Parallel()

	oids := buildQueryOIDs(QueryEssential)

	if len(oids) == 0 {
		t.Error("expected non-empty OID list for QueryEssential")
	}

	// QueryEssential should include serial + toner + pages + status
	if len(oids) < 3 {
		t.Errorf("expected at least 3 OIDs for QueryEssential, got %d", len(oids))
	}

	t.Logf("QueryEssential returned %d OIDs", len(oids))
}

func TestBuildQueryOIDs_Metrics(t *testing.T) {
	t.Parallel()

	oids := buildQueryOIDs(QueryMetrics)

	if len(oids) == 0 {
		t.Error("expected non-empty OID list for QueryMetrics")
	}

	// QueryMetrics should include standard metrics OIDs
	expectedMetrics := "1.3.6.1.2.1.43.10.2.1.4.1" // page count OID
	found := false
	for _, oid := range oids {
		if oid == expectedMetrics {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected page count OID %s in metrics query", expectedMetrics)
	}
}

func TestBuildQueryOIDs_Full(t *testing.T) {
	t.Parallel()

	oids := buildQueryOIDs(QueryFull)

	// QueryFull uses Walk, so OID list should be nil
	if oids != nil {
		t.Errorf("expected nil for QueryFull (walk mode), got %v", oids)
	}
}
