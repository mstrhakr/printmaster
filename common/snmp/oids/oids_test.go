package oids

import (
	"strings"
	"testing"
)

func TestOIDsAreValidFormat(t *testing.T) {
	t.Parallel()

	// All OIDs should be valid dotted decimal format
	oids := []struct {
		name string
		oid  string
	}{
		// MIB-II System
		{"SysDescr", SysDescr},
		{"SysObjectID", SysObjectID},
		{"SysUpTime", SysUpTime},
		{"SysName", SysName},
		{"SysLocation", SysLocation},

		// MIB-II Interfaces
		{"IfPhysAddress", IfPhysAddress},

		// Host Resources MIB
		{"HrSystemUptime", HrSystemUptime},
		{"HrDeviceDescr", HrDeviceDescr},
		{"HrDeviceType", HrDeviceType},
		{"HrDeviceStatus", HrDeviceStatus},

		// Printer MIB
		{"PrtGeneralSerialNumber", PrtGeneralSerialNumber},
		{"PrtGeneralPrinterName", PrtGeneralPrinterName},
		{"PrtMarkerLifeCount", PrtMarkerLifeCount},
		{"HrPrinterStatus", HrPrinterStatus},
		{"HrPrinterDetectedErrorState", HrPrinterDetectedErrorState},

		// Supplies
		{"PrtMarkerSuppliesEntry", PrtMarkerSuppliesEntry},
		{"PrtMarkerSuppliesDesc", PrtMarkerSuppliesDesc},
		{"PrtMarkerSuppliesLevel", PrtMarkerSuppliesLevel},
		{"PrtMarkerSuppliesMaxCap", PrtMarkerSuppliesMaxCap},

		// Port Monitor
		{"PpmPrinterIEEE1284DeviceID", PpmPrinterIEEE1284DeviceID},

		// Epson
		{"EpsonEnterprise", EpsonEnterprise},
		{"EpsonModelName", EpsonModelName},
		{"EpsonSerialNumber", EpsonSerialNumber},
	}

	for _, tc := range oids {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.oid == "" {
				t.Errorf("%s is empty", tc.name)
				return
			}

			// Should start with "1."
			if !strings.HasPrefix(tc.oid, "1.") {
				t.Errorf("%s = %q should start with '1.'", tc.name, tc.oid)
			}

			// Should only contain digits and dots
			for _, c := range tc.oid {
				if c != '.' && (c < '0' || c > '9') {
					t.Errorf("%s = %q contains invalid character %q", tc.name, tc.oid, c)
					break
				}
			}

			// Should not have consecutive dots
			if strings.Contains(tc.oid, "..") {
				t.Errorf("%s = %q contains consecutive dots", tc.name, tc.oid)
			}

			// Should not end with dot
			if strings.HasSuffix(tc.oid, ".") {
				t.Errorf("%s = %q ends with dot", tc.name, tc.oid)
			}
		})
	}
}

func TestOIDsAreUnique(t *testing.T) {
	t.Parallel()

	// Collect all OIDs and ensure no duplicates
	oids := map[string]string{
		"SysDescr":                     SysDescr,
		"SysObjectID":                  SysObjectID,
		"SysUpTime":                    SysUpTime,
		"SysName":                      SysName,
		"SysLocation":                  SysLocation,
		"IfPhysAddress":                IfPhysAddress,
		"HrSystemUptime":               HrSystemUptime,
		"HrDeviceDescr":                HrDeviceDescr,
		"PrtGeneralSerialNumber":       PrtGeneralSerialNumber,
		"PrtGeneralPrinterName":        PrtGeneralPrinterName,
		"PrtMarkerLifeCount":           PrtMarkerLifeCount,
		"PpmPrinterIEEE1284DeviceID":   PpmPrinterIEEE1284DeviceID,
		"EpsonEnterprise":              EpsonEnterprise,
	}

	seen := make(map[string]string)
	for name, oid := range oids {
		if existing, ok := seen[oid]; ok {
			t.Errorf("duplicate OID %q used by both %s and %s", oid, existing, name)
		}
		seen[oid] = name
	}
}

func TestMIBPrefixes(t *testing.T) {
	t.Parallel()

	// Verify MIB prefixes are correct
	tests := []struct {
		name   string
		oid    string
		prefix string
	}{
		{"SysDescr is MIB-II System", SysDescr, "1.3.6.1.2.1.1."},
		{"HrSystemUptime is Host Resources", HrSystemUptime, "1.3.6.1.2.1.25."},
		{"PrtGeneralSerialNumber is Printer MIB", PrtGeneralSerialNumber, "1.3.6.1.2.1.43."},
		{"EpsonEnterprise is Enterprise", EpsonEnterprise, "1.3.6.1.4.1."},
		{"PpmPrinterIEEE1284DeviceID is PWG", PpmPrinterIEEE1284DeviceID, "1.3.6.1.4.1.2699."},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.HasPrefix(tc.oid, tc.prefix) {
				t.Errorf("%s = %q should have prefix %q", tc.name, tc.oid, tc.prefix)
			}
		})
	}
}
