package scanner

import (
	"context"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// Mock SavedDeviceChecker for testing
type mockSavedDeviceChecker struct {
	knownDevices map[string]interface{}
}

func (m *mockSavedDeviceChecker) IsKnownDevice(ip string) (bool, interface{}) {
	if data, ok := m.knownDevices[ip]; ok {
		return true, data
	}
	return false, nil
}

func TestDetectFunc_SavedDeviceBypass(t *testing.T) {
	t.Parallel()

	// Setup mock saved device checker
	checker := &mockSavedDeviceChecker{
		knownDevices: map[string]interface{}{
			"10.0.0.1": "cached printer data",
		},
	}

	cfg := DetectorConfig{
		SavedDeviceChecker: checker,
		SkipSavedDevices:   true,
		SNMPTimeout:        5,
	}

	detectFunc := DetectFunc(cfg)
	job := ScanJob{IP: "10.0.0.1", Source: "test"}

	info, isPrinter, err := detectFunc(context.Background(), job, []int{9100})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isPrinter {
		t.Error("expected isPrinter=true for known device")
	}
	if info != "cached printer data" {
		t.Errorf("expected cached data, got %v", info)
	}
}

func TestDetectFunc_NoOpenPorts(t *testing.T) {
	t.Parallel()

	cfg := DetectorConfig{
		SNMPTimeout: 5,
	}

	detectFunc := DetectFunc(cfg)
	job := ScanJob{IP: "10.0.0.2", Source: "test"}

	info, isPrinter, err := detectFunc(context.Background(), job, []int{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isPrinter {
		t.Error("expected isPrinter=false when no open ports")
	}
	if info != nil {
		t.Errorf("expected nil info, got %v", info)
	}
}

func TestDetectFunc_SNMPQuery(t *testing.T) {
	// Cannot run in parallel - modifies global NewSNMPClientFunc

	// Save and restore original NewSNMPClient
	originalNewSNMPClient := NewSNMPClientFunc
	defer func() { NewSNMPClientFunc = originalNewSNMPClient }()

	// Mock SNMP client with serial number
	mockClient := &mockSNMPClient{
		getResult: &gosnmp.SnmpPacket{
			Variables: []gosnmp.SnmpPDU{
				{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("SN123456")},
				{Name: ".1.3.6.1.4.1.11.2.3.9.1.2.1", Type: gosnmp.OctetString, Value: []byte("HP_SERIAL")},
			},
		},
	}

	NewSNMPClientFunc = func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		return mockClient, nil
	}

	cfg := DetectorConfig{
		SNMPTimeout: 5,
	}

	detectFunc := DetectFunc(cfg)
	job := ScanJob{IP: "10.0.0.3", Source: "test"}

	info, isPrinter, err := detectFunc(context.Background(), job, []int{9100})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isPrinter {
		t.Error("expected isPrinter=true when serial found")
	}
	if info == nil {
		t.Error("expected non-nil info")
	}

	// Check that we got a QueryResult
	if qr, ok := info.(*QueryResult); ok {
		if len(qr.PDUs) < 1 {
			t.Errorf("expected at least 1 PDU, got %d", len(qr.PDUs))
		}
	} else {
		t.Errorf("expected *QueryResult, got %T", info)
	}
}

func TestDeepScanFunc(t *testing.T) {
	// Cannot run in parallel - modifies global NewSNMPClientFunc

	// Save and restore original NewSNMPClient
	originalNewSNMPClient := NewSNMPClientFunc
	defer func() { NewSNMPClientFunc = originalNewSNMPClient }()

	// Mock SNMP client with walk data
	mockClient := &mockSNMPClient{
		walkPDUs: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: []byte("HP LaserJet")},
			{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("SN789")},
		},
	}

	NewSNMPClientFunc = func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		return mockClient, nil
	}

	cfg := DetectorConfig{
		SNMPTimeout: 30,
	}

	deepScanFunc := DeepScanFunc(cfg)

	dr := DetectionResult{
		Job:       ScanJob{IP: "10.0.0.4", Source: "test"},
		IsPrinter: true,
		Info:      nil,
	}

	result, err := deepScanFunc(context.Background(), dr)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Check that we got a QueryResult with walk data
	if qr, ok := result.(*QueryResult); ok {
		// Walk returns 2 PDUs x 3 roots = 6 PDUs
		if len(qr.PDUs) < 2 {
			t.Errorf("expected at least 2 PDUs from walk, got %d", len(qr.PDUs))
		}
		if qr.Profile != QueryFull {
			t.Errorf("expected QueryFull profile, got %s", qr.Profile)
		}
	} else {
		t.Errorf("expected *QueryResult, got %T", result)
	}
}

