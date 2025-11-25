package updatepolicy

import "time"

// VersionPinStrategy defines the granularity of version pinning when
// calculating acceptable update targets.
type VersionPinStrategy string

const (
	VersionPinMajor VersionPinStrategy = "major"
	VersionPinMinor VersionPinStrategy = "minor"
	VersionPinPatch VersionPinStrategy = "patch"
)

// PolicySpec captures the knobs shared by both fleet and agent policies.
type PolicySpec struct {
	UpdateCheckDays    int                `json:"update_check_days" toml:"update_check_days"`
	VersionPinStrategy VersionPinStrategy `json:"version_pin_strategy" toml:"version_pin_strategy"`
	AllowMajorUpgrade  bool               `json:"allow_major_upgrade" toml:"allow_major_upgrade"`
	TargetVersion      string             `json:"target_version" toml:"target_version"`
	MaintenanceWindow  MaintenanceWindow  `json:"maintenance_window" toml:"maintenance_window"`
	RolloutControl     RolloutControl     `json:"rollout_control" toml:"rollout_control"`
	CollectTelemetry   bool               `json:"collect_telemetry" toml:"collect_telemetry"`
}

// MaintenanceWindow defines when updates can be applied.
type MaintenanceWindow struct {
	Enabled    bool   `json:"enabled" toml:"enabled"`
	StartHour  int    `json:"start_hour" toml:"start_hour"`
	StartMin   int    `json:"start_min" toml:"start_min"`
	EndHour    int    `json:"end_hour" toml:"end_hour"`
	EndMin     int    `json:"end_min" toml:"end_min"`
	Timezone   string `json:"timezone" toml:"timezone"`
	DaysOfWeek []int  `json:"days_of_week" toml:"days_of_week"`
}

// RolloutControl defines how updates are staged across the fleet.
type RolloutControl struct {
	Staggered         bool `json:"staggered" toml:"staggered"`
	MaxConcurrent     int  `json:"max_concurrent" toml:"max_concurrent"`
	BatchSize         int  `json:"batch_size" toml:"batch_size"`
	DelayBetweenWaves int  `json:"delay_between_waves" toml:"delay_between_waves"`
	JitterSeconds     int  `json:"jitter_seconds" toml:"jitter_seconds"`
	EmergencyAbort    bool `json:"emergency_abort" toml:"emergency_abort"`
}

// FleetUpdatePolicy defines tenant-wide auto-update configuration.
type FleetUpdatePolicy struct {
	PolicySpec
	TenantID  string    `json:"tenant_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentOverrideMode specifies how a standalone agent should handle
// auto-update state relative to the fleet policy.
type AgentOverrideMode string

const (
	AgentOverrideInherit AgentOverrideMode = "inherit"
	AgentOverrideLocal   AgentOverrideMode = "local"
	AgentOverrideNever   AgentOverrideMode = "disabled"
)

// PolicySource describes where the effective update policy originated.
type PolicySource string

const (
	PolicySourceFleet    PolicySource = "fleet"
	PolicySourceLocal    PolicySource = "local"
	PolicySourceFallback PolicySource = "fallback"
	PolicySourceDisabled PolicySource = "disabled"
)
