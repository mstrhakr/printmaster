package main

import (
	"testing"

	"printmaster/common/updatepolicy"
)

func TestEffectivePolicyPrefersFleetWhenAvailable(t *testing.T) {
	t.Parallel()

	cfg := defaultAutoUpdateConfig()
	cfg.LocalPolicy.UpdateCheckDays = 2

	fleet := &updatepolicy.FleetUpdatePolicy{
		PolicySpec: updatepolicy.PolicySpec{
			UpdateCheckDays:    5,
			VersionPinStrategy: updatepolicy.VersionPinMinor,
		},
	}

	eff, ok := cfg.EffectivePolicy(fleet)
	if !ok {
		t.Fatal("expected fleet policy to enable updates")
	}
	if eff.Source != updatepolicy.PolicySourceFleet {
		t.Fatalf("expected fleet source, got %s", eff.Source)
	}
	if eff.Policy.UpdateCheckDays != 5 {
		t.Fatalf("expected fleet cadence propagated, got %d", eff.Policy.UpdateCheckDays)
	}
}

func TestEffectivePolicyFallsBackToLocalWhenNoFleet(t *testing.T) {
	t.Parallel()

	cfg := defaultAutoUpdateConfig()
	cfg.LocalPolicy.UpdateCheckDays = 9

	eff, ok := cfg.EffectivePolicy(nil)
	if !ok {
		t.Fatal("expected fallback local policy to enable updates")
	}
	if eff.Source != updatepolicy.PolicySourceFallback {
		t.Fatalf("expected fallback source, got %s", eff.Source)
	}
	if eff.Policy.UpdateCheckDays != 9 {
		t.Fatalf("expected fallback cadence 9 days, got %d", eff.Policy.UpdateCheckDays)
	}
}

func TestEffectivePolicyRespectsLocalOverride(t *testing.T) {
	t.Parallel()

	cfg := defaultAutoUpdateConfig()
	cfg.Mode = updatepolicy.AgentOverrideLocal
	cfg.LocalPolicy.UpdateCheckDays = 4

	fleet := &updatepolicy.FleetUpdatePolicy{
		PolicySpec: updatepolicy.PolicySpec{UpdateCheckDays: 11},
	}

	eff, ok := cfg.EffectivePolicy(fleet)
	if !ok {
		t.Fatal("expected local override to enable updates")
	}
	if eff.Source != updatepolicy.PolicySourceLocal {
		t.Fatalf("expected local source, got %s", eff.Source)
	}
	if eff.Policy.UpdateCheckDays != 4 {
		t.Fatalf("expected local cadence 4 days, got %d", eff.Policy.UpdateCheckDays)
	}
}

func TestEffectivePolicyDisablesWhenFleetTurnsItOff(t *testing.T) {
	t.Parallel()

	cfg := defaultAutoUpdateConfig()
	cfg.LocalPolicy.UpdateCheckDays = 3
	fleet := &updatepolicy.FleetUpdatePolicy{
		PolicySpec: updatepolicy.PolicySpec{UpdateCheckDays: 0},
	}

	eff, ok := cfg.EffectivePolicy(fleet)
	if ok {
		t.Fatal("expected fleet-disabled policy to short-circuit updates")
	}
	if eff.Source != updatepolicy.PolicySourceFleet {
		t.Fatalf("expected fleet source even when disabled, got %s", eff.Source)
	}
}

func TestEffectivePolicyDisabledMode(t *testing.T) {
	t.Parallel()

	cfg := defaultAutoUpdateConfig()
	cfg.Mode = updatepolicy.AgentOverrideNever

	eff, ok := cfg.EffectivePolicy(nil)
	if ok {
		t.Fatal("expected disabled mode to return no policy")
	}
	if eff.Source != updatepolicy.PolicySourceDisabled {
		t.Fatalf("expected disabled source, got %s", eff.Source)
	}
}
