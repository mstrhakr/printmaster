package agent

import (
	"os"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// SNMPConfig holds SNMP discovery settings
type SNMPConfig struct {
	// Community is the community string for SNMPv1/v2c
	Community string
	// Version describes the SNMP protocol version to use (v1, v2c, or v3).
	Version gosnmp.SnmpVersion

	// SNMPv3 security parameters
	// SecurityLevel: noAuthNoPriv, authNoPriv, or authPriv
	SecurityLevel gosnmp.SnmpV3MsgFlags
	// Username is the SNMPv3 security name (USM user)
	Username string
	// AuthProtocol: MD5, SHA, SHA224, SHA256, SHA384, SHA512
	AuthProtocol gosnmp.SnmpV3AuthProtocol
	// AuthPassword is the authentication passphrase
	AuthPassword string
	// PrivProtocol: DES, AES, AES192, AES256
	PrivProtocol gosnmp.SnmpV3PrivProtocol
	// PrivPassword is the privacy passphrase
	PrivPassword string
	// ContextName is the SNMPv3 context name (optional)
	ContextName string
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
	case "", "2c", "v2c", "2":
		sver = gosnmp.Version2c // default to v2c for better compatibility
	case "1", "v1":
		sver = gosnmp.Version1
	case "3", "v3":
		sver = gosnmp.Version3
	default:
		sver = gosnmp.Version2c
	}

	cfg := &SNMPConfig{Community: community, Version: sver}

	// SNMPv3 configuration from environment
	if sver == gosnmp.Version3 {
		cfg.Username = os.Getenv("SNMP_USERNAME")
		cfg.ContextName = os.Getenv("SNMP_CONTEXT_NAME")

		// Security level
		secLevel := strings.ToLower(os.Getenv("SNMP_SECURITY_LEVEL"))
		switch secLevel {
		case "authpriv":
			cfg.SecurityLevel = gosnmp.AuthPriv
		case "authnopriv":
			cfg.SecurityLevel = gosnmp.AuthNoPriv
		default:
			cfg.SecurityLevel = gosnmp.NoAuthNoPriv
		}

		// Auth protocol
		authProto := strings.ToUpper(os.Getenv("SNMP_AUTH_PROTOCOL"))
		switch authProto {
		case "MD5":
			cfg.AuthProtocol = gosnmp.MD5
		case "SHA", "SHA1":
			cfg.AuthProtocol = gosnmp.SHA
		case "SHA224":
			cfg.AuthProtocol = gosnmp.SHA224
		case "SHA256":
			cfg.AuthProtocol = gosnmp.SHA256
		case "SHA384":
			cfg.AuthProtocol = gosnmp.SHA384
		case "SHA512":
			cfg.AuthProtocol = gosnmp.SHA512
		default:
			cfg.AuthProtocol = gosnmp.NoAuth
		}
		cfg.AuthPassword = os.Getenv("SNMP_AUTH_PASSWORD")

		// Privacy protocol
		privProto := strings.ToUpper(os.Getenv("SNMP_PRIV_PROTOCOL"))
		switch privProto {
		case "DES":
			cfg.PrivProtocol = gosnmp.DES
		case "AES", "AES128":
			cfg.PrivProtocol = gosnmp.AES
		case "AES192":
			cfg.PrivProtocol = gosnmp.AES192
		case "AES256":
			cfg.PrivProtocol = gosnmp.AES256
		case "AES192C":
			cfg.PrivProtocol = gosnmp.AES192C
		case "AES256C":
			cfg.PrivProtocol = gosnmp.AES256C
		default:
			cfg.PrivProtocol = gosnmp.NoPriv
		}
		cfg.PrivPassword = os.Getenv("SNMP_PRIV_PASSWORD")
	}

	return cfg, nil
}

// GetRetentionConfig loads retention settings from environment or defaults
func GetRetentionConfig() *RetentionConfig {
	// Default to 30 days for both
	return &RetentionConfig{
		ScanHistoryDays:   30,
		HiddenDevicesDays: 30,
	}
}
