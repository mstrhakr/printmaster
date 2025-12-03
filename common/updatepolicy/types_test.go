package updatepolicy

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVersionPinStrategyConstants(t *testing.T) {
	t.Parallel()

	// Verify constants have expected values
	if VersionPinMajor != "major" {
		t.Errorf("VersionPinMajor = %q, want %q", VersionPinMajor, "major")
	}
	if VersionPinMinor != "minor" {
		t.Errorf("VersionPinMinor = %q, want %q", VersionPinMinor, "minor")
	}
	if VersionPinPatch != "patch" {
		t.Errorf("VersionPinPatch = %q, want %q", VersionPinPatch, "patch")
	}
}

func TestAgentOverrideModeConstants(t *testing.T) {
	t.Parallel()

	if AgentOverrideInherit != "inherit" {
		t.Errorf("AgentOverrideInherit = %q, want %q", AgentOverrideInherit, "inherit")
	}
	if AgentOverrideLocal != "local" {
		t.Errorf("AgentOverrideLocal = %q, want %q", AgentOverrideLocal, "local")
	}
	if AgentOverrideNever != "disabled" {
		t.Errorf("AgentOverrideNever = %q, want %q", AgentOverrideNever, "disabled")
	}
}

func TestPolicySourceConstants(t *testing.T) {
	t.Parallel()

	if PolicySourceFleet != "fleet" {
		t.Errorf("PolicySourceFleet = %q, want %q", PolicySourceFleet, "fleet")
	}
	if PolicySourceLocal != "local" {
		t.Errorf("PolicySourceLocal = %q, want %q", PolicySourceLocal, "local")
	}
	if PolicySourceFallback != "fallback" {
		t.Errorf("PolicySourceFallback = %q, want %q", PolicySourceFallback, "fallback")
	}
	if PolicySourceDisabled != "disabled" {
		t.Errorf("PolicySourceDisabled = %q, want %q", PolicySourceDisabled, "disabled")
	}
}

func TestPolicySpecJSONRoundTrip(t *testing.T) {
	t.Parallel()

	spec := PolicySpec{
		UpdateCheckDays:    7,
		VersionPinStrategy: VersionPinMinor,
		AllowMajorUpgrade:  false,
		TargetVersion:      "1.2.0",
		MaintenanceWindow: MaintenanceWindow{
			Enabled:    true,
			StartHour:  2,
			StartMin:   0,
			EndHour:    5,
			EndMin:     0,
			Timezone:   "America/New_York",
			DaysOfWeek: []int{0, 6}, // Sunday and Saturday
		},
		RolloutControl: RolloutControl{
			Staggered:         true,
			MaxConcurrent:     10,
			BatchSize:         5,
			DelayBetweenWaves: 300,
			JitterSeconds:     60,
			EmergencyAbort:    false,
		},
		CollectTelemetry: true,
	}

	// Marshal to JSON
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded PolicySpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify key fields
	if decoded.UpdateCheckDays != spec.UpdateCheckDays {
		t.Errorf("UpdateCheckDays = %d, want %d", decoded.UpdateCheckDays, spec.UpdateCheckDays)
	}
	if decoded.VersionPinStrategy != spec.VersionPinStrategy {
		t.Errorf("VersionPinStrategy = %q, want %q", decoded.VersionPinStrategy, spec.VersionPinStrategy)
	}
	if decoded.AllowMajorUpgrade != spec.AllowMajorUpgrade {
		t.Errorf("AllowMajorUpgrade = %v, want %v", decoded.AllowMajorUpgrade, spec.AllowMajorUpgrade)
	}
	if decoded.MaintenanceWindow.Enabled != spec.MaintenanceWindow.Enabled {
		t.Errorf("MaintenanceWindow.Enabled = %v, want %v", decoded.MaintenanceWindow.Enabled, spec.MaintenanceWindow.Enabled)
	}
	if decoded.RolloutControl.Staggered != spec.RolloutControl.Staggered {
		t.Errorf("RolloutControl.Staggered = %v, want %v", decoded.RolloutControl.Staggered, spec.RolloutControl.Staggered)
	}
}

func TestFleetUpdatePolicyJSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	policy := FleetUpdatePolicy{
		PolicySpec: PolicySpec{
			UpdateCheckDays:    14,
			VersionPinStrategy: VersionPinPatch,
			AllowMajorUpgrade:  true,
		},
		TenantID:  "tenant-123",
		UpdatedAt: now,
	}

	// Marshal to JSON
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded FleetUpdatePolicy
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.TenantID != policy.TenantID {
		t.Errorf("TenantID = %q, want %q", decoded.TenantID, policy.TenantID)
	}
	if decoded.UpdateCheckDays != policy.UpdateCheckDays {
		t.Errorf("UpdateCheckDays = %d, want %d", decoded.UpdateCheckDays, policy.UpdateCheckDays)
	}
}

func TestMaintenanceWindowDefaults(t *testing.T) {
	t.Parallel()

	window := MaintenanceWindow{}

	// Should have sensible zero values
	if window.Enabled != false {
		t.Error("Enabled should default to false")
	}
	if window.StartHour != 0 {
		t.Errorf("StartHour = %d, want 0", window.StartHour)
	}
	if len(window.DaysOfWeek) != 0 {
		t.Errorf("DaysOfWeek should default to empty, got %v", window.DaysOfWeek)
	}
}

func TestRolloutControlDefaults(t *testing.T) {
	t.Parallel()

	control := RolloutControl{}

	if control.Staggered != false {
		t.Error("Staggered should default to false")
	}
	if control.MaxConcurrent != 0 {
		t.Errorf("MaxConcurrent = %d, want 0", control.MaxConcurrent)
	}
	if control.EmergencyAbort != false {
		t.Error("EmergencyAbort should default to false")
	}
}
