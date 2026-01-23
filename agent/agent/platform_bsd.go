//go:build freebsd || openbsd || netbsd || dragonfly
// +build freebsd openbsd netbsd dragonfly

package agent

import (
	"os/exec"
	"runtime"
	"strings"
)

// getPlatformDetailed returns detailed BSD version info.
// Examples: "FreeBSD 14.1-RELEASE", "OpenBSD 7.5", "NetBSD 10.0"
func getPlatformDetailed() string {
	// Use uname -sr to get kernel name and release
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
