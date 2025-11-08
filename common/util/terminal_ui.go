package util

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Global quiet and silent mode flags
var quietMode bool
var silentMode bool

// SetQuietMode enables or disables quiet mode for all terminal output
func SetQuietMode(quiet bool) {
	quietMode = quiet
}

// SetSilentMode enables or disables silent mode (suppresses ALL output including errors)
func SetSilentMode(silent bool) {
	silentMode = silent
	if silent {
		quietMode = true // Silent mode implies quiet mode
	}
}

// IsQuietMode returns true if quiet mode is enabled
func IsQuietMode() bool {
	return quietMode
}

// IsSilentMode returns true if silent mode is enabled
func IsSilentMode() bool {
	return silentMode
}

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
	ColorBold    = "\033[1m"
	ColorDim     = "\033[2m"
)

// ASCII art for PRINTMASTER
const asciiArt = `
  ┌────────────────────────────────────────────────────────────────────────────┐
  │                                                                            │
  │   ____________ _____ _   _ ________  ___  ___   _____ _____ ___________    │
  │   | ___ \ ___ \_   _| \ | |_   _|  \/  | / _ \ /  ___|_   _|  ___| ___ \   │
  │   | |_/ / |_/ / | | |  \| | | | | .  . |/ /_\ \\ ` + "`" + `--.  | | | |__ | |_/ /   │
  │   |  __/|    /  | | | . ` + "`" + ` | | | | |\/| ||  _  | ` + "`" + `--. \ | | |  __||    /    │
  │   | |   | |\ \ _| |_| |\  | | | | |  | || | | |/\__/ / | | | |___| |\ \    │
  │   \_|   \_| \_|\___/\_| \_/ \_/ \_|  |_/\_| |_/\____/  \_/ \____/\_| \_|   │
  │                                                                            │
  └────────────────────────────────────────────────────────────────────────────┘
`

// ShowBanner displays the PRINTMASTER banner with version and system info
// componentName should be "Fleet Management Agent" or "Central Management Server"
func ShowBanner(version, gitCommit, buildDate, componentName string) {
	if quietMode {
		return
	}
	ClearScreen()

	// Show ASCII art
	fmt.Print(ColorCyan + asciiArt + ColorReset)

	// Show version info centered
	fmt.Println()
	centerPrint(fmt.Sprintf("%s%s%s", ColorBold, componentName, ColorReset))
	centerPrint(fmt.Sprintf("Version %s%s%s | Build %s%s%s | %s",
		ColorGreen, version, ColorReset,
		ColorYellow, gitCommit, ColorReset,
		buildDate))
	fmt.Println()

	// Get and show detailed system info
	sysInfo := GetSystemInfo()
	centerPrint(fmt.Sprintf("%sOS:%s %s (%s) | %sHost:%s %s",
		ColorDim, ColorReset, sysInfo.OSVersion, sysInfo.Arch,
		ColorDim, ColorReset, sysInfo.Hostname))
	centerPrint(fmt.Sprintf("%sCPU:%s %s (%d cores)",
		ColorDim, ColorReset, sysInfo.CPUModel, sysInfo.NumCPU))
	fmt.Println()
	fmt.Println()
}

// ShowProgress displays an animated progress bar with a message
func ShowProgress(percent int, message string) {
	if quietMode {
		return
	}
	barWidth := 40
	filled := (percent * barWidth) / 100
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	fmt.Printf("\r  %s[%s]%s %3d%% %s%s%s   ",
		ColorCyan, bar, ColorReset,
		percent,
		ColorWhite, message, ColorReset)
}

// ShowSpinner displays a spinner with a message
func ShowSpinner(spinner string, message string) {
	if quietMode {
		return
	}
	fmt.Printf("\r  %s%s%s %s%s%s   ",
		ColorCyan, spinner, ColorReset,
		ColorWhite, message, ColorReset)
}

// ClearLine clears the current line
func ClearLine() {
	if quietMode {
		return
	}
	fmt.Print("\r" + strings.Repeat(" ", 100) + "\r")
}

// ShowSuccess displays a success message
func ShowSuccess(message string) {
	if silentMode {
		return // Silent mode suppresses everything
	}
	if quietMode {
		// In quiet mode, output as a log entry
		// Format: dim-timestamp colorized-level message
		// INFO level = Blue (consistent with ShowInfo)
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Printf("%s%s%s %s[INFO]%s %s\n", ColorDim, timestamp, ColorReset, ColorBlue, ColorReset, message)
		return
	}
	ClearLine()
	fmt.Printf("  %s✓%s %s\n", ColorGreen, ColorReset, message)
}

// ShowError displays an error message
func ShowError(message string) {
	if silentMode {
		return // Silent mode suppresses everything
	}
	// Errors are always shown, even in quiet mode
	if quietMode {
		// In quiet mode, output as a log entry
		// Format: dim-timestamp colorized-level message
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Printf("%s%s%s %s[ERROR]%s %s\n", ColorDim, timestamp, ColorReset, ColorRed, ColorReset, message)
		return
	}
	ClearLine()
	fmt.Printf("  %s✗%s %s\n", ColorRed, ColorReset, message)
}

