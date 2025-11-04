package scanner

import (
	"fmt"
	"os"
	"time"

	"github.com/gosnmp/gosnmp"
)

// SNMPConfig holds SNMP connection parameters.
type SNMPConfig struct {
	Community string
	Version   gosnmp.SnmpVersion
}

// SNMPClient defines the interface for SNMP operations.
type SNMPClient interface {
	Connect() error
	Get(oids []string) (*gosnmp.SnmpPacket, error)
	Walk(rootOid string, walkFn gosnmp.WalkFunc) error
	Close() error
}

// gosnmpClient wraps gosnmp.GoSNMP to implement SNMPClient.
type gosnmpClient struct {
	conn *gosnmp.GoSNMP
}

// Connect establishes the SNMP connection.
func (c *gosnmpClient) Connect() error {
	return c.conn.Connect()
}

// Get performs an SNMP GET request.
func (c *gosnmpClient) Get(oids []string) (*gosnmp.SnmpPacket, error) {
	return c.conn.Get(oids)
}

// Walk performs an SNMP WALK request.
func (c *gosnmpClient) Walk(rootOid string, walkFn gosnmp.WalkFunc) error {
	return c.conn.Walk(rootOid, walkFn)
}

// Close closes the SNMP connection.
func (c *gosnmpClient) Close() error {
	return c.conn.Conn.Close()
}

// GetSNMPConfig loads SNMP configuration from environment variables.
// Defaults to community="public" and version=v1 if not specified.
func GetSNMPConfig() (*SNMPConfig, error) {
	community := os.Getenv("SNMP_COMMUNITY")
	if community == "" {
		community = "public"
	}

	versionStr := os.Getenv("SNMP_VERSION")
	version := gosnmp.Version1 // default

	if versionStr != "" {
		switch versionStr {
		case "1":
			version = gosnmp.Version1
		case "2c":
			version = gosnmp.Version2c
		case "3":
			version = gosnmp.Version3
		default:
			return nil, fmt.Errorf("unsupported SNMP version: %s", versionStr)
		}
	}

	return &SNMPConfig{
		Community: community,
		Version:   version,
	}, nil
}

// newSNMPClientImpl is the actual implementation of NewSNMPClient.
func newSNMPClientImpl(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("SNMP config required")
	}
	if target == "" {
		return nil, fmt.Errorf("target IP required")
	}

	timeout := timeoutSeconds
	if timeout == 0 {
		timeout = 30
	}

	conn := &gosnmp.GoSNMP{
		Target:    target,
		Port:      161,
		Community: cfg.Community,
		Version:   cfg.Version,
		Timeout:   time.Duration(timeout) * time.Second,
		Retries:   3,
	}

	client := &gosnmpClient{conn: conn}
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", target, err)
	}

	return client, nil
}

// NewSNMPClientFunc is the function used to create SNMP clients.
// It can be replaced with a mock for testing.
var NewSNMPClientFunc = newSNMPClientImpl

// NewSNMPClient creates a new SNMP client for the specified target.
// If timeoutSeconds is 0, defaults to 30 seconds.
func NewSNMPClient(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
	return NewSNMPClientFunc(cfg, target, timeoutSeconds)
}
