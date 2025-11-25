package main

import (
	"strings"

	"printmaster/common/updatepolicy"
)

// EffectiveAutoUpdatePolicy represents the concrete policy the agent should
// follow after reconciling fleet settings with local overrides.
type EffectiveAutoUpdatePolicy struct {
	Source updatepolicy.PolicySource
	Policy updatepolicy.PolicySpec
}

func defaultAutoUpdateConfig() AutoUpdateConfig {
	return AutoUpdateConfig{
		Mode:        updatepolicy.AgentOverrideInherit,
		LocalPolicy: defaultLocalPolicySpec(),
	}
}

func defaultLocalPolicySpec() updatepolicy.PolicySpec {
	return updatepolicy.PolicySpec{
		UpdateCheckDays:    7,
		VersionPinStrategy: updatepolicy.VersionPinMinor,
		AllowMajorUpgrade:  false,
		TargetVersion:      "",
		MaintenanceWindow: updatepolicy.MaintenanceWindow{
			Enabled: false,
		},
		RolloutControl: updatepolicy.RolloutControl{
			Staggered: true,
		},
		CollectTelemetry: true,
	}
}

// EffectivePolicy resolves which update policy should drive behavior based on
// the desired override mode and the currently known fleet policy snapshot. The
// boolean indicates if auto-update should proceed (false = disabled).
func (cfg AutoUpdateConfig) EffectivePolicy(fleet *updatepolicy.FleetUpdatePolicy) (EffectiveAutoUpdatePolicy, bool) {
	mode := normalizeAutoUpdateMode(string(cfg.Mode))

	switch mode {
	case updatepolicy.AgentOverrideNever:
		return EffectiveAutoUpdatePolicy{Source: updatepolicy.PolicySourceDisabled}, false
	case updatepolicy.AgentOverrideLocal:
		return policyFromSpec(cfg.LocalPolicy, updatepolicy.PolicySourceLocal)
	default:
		if fleet != nil {
			res, ok := policyFromSpec(fleet.PolicySpec, updatepolicy.PolicySourceFleet)
			return res, ok
		}
		return policyFromSpec(cfg.LocalPolicy, updatepolicy.PolicySourceFallback)
	}
}

func policyFromSpec(spec updatepolicy.PolicySpec, source updatepolicy.PolicySource) (EffectiveAutoUpdatePolicy, bool) {
	cloned := clonePolicySpec(spec)
	if cloned.UpdateCheckDays <= 0 {
		return EffectiveAutoUpdatePolicy{Source: source}, false
	}
	return EffectiveAutoUpdatePolicy{Source: source, Policy: cloned}, true
}

func clonePolicySpec(spec updatepolicy.PolicySpec) updatepolicy.PolicySpec {
	clone := spec
	if len(spec.MaintenanceWindow.DaysOfWeek) > 0 {
		clone.MaintenanceWindow.DaysOfWeek = append([]int(nil), spec.MaintenanceWindow.DaysOfWeek...)
	}
	return clone
}

func normalizeAutoUpdateMode(value string) updatepolicy.AgentOverrideMode {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "local":
		return updatepolicy.AgentOverrideLocal
	case "disabled", "never", "off":
		return updatepolicy.AgentOverrideNever
	case "inherit", "fleet", "auto", "":
		return updatepolicy.AgentOverrideInherit
	default:
		return updatepolicy.AgentOverrideInherit
	}
}
