package storage

import "printmaster/common/updatepolicy"

// Re-export the shared update policy types so existing storage interfaces can
// remain unchanged within the server codebase while the agent shares the same
// definitions via the common module.
type (
	PolicySpec         = updatepolicy.PolicySpec
	VersionPinStrategy = updatepolicy.VersionPinStrategy
	MaintenanceWindow  = updatepolicy.MaintenanceWindow
	RolloutControl     = updatepolicy.RolloutControl
	FleetUpdatePolicy  = updatepolicy.FleetUpdatePolicy
)

const GlobalFleetPolicyTenantID = "__global_auto_update__"
