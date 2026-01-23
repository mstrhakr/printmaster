//go:build darwin
// +build darwin

package agent

import (
	"bytes"
	"os/exec"
	"strings"
)

// getPlatformDetailed returns detailed macOS version info.
// Examples: "macOS 15.2 Sequoia", "macOS 14.7 Sonoma", "macOS 13.6 Ventura"
func getPlatformDetailed() string {
	// Get version using sw_vers
	versionCmd := exec.Command("sw_vers", "-productVersion")
	versionOut, err := versionCmd.Output()
	if err != nil {
		return "macOS"
	}
	version := strings.TrimSpace(string(versionOut))
	if version == "" {
		return "macOS"
	}

	// Get codename from version
	codename := getMacOSCodename(version)
	if codename != "" {
		return "macOS " + version + " " + codename
	}

	return "macOS " + version
}

// getMacOSCodename returns the marketing name for a macOS version
func getMacOSCodename(version string) string {
	// Extract major version
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return ""
	}

	// macOS version codenames (major version numbers)
	codenames := map[string]string{
		"15": "Sequoia",
		"14": "Sonoma",
		"13": "Ventura",
		"12": "Monterey",
		"11": "Big Sur",
		"10": "", // 10.x versions have their own names
	}

	if name, ok := codenames[parts[0]]; ok {
		if name != "" {
			return name
		}
		// Handle 10.x versions
		if parts[0] == "10" && len(parts) >= 2 {
			return getMacOS10Codename(parts[1])
		}
	}

	return ""
}

// getMacOS10Codename returns codenames for macOS 10.x versions
func getMacOS10Codename(minor string) string {
	codenames := map[string]string{
		"15": "Catalina",
		"14": "Mojave",
		"13": "High Sierra",
		"12": "Sierra",
		"11": "El Capitan",
		"10": "Yosemite",
		"9":  "Mavericks",
		"8":  "Mountain Lion",
		"7":  "Lion",
		"6":  "Snow Leopard",
	}
	return codenames[minor]
}

// GetMacOSBuildNumber returns the macOS build number for diagnostics
func GetMacOSBuildNumber() string {
	cmd := exec.Command("sw_vers", "-buildVersion")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes.TrimSpace(out)))
}
