//go:build !windows && !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly
// +build !windows,!darwin,!linux,!freebsd,!openbsd,!netbsd,!dragonfly

package agent

import (
	"os/exec"
	"runtime"
	"strings"
)

// getPlatformDetailed returns OS info for other Unix-like systems.
// Falls back to uname output.
func getPlatformDetailed() string {
	// Try uname -sr for other Unix systems
	cmd := exec.Command("uname", "-sr")
	out, err := cmd.Output()
	if err != nil {
		return normalizePlatformName(runtime.GOOS)
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		return normalizePlatformName(runtime.GOOS)
	}

	return version
}
