package agent

import (
	"os"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// SNMPConfig holds SNMP discovery settings
type SNMPConfig struct {
	Community string
	// Version describes the SNMP protocol version to use (v1 or v2c).
	Version gosnmp.SnmpVersion
}

// RetentionConfig holds data retention settings
type RetentionConfig struct {
	// ScanHistoryDays is how many days of scan history to keep (default 30)
	ScanHistoryDays int
	// HiddenDevicesDays is how many days to keep hidden devices before deletion (default 30)
	HiddenDevicesDays int
}

// GetSNMPConfig loads SNMP config from environment or defaults
func GetSNMPConfig() (*SNMPConfig, error) {
	community := os.Getenv("SNMP_COMMUNITY")
	if community == "" {
		community = "public"
	}
	ver := strings.ToLower(os.Getenv("SNMP_VERSION"))
	var sver gosnmp.SnmpVersion
	switch ver {
	case "", "1", "v1":
		sver = gosnmp.Version1
	case "2", "2c", "v2", "v2c":
		sver = gosnmp.Version2c
	default:
		// default to v1 for maximum compatibility
		sver = gosnmp.Version1
	}
	return &SNMPConfig{Community: community, Version: sver}, nil
}

// GetRetentionConfig loads retention settings from environment or defaults
func GetRetentionConfig() *RetentionConfig {
	// Default to 30 days for both
	return &RetentionConfig{
		ScanHistoryDays:   30,
		HiddenDevicesDays: 30,
	}
}
