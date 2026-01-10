package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"time"
)

// httpRedirectListener wraps a net.Listener to detect plain HTTP requests
// on a TLS port and redirect them to HTTPS instead of showing a TLS error.
type httpRedirectListener struct {
	net.Listener
	httpsPort string
}

// newHTTPRedirectListener creates a listener that detects HTTP on HTTPS port
// and sends a redirect response instead of a TLS handshake error.
func newHTTPRedirectListener(inner net.Listener, httpsPort string) net.Listener {
	return &httpRedirectListener{
		Listener:  inner,
		httpsPort: httpsPort,
	}
}

// Accept waits for and returns the next connection to the listener.
// If the connection starts with plain HTTP, it sends a redirect and closes.
func (l *httpRedirectListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		// Wrap connection to peek at first byte
		peekedConn := &peekConn{Conn: conn, reader: bufio.NewReader(conn)}

		// Peek at first byte to determine protocol
		firstByte, err := peekedConn.reader.Peek(1)
		if err != nil {
			conn.Close()
			continue
		}

		// TLS ClientHello starts with 0x16 (handshake record type)
		// HTTP methods start with uppercase letters (G, P, H, D, O, C, T)
		if firstByte[0] == 0x16 {
			// TLS connection - return wrapped conn that replays peeked byte
			return peekedConn, nil
		}

		// Plain HTTP request on HTTPS port - send redirect
		go l.handleHTTPRedirect(peekedConn)
		// Continue accepting - don't return this connection
	}
}

// handleHTTPRedirect reads the HTTP request and sends a redirect to HTTPS
func (l *httpRedirectListener) handleHTTPRedirect(conn *peekConn) {
	defer conn.Close()

	// Set a reasonable timeout for reading the request
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Read the first line to get the request path
	line, err := conn.reader.ReadString('\n')
	if err != nil {
		return
	}

	// Parse minimal request info (e.g., "GET /path HTTP/1.1")
	var method, path string
	fmt.Sscanf(line, "%s %s", &method, &path)

	if path == "" {
		path = "/"
	}

	// Determine the host from Host header or use localhost
	host := fmt.Sprintf("localhost:%s", l.httpsPort)

	// Read headers to find Host
	for {
		headerLine, err := conn.reader.ReadString('\n')
		if err != nil || headerLine == "\r\n" || headerLine == "\n" {
			break
		}
		if len(headerLine) > 6 && (headerLine[:5] == "Host:" || headerLine[:5] == "host:") {
			host = headerLine[6 : len(headerLine)-2] // Strip "Host: " and "\r\n"
			// Ensure we use HTTPS port if host doesn't include port
			if !hasPort(host) {
				host = fmt.Sprintf("%s:%s", host, l.httpsPort)
			}
			break
		}
	}

	// Build redirect URL
	redirectURL := fmt.Sprintf("https://%s%s", host, path)

	// Build HTML body
	htmlBody := fmt.Sprintf(
		"<html><head><title>Redirecting</title></head><body>"+
			"<h1>Moved Permanently</h1>"+
			"<p>This server requires HTTPS. Redirecting to <a href=\"%s\">%s</a></p>"+
			"</body></html>",
		redirectURL, redirectURL)

	// Send HTTP 301 redirect response
	response := fmt.Sprintf(
		"HTTP/1.1 301 Moved Permanently\r\n"+
			"Location: %s\r\n"+
			"Content-Type: text/html; charset=utf-8\r\n"+
			"Content-Length: %d\r\n"+
			"Connection: close\r\n"+
			"\r\n"+
			"%s",
		redirectURL,
		len(htmlBody),
		htmlBody,
	)

	conn.Write([]byte(response))

	appLogger.Debug("Redirected HTTP request to HTTPS",
		"remote_addr", conn.RemoteAddr().String(),
		"path", path,
		"redirect_url", redirectURL)
}

// hasPort checks if a host string contains a port
func hasPort(host string) bool {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return true
		}
		if host[i] == ']' { // IPv6 address
			return false
		}
	}
	return false
}

// peekConn wraps a net.Conn with a buffered reader to allow peeking
type peekConn struct {
	net.Conn
	reader *bufio.Reader
}

// Read reads data from the connection, using buffered data first
func (c *peekConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// WriteTo implements io.WriterTo for efficient copying
func (c *peekConn) WriteTo(w io.Writer) (int64, error) {
	return c.reader.WriteTo(w)
}
