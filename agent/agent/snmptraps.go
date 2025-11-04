package agent

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/gosnmp/gosnmp"
)

// StartSNMPTrapListener listens for SNMP trap notifications on UDP port 162
// and enqueues discovered devices for SNMP enrichment. Runs until context is canceled.
//
// SNMP traps provide event-driven discovery when printers:
// - Power on or boot up
// - Change status (errors, warnings, ready)
// - Experience supply issues (toner low, paper jam, etc.)
//
// Note: Port 162 requires elevated privileges on most systems (admin/root)
func StartSNMPTrapListener(ctx context.Context, enqueue func(string) bool, port uint16) error {
	if port == 0 {
		port = 162 // Standard SNMP trap port
	}

	// Create trap listener
	tl := gosnmp.NewTrapListener()
	tl.OnNewTrap = func(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
		handleTrap(packet, addr, enqueue)
	}

	// Set listener parameters
	tl.Params = gosnmp.Default
	tl.Params.Version = gosnmp.Version2c // Support both v1 and v2c
	tl.Params.Community = "public"       // Most printers use "public" for traps

	listenAddr := fmt.Sprintf("0.0.0.0:%d", port)

	Info(fmt.Sprintf("SNMP Traps: listening on %s (requires admin/root privileges)", listenAddr))

	// Listen on specified port
	if err := tl.Listen(listenAddr); err != nil {
		return fmt.Errorf("failed to start trap listener: %w", err)
	}
	defer tl.Close()

	Info("SNMP Traps: listener started successfully")

	// Block until context is canceled
	<-ctx.Done()

	Info("SNMP Traps: stopping listener")

	return nil
}

// handleTrap processes incoming SNMP trap notifications
func handleTrap(packet *gosnmp.SnmpPacket, addr *net.UDPAddr, enqueue func(string) bool) {
	if addr == nil {
		return
	}

	ip := addr.IP.String()

	// Log trap reception
	trapType := "Generic"
	trapOID := ""

	// Extract trap information from PDUs
	for _, pdu := range packet.Variables {
		oidStr := pdu.Name

		// SNMPv2-MIB::snmpTrapOID (identifies the trap type)
		if oidStr == "1.3.6.1.6.3.1.1.4.1.0" {
			trapOID = fmt.Sprintf("%v", pdu.Value)

			// Common printer trap OIDs
			switch trapOID {
			case "1.3.6.1.2.1.43.18.2.0.1":
				trapType = "Printer Status Change"
			case "1.3.6.1.2.1.43.18.2.0.2":
				trapType = "Printer Warming Up"
			case "1.3.6.1.2.1.43.18.2.0.3":
				trapType = "Printer Supply Low"
			case "1.3.6.1.2.1.43.18.2.0.4":
				trapType = "Printer Cover Open"
			case "1.3.6.1.2.1.43.18.2.0.5":
				trapType = "Printer Configuration Change"
			default:
				trapType = "Printer Event"
			}
		}
	}

	Info(fmt.Sprintf("SNMP Trap: received %s from %s (OID: %s)", trapType, ip, trapOID))

	// Enqueue device IP for discovery
	if enqueue(ip) {
		Info(fmt.Sprintf("SNMP Trap: enqueued %s for discovery", ip))
	}
}

// StartSNMPTrapBrowser is a wrapper that handles the trap listener lifecycle
// with automatic restart on errors and throttling to prevent duplicate discoveries
func StartSNMPTrapBrowser(ctx context.Context, enqueue func(string) bool, seen map[string]time.Time, throttleWindow time.Duration) {
	port := uint16(162) // Standard SNMP trap port

	// Try to start trap listener
	// Note: This will fail if not running with elevated privileges
	for {
		select {
		case <-ctx.Done():
			Info("SNMP Trap Browser: stopped")
			return
		default:
		}

		// Wrap enqueue with throttling logic
		throttledEnqueue := func(ip string) bool {
			now := time.Now()

			// Check if we've seen this IP recently
			if lastSeen, exists := seen[ip]; exists {
				if now.Sub(lastSeen) < throttleWindow {
					return false // Skip, too soon
				}
			}

			// Update last seen time
			seen[ip] = now

			// Call original enqueue
			return enqueue(ip)
		}

		// Start trap listener (blocking)
		err := StartSNMPTrapListener(ctx, throttledEnqueue, port)

		if err != nil {
			Info("SNMP Trap Browser: " + err.Error())

			// Check if it's a permission error
			if netErr, ok := err.(*net.OpError); ok {
				if netErr.Op == "listen" {
					Info("SNMP Trap Browser: Port 162 requires administrator/root privileges")
					Info("SNMP Trap Browser: Run as admin or disable trap monitoring")
					return // Don't retry if it's a permission issue
				}
			}
		}

		// If context was canceled, exit immediately
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Otherwise, wait a bit before retrying
		Info("SNMP Trap Browser: restarting in 30 seconds...")
		time.Sleep(30 * time.Second)
	}
}
