package settings

// Scope defines where a setting is enforced.
type Scope string

const (
	// ScopeGlobal indicates the setting is controlled at the server level for all tenants/agents.
	ScopeGlobal Scope = "global"
	// ScopeTenant indicates the setting can be overridden per-tenant but inherits from the global default.
	ScopeTenant Scope = "tenant"
	// ScopeAgentLocal indicates the setting only applies to the local agent instance.
	ScopeAgentLocal Scope = "agent"
)

// EditableRole constrains which roles may edit a field when rendered in a UI or API.
type EditableRole string

const (
	RoleServerAdmin    EditableRole = "server-admin"
	RoleTenantAdmin    EditableRole = "tenant-admin"
	RoleTenantOperator EditableRole = "tenant-operator"
	RoleAgentLocal     EditableRole = "agent-local"
)

// Settings captures the canonical configuration surface for PrintMaster agents/tenants.
// Fleet-managed sections: Discovery, SNMP, Features, Spooler
// Agent-local sections: Logging, Web
type Settings struct {
	Discovery DiscoverySettings `json:"discovery" toml:"discovery"`
	SNMP      SNMPSettings      `json:"snmp" toml:"snmp"`
	Features  FeaturesSettings  `json:"features" toml:"features"`
	Spooler   SpoolerSettings   `json:"spooler" toml:"spooler"`
	Logging   LoggingSettings   `json:"logging" toml:"logging"`
	Web       WebSettings       `json:"web" toml:"web"`
}

// DiscoverySettings control how printers are discovered and rescanned (fleet-managed).
type DiscoverySettings struct {
	// IP Scanning
	SubnetScan        bool   `json:"subnet_scan"`
	ManualRanges      bool   `json:"manual_ranges"`
	RangesText        string `json:"ranges_text"`
	DetectedSubnet    string `json:"detected_subnet"`
	IPScanningEnabled bool   `json:"ip_scanning_enabled"`
	Concurrency       int    `json:"concurrency"`

	// Probe Methods
	ARPEnabled  bool `json:"arp_enabled"`
	ICMPEnabled bool `json:"icmp_enabled"`
	TCPEnabled  bool `json:"tcp_enabled"`
	SNMPEnabled bool `json:"snmp_enabled"`
	MDNSEnabled bool `json:"mdns_enabled"`

	// Automatic Discovery
	AutoDiscoverEnabled         bool `json:"auto_discover_enabled"`
	AutosaveDiscoveredDevices   bool `json:"autosave_discovered_devices"`
	ShowDiscoverButtonAnyway    bool `json:"show_discover_button_anyway"`
	ShowDiscoveredDevicesAnyway bool `json:"show_discovered_devices_anyway"`

	// Passive Listeners
	PassiveDiscoveryEnabled  bool `json:"passive_discovery_enabled"`
	AutoDiscoverLiveMDNS     bool `json:"auto_discover_live_mdns"`
	AutoDiscoverLiveWSD      bool `json:"auto_discover_live_wsd"`
	AutoDiscoverLiveSSDP     bool `json:"auto_discover_live_ssdp"`
	AutoDiscoverLiveSNMPTrap bool `json:"auto_discover_live_snmptrap"`
	AutoDiscoverLiveLLMNR    bool `json:"auto_discover_live_llmnr"`

	// Metrics Collection
	MetricsRescanEnabled         bool `json:"metrics_rescan_enabled"`
	MetricsRescanIntervalMinutes int  `json:"metrics_rescan_interval_minutes"`
}

// SNMPSettings configure SNMP queries (fleet-managed).
type SNMPSettings struct {
	Community string `json:"community"`
	TimeoutMS int    `json:"timeout_ms"`
	Retries   int    `json:"retries"`
}

// FeaturesSettings toggle optional features (fleet-managed).
type FeaturesSettings struct {
	EpsonRemoteModeEnabled bool   `json:"epson_remote_mode_enabled"`
	CredentialsEnabled     bool   `json:"credentials_enabled"`
	AssetIDRegex           string `json:"asset_id_regex"`
}

// LoggingSettings configure agent logging (agent-local).
type LoggingSettings struct {
	Level          string `json:"level"`
	DumpParseDebug bool   `json:"dump_parse_debug"`
}

// SpoolerSettings configure local printer tracking via OS print spooler (fleet-managed).
type SpoolerSettings struct {
	Enabled                bool `json:"enabled"`
	PollIntervalSeconds    int  `json:"poll_interval_seconds"`
	IncludeNetworkPrinters bool `json:"include_network_printers"`
	IncludeVirtualPrinters bool `json:"include_virtual_printers"`
}

// WebSettings configure the agent's embedded web server (agent-local).
type WebSettings struct {
	EnableHTTP          bool   `json:"enable_http"`
	EnableHTTPS         bool   `json:"enable_https"`
	HTTPPort            string `json:"http_port"`
	HTTPSPort           string `json:"https_port"`
	RedirectHTTPToHTTPS bool   `json:"redirect_http_to_https"`
	CustomCertPath      string `json:"custom_cert_path"`
	CustomKeyPath       string `json:"custom_key_path"`
}
