package scanner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"printmaster/common/snmp/oids"

	"github.com/gosnmp/gosnmp"
)

// mockSNMPClient implements SNMPClient for testing
type mockSNMPClient struct {
	connectErr error
	getResult  *gosnmp.SnmpPacket
	getResults []*gosnmp.SnmpPacket
	getErr     error
	walkPDUs   []gosnmp.SnmpPDU
	walkErr    error
	getInputs  [][]string
	getCalls   int
}

func (m *mockSNMPClient) Connect() error {
	return m.connectErr
}

func (m *mockSNMPClient) Get(oids []string) (*gosnmp.SnmpPacket, error) {
	m.getCalls++
	copyOids := make([]string, len(oids))
	copy(copyOids, oids)
	m.getInputs = append(m.getInputs, copyOids)
	if m.getErr != nil {
		return nil, m.getErr
	}
	if len(m.getResults) > 0 {
		idx := m.getCalls - 1
		if idx < len(m.getResults) {
			return m.getResults[idx], nil
		}
		return m.getResults[len(m.getResults)-1], nil
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

	preflight := &gosnmp.SnmpPacket{
		Variables: []gosnmp.SnmpPDU{
			{Name: "." + oids.SysObjectID, Type: gosnmp.OctetString, Value: []byte(".1.3.6.1.4.1.11")},
			{Name: "." + oids.SysDescr, Type: gosnmp.OctetString, Value: []byte("HP LaserJet")},
			{Name: "." + oids.HrDeviceDescr, Type: gosnmp.OctetString, Value: []byte("HP Model")},
		},
	}

	expectedOIDs := buildQueryOIDs(QueryMinimal)
	batches := clusterOIDs(expectedOIDs, defaultOIDBatchSize)
	batchPackets := make([]*gosnmp.SnmpPacket, 0, len(batches))
	for _, batch := range batches {
		vars := make([]gosnmp.SnmpPDU, len(batch))
		for i, oid := range batch {
			value := []byte("GENERIC")
			if oid == oids.PrtGeneralSerialNumber {
				value = []byte("SN12345")
			}
			vars[i] = gosnmp.SnmpPDU{Name: oid, Type: gosnmp.OctetString, Value: value}
		}
		batchPackets = append(batchPackets, &gosnmp.SnmpPacket{Variables: vars})
	}

	mockClient := &mockSNMPClient{
		getResults: append([]*gosnmp.SnmpPacket{preflight}, batchPackets...),
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

	if len(result.PDUs) != len(expectedOIDs) {
		t.Errorf("expected %d PDUs, got %d", len(expectedOIDs), len(result.PDUs))
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
			{Name: "." + oids.SysDescr, Type: gosnmp.OctetString, Value: []byte("HP LaserJet")},
			{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("printer-01")},
			{Name: "." + oids.PrtGeneralSerialNumber, Type: gosnmp.OctetString, Value: []byte("SN98765")},
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

	if !strings.Contains(err.Error(), "SNMP timeout") {
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
						{Name: "." + oids.PrtGeneralSerialNumber, Type: gosnmp.OctetString, Value: []byte("SERIAL123")},
						{Name: "." + oids.PrtMarkerLifeCount + ".1", Type: gosnmp.Integer, Value: 1000},
						{Name: "." + oids.PrtMarkerSuppliesDesc + ".1.1", Type: gosnmp.Integer, Value: 50},
					},
				},
				walkPDUs: []gosnmp.SnmpPDU{
					{Name: "." + oids.SysDescr, Type: gosnmp.OctetString, Value: []byte("Test Device")},
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
						{Name: "." + oids.PrtGeneralSerialNumber, Type: gosnmp.OctetString, Value: []byte("TEST")},
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

			// Vendor module verification removed (no longer using vendor package)
		})
	}
}

func TestBuildQueryOIDs_Minimal(t *testing.T) {
	t.Parallel()

	queryOIDs := buildQueryOIDs(QueryMinimal)

	if len(queryOIDs) == 0 {
		t.Error("expected non-empty OID list for QueryMinimal")
	}

	// QueryMinimal should include serial OID
	expectedSerial := oids.PrtGeneralSerialNumber
	found := false
	for _, oid := range queryOIDs {
		if oid == expectedSerial {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected serial OID %s in minimal query", expectedSerial)
	}

	// QueryMinimal should also include at least one vendor-specific device ID OID (HP IEEE-1284)
	vendorOID := "1.3.6.1.4.1.11.2.3.9.1.1.7.0"
	if !containsOID(queryOIDs, vendorOID) {
		t.Errorf("expected vendor device ID OID %s in minimal query", vendorOID)
	}
}

func TestBuildQueryOIDs_Essential(t *testing.T) {
	t.Parallel()

	queryOIDs := buildQueryOIDs(QueryEssential)

	if len(queryOIDs) == 0 {
		t.Error("expected non-empty OID list for QueryEssential")
	}

	// QueryEssential should include serial + toner + pages + status
	if len(queryOIDs) < 3 {
		t.Errorf("expected at least 3 OIDs for QueryEssential, got %d", len(queryOIDs))
	}

	t.Logf("QueryEssential returned %d OIDs", len(queryOIDs))
}

func TestBuildQueryOIDs_Metrics(t *testing.T) {
	t.Parallel()

	queryOIDs := buildQueryOIDs(QueryMetrics)

	if len(queryOIDs) == 0 {
		t.Error("expected non-empty OID list for QueryMetrics")
	}

	// QueryMetrics should include standard metrics OIDs (with .1 instance suffix)
	expectedMetrics := oids.PrtMarkerLifeCount + ".1" // page count OID (instance .1)
	found := false
	for _, oid := range queryOIDs {
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

	queryOIDs := buildQueryOIDs(QueryFull)

	// QueryFull uses Walk, so OID list should be nil
	if queryOIDs != nil {
		t.Errorf("expected nil for QueryFull (walk mode), got %v", queryOIDs)
	}
}

func containsOID(list []string, target string) bool {
	for _, oid := range list {
		if oid == target {
			return true
		}
	}
	return false
}
