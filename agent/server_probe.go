package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ServerProbeResult captures the outcome of probing a server URL prior to joining it.
type ServerProbeResult struct {
	ServerURL string          `json:"server_url"`
	Scheme    string          `json:"scheme"`
	Reachable bool            `json:"reachable"`
	TLS       TLSProbeSummary `json:"tls"`
}

// TLSProbeSummary captures TLS verification details for a server probe.
type TLSProbeSummary struct {
	Enabled     bool                  `json:"enabled"`
	Valid       bool                  `json:"valid"`
	Error       string                `json:"error,omitempty"`
	ErrorCode   string                `json:"error_code,omitempty"`
	Certificate *TLSCertificateInfo   `json:"certificate,omitempty"`
	Chain       []*TLSCertificateInfo `json:"chain,omitempty"`
}

// TLSCertificateInfo describes a peer certificate returned by the server.
type TLSCertificateInfo struct {
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	DNSNames  []string  `json:"dns_names,omitempty"`
}

// probeServer attempts to reach the provided server URL and, when TLS is in use,
// collects certificate validity details. It prefers TLS connections even if the
// user omits the scheme (defaults to HTTPS).
func probeServer(ctx context.Context, rawURL string) (*ServerProbeResult, error) {
	normalized, err := normalizeServerURL(rawURL)
	if err != nil {
		return nil, err
	}

	result := &ServerProbeResult{
		ServerURL: normalized.String(),
		Scheme:    strings.ToLower(normalized.Scheme),
		TLS: TLSProbeSummary{
			Enabled: strings.EqualFold(normalized.Scheme, "https"),
		},
	}

	dialTimeout := 6 * time.Second
	hostPort := normalized.Host
	if _, _, err := net.SplitHostPort(hostPort); err != nil {
		port := defaultPortForScheme(normalized.Scheme)
		hostPort = net.JoinHostPort(normalized.Hostname(), port)
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}

	if !result.TLS.Enabled {
		conn, err := dialer.DialContext(ctx, "tcp", hostPort)
		if err != nil {
			result.TLS.Error = err.Error()
			result.TLS.ErrorCode = "unreachable"
			return result, nil
		}
		_ = conn.Close()
		result.Reachable = true
		return result, nil
	}

	serverName := normalized.Hostname()
	tlsCfg := &tls.Config{ServerName: serverName}
	conn, err := tls.DialWithDialer(dialer, "tcp", hostPort, tlsCfg)
	if err != nil {
		result.TLS.Error = err.Error()
		result.TLS.ErrorCode = classifyTLSError(err)
		return result, nil
	}
	defer conn.Close()

	state := conn.ConnectionState()
	result.Reachable = true
	result.TLS.Valid = true
	if len(state.PeerCertificates) > 0 {
		result.TLS.Certificate = convertCertificate(state.PeerCertificates[0])
		if len(state.PeerCertificates) > 1 {
			chain := make([]*TLSCertificateInfo, 0, len(state.PeerCertificates)-1)
			for _, cert := range state.PeerCertificates[1:] {
				chain = append(chain, convertCertificate(cert))
			}
			result.TLS.Chain = chain
		}
	}

	return result, nil
}

func convertCertificate(cert *x509.Certificate) *TLSCertificateInfo {
	if cert == nil {
		return nil
	}
	return &TLSCertificateInfo{
		Subject:   cert.Subject.String(),
		Issuer:    cert.Issuer.String(),
		NotBefore: cert.NotBefore,
		NotAfter:  cert.NotAfter,
		DNSNames:  append([]string(nil), cert.DNSNames...),
	}
}

func normalizeServerURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("server_url is required")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid server_url: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Host == "" {
		return nil, fmt.Errorf("server_url missing host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
	default:
		return nil, fmt.Errorf("unsupported scheme %s", u.Scheme)
	}
	return u, nil
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(scheme) {
	case "http":
		return "80"
	default:
		return "443"
	}
}

func classifyTLSError(err error) string {
	if err == nil {
		return ""
	}
	var hostnameErr *x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return "hostname_mismatch"
	}
	var unknownAuth *x509.UnknownAuthorityError
	if errors.As(err, &unknownAuth) {
		return "unknown_authority"
	}
	var certInvalid *x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		if certInvalid.Reason == x509.Expired {
			return "expired"
		}
		return "certificate_invalid"
	}
	var tlsErr *tls.RecordHeaderError
	if errors.As(err, &tlsErr) {
		return "handshake_failed"
	}
	return "handshake_failed"
}
