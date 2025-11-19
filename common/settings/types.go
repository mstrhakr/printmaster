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
type Settings struct {
	Discovery DiscoverySettings `json:"discovery" toml:"discovery"`
	Developer DeveloperSettings `json:"developer" toml:"developer"`
	Security  SecuritySettings  `json:"security" toml:"security"`
}

// DiscoverySettings control how printers are discovered and rescanned.
type DiscoverySettings struct {
	SubnetScan                   bool   `json:"subnet_scan"`
	ManualRanges                 bool   `json:"manual_ranges"`
	RangesText                   string `json:"ranges_text"`
	DetectedSubnet               string `json:"detected_subnet"`
	IPScanningEnabled            bool   `json:"ip_scanning_enabled"`
	ARPEnabled                   bool   `json:"arp_enabled"`
	ICMPEnabled                  bool   `json:"icmp_enabled"`
	TCPEnabled                   bool   `json:"tcp_enabled"`
	MDNSEnabled                  bool   `json:"mdns_enabled"`
	SNMPEnabled                  bool   `json:"snmp_enabled"`
	AutoDiscoverEnabled          bool   `json:"auto_discover_enabled"`
	AutosaveDiscoveredDevices    bool   `json:"autosave_discovered_devices"`
	ShowDiscoverButtonAnyway     bool   `json:"show_discover_button_anyway"`
	ShowDiscoveredDevicesAnyway  bool   `json:"show_discovered_devices_anyway"`
	PassiveDiscoveryEnabled      bool   `json:"passive_discovery_enabled"`
	AutoDiscoverLiveMDNS         bool   `json:"auto_discover_live_mdns"`
	AutoDiscoverLiveWSD          bool   `json:"auto_discover_live_wsd"`
	AutoDiscoverLiveSSDP         bool   `json:"auto_discover_live_ssdp"`
	AutoDiscoverLiveSNMPTrap     bool   `json:"auto_discover_live_snmptrap"`
	AutoDiscoverLiveLLMNR        bool   `json:"auto_discover_live_llmnr"`
	MetricsRescanEnabled         bool   `json:"metrics_rescan_enabled"`
	MetricsRescanIntervalMinutes int    `json:"metrics_rescan_interval_minutes"`
}

// DeveloperSettings include diagnostic and advanced tuning options intended for administrators.
type DeveloperSettings struct {
	AssetIDRegex        string `json:"asset_id_regex"`
	SNMPCommunity       string `json:"snmp_community"`
	LogLevel            string `json:"log_level"`
	DumpParseDebug      bool   `json:"dump_parse_debug"`
	ShowLegacy          bool   `json:"show_legacy"`
	SNMPTimeoutMS       int    `json:"snmp_timeout_ms"`
	SNMPRetries         int    `json:"snmp_retries"`
	DiscoverConcurrency int    `json:"discover_concurrency"`
}

// SecuritySettings describe listener and credential behaviors for the embedded agent UI/API.
type SecuritySettings struct {
	CredentialsEnabled  bool   `json:"credentials_enabled"`
	EnableHTTP          bool   `json:"enable_http"`
	EnableHTTPS         bool   `json:"enable_https"`
	HTTPPort            string `json:"http_port"`
	HTTPSPort           string `json:"https_port"`
	RedirectHTTPToHTTPS bool   `json:"redirect_http_to_https"`
	CustomCertPath      string `json:"custom_cert_path"`
	CustomKeyPath       string `json:"custom_key_path"`
}
