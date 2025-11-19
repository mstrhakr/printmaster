package settings

import "testing"

func TestDefaultSettings(t *testing.T) {
	def := DefaultSettings()
	if !def.Discovery.SubnetScan {
		t.Fatal("expected subnet_scan default true")
	}
	if def.Developer.LogLevel != "info" {
		t.Fatalf("unexpected log level %s", def.Developer.LogLevel)
	}
	if !def.Security.CredentialsEnabled {
		t.Fatal("credentials enabled default expected true")
	}
}

func TestSanitize(t *testing.T) {
	s := DefaultSettings()
	s.Discovery.MetricsRescanIntervalMinutes = 999999
	s.Developer.SNMPTimeoutMS = -10
	s.Developer.SNMPRetries = -1
	s.Developer.DiscoverConcurrency = 0
	Sanitize(&s)
	if s.Discovery.MetricsRescanIntervalMinutes != 1440 {
		t.Fatalf("interval not clamped, got %d", s.Discovery.MetricsRescanIntervalMinutes)
	}
	if s.Developer.SNMPTimeoutMS != 500 {
		t.Fatalf("timeout not clamped, got %d", s.Developer.SNMPTimeoutMS)
	}
	if s.Developer.SNMPRetries != 0 {
		t.Fatalf("retries not clamped, got %d", s.Developer.SNMPRetries)
	}
	if s.Developer.DiscoverConcurrency != 1 {
		t.Fatalf("concurrency not clamped, got %d", s.Developer.DiscoverConcurrency)
	}
}

func TestDefaultSchemaIncludesCriticalFields(t *testing.T) {
	schema := DefaultSchema()
	if schema.Version == "" {
		t.Fatal("schema version empty")
	}
	checks := map[string]bool{
		"discovery.subnet_scan":                 false,
		"discovery.autosave_discovered_devices": false,
		"developer.show_legacy":                 false,
	}
	for _, field := range schema.Fields {
		if _, ok := checks[field.Path]; ok {
			checks[field.Path] = true
		}
	}
	for path, seen := range checks {
		if !seen {
			t.Fatalf("expected metadata entry for %s", path)
		}
	}
}
