package main

import (
	"context"
	"sync"
	"testing"

	"printmaster/agent/agent"
	"printmaster/agent/storage"
)

func TestShouldRefreshIdentitiesForVersion(t *testing.T) {
	configStore := newFakeConfigStore()

	shouldRefresh, err := shouldRefreshIdentitiesForVersion(configStore, "0.30.6+abc123")
	if err != nil {
		t.Fatalf("shouldRefreshIdentitiesForVersion() error = %v", err)
	}
	if !shouldRefresh {
		t.Fatal("expected first startup to schedule an identity refresh")
	}

	if err := configStore.SetConfigValue(identityRefreshVersionConfigKey, "0.30.6+abc123"); err != nil {
		t.Fatalf("SetConfigValue() error = %v", err)
	}
	shouldRefresh, err = shouldRefreshIdentitiesForVersion(configStore, "0.30.6+abc123")
	if err != nil {
		t.Fatalf("shouldRefreshIdentitiesForVersion() error = %v", err)
	}
	if shouldRefresh {
		t.Fatal("did not expect refresh when the version marker matches")
	}

	shouldRefresh, err = shouldRefreshIdentitiesForVersion(configStore, "0.30.7+def456")
	if err != nil {
		t.Fatalf("shouldRefreshIdentitiesForVersion() error = %v", err)
	}
	if !shouldRefresh {
		t.Fatal("expected refresh after a version change")
	}
}

func TestRefreshKnownDeviceIdentitiesPersistsMatchingSerial(t *testing.T) {
	device := &storage.Device{}
	device.IP = "192.168.100.99"
	device.Serial = "9175R401338"
	var persisted []agent.PrinterInfo
	var persistedMu sync.Mutex

	result := refreshKnownDeviceIdentities(
		context.Background(),
		[]*storage.Device{device},
		func(_ context.Context, ip string, timeoutSeconds int) (*agent.PrinterInfo, error) {
			if ip != device.IP || timeoutSeconds != 10 {
				t.Fatalf("refresh called with ip=%q timeout=%d", ip, timeoutSeconds)
			}
			return &agent.PrinterInfo{IP: ip, Serial: "9175R401338", Manufacturer: "Ricoh"}, nil
		},
		func(_ context.Context, pi agent.PrinterInfo) error {
			persistedMu.Lock()
			defer persistedMu.Unlock()
			persisted = append(persisted, pi)
			return nil
		},
	)

	if result.Attempted != 1 || result.Refreshed != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted %d devices, want 1", len(persisted))
	}
	if persisted[0].Manufacturer != "Ricoh" || persisted[0].Serial != device.Serial {
		t.Errorf("persisted device = %+v, want Ricoh with serial %q", persisted[0], device.Serial)
	}
}

func TestRefreshKnownDeviceIdentitiesSkipsUnsafeResponses(t *testing.T) {
	deviceA := &storage.Device{}
	deviceA.IP = "192.168.100.10"
	deviceA.Serial = "SERIAL-A"
	deviceB := &storage.Device{}
	deviceB.IP = "192.168.100.11"
	deviceB.Serial = "SERIAL-B"
	deviceC := &storage.Device{}
	deviceC.Serial = "SERIAL-C"
	devices := []*storage.Device{deviceA, deviceB, deviceC}
	persisted := false

	result := refreshKnownDeviceIdentities(
		context.Background(),
		devices,
		func(_ context.Context, ip string, _ int) (*agent.PrinterInfo, error) {
			switch ip {
			case "192.168.100.10":
				return &agent.PrinterInfo{IP: ip, Serial: "OTHER-SERIAL", Manufacturer: "Ricoh"}, nil
			case "192.168.100.11":
				return &agent.PrinterInfo{IP: ip, Serial: "SERIAL-B"}, nil
			default:
				t.Fatalf("unexpected refresh for %q", ip)
				return nil, nil
			}
		},
		func(_ context.Context, _ agent.PrinterInfo) error {
			persisted = true
			return nil
		},
	)

	if result.Attempted != 2 || result.Refreshed != 0 || result.Skipped != 3 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if persisted {
		t.Fatal("unsafe refresh response was persisted")
	}
}
