//go:build !windows

package autoupdate

// checkMSIInstallation is a stub for non-Windows platforms.
// MSI installation is only relevant on Windows.
func checkMSIInstallation() (productCode string, isMSI bool) {
	return "", false
}
