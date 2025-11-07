package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
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
			if serverLogger != nil {
				serverLogger.Debug("Loading existing self-signed certificate", "cert", certPath, "key", keyPath)
			}
			cfg.CertPath = certPath
			cfg.KeyPath = keyPath
			return cfg.getCustomCertConfig()
		}
	}

	// Generate new self-signed certificate
	if serverLogger != nil {
		serverLogger.Info("Generating self-signed TLS certificate", "domain", cfg.Domain, "cert", certPath, "key", keyPath)
	}

	if err := generateSelfSignedCert(certPath, keyPath, cfg.Domain); err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	if serverLogger != nil {
		serverLogger.Info("Self-signed certificate generated successfully", "cert", certPath, "key", keyPath)
	}

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

	if serverLogger != nil {
		serverLogger.Info("Generated self-signed certificate", "cert", certPath, "key", keyPath, "domain", domain)
	}

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
