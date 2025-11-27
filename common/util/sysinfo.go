package util

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// SystemInfo contains detailed system information
type SystemInfo struct {
	OS        string
	OSVersion string
	Arch      string
	CPUModel  string
	Hostname  string
	NumCPU    int
}

// GetSystemInfo returns detailed system information
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		NumCPU: runtime.NumCPU(),
	}

	info.Hostname, _ = os.Hostname()
	info.OSVersion = getOSVersion()
	info.CPUModel = getCPUModel()

	return info
}

// getOSVersion returns the OS version string
func getOSVersion() string {
	switch runtime.GOOS {
	case "windows":
		return getWindowsVersion()
	case "linux":
		return getLinuxVersion()
	case "darwin":
		return getMacOSVersion()
	default:
		return "Unknown"
	}
}

// getWindowsVersion returns Windows version
func getWindowsVersion() string {
	cmd := exec.Command("cmd", "/c", "ver")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try wmic
		cmd = exec.Command("wmic", "os", "get", "Caption,Version", "/value")
		output, err = cmd.Output()
		if err != nil {
			return "Windows"
		}

		lines := strings.Split(string(output), "\n")
		caption := ""
		version := ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Caption=") {
				caption = strings.TrimPrefix(line, "Caption=")
			}
			if strings.HasPrefix(line, "Version=") {
				version = strings.TrimPrefix(line, "Version=")
			}
		}

		if caption != "" {
			return strings.TrimSpace(caption)
		}
		if version != "" {
			return "Windows " + strings.TrimSpace(version)
		}
		return "Windows"
	}

	versionStr := string(output)
	versionStr = strings.TrimSpace(versionStr)

	// Parse version string like "Microsoft Windows [Version 10.0.19045.5131]"
	if strings.Contains(versionStr, "Windows") {
		// Extract just the meaningful part
		if strings.Contains(versionStr, "11") {
			return "Windows 11"
		} else if strings.Contains(versionStr, "10") {
			return "Windows 10"
		} else if strings.Contains(versionStr, "Server") {
			if strings.Contains(versionStr, "2022") {
				return "Windows Server 2022"
			} else if strings.Contains(versionStr, "2019") {
				return "Windows Server 2019"
			}
			return "Windows Server"
		}
	}

	return "Windows"
}

// getLinuxVersion returns Linux distribution and version
func getLinuxVersion() string {
	// Try /etc/os-release first (most modern distros)
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		prettyName := ""
		name := ""
		version := ""

		for _, line := range lines {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				prettyName = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
			if strings.HasPrefix(line, "NAME=") {
				name = strings.Trim(strings.TrimPrefix(line, "NAME="), `"`)
			}
			if strings.HasPrefix(line, "VERSION=") {
				version = strings.Trim(strings.TrimPrefix(line, "VERSION="), `"`)
			}
		}

		if prettyName != "" {
			return prettyName
		}
		if name != "" && version != "" {
			return name + " " + version
		}
		if name != "" {
			return name
		}
	}

	// Try lsb_release
	cmd := exec.Command("lsb_release", "-d")
	output, err := cmd.Output()
	if err == nil {
		desc := string(output)
		desc = strings.TrimPrefix(desc, "Description:")
		return strings.TrimSpace(desc)
	}

	// Fallback
	return "Linux"
}

// getMacOSVersion returns macOS version
func getMacOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return "macOS"
	}

	version := strings.TrimSpace(string(output))

	// Get product name
	cmd = exec.Command("sw_vers", "-productName")
	output, err = cmd.Output()
	if err == nil {
		name := strings.TrimSpace(string(output))
		return name + " " + version
	}

	return "macOS " + version
}

// getCPUModel returns the CPU model name
func getCPUModel() string {
	switch runtime.GOOS {
	case "windows":
		return getWindowsCPU()
	case "linux":
		return getLinuxCPU()
	case "darwin":
		return getMacOSCPU()
	default:
		return "Unknown"
	}
}

// getWindowsCPU returns Windows CPU info
func getWindowsCPU() string {
	cmd := exec.Command("wmic", "cpu", "get", "name", "/value")
	output, err := cmd.Output()
	if err != nil {
		return runtime.GOARCH
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Name=") {
			cpu := strings.TrimPrefix(line, "Name=")
			cpu = strings.TrimSpace(cpu)
			// Simplify long CPU names
			cpu = strings.ReplaceAll(cpu, "(R)", "")
			cpu = strings.ReplaceAll(cpu, "(TM)", "")
			cpu = strings.ReplaceAll(cpu, "  ", " ")
			return strings.TrimSpace(cpu)
		}
	}

	return runtime.GOARCH
}

// getLinuxCPU returns Linux CPU info
func getLinuxCPU() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return runtime.GOARCH
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				cpu := strings.TrimSpace(parts[1])
				// Simplify
				cpu = strings.ReplaceAll(cpu, "(R)", "")
				cpu = strings.ReplaceAll(cpu, "(TM)", "")
				cpu = strings.ReplaceAll(cpu, "  ", " ")
				return strings.TrimSpace(cpu)
			}
		}
	}

	return runtime.GOARCH
}

// getMacOSCPU returns macOS CPU info
func getMacOSCPU() string {
	cmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
	output, err := cmd.Output()
	if err != nil {
		return runtime.GOARCH
	}

	cpu := strings.TrimSpace(string(output))
	cpu = strings.ReplaceAll(cpu, "(R)", "")
	cpu = strings.ReplaceAll(cpu, "(TM)", "")
	cpu = strings.ReplaceAll(cpu, "  ", " ")
	return strings.TrimSpace(cpu)
}
