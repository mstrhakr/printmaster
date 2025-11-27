package selfupdate

import (
	"os"
	"strings"
)

const (
	envDisableKey = "PM_DISABLE_SELFUPDATE"
)

func disableReason(enabled bool) string {
	if !enabled {
		return "config disabled"
	}
	if reason, ok := forcedEnvDisable(os.Getenv(envDisableKey)); ok {
		return reason
	}
	return ""
}

func runtimeSkipReason() string {
	if runningInsideContainer() {
		return "container environment detected"
	}
	if isCIEnvironment() {
		return "ci environment detected"
	}
	return ""
}

func forcedEnvDisable(value string) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(value), "true") || value == "1" {
		return envDisableKey + " set", true
	}
	return "", false
}

func runningInsideContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	if v := strings.ToLower(os.Getenv("CONTAINER")); v == "docker" || v == "lxc" {
		return true
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	return false
}

func isCIEnvironment() bool {
	ciEnvVars := []string{
		"CI", "GITHUB_ACTIONS", "BUILDKITE", "GITLAB_CI", "TF_BUILD",
	}
	for _, key := range ciEnvVars {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}
