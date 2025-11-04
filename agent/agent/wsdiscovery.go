package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"strings"
	"time"
)

// WS-Discovery constants
const (
	wsDiscoveryMulticastAddr = "239.255.255.250:3702"
)

// WS-Discovery SOAP envelope structures
type wsDiscoveryEnvelope struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  wsDiscoveryHeader
	Body    wsDiscoveryBody
}

type wsDiscoveryHeader struct {
	Action    string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Action"`
	MessageID string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing MessageID"`
	To        string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing To"`
}

type wsDiscoveryBody struct {
	Hello      *wsHello      `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery Hello,omitempty"`
	Bye        *wsBye        `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery Bye,omitempty"`
	ProbeMatch *wsProbeMatch `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery ProbeMatch,omitempty"`
}

type wsHello struct {
	EndpointReference wsEndpointReference
	Types             string `xml:"Types"`
	Scopes            string `xml:"Scopes,omitempty"`
	XAddrs            string `xml:"XAddrs"`
	MetadataVersion   int    `xml:"MetadataVersion"`
}

type wsBye struct {
	EndpointReference wsEndpointReference
}

type wsProbeMatch struct {
	EndpointReference wsEndpointReference
	Types             string `xml:"Types"`
	Scopes            string `xml:"Scopes,omitempty"`
	XAddrs            string `xml:"XAddrs"`
	MetadataVersion   int    `xml:"MetadataVersion"`
}

type wsEndpointReference struct {
	Address string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Address"`
}

// StartWSDiscoveryBrowser listens for WS-Discovery Hello/Bye messages and ProbeMatches
// on the multicast group 239.255.255.250:3702. It invokes enqueue for each discovered
// IPv4 address. Runs until context is canceled.
func StartWSDiscoveryBrowser(ctx context.Context, enqueue func(string) bool) {
	addr, err := net.ResolveUDPAddr("udp4", wsDiscoveryMulticastAddr)
	if err != nil {
		Info("WS-Discovery: failed to resolve multicast address: " + err.Error())
		return
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		Info("WS-Discovery: failed to join multicast group: " + err.Error())
		return
	}
	defer conn.Close()

	Info("WS-Discovery: listening on " + wsDiscoveryMulticastAddr)

	// Set read buffer size
	conn.SetReadBuffer(65536)

	// Send initial Probe to discover existing devices
	go func() {
		time.Sleep(500 * time.Millisecond) // Brief delay to ensure listener is ready
		sendProbe()
	}()

	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			Info("WS-Discovery: stopping listener")
			return
		default:
			// Set read deadline to allow periodic context checks
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Read timeout, check context and retry
				}
				Info("WS-Discovery: read error: " + err.Error())
				continue
			}

			// Parse WS-Discovery message
			var envelope wsDiscoveryEnvelope
			if err := xml.Unmarshal(buf[:n], &envelope); err != nil {
				// Not all UDP traffic is WS-Discovery, ignore parse errors
				continue
			}

			// Process Hello messages (device announcements)
			if envelope.Body.Hello != nil {
				hello := envelope.Body.Hello
				Info(fmt.Sprintf("WS-Discovery: Hello from %s (Types: %s, XAddrs: %s)",
					hello.EndpointReference.Address, hello.Types, hello.XAddrs))

				// Extract IP addresses from XAddrs (can be multiple URLs)
				ips := extractIPsFromXAddrs(hello.XAddrs)
				for _, ip := range ips {
					enqueue(ip)
				}
			}

			// Process ProbeMatch messages (responses to Probe)
			if envelope.Body.ProbeMatch != nil {
				match := envelope.Body.ProbeMatch
				Info(fmt.Sprintf("WS-Discovery: ProbeMatch from %s (Types: %s, XAddrs: %s)",
					match.EndpointReference.Address, match.Types, match.XAddrs))

				ips := extractIPsFromXAddrs(match.XAddrs)
				for _, ip := range ips {
					enqueue(ip)
				}
			}

			// Process Bye messages (device leaving)
			if envelope.Body.Bye != nil {
				bye := envelope.Body.Bye
				Info(fmt.Sprintf("WS-Discovery: Bye from %s", bye.EndpointReference.Address))
				// Note: We don't remove devices on Bye, just log it
			}

			// Also try to extract IP from source address as fallback
			if src != nil && src.IP != nil {
				srcIP := src.IP.String()
				if srcIP != "" && !strings.Contains(srcIP, ":") { // IPv4 only
					// Only enqueue source IP if we haven't already from XAddrs
					// This is a backup in case XAddrs parsing fails
				}
			}
		}
	}
}

// sendProbe sends a WS-Discovery Probe message to discover existing devices
func sendProbe() {
	probeXML := `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:wsd="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <soap:Header>
    <wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</wsa:Action>
    <wsa:MessageID>urn:uuid:` + generateUUID() + `</wsa:MessageID>
    <wsa:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</wsa:To>
  </soap:Header>
  <soap:Body>
    <wsd:Probe>
      <wsd:Types>wsdp:Device</wsd:Types>
    </wsd:Probe>
  </soap:Body>
</soap:Envelope>`

	addr, err := net.ResolveUDPAddr("udp4", wsDiscoveryMulticastAddr)
	if err != nil {
		Info("WS-Discovery Probe: failed to resolve address: " + err.Error())
		return
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		Info("WS-Discovery Probe: failed to dial: " + err.Error())
		return
	}
	defer conn.Close()

	_, err = conn.Write([]byte(probeXML))
	if err != nil {
		Info("WS-Discovery Probe: failed to send: " + err.Error())
		return
	}

	Info("WS-Discovery: Probe sent to discover existing devices")
}

// extractIPsFromXAddrs parses XAddrs field (space-separated URLs) and extracts IPv4 addresses
func extractIPsFromXAddrs(xaddrs string) []string {
	var ips []string
	urls := strings.Fields(xaddrs)
	for _, url := range urls {
		// XAddrs typically contains URLs like http://192.168.1.100:5357/
		// Extract IP using simple parsing
		url = strings.TrimPrefix(url, "http://")
		url = strings.TrimPrefix(url, "https://")
		if idx := strings.Index(url, ":"); idx > 0 {
			url = url[:idx]
		}
		if idx := strings.Index(url, "/"); idx > 0 {
			url = url[:idx]
		}
		// Validate it's an IP
		if ip := net.ParseIP(url); ip != nil && ip.To4() != nil {
			ips = append(ips, ip.To4().String())
		}
	}
	return ips
}

// generateUUID creates a simple UUID for WS-Discovery messages
func generateUUID() string {
	// Simple UUID generation for message IDs
	return fmt.Sprintf("%d-%d-%d-%d-%d",
		time.Now().UnixNano()%100000,
		time.Now().UnixNano()%10000,
		time.Now().UnixNano()%1000,
		time.Now().UnixNano()%100,
		time.Now().UnixNano()%10)
}
