package authz

import (
	"errors"
	"fmt"
	"strings"

	"printmaster/server/storage"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// Action represents a permissionable operation within the server API surface.
type Action string

const (
	ActionTenantsRead      Action = "tenants.read"
	ActionTenantsWrite     Action = "tenants.write"
	ActionJoinTokensRead   Action = "join_tokens.read"
	ActionJoinTokensWrite  Action = "join_tokens.write"
	ActionPackagesGenerate Action = "packages.generate"

	ActionConfigRead         Action = "config.read"
	ActionEventsSubscribe    Action = "events.subscribe"
	ActionUIWebsocketConnect Action = "ui.websocket.connect"

	ActionSSOProvidersRead  Action = "sso.providers.read"
	ActionSSOProvidersWrite Action = "sso.providers.write"

	ActionUsersRead     Action = "users.read"
	ActionUsersWrite    Action = "users.write"
	ActionSessionsRead  Action = "sessions.read"
	ActionSessionsWrite Action = "sessions.write"

	ActionAgentsRead   Action = "agents.read"
	ActionAgentsWrite  Action = "agents.write"
	ActionAgentsDelete Action = "agents.delete"

	ActionDevicesRead Action = "devices.read"

	ActionMetricsSummaryRead Action = "metrics.summary.read"
	ActionMetricsHistoryRead Action = "metrics.history.read"

	ActionProxyAgentConnect  Action = "proxy.agent"
	ActionProxyDeviceConnect Action = "proxy.device"

	ActionSettingsRead      Action = "settings.read"
	ActionSettingsWrite     Action = "settings.write"
	ActionSettingsTestEmail Action = "settings.test_email"

	ActionLogsRead      Action = "logs.read"
	ActionAuditLogsRead Action = "audit.logs.read"

	ActionReleasesRead  Action = "releases.read"
	ActionReleasesWrite Action = "releases.write"
)

// ResourceRef carries contextual identifiers relevant for authorization checks.
type ResourceRef struct {
	TenantIDs []string
}

// Subject describes the caller being authorized.
type Subject struct {
	Role             storage.Role
	AllowedTenantIDs []string
	IsAdmin          bool
}

// Authorize ensures subject can perform action on the resource.

func Authorize(subject Subject, action Action, resource ResourceRef) error {
	if !roleAllows(subject.Role, action) {
		return fmt.Errorf("%w: role %s cannot perform %s", ErrForbidden, subject.Role, action)
	}

	if len(resource.TenantIDs) > 0 && !subject.IsAdmin {
		allowed := make(map[string]struct{}, len(subject.AllowedTenantIDs))
		for _, tid := range subject.AllowedTenantIDs {
			allowed[tid] = struct{}{}
		}
		for _, tid := range resource.TenantIDs {
			if tid == "" {
				continue
			}
			if _, ok := allowed[tid]; !ok {
				return fmt.Errorf("%w: tenant %s not permitted", ErrForbidden, tid)
			}
		}
	}

	return nil
}

var rolePolicies = map[storage.Role][]string{
	storage.RoleAdmin: {"*"},
	storage.RoleOperator: {
		"config.read",
		"events.subscribe",
		"ui.websocket.connect",
		"agents.*",
		"packages.generate",
		"devices.read",
		"metrics.summary.read",
		"metrics.history.read",
		"proxy.agent",
		"proxy.device",
		"logs.read",
		"settings.read",  // Allow operators to read fleet settings (scoped to their tenants)
		"settings.write", // Allow operators to write fleet settings (scoped to their tenants)
	},
	storage.RoleViewer: {
		"config.read",
		"events.subscribe",
		"ui.websocket.connect",
		"agents.read",
		"devices.read",
		"metrics.summary.read",
		"metrics.history.read",
		"logs.read",
		"settings.read", // Allow viewers to read fleet settings (scoped to their tenants)
	},
}

func roleAllows(role storage.Role, action Action) bool {
	patterns, ok := rolePolicies[role]
	if !ok {
		return false
	}

	needle := strings.ToLower(string(action))
	for _, pattern := range patterns {
		switch {
		case pattern == "*":
			return true
		case strings.EqualFold(pattern, needle):
			return true
		case strings.HasSuffix(pattern, ".*"):
			prefix := strings.TrimSuffix(strings.ToLower(pattern), ".*")
			if strings.HasPrefix(needle, prefix+".") {
				return true
			}
		}
	}
	return false
}
