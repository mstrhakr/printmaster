package autoupdate

import (
	"sync"

	"printmaster/common/updatepolicy"
)

// AutoUpdateConfigProvider is an interface for getting the current auto-update config.
// This decouples the autoupdate package from the main package's AgentConfig.
type AutoUpdateConfigProvider interface {
	// GetAutoUpdateMode returns the current override mode.
	GetAutoUpdateMode() updatepolicy.AgentOverrideMode
	// GetLocalPolicy returns the local policy spec.
	GetLocalPolicy() updatepolicy.PolicySpec
}

// FleetPolicyProvider is an interface for getting the current fleet policy.
type FleetPolicyProvider interface {
	// GetFleetPolicy returns the current fleet policy if available.
	GetFleetPolicy() *updatepolicy.FleetUpdatePolicy
}

// PolicyAdapter bridges auto-update config to the PolicyProvider interface.
type PolicyAdapter struct {
	mu             sync.RWMutex
	configProvider AutoUpdateConfigProvider
	fleetProvider  FleetPolicyProvider
}

// NewPolicyAdapter creates a new PolicyAdapter.
func NewPolicyAdapter(configProvider AutoUpdateConfigProvider, fleetProvider FleetPolicyProvider) *PolicyAdapter {
	return &PolicyAdapter{
		configProvider: configProvider,
		fleetProvider:  fleetProvider,
	}
}

// EffectivePolicy implements PolicyProvider.
func (a *PolicyAdapter) EffectivePolicy() (updatepolicy.PolicySpec, updatepolicy.PolicySource, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.configProvider == nil {
		return updatepolicy.PolicySpec{}, updatepolicy.PolicySourceDisabled, false
	}

	mode := a.configProvider.GetAutoUpdateMode()

	switch mode {
	case updatepolicy.AgentOverrideNever:
		return updatepolicy.PolicySpec{}, updatepolicy.PolicySourceDisabled, false

	case updatepolicy.AgentOverrideLocal:
		spec := a.configProvider.GetLocalPolicy()
		if spec.UpdateCheckDays <= 0 {
			return spec, updatepolicy.PolicySourceLocal, false
		}
		return spec, updatepolicy.PolicySourceLocal, true

	default: // AgentOverrideInherit
		var fleet *updatepolicy.FleetUpdatePolicy
		if a.fleetProvider != nil {
			fleet = a.fleetProvider.GetFleetPolicy()
		}

		if fleet != nil {
			if fleet.PolicySpec.UpdateCheckDays <= 0 {
				return fleet.PolicySpec, updatepolicy.PolicySourceFleet, false
			}
			return fleet.PolicySpec, updatepolicy.PolicySourceFleet, true
		}

		// Fallback to local policy
		spec := a.configProvider.GetLocalPolicy()
		if spec.UpdateCheckDays <= 0 {
			return spec, updatepolicy.PolicySourceFallback, false
		}
		return spec, updatepolicy.PolicySourceFallback, true
	}
}
