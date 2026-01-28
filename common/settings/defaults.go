package settings

const SchemaVersion = "v2"

// DefaultSettings returns the canonical defaults used when no persisted settings exist.
func DefaultSettings() Settings {
	return Settings{
		Discovery: DiscoverySettings{
			// IP Scanning
			SubnetScan:        true,
			ManualRanges:      false,
			RangesText:        "",
			DetectedSubnet:    "",
			IPScanningEnabled: true,
			Concurrency:       50,

			// Probe Methods
			ARPEnabled:  true,
			ICMPEnabled: true,
			TCPEnabled:  true,
			SNMPEnabled: true,
			MDNSEnabled: false,

			// Automatic Discovery
			AutoDiscoverEnabled:         false,
			AutosaveDiscoveredDevices:   false,
			ShowDiscoverButtonAnyway:    false,
			ShowDiscoveredDevicesAnyway: false,

			// Passive Listeners
			PassiveDiscoveryEnabled:  true,
			AutoDiscoverLiveMDNS:     true,
			AutoDiscoverLiveWSD:      true,
			AutoDiscoverLiveSSDP:     false,
			AutoDiscoverLiveSNMPTrap: false,
			AutoDiscoverLiveLLMNR:    false,

			// Metrics Collection
			MetricsRescanEnabled:         false,
			MetricsRescanIntervalMinutes: 60,
			MetricsRescanIntervalSeconds: 0, // 0 means use minutes-based interval
		},
		SNMP: SNMPSettings{
			Version:       "2c",
			Community:     "",
			TimeoutMS:     2000,
			Retries:       1,
			SecurityLevel: "",
			Username:      "",
			AuthProtocol:  "",
			AuthPassword:  "",
			PrivProtocol:  "",
			PrivPassword:  "",
			ContextName:   "",
		},
		Features: FeaturesSettings{
			EpsonRemoteModeEnabled: false,
			CredentialsEnabled:     true,
			AssetIDRegex:           "",
		},
		Spooler: SpoolerSettings{
			Enabled:                true,
			PollIntervalSeconds:    30,
			IncludeNetworkPrinters: false,
			IncludeVirtualPrinters: false,
		},
		Logging: LoggingSettings{
			Level:          "info",
			DumpParseDebug: false,
		},
		Web: WebSettings{
			EnableHTTP:          true,
			EnableHTTPS:         true,
			HTTPPort:            "8080",
			HTTPSPort:           "8443",
			RedirectHTTPToHTTPS: false,
			CustomCertPath:      "",
			CustomKeyPath:       "",
		},
	}
}