// ShowInfo displays an info message
func ShowInfo(message string) {
	if silentMode {
		return // Silent mode suppresses everything
	}
	if quietMode {
		// In quiet mode, output as a log entry
		// Format: dim-timestamp colorized-level message
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Printf("%s%s%s %s[INFO]%s %s\n", ColorDim, timestamp, ColorReset, ColorBlue, ColorReset, message)
		return
	}
	ClearLine()
	fmt.Printf("  %s•%s %s\n", ColorCyan, ColorReset, message)
}

// ShowWarning displays a warning message
func ShowWarning(message string) {
	if silentMode {
		return // Silent mode suppresses everything
	}
	// Warnings are always shown, even in quiet mode (safety-critical)
	if quietMode {
		// In quiet mode, output as a log entry
		// Format: dim-timestamp colorized-level message
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Printf("%s%s%s %s[WARN]%s %s\n", ColorDim, timestamp, ColorReset, ColorYellow, ColorReset, message)
		return
	}
	ClearLine()
	fmt.Printf("  %s⚠%s %s\n", ColorYellow, ColorReset, message)
}

// PromptToContinue waits for the user to press Enter
func PromptToContinue() {
	if quietMode {
		return
	}
	fmt.Println()
	fmt.Printf("  %sPress Enter to continue...%s", ColorDim, ColorReset)
	fmt.Scanln()
}

// ShowCompletionScreen shows a final completion screen
func ShowCompletionScreen(success bool, message string) {
	if silentMode {
		return // Silent mode suppresses everything
	}
	if quietMode {
		// In quiet mode, output as a log entry
		// Format: dim-timestamp colorized-level message
		// Colors based on log level: INFO=Blue, ERROR=Red
		timestamp := time.Now().Format(time.RFC3339)
		if success {
			fmt.Printf("%s%s%s %s[INFO]%s %s\n", ColorDim, timestamp, ColorReset, ColorBlue, ColorReset, message)
		} else {
			fmt.Printf("%s%s%s %s[ERROR]%s %s\n", ColorDim, timestamp, ColorReset, ColorRed, ColorReset, message)
		}
		return
	}

	fmt.Println()
	fmt.Println()

	// Calculate box width based on message length (minimum 40 chars)
	messageLen := len(message)
	boxWidth := messageLen + 12 // 4 for "  ✓  " + 4 for padding + 4 for borders
	if boxWidth < 40 {
		boxWidth = 40
	}

	// Create horizontal lines
	topBottom := strings.Repeat("═", boxWidth-2)
	emptyLine := strings.Repeat(" ", boxWidth-2)

	// Calculate padding for centered message
	contentWidth := boxWidth - 4 // Subtract borders
	iconAndSpace := 4            // "  ✓  " or "  ✗  "
	textWidth := contentWidth - iconAndSpace
	paddingLeft := (textWidth - messageLen) / 2
	paddingRight := textWidth - messageLen - paddingLeft

	icon := "✓"
	color := ColorGreen
	if !success {
		icon = "✗"
		color = ColorRed
	}

	// Print centered box
	indent := strings.Repeat(" ", (80-boxWidth)/2)

	fmt.Printf("%s%s╔%s╗%s\n", indent, color, topBottom, ColorReset)
	fmt.Printf("%s%s║%s║%s\n", indent, color, emptyLine, ColorReset)
	fmt.Printf("%s%s║  %s  %s%s%s%s%s  ║%s\n",
		indent, color, icon,
		strings.Repeat(" ", paddingLeft),
		ColorBold, message, ColorReset+color,
		strings.Repeat(" ", paddingRight),
		ColorReset)
	fmt.Printf("%s%s║%s║%s\n", indent, color, emptyLine, ColorReset)
	fmt.Printf("%s%s╚%s╝%s\n", indent, color, topBottom, ColorReset)

	fmt.Println()
	PromptToContinue()
}

// ClearScreen clears the terminal screen
func ClearScreen() {
	if runtime.GOOS == "windows" {
		fmt.Print("\033[H\033[2J")
	} else {
		fmt.Print("\033[H\033[2J")
	}
}

// centerPrint prints text centered (assumes 80 char width)
func centerPrint(text string) {
	// Strip ANSI codes for length calculation
	visibleText := stripAnsi(text)
	width := 80
	padding := (width - len(visibleText)) / 2
	if padding > 0 {
		fmt.Print(strings.Repeat(" ", padding))
	}
	fmt.Println(text)
}

// stripAnsi removes ANSI escape codes from a string
func stripAnsi(str string) string {
	// Simple implementation - just removes \033[...m sequences
	result := ""
	inEscape := false
	for i := 0; i < len(str); i++ {
		if str[i] == '\033' && i+1 < len(str) && str[i+1] == '[' {
			inEscape = true
			i++
			continue
		}
		if inEscape {
			if str[i] == 'm' {
				inEscape = false
			}
			continue
		}
		result += string(str[i])
	}
	return result
}

// AnimateProgress runs an animated progress bar for a duration
func AnimateProgress(duration time.Duration, message string, done chan bool) {
	spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinIndex := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			ClearLine()
			return
		case <-ticker.C:
			ShowSpinner(spinChars[spinIndex], message)
			spinIndex = (spinIndex + 1) % len(spinChars)
		}
	}
}
