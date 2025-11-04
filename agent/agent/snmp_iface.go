package agent

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// SNMPClient abstracts gosnmp for easier testing/mocking.
type SNMPClient interface {
	Connect() error
	Get(oids []string) (*gosnmp.SnmpPacket, error)
	Walk(root string, walkFn gosnmp.WalkFunc) error
	Close() error
}

// NewSNMPClient is a factory used by production code; tests can replace this
// variable to inject mock clients.
var NewSNMPClient = func(cfg *SNMPConfig, target string, timeoutSeconds int) (SNMPClient, error) {
	// Ensure a minimum SNMP timeout to be tolerant of slow devices/networks.
	tsec := timeoutSeconds
	if tsec < 30 {
		tsec = 30
	}
	snmp := &gosnmp.GoSNMP{
		Target:    target,
		Port:      161,
		Community: cfg.Community,
		Version:   cfg.Version,
		Timeout:   time.Duration(tsec) * time.Second,
		// increase retries to be more tolerant on lossy networks
		Retries: 3,
	}
	if err := snmp.Connect(); err != nil {
		return nil, err
	}
	return &gosnmpWrapper{snmp: snmp}, nil
}

// gosnmpWrapper implements SNMPClient by delegating to gosnmp.GoSNMP.
type gosnmpWrapper struct {
	snmp *gosnmp.GoSNMP
}

func (w *gosnmpWrapper) Connect() error {
	if w.snmp == nil {
		return fmt.Errorf("nil gosnmp client")
	}
	return nil
}

func (w *gosnmpWrapper) Get(oids []string) (*gosnmp.SnmpPacket, error) {
	return w.snmp.Get(oids)
}

func (w *gosnmpWrapper) Walk(root string, walkFn gosnmp.WalkFunc) error {
	return w.snmp.Walk(root, walkFn)
}

func (w *gosnmpWrapper) Close() error {
	if w.snmp != nil && w.snmp.Conn != nil {
		_ = w.snmp.Conn.Close()
	}
	return nil
}

// PingFunc allows tests to override ping behavior.
type PingFunc func(ip string, logFn func(string)) bool

// DoPing is the package-level ping function used by the scanner. Tests may
// replace this with a fake implementation that returns deterministic results.
var DoPing PingFunc = pingWithExec
