//go:build windows

package autoupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	// PrintMaster MSI UpgradeCode GUID - must match the WiX template
	// (Currently unused but reserved for future upgrade code matching)
	_ = "{12345678-1234-1234-1234-123456789012}"
)

// checkMSIInstallation detects if the agent was installed via MSI.
// Returns the product code if found, or empty string if not MSI-installed.
func checkMSIInstallation() (productCode string, isMSI bool) {
	// Check standard install location
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	expectedPath := filepath.Join(programFiles, "PrintMaster", "printmaster-agent.exe")

	// If we're not running from the standard MSI install location, assume not MSI
	exePath, err := os.Executable()
	if err != nil {
		return "", false
	}
	exePath = filepath.Clean(exePath)
	expectedPath = filepath.Clean(expectedPath)

	if !strings.EqualFold(exePath, expectedPath) {
		// Not running from standard MSI install location
		return "", false
	}

	// Look for our product in Windows Installer registry
	// Check both 64-bit and 32-bit registry locations
	productCode = findMSIProductCode()
	if productCode != "" {
		return productCode, true
	}

	// Also check by looking for uninstall entry
	productCode = findUninstallEntry()
	if productCode != "" {
		return productCode, true
	}

	return "", false
}

// findMSIProductCode searches the Windows Installer registry for PrintMaster.
func findMSIProductCode() string {
	// Windows Installer stores products under:
	// HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Installer\UserData\S-1-5-18\Products\{GUID}
	// or HKLM\SOFTWARE\Classes\Installer\Products\{GUID}

	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Classes\Installer\Products`, registry.READ|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return ""
	}
	defer key.Close()

	subkeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return ""
	}

	for _, subkey := range subkeys {
		productKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
			`SOFTWARE\Classes\Installer\Products\`+subkey, registry.READ)
		if err != nil {
			continue
		}

		productName, _, err := productKey.GetStringValue("ProductName")
		productKey.Close()
		if err != nil {
			continue
		}

		if strings.Contains(strings.ToLower(productName), "printmaster") {
			// Convert packed GUID back to standard format
			return unpackGUID(subkey)
		}
	}

	return ""
}

// findUninstallEntry looks for PrintMaster in the Uninstall registry.
func findUninstallEntry() string {
	paths := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}

	for _, path := range paths {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.READ|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}

		subkeys, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			continue
		}

		for _, subkey := range subkeys {
			uninstallKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
				path+`\`+subkey, registry.READ)
			if err != nil {
				continue
			}

			displayName, _, err := uninstallKey.GetStringValue("DisplayName")
			if err != nil {
				uninstallKey.Close()
				continue
			}

			if strings.Contains(strings.ToLower(displayName), "printmaster") {
				// Check if this is an MSI-installed product
				windowsInstaller, _, err := uninstallKey.GetIntegerValue("WindowsInstaller")
				uninstallKey.Close()
				if err == nil && windowsInstaller == 1 {
					// subkey is the product code for MSI products
					return subkey
				}
				continue
			}
			uninstallKey.Close()
		}
	}

	return ""
}

// unpackGUID converts a packed GUID (registry format) to standard GUID format.
// Packed: 12345678123412341234123456789012
// Standard: {12345678-1234-1234-1234-123456789012}
func unpackGUID(packed string) string {
	if len(packed) != 32 {
		return packed // Not a valid packed GUID
	}

	// The packed format reverses some sections
	// Section 1: reverse 8 chars
	// Section 2: reverse 4 chars
	// Section 3: reverse 4 chars
	// Section 4: reverse pairs of chars (8 total)
	// Section 5: reverse pairs of chars (12 total)

	reverse := func(s string) string {
		r := []rune(s)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return string(r)
	}

	reversePairs := func(s string) string {
		result := make([]byte, len(s))
		for i := 0; i < len(s); i += 2 {
			result[i] = s[i+1]
			result[i+1] = s[i]
		}
		return string(result)
	}

	part1 := reverse(packed[0:8])
	part2 := reverse(packed[8:12])
	part3 := reverse(packed[12:16])
	part4 := reversePairs(packed[16:24])
	part5 := reversePairs(packed[24:32])

	return fmt.Sprintf("{%s-%s-%s-%s-%s}", part1, part2, part3, part4, part5)
}
