package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// TLSMode represents the TLS certificate mode
type TLSMode string

const (
	TLSModeSelfSigned  TLSMode = "self-signed"
	TLSModeCustom      TLSMode = "custom"
	TLSModeLetsEncrypt TLSMode = "letsencrypt"
)

// TLSConfig holds TLS/HTTPS configuration
type TLSConfig struct {
	// Mode: "self-signed", "custom", "letsencrypt"
	Mode TLSMode

	// Self-signed mode
	Domain string // Used in cert CN (default: "localhost")

	// Custom certificate mode
	CertPath string
	KeyPath  string

	// Let's Encrypt mode
	LetsEncryptDomain string
	LetsEncryptEmail  string
	LetsEncryptCache  string
	AcceptTOS         bool

	// Server ports
	HTTPPort  int
	HTTPSPort int

	// Reverse proxy mode
	BehindProxy   bool
	ProxyUseHTTPS bool   // Use HTTPS even when behind proxy (end-to-end encryption)
	BindAddress   string // Address to bind to (e.g., "0.0.0.0", "127.0.0.1")
}

// GetTLSConfig returns a configured *tls.Config based on the mode
func (cfg *TLSConfig) GetTLSConfig() (*tls.Config, error) {
	switch cfg.Mode {
	case TLSModeLetsEncrypt:
		return cfg.getLetsEncryptConfig()
	case TLSModeCustom:
		return cfg.getCustomCertConfig()
	case TLSModeSelfSigned:
		return cfg.getSelfSignedConfig()
	default:
		return nil, fmt.Errorf("invalid TLS mode: %s", cfg.Mode)
	}
}

// getLetsEncryptConfig creates TLS config with automatic Let's Encrypt certificates
func (cfg *TLSConfig) getLetsEncryptConfig() (*tls.Config, error) {
	if cfg.LetsEncryptDomain == "" {
		return nil, fmt.Errorf("domain required for Let's Encrypt")
	}
	if !cfg.AcceptTOS {
		return nil, fmt.Errorf("must accept Let's Encrypt Terms of Service (set accept_tos: true)")
	}

	// Default cache directory
	if cfg.LetsEncryptCache == "" {
		cfg.LetsEncryptCache = "letsencrypt-cache"
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cfg.LetsEncryptCache, 0700); err != nil {
		return nil, fmt.Errorf("failed to create Let's Encrypt cache directory: %w", err)
	}

	m := &autocert.Manager{
		Prompt:      autocert.AcceptTOS,
		Cache:       autocert.DirCache(cfg.LetsEncryptCache),
		HostPolicy:  autocert.HostWhitelist(cfg.LetsEncryptDomain),
		Email:       cfg.LetsEncryptEmail,
		RenewBefore: 30 * 24 * time.Hour, // Renew 30 days before expiry
	}

	return &tls.Config{
		GetCertificate: m.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
		MinVersion:     tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}, nil
}

// getCustomCertConfig loads custom certificate and key files
func (cfg *TLSConfig) getCustomCertConfig() (*tls.Config, error) {
	if cfg.CertPath == "" || cfg.KeyPath == "" {
		return nil, fmt.Errorf("cert_path and key_path required for custom mode")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load custom certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}, nil
}

// getSelfSignedConfig generates or loads a self-signed certificate
func (cfg *TLSConfig) getSelfSignedConfig() (*tls.Config, error) {
	certDir := "certs"
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create certs directory: %w", err)
	}

	certPath := filepath.Join(certDir, "server.crt")
	keyPath := filepath.Join(certDir, "server.key")

	// Check if certificates already exist
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			// Both files exist, try to load them
			logDebug("Loading existing self-signed certificate", "cert", certPath, "key", keyPath)
			cfg.CertPath = certPath
			cfg.KeyPath = keyPath
			return cfg.getCustomCertConfig()
		}
	}

	// Generate new self-signed certificate
	logInfo("Generating self-signed TLS certificate", "domain", cfg.Domain, "cert", certPath, "key", keyPath)

	if err := generateSelfSignedCert(certPath, keyPath, cfg.Domain); err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	logInfo("Self-signed certificate generated successfully", "cert", certPath, "key", keyPath)

	cfg.CertPath = certPath
	cfg.KeyPath = keyPath
	return cfg.getCustomCertConfig()
}

// generateSelfSignedCert creates a new self-signed certificate
func generateSelfSignedCert(certPath, keyPath, domain string) error {
	// Generate RSA private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(10 * 365 * 24 * time.Hour) // 10 years

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	if domain == "" {
		domain = "localhost"
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"PrintMaster"},
			CommonName:   "PrintMaster Server",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain, "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate file
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("failed to write cert: %w", err)
	}

	// Write private key file
	keyOut, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyOut.Close()

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	logInfo("Generated self-signed certificate", "cert", certPath, "key", keyPath, "domain", domain)

	return nil
}

// GetACMEHTTPHandler returns the HTTP handler for Let's Encrypt ACME challenges
func (cfg *TLSConfig) GetACMEHTTPHandler() (*autocert.Manager, error) {
	if cfg.Mode != TLSModeLetsEncrypt {
		return nil, fmt.Errorf("ACME handler only available in letsencrypt mode")
	}

	if cfg.LetsEncryptCache == "" {
		cfg.LetsEncryptCache = "letsencrypt-cache"
	}

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cfg.LetsEncryptCache),
		HostPolicy: autocert.HostWhitelist(cfg.LetsEncryptDomain),
		Email:      cfg.LetsEncryptEmail,
	}

	return m, nil
}

// httpRedirectListener wraps a net.Listener to detect plain HTTP requests
// on a TLS port and redirect them to HTTPS instead of showing a TLS error.
type httpRedirectListener struct {
	net.Listener
	httpsPort int
}

// newHTTPRedirectListener creates a listener that detects HTTP on HTTPS port
// and sends a redirect response instead of a TLS handshake error.
func newHTTPRedirectListener(inner net.Listener, httpsPort int) net.Listener {
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
	host := fmt.Sprintf("localhost:%d", l.httpsPort)

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
				host = fmt.Sprintf("%s:%d", host, l.httpsPort)
			}
			break
		}
	}

	// Build redirect URL
	redirectURL := fmt.Sprintf("https://%s%s", host, path)

	// Send HTTP 301 redirect response
	response := fmt.Sprintf(
		"HTTP/1.1 301 Moved Permanently\r\n"+
			"Location: %s\r\n"+
			"Content-Type: text/html; charset=utf-8\r\n"+
			"Content-Length: %d\r\n"+
			"Connection: close\r\n"+
			"\r\n"+
			"<html><head><title>Redirecting</title></head><body>"+
			"<h1>Moved Permanently</h1>"+
			"<p>This server requires HTTPS. Redirecting to <a href=\"%s\">%s</a></p>"+
			"</body></html>",
		redirectURL,
		len(fmt.Sprintf("<html><head><title>Redirecting</title></head><body><h1>Moved Permanently</h1><p>This server requires HTTPS. Redirecting to <a href=\"%s\">%s</a></p></body></html>", redirectURL, redirectURL)),
		redirectURL,
		redirectURL,
	)

	conn.Write([]byte(response))

	logDebug("Redirected HTTP request to HTTPS",
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
