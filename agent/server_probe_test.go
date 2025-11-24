package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
)

func TestNormalizeServerURL(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantErr  bool
	}{
		{"https://example.com:9443", "example.com:9443", false},
		{"example.com", "example.com", false},
		{"http://example.com", "example.com", false},
		{"", "", true},
		{"ftp://example.com", "", true},
	}
	for _, tc := range cases {
		u, err := normalizeServerURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.in, err)
		}
		if u.Hostname() != tc.wantHost && u.Host != tc.wantHost {
			t.Fatalf("unexpected host for %q: got %s", tc.in, u.Host)
		}
	}
}

func TestClassifyTLSError(t *testing.T) {
	cases := []struct {
		err    error
		expect string
	}{
		{&x509.HostnameError{}, "hostname_mismatch"},
		{&x509.UnknownAuthorityError{}, "unknown_authority"},
		{&x509.CertificateInvalidError{Reason: x509.Expired}, "expired"},
		{&x509.CertificateInvalidError{}, "certificate_invalid"},
		{&tls.RecordHeaderError{}, "handshake_failed"},
		{errors.New("generic"), "handshake_failed"},
	}
	for _, tc := range cases {
		if got := classifyTLSError(tc.err); got != tc.expect {
			t.Fatalf("expected %s got %s", tc.expect, got)
		}
	}
}
