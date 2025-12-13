package vendor

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// snmpHelper provides SNMP connection capabilities for the vendor package
// without importing the scanner package (which would create a cycle).

// NewVendorSNMPClient creates a simple SNMP client for vendor-specific queries.
// This is intentionally minimal - just enough for Epson remote-mode commands.
func NewVendorSNMPClient(ip string, community string, timeoutSeconds int) (SNMPClient, error) {
	if ip == "" {
		return nil, fmt.Errorf("empty IP address")
	}

	if community == "" {
		community = "public"
	}

	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}

	conn := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		Retries:   1,
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("SNMP connect failed: %w", err)
	}

	return &vendorSNMPClient{conn: conn}, nil
}

// vendorSNMPClient wraps gosnmp.GoSNMP to implement SNMPClient.
type vendorSNMPClient struct {
	conn *gosnmp.GoSNMP
}

func (c *vendorSNMPClient) Get(oids []string) (*gosnmp.SnmpPacket, error) {
	return c.conn.Get(oids)
}

func (c *vendorSNMPClient) Walk(rootOid string, walkFn gosnmp.WalkFunc) error {
	return c.conn.Walk(rootOid, walkFn)
}

func (c *vendorSNMPClient) Close() error {
	return c.conn.Conn.Close()
}
