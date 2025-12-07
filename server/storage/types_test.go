package storage

import (
	"testing"
)

func TestNormalizeRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Role
	}{
		{"admin lowercase", "admin", RoleAdmin},
		{"admin uppercase", "ADMIN", RoleAdmin},
		{"admin mixed case", "Admin", RoleAdmin},
		{"admin with spaces", "  admin  ", RoleAdmin},
		{"operator lowercase", "operator", RoleOperator},
		{"operator uppercase", "OPERATOR", RoleOperator},
		{"viewer lowercase", "viewer", RoleViewer},
		{"viewer uppercase", "VIEWER", RoleViewer},
		{"legacy user maps to operator", "user", RoleOperator},
		{"legacy USER maps to operator", "USER", RoleOperator},
		{"unknown defaults to viewer", "unknown", RoleViewer},
		{"empty defaults to viewer", "", RoleViewer},
		{"random string defaults to viewer", "random", RoleViewer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRole(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeRole(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultRoles(t *testing.T) {
	t.Parallel()

	roles := DefaultRoles()
	if len(roles) != 3 {
		t.Errorf("DefaultRoles() length = %d, want 3", len(roles))
	}

	// Check order: admin, operator, viewer
	expected := []Role{RoleAdmin, RoleOperator, RoleViewer}
	for i, role := range roles {
		if role != expected[i] {
			t.Errorf("DefaultRoles()[%d] = %v, want %v", i, role, expected[i])
		}
	}
}

func TestSortTenantIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"nil input", nil, []string{}},
		{"empty input", []string{}, []string{}},
		{"single element", []string{"a"}, []string{"a"}},
		{"already sorted", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"reverse order", []string{"c", "b", "a"}, []string{"a", "b", "c"}},
		{"with duplicates", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"with empty strings", []string{"a", "", "b", ""}, []string{"a", "b"}},
		{"with whitespace", []string{"  a  ", "b ", " c"}, []string{"a", "b", "c"}},
		{"all empty", []string{"", "", ""}, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SortTenantIDs(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("SortTenantIDs(%v) length = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SortTenantIDs(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeTenantDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"simple domain", "example.com", "example.com"},
		{"uppercase domain", "EXAMPLE.COM", "example.com"},
		{"mixed case", "Example.Com", "example.com"},
		{"with http scheme", "http://example.com", "example.com"},
		{"with https scheme", "https://example.com", "example.com"},
		{"with port", "example.com:8080", "example.com"},
		{"with path", "example.com/path/to/page", "example.com"},
		{"with query", "example.com?query=value", "example.com"},
		{"full URL", "https://user@example.com:8080/path?query=value", "example.com"},
		{"email address", "user@example.com", "example.com"},
		{"email with subdomain", "user@mail.example.com", "mail.example.com"},
		{"with leading dot", ".example.com", "example.com"},
		{"with trailing dot", "example.com.", "example.com"},
		{"with leading/trailing dots", ".example.com.", "example.com"},
		{"at sign only", "@", ""},
		{"subdomain", "subdomain.example.com", "subdomain.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTenantDomain(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeTenantDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSiteFilterRule_MatchesDevice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rule     SiteFilterRule
		ip       string
		hostname string
		serial   string
		want     bool
	}{
		// Empty pattern
		{"empty pattern returns false", SiteFilterRule{Type: "ip_range", Pattern: ""}, "192.168.1.1", "", "", false},
		{"whitespace pattern returns false", SiteFilterRule{Type: "ip_range", Pattern: "   "}, "192.168.1.1", "", "", false},

		// IP range (CIDR)
		{"ip_range match", SiteFilterRule{Type: "ip_range", Pattern: "192.168.1.0/24"}, "192.168.1.100", "", "", true},
		{"ip_range no match", SiteFilterRule{Type: "ip_range", Pattern: "192.168.1.0/24"}, "192.168.2.100", "", "", false},
		{"ip_range invalid CIDR", SiteFilterRule{Type: "ip_range", Pattern: "invalid"}, "192.168.1.1", "", "", false},
		{"ip_range invalid device IP", SiteFilterRule{Type: "ip_range", Pattern: "192.168.1.0/24"}, "invalid", "", "", false},

		// IP prefix
		{"ip_prefix match", SiteFilterRule{Type: "ip_prefix", Pattern: "192.168.1."}, "192.168.1.100", "", "", true},
		{"ip_prefix no match", SiteFilterRule{Type: "ip_prefix", Pattern: "192.168.1."}, "192.168.2.100", "", "", false},
		{"ip_prefix exact", SiteFilterRule{Type: "ip_prefix", Pattern: "10."}, "10.0.0.1", "", "", true},

		// Hostname pattern
		{"hostname_pattern wildcard match", SiteFilterRule{Type: "hostname_pattern", Pattern: "printer-*"}, "", "printer-001", "", true},
		{"hostname_pattern wildcard no match", SiteFilterRule{Type: "hostname_pattern", Pattern: "printer-*"}, "", "scanner-001", "", false},
		{"hostname_pattern suffix", SiteFilterRule{Type: "hostname_pattern", Pattern: "*-floor2"}, "", "printer-floor2", "", true},
		{"hostname_pattern star only", SiteFilterRule{Type: "hostname_pattern", Pattern: "*"}, "", "anything", "", true},
		{"hostname_pattern case insensitive", SiteFilterRule{Type: "hostname_pattern", Pattern: "PRINTER-*"}, "", "printer-001", "", true},
		{"hostname_pattern question mark", SiteFilterRule{Type: "hostname_pattern", Pattern: "printer-00?"}, "", "printer-001", "", true},

		// Serial pattern
		{"serial_pattern match", SiteFilterRule{Type: "serial_pattern", Pattern: "HP*"}, "", "", "HP12345", true},
		{"serial_pattern no match", SiteFilterRule{Type: "serial_pattern", Pattern: "HP*"}, "", "", "CANON123", false},
		{"serial_pattern middle wildcard", SiteFilterRule{Type: "serial_pattern", Pattern: "HP*XL"}, "", "", "HP123XL", true},

		// Unknown type
		{"unknown type returns false", SiteFilterRule{Type: "unknown", Pattern: "test"}, "192.168.1.1", "test", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.MatchesDevice(tt.ip, tt.hostname, tt.serial)
			if got != tt.want {
				t.Errorf("MatchesDevice(%q, %q, %q) = %v, want %v", tt.ip, tt.hostname, tt.serial, got, tt.want)
			}
		})
	}
}

func TestMatchGlobPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{"star matches all", "*", "anything", true},
		{"prefix match", "test*", "testing", true},
		{"suffix match", "*test", "mytest", true},
		{"middle match", "test*end", "test-middle-end", true},
		{"no match", "test*", "other", false},
		{"case insensitive", "TEST*", "testing", true},
		{"question mark single char", "test?", "testX", true},
		{"question mark no match", "test?", "test", false},
		{"special chars escaped - dot", "test.file", "test.file", true},
		{"special chars escaped - plus", "test+file", "test+file", true},
		{"special chars escaped - parens", "test(1)", "test(1)", true},
		{"special chars escaped - brackets", "test[1]", "test[1]", true},
		{"empty pattern matches empty value", "", "", true},
		{"empty pattern no match non-empty", "", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchGlobPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("matchGlobPattern(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestSite_MatchesDevice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		site     Site
		ip       string
		hostname string
		serial   string
		want     bool
	}{
		{
			name:     "no rules matches all",
			site:     Site{FilterRules: nil},
			ip:       "192.168.1.1",
			hostname: "printer",
			serial:   "HP123",
			want:     true,
		},
		{
			name:     "empty rules matches all",
			site:     Site{FilterRules: []SiteFilterRule{}},
			ip:       "192.168.1.1",
			hostname: "printer",
			serial:   "HP123",
			want:     true,
		},
		{
			name: "single matching rule",
			site: Site{FilterRules: []SiteFilterRule{
				{Type: "ip_prefix", Pattern: "192.168.1."},
			}},
			ip:       "192.168.1.100",
			hostname: "printer",
			serial:   "HP123",
			want:     true,
		},
		{
			name: "single non-matching rule",
			site: Site{FilterRules: []SiteFilterRule{
				{Type: "ip_prefix", Pattern: "10.0.0."},
			}},
			ip:       "192.168.1.100",
			hostname: "printer",
			serial:   "HP123",
			want:     false,
		},
		{
			name: "multiple rules - first matches",
			site: Site{FilterRules: []SiteFilterRule{
				{Type: "ip_prefix", Pattern: "192.168.1."},
				{Type: "hostname_pattern", Pattern: "scanner-*"},
			}},
			ip:       "192.168.1.100",
			hostname: "printer",
			serial:   "HP123",
			want:     true,
		},
		{
			name: "multiple rules - second matches",
			site: Site{FilterRules: []SiteFilterRule{
				{Type: "ip_prefix", Pattern: "10.0.0."},
				{Type: "hostname_pattern", Pattern: "printer*"},
			}},
			ip:       "192.168.1.100",
			hostname: "printer01",
			serial:   "HP123",
			want:     true,
		},
		{
			name: "multiple rules - none match",
			site: Site{FilterRules: []SiteFilterRule{
				{Type: "ip_prefix", Pattern: "10.0.0."},
				{Type: "hostname_pattern", Pattern: "scanner-*"},
			}},
			ip:       "192.168.1.100",
			hostname: "printer",
			serial:   "HP123",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.site.MatchesDevice(tt.ip, tt.hostname, tt.serial)
			if got != tt.want {
				t.Errorf("Site.MatchesDevice(%q, %q, %q) = %v, want %v", tt.ip, tt.hostname, tt.serial, got, tt.want)
			}
		})
	}
}
