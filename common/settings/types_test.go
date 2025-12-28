package settings

import "testing"

func TestDefaultSettings(t *testing.T) {
	def := DefaultSettings()
	if !def.Discovery.SubnetScan {
		t.Fatal("expected subnet_scan default true")
	}
	if def.Logging.Level != "info" {
		t.Fatalf("unexpected log level %s", def.Logging.Level)
	}
	if !def.Features.CredentialsEnabled {
		t.Fatal("credentials enabled default expected true")
	}
}

func TestSanitize(t *testing.T) {
	s := DefaultSettings()
	s.Discovery.MetricsRescanIntervalMinutes = 999999
	s.Discovery.MetricsRescanIntervalSeconds = 5 // too low, should clamp to 15
	s.SNMP.TimeoutMS = -10
	s.SNMP.Retries = -1
	s.Discovery.Concurrency = 0
	Sanitize(&s)
	if s.Discovery.MetricsRescanIntervalMinutes != 1440 {
		t.Fatalf("interval not clamped, got %d", s.Discovery.MetricsRescanIntervalMinutes)
	}
	if s.Discovery.MetricsRescanIntervalSeconds != 15 {
		t.Fatalf("seconds interval not clamped to min 15, got %d", s.Discovery.MetricsRescanIntervalSeconds)
	}
	if s.SNMP.TimeoutMS != 500 {
		t.Fatalf("timeout not clamped, got %d", s.SNMP.TimeoutMS)
	}
	if s.SNMP.Retries != 0 {
		t.Fatalf("retries not clamped, got %d", s.SNMP.Retries)
	}
	if s.Discovery.Concurrency != 1 {
		t.Fatalf("concurrency not clamped, got %d", s.Discovery.Concurrency)
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
		"logging.level":                         false,
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
