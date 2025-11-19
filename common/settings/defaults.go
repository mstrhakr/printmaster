package settings

const SchemaVersion = "v1"

// DefaultSettings returns the canonical defaults used when no persisted settings exist.
func DefaultSettings() Settings {
	return Settings{
		Discovery: DiscoverySettings{
			SubnetScan:                   true,
			ManualRanges:                 false,
			RangesText:                   "",
			DetectedSubnet:               "",
			IPScanningEnabled:            true,
			ARPEnabled:                   true,
			ICMPEnabled:                  true,
			TCPEnabled:                   true,
			MDNSEnabled:                  false,
			SNMPEnabled:                  true,
			AutoDiscoverEnabled:          false,
			AutosaveDiscoveredDevices:    false,
			ShowDiscoverButtonAnyway:     false,
			ShowDiscoveredDevicesAnyway:  false,
			PassiveDiscoveryEnabled:      true,
			AutoDiscoverLiveMDNS:         true,
			AutoDiscoverLiveWSD:          true,
			AutoDiscoverLiveSSDP:         false,
			AutoDiscoverLiveSNMPTrap:     false,
			AutoDiscoverLiveLLMNR:        false,
			MetricsRescanEnabled:         false,
			MetricsRescanIntervalMinutes: 60,
		},
		Developer: DeveloperSettings{
			AssetIDRegex:        "",
			SNMPCommunity:       "",
			LogLevel:            "info",
			DumpParseDebug:      false,
			ShowLegacy:          false,
			SNMPTimeoutMS:       2000,
			SNMPRetries:         1,
			DiscoverConcurrency: 50,
		},
		Security: SecuritySettings{
			CredentialsEnabled:  true,
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