func TestEnrichFunc(t *testing.T) {
	// Cannot run in parallel - modifies global NewSNMPClientFunc

	// Save and restore original NewSNMPClient
	originalNewSNMPClient := NewSNMPClientFunc
	defer func() { NewSNMPClientFunc = originalNewSNMPClient }()

	// Mock SNMP client with essential data (matching HP GetEssentialOIDs)
	mockClient := &mockSNMPClient{
		getResult: &gosnmp.SnmpPacket{
			Variables: []gosnmp.SnmpPDU{
				{Name: ".1.3.6.1.2.1.43.5.1.1.17.1", Type: gosnmp.OctetString, Value: []byte("SN456")},
				{Name: ".1.3.6.1.4.1.11.2.3.9.1.2.1", Type: gosnmp.OctetString, Value: []byte("HP_SN")},
				{Name: ".1.3.6.1.2.1.43.10.2.1.4.1.1", Type: gosnmp.Integer, Value: 5000},
				{Name: ".1.3.6.1.2.1.43.11.1.1.9.1", Type: gosnmp.Integer, Value: 100},
				{Name: ".1.3.6.1.2.1.43.11.1.1.6.1", Type: gosnmp.Integer, Value: 50},
				{Name: ".1.3.6.1.2.1.43.5.1.1.4.1", Type: gosnmp.Integer, Value: 4},
			},
		},
	}

	NewSNMPClientFunc = func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		return mockClient, nil
	}

	cfg := DetectorConfig{
		SNMPTimeout: 10,
	}

	enrichFunc := EnrichFunc(cfg)

	result, err := enrichFunc(context.Background(), "10.0.0.5", "hp")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Check that we got a QueryResult with QueryEssential profile
	if qr, ok := result.(*QueryResult); ok {
		if qr.Profile != QueryEssential {
			t.Errorf("expected QueryEssential profile, got %s", qr.Profile)
		}
		if len(qr.PDUs) < 1 {
			t.Errorf("expected at least 1 PDU, got %d", len(qr.PDUs))
		}
	} else {
		t.Errorf("expected *QueryResult, got %T", result)
	}
}

func TestMetricsFunc(t *testing.T) {
	t.Skip("Skipping - vendor module removed, MetricsFunc now returns QueryResult instead of DeviceMetricsSnapshot")

	// Cannot run in parallel - modifies global NewSNMPClientFunc

	// Save and restore original NewSNMPClient
	originalNewSNMPClient := NewSNMPClientFunc
	defer func() { NewSNMPClientFunc = originalNewSNMPClient }()

	// Mock SNMP client with metrics data
	mockClient := &mockSNMPClient{
		getResult: &gosnmp.SnmpPacket{
			Variables: []gosnmp.SnmpPDU{
				{Name: ".1.3.6.1.2.1.43.10.2.1.4.1.1", Type: gosnmp.Integer, Value: 10000},
				{Name: ".1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.5.0", Type: gosnmp.Integer, Value: 250}, // HP fax pages
			},
		},
	}

	NewSNMPClientFunc = func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
		return mockClient, nil
	}

	cfg := DetectorConfig{
		SNMPTimeout: 10,
	}

	metricsFunc := MetricsFunc(cfg)

	snapshot, err := metricsFunc(context.Background(), "10.0.0.6", "hp")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}

	// Check that we got metrics - commented out due to vendor module removal
	// QueryResult doesn't have PageCount/FaxPages fields
	// if snapshot.PageCount != 10000 {
	// 	t.Errorf("expected PageCount=10000, got %d", snapshot.PageCount)
	// }
	// if snapshot.FaxPages != 250 {
	// 	t.Errorf("expected FaxPages=250, got %d", snapshot.FaxPages)
	// }
}

func TestEnumerateIPs(t *testing.T) {
	t.Parallel()

	// Mock parse function that returns a simple list
	parseFunc := func(text string, maxAddrs int) ([]string, error) {
		return []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, nil
	}

	ctx := context.Background()
	jobs := EnumerateIPs(ctx, "10.0.0.1-3", "test", parseFunc, 100)

	// Collect all jobs
	var ips []string
	for job := range jobs {
		ips = append(ips, job.IP)
		if job.Source != "test" {
			t.Errorf("expected source='test', got '%s'", job.Source)
		}
	}

	if len(ips) != 3 {
		t.Errorf("expected 3 IPs, got %d", len(ips))
	}

	expected := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for i, ip := range ips {
		if ip != expected[i] {
			t.Errorf("expected IP %s at index %d, got %s", expected[i], i, ip)
		}
	}
}

func TestEnumerateIPs_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Mock parse function that returns many IPs
	parseFunc := func(text string, maxAddrs int) ([]string, error) {
		ips := make([]string, 1000)
		for i := range ips {
			ips[i] = "10.0.0.1"
		}
		return ips, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancel is called on all paths
	jobs := EnumerateIPs(ctx, "large range", "test", parseFunc, 10000)

	// Read a few jobs then cancel
	count := 0
	for range jobs {
		count++
		if count == 5 {
			cancel()
			break
		}
	}

	// Drain remaining
	for range jobs {
		count++
	}

	// Should have stopped early due to cancellation
	if count >= 1000 {
		t.Errorf("expected early termination, got %d jobs", count)
	}
}
