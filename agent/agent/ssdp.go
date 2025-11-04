package agent

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// SSDP/UPnP constants
const (
	ssdpMulticastAddr = "239.255.255.250:1900"
	ssdpSearchTarget  = "upnp:rootdevice" // Could also use "ssdp:all" for broader discovery
)

// StartSSDPBrowser listens for SSDP NOTIFY messages (device announcements) and
// optionally sends M-SEARCH to discover existing devices. Invokes enqueue for
// each discovered IPv4 address. Runs until context is canceled.
func StartSSDPBrowser(ctx context.Context, enqueue func(string) bool) {
	addr, err := net.ResolveUDPAddr("udp4", ssdpMulticastAddr)
	if err != nil {
		Info("SSDP: failed to resolve multicast address: " + err.Error())
		return
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		Info("SSDP: failed to join multicast group: " + err.Error())
		return
	}
	defer conn.Close()

	Info("SSDP: listening on " + ssdpMulticastAddr)

	// Set read buffer size
	conn.SetReadBuffer(65536)

	// Send initial M-SEARCH to discover existing devices
	go func() {
		time.Sleep(500 * time.Millisecond)
		sendMSearch()
		// Repeat M-SEARCH periodically (every 5 minutes) to catch devices that weren't responding
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sendMSearch()
			}
		}
	}()

	buf := make([]byte, 8192)
	for {
		select {
		case <-ctx.Done():
			Info("SSDP: stopping listener")
			return
		default:
			// Set read deadline to allow periodic context checks
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				Info("SSDP: read error: " + err.Error())
				continue
			}

			message := string(buf[:n])

			// Parse SSDP message headers
			headers := parseSSDPHeaders(message)

			// Check if this is a NOTIFY (device announcement) or M-SEARCH response
			if strings.Contains(message, "NOTIFY * HTTP/1.1") {
				// NOTIFY message - device announcing presence
				nts := headers["nts"]
				switch nts {
				case "ssdp:alive":
					location := headers["location"]
					usn := headers["usn"]
					st := headers["st"]

					// Filter out non-printer device types
					if isNonPrinterDevice(st, usn) {
						// Silently ignore gateways, routers, media renderers, etc.
						continue
					}

					Debug(fmt.Sprintf("SSDP: NOTIFY alive from %s (ST: %s, USN: %s, Location: %s)",
						src.IP.String(), st, usn, location))

					// Enqueue the source IP
					if src.IP.To4() != nil {
						enqueue(src.IP.String())
					}

					// Also try to extract IP from Location header
					if location != "" {
						if ip := extractIPFromURL(location); ip != "" {
							enqueue(ip)
						}
					}
				case "ssdp:byebye":
					// Device leaving
					Debug(fmt.Sprintf("SSDP: NOTIFY byebye from %s", src.IP.String()))
				}
			} else if strings.Contains(message, "HTTP/1.1 200 OK") {
				// M-SEARCH response
				location := headers["location"]
				usn := headers["usn"]
				st := headers["st"]

				// Filter out non-printer device types
				if isNonPrinterDevice(st, usn) {
					// Silently ignore gateways, routers, media renderers, etc.
					continue
				}

				Info(fmt.Sprintf("SSDP: M-SEARCH response from %s (ST: %s, USN: %s, Location: %s)",
					src.IP.String(), st, usn, location))

				// Enqueue the source IP
				if src.IP.To4() != nil {
					enqueue(src.IP.String())
				}

				// Also try to extract IP from Location header
				if location != "" {
					if ip := extractIPFromURL(location); ip != "" {
						enqueue(ip)
					}
				}
			}
		}
	}
}

// sendMSearch sends an SSDP M-SEARCH message to discover existing devices
func sendMSearch() {
	searchMsg := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 3\r\n" +
		"ST: " + ssdpSearchTarget + "\r\n" +
		"\r\n"

	addr, err := net.ResolveUDPAddr("udp4", ssdpMulticastAddr)
	if err != nil {
		Info("SSDP M-SEARCH: failed to resolve address: " + err.Error())
		return
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		Info("SSDP M-SEARCH: failed to dial: " + err.Error())
		return
	}
	defer conn.Close()

	_, err = conn.Write([]byte(searchMsg))
	if err != nil {
		Info("SSDP M-SEARCH: failed to send: " + err.Error())
		return
	}

	Info("SSDP: M-SEARCH sent to discover existing devices")
}

// parseSSDPHeaders parses HTTP-style headers from SSDP message
func parseSSDPHeaders(message string) map[string]string {
	headers := make(map[string]string)
	lines := strings.Split(message, "\r\n")
	for _, line := range lines {
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.ToLower(strings.TrimSpace(line[:idx]))
			value := strings.TrimSpace(line[idx+1:])
			headers[key] = value
		}
	}
	return headers
}

// extractIPFromURL extracts IPv4 address from URL (e.g., http://192.168.1.100:8080/desc.xml)
func extractIPFromURL(url string) string {
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	if idx := strings.Index(url, ":"); idx > 0 {
		url = url[:idx]
	}
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}
	if ip := net.ParseIP(url); ip != nil && ip.To4() != nil {
		return ip.To4().String()
	}
	return ""
}

// isNonPrinterDevice checks if the device type is definitely not a printer
// Returns true for routers, gateways, media devices, etc.
func isNonPrinterDevice(st, usn string) bool {
	st = strings.ToLower(st)
	usn = strings.ToLower(usn)

	// Known non-printer device types
	nonPrinterTypes := []string{
		"internetgatewaydevice", // Routers/gateways
		"wanconnectiondevice",   // WAN devices
		"wandevice",             // WAN devices
		"mediarenderer",         // Media players (Chromecast, etc.)
		"mediaserver",           // Media servers
		"dial",                  // DIAL protocol (smart TVs)
		"upnp:rootdevice",       // Generic root device (too broad, but often routers)
	}

	// Check ST (Service Type) header
	for _, nonPrinter := range nonPrinterTypes {
		if strings.Contains(st, nonPrinter) {
			return true
		}
	}

	// Check USN (Unique Service Name)
	for _, nonPrinter := range nonPrinterTypes {
		if strings.Contains(usn, nonPrinter) {
			return true
		}
	}

	// Additional check for specific service types that are never printers
	if strings.Contains(st, "wanipconnection") ||
		strings.Contains(st, "wanpppconnection") ||
		strings.Contains(st, "layer3forwarding") ||
		strings.Contains(st, "wancommoninterface") ||
		strings.Contains(st, "wanethernetlink") {
		return true
	}

	return false
}
