//go:build linux
// +build linux

package agent

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// getPlatformDetailed returns detailed Linux distribution info.
// Examples: "Ubuntu 24.04 LTS", "Debian 12", "Fedora 41", "RHEL 9.4"
func getPlatformDetailed() string {
	// Try /etc/os-release first (most modern distros)
	if info := parseOSRelease(); info != "" {
		return info
	}

	// Fallback to /etc/lsb-release (older Ubuntu)
	if info := parseLSBRelease(); info != "" {
		return info
	}

	// Fallback to specific release files
	if info := parseReleaseFiles(); info != "" {
		return info
	}

	return "Linux"
}

// parseOSRelease parses /etc/os-release to get distro info
func parseOSRelease() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()

	data := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := strings.Trim(parts[1], `"'`)
		data[key] = value
	}

	// Try PRETTY_NAME first (e.g., "Ubuntu 24.04.1 LTS")
	if pretty := data["PRETTY_NAME"]; pretty != "" {
		return normalizeLinuxName(pretty)
	}

	// Fall back to NAME + VERSION_ID
	name := data["NAME"]
	versionID := data["VERSION_ID"]
	if name != "" {
		if versionID != "" {
			return normalizeLinuxName(name + " " + versionID)
		}
		return normalizeLinuxName(name)
	}

	return ""
}

// parseLSBRelease parses /etc/lsb-release (older Ubuntu systems)
func parseLSBRelease() string {
	f, err := os.Open("/etc/lsb-release")
	if err != nil {
		return ""
	}
	defer f.Close()

	data := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		data[parts[0]] = strings.Trim(parts[1], `"'`)
	}

	if desc := data["DISTRIB_DESCRIPTION"]; desc != "" {
		return normalizeLinuxName(desc)
	}

	distrib := data["DISTRIB_ID"]
	release := data["DISTRIB_RELEASE"]
	if distrib != "" {
		if release != "" {
			return normalizeLinuxName(distrib + " " + release)
		}
		return normalizeLinuxName(distrib)
	}

	return ""
}

// parseReleaseFiles tries distro-specific release files
func parseReleaseFiles() string {
	releaseFiles := []struct {
		path   string
		prefix string
	}{
		{"/etc/redhat-release", ""},
		{"/etc/centos-release", ""},
		{"/etc/fedora-release", ""},
		{"/etc/debian_version", "Debian "},
		{"/etc/alpine-release", "Alpine Linux "},
		{"/etc/arch-release", "Arch Linux"},
		{"/etc/gentoo-release", ""},
		{"/etc/slackware-version", ""},
	}

	for _, rf := range releaseFiles {
		if content, err := os.ReadFile(rf.path); err == nil {
			text := strings.TrimSpace(string(content))
			if text != "" {
				if rf.prefix != "" && !strings.HasPrefix(text, rf.prefix) {
					text = rf.prefix + text
				}
				return normalizeLinuxName(text)
			}
		}
	}

	return ""
}

// normalizeLinuxName cleans up Linux distribution names
func normalizeLinuxName(name string) string {
	// Remove "GNU/Linux" suffix
	name = strings.TrimSuffix(name, " GNU/Linux")
	name = strings.ReplaceAll(name, "GNU/Linux", "")

	// Normalize common distro names
	lowered := strings.ToLower(name)

	// Common fixes
	if strings.Contains(lowered, "red hat enterprise") {
		// Shorten "Red Hat Enterprise Linux X.Y" to "RHEL X.Y"
		re := regexp.MustCompile(`(?i)red\s*hat\s*enterprise\s*linux\s*`)
		name = re.ReplaceAllString(name, "RHEL ")
	}

	if strings.Contains(lowered, "centos stream") {
		// Keep CentOS Stream as-is
	} else if strings.Contains(lowered, "centos linux") {
		name = strings.ReplaceAll(name, "CentOS Linux", "CentOS")
	}

	// Clean up extra whitespace
	name = strings.Join(strings.Fields(name), " ")

	return strings.TrimSpace(name)
}
