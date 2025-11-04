package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"printmaster/agent/scanner"
	"strings"
	"time"
)

// LLMNR (Link-Local Multicast Name Resolution) constants
// RFC 4795 - Windows native alternative to mDNS
const (
	llmnrMulticastAddr = "224.0.0.252:5355"
)

// StartLLMNRBrowser listens for LLMNR queries and responses on multicast group
// 224.0.0.252:5355. It filters for printer-related hostnames and resolves them
// to IPs for discovery. Runs until context is canceled.
//
// LLMNR is a Windows protocol for hostname resolution in networks without DNS.
// Useful for discovering printers by hostname in Windows-only environments.
func StartLLMNRBrowser(ctx context.Context, enqueue func(scanner.ScanJob) bool, seen map[string]time.Time, throttleWindow time.Duration) {
	addr, err := net.ResolveUDPAddr("udp4", llmnrMulticastAddr)
	if err != nil {
		Info("LLMNR: failed to resolve multicast address: " + err.Error())
		return
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		Info("LLMNR: failed to join multicast group: " + err.Error())
		return
	}
	defer conn.Close()

	Info("LLMNR: listening on " + llmnrMulticastAddr)

	conn.SetReadBuffer(65536)

	buf := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			Info("LLMNR: stopping listener")
			return
		default:
			// Set read deadline to allow periodic context checks
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				Info("LLMNR: read error: " + err.Error())
				continue
			}

			// Parse LLMNR packet
			if n < 12 { // Minimum DNS header size
				continue
			}

			hostname, isResponse, hasIPv4 := parseLLMNRPacket(buf[:n])
			if hostname == "" {
				continue
			}

			// Filter for printer-related hostnames
			if !isPrinterHostname(hostname) {
				continue
			}

			msgType := "query"
			if isResponse {
				msgType = "response"
			}
			Info(fmt.Sprintf("LLMNR: %s for %s from %s", msgType, hostname, src.IP))

			// If it's a response with an IPv4 address, use the address from the packet
			// Otherwise, use the source IP
			var targetIP string
			if isResponse && hasIPv4 != "" {
				targetIP = hasIPv4
			} else {
				targetIP = src.IP.String()
			}

			// Check throttling
			now := time.Now()
			if lastSeen, exists := seen[targetIP]; exists {
				if now.Sub(lastSeen) < throttleWindow {
					continue // Skip, too soon
				}
			}

			// Update last seen time
			seen[targetIP] = now

			// Enqueue for discovery
			job := scanner.ScanJob{
				IP:     targetIP,
				Source: "llmnr",
				Meta:   &ScanMeta{DiscoveryMethods: []string{"llmnr"}, Hostname: hostname},
			}

			if enqueue(job) {
				Info(fmt.Sprintf("LLMNR: enqueued %s (%s)", targetIP, hostname))
			}
		}
	}
}

// parseLLMNRPacket extracts hostname and IP from LLMNR DNS packet
func parseLLMNRPacket(data []byte) (hostname string, isResponse bool, ipv4Addr string) {
	if len(data) < 12 {
		return "", false, ""
	}

	// Parse DNS header
	flags := binary.BigEndian.Uint16(data[2:4])
	isResponse = (flags & 0x8000) != 0 // QR bit
	qdCount := binary.BigEndian.Uint16(data[4:6])
	anCount := binary.BigEndian.Uint16(data[6:8])

	offset := 12

	// Parse question section (if present)
	if qdCount > 0 && offset < len(data) {
		name, newOffset := parseDNSName(data, offset)
		hostname = name
		offset = newOffset + 4 // Skip QTYPE and QCLASS
	}

	// Parse answer section for IPv4 addresses (A records)
	if isResponse && anCount > 0 {
		for i := 0; i < int(anCount) && offset < len(data); i++ {
			// Parse answer name (usually compressed pointer)
			_, newOffset := parseDNSName(data, offset)
			offset = newOffset

			if offset+10 > len(data) {
				break
			}

			rrType := binary.BigEndian.Uint16(data[offset : offset+2])
			// rrClass := binary.BigEndian.Uint16(data[offset+2 : offset+4])
			// ttl := binary.BigEndian.Uint32(data[offset+4 : offset+8])
			rdLength := binary.BigEndian.Uint16(data[offset+8 : offset+10])
			offset += 10

			// Check if it's an A record (IPv4)
			if rrType == 1 && rdLength == 4 && offset+4 <= len(data) {
				ipv4Addr = fmt.Sprintf("%d.%d.%d.%d",
					data[offset], data[offset+1], data[offset+2], data[offset+3])
			}

			offset += int(rdLength)
		}
	}

	return hostname, isResponse, ipv4Addr
}

// parseDNSName extracts a DNS name from a packet starting at offset
func parseDNSName(data []byte, offset int) (string, int) {
	var parts []string
	jumped := false
	jumpOffset := 0

	for offset < len(data) {
		length := int(data[offset])

		// Check for compression pointer (top 2 bits set)
		if length&0xC0 == 0xC0 {
			if offset+1 >= len(data) {
				break
			}
			// Pointer to another location in the packet
			pointer := int(binary.BigEndian.Uint16(data[offset:offset+2]) & 0x3FFF)
			if !jumped {
				jumpOffset = offset + 2
			}
			offset = pointer
			jumped = true
			continue
		}

		// End of name
		if length == 0 {
			offset++
			break
		}

		// Check bounds
		if offset+1+length > len(data) {
			break
		}

		// Extract label
		label := string(data[offset+1 : offset+1+length])
		parts = append(parts, label)
		offset += 1 + length
	}

	if jumped {
		offset = jumpOffset
	}

	return strings.Join(parts, "."), offset
}

// isPrinterHostname checks if a hostname likely belongs to a printer
func isPrinterHostname(hostname string) bool {
	hostname = strings.ToLower(hostname)

	// Common printer hostname patterns
	printerKeywords := []string{
		"print", "printer", "mfp", "copier", "scanner",
		"hp", "canon", "epson", "brother", "xerox", "ricoh",
		"laserjet", "deskjet", "officejet", "colorjet",
		"bizhub", "imagerunner", "workcentre",
	}

	for _, keyword := range printerKeywords {
		if strings.Contains(hostname, keyword) {
			return true
		}
	}

	// Match patterns like "PRN-", "PRINT-", "MFP-" prefixes
	if len(hostname) > 4 {
		prefix := hostname[:4]
		if prefix == "prn-" || prefix == "mfp-" {
			return true
		}
	}

	return false
}
