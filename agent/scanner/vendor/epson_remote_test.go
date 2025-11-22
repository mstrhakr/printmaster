package vendor

import "testing"

func TestBuildEpsonRemoteOIDExamples(t *testing.T) {
	t.Parallel()

	oid, err := BuildEpsonRemoteOID(EpsonRemoteDeviceIDCommand, nil)
	if err != nil {
		t.Fatalf("device id build failed: %v", err)
	}
	expected := epsonRemoteBaseOID + ".100.105.1.0.1"
	if oid != expected {
		t.Fatalf("expected %s got %s", expected, oid)
	}

	statusOID, err := BuildEpsonRemoteOID(EpsonRemoteStatusCommand, nil)
	if err != nil {
		t.Fatalf("status build failed: %v", err)
	}
	expectedStatus := epsonRemoteBaseOID + ".115.116.1.0.1"
	if statusOID != expectedStatus {
		t.Fatalf("expected %s got %s", expectedStatus, statusOID)
	}
}

func TestBuildEpsonInkSlotOID(t *testing.T) {
	t.Parallel()

	oid, err := BuildEpsonInkSlotOID(3)
	if err != nil {
		t.Fatalf("ink slot build failed: %v", err)
	}
	expected := epsonRemoteBaseOID + ".105.105.2.0.1.3"
	if oid != expected {
		t.Fatalf("expected %s got %s", expected, oid)
	}
}

func TestBuildEpsonEEPROMReadOID(t *testing.T) {
	t.Parallel()

	payload := []byte{0x65, 0x00, 0x12, 0x34}
	oid, err := BuildEpsonEEPROMReadOID(payload)
	if err != nil {
		t.Fatalf("eeprom build failed: %v", err)
	}
	expected := epsonRemoteBaseOID + ".124.124.4.0.101.0.18.52"
	if oid != expected {
		t.Fatalf("expected %s got %s", expected, oid)
	}
}

func TestBuildEpsonEEPROMReadOIDRequiresPayload(t *testing.T) {
	t.Parallel()
	if _, err := BuildEpsonEEPROMReadOID(nil); err == nil {
		t.Fatalf("expected error for empty payload")
	}
}
