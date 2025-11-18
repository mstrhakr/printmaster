package main

import (
	"fmt"
	"strings"

	"printmaster/server/storage"
)

// Action represents a specific permissionable operation.
type Action string

const (
	ActionTenantsRead      Action = "tenants.read"
	ActionTenantsWrite     Action = "tenants.write"
	ActionJoinTokensRead   Action = "join_tokens.read"
	ActionJoinTokensWrite  Action = "join_tokens.write"
	ActionPackagesGenerate Action = "packages.generate"
)

// ResourceRef carries context about the resource being accessed.
type ResourceRef struct {
	TenantIDs []string
}

// authorizeAction validates that the principal can perform the action on the resource.
func authorizeAction(principal *Principal, action Action, resource ResourceRef) error {
	if principal == nil {
		return fmt.Errorf("unauthenticated")
	}

	if !roleAllows(principal.Role, action) {
		return fmt.Errorf("role %s cannot perform %s", principal.Role, action)
	}

	if len(resource.TenantIDs) > 0 && !principal.IsAdmin() {
		allowed := make(map[string]struct{}, len(principal.TenantIDs))
		for _, tid := range principal.TenantIDs {
			allowed[tid] = struct{}{}
		}
		for _, tid := range resource.TenantIDs {
			if tid == "" {
				continue
			}
			if _, ok := allowed[tid]; !ok {
				return fmt.Errorf("tenant %s not permitted", tid)
			}
		}
	}

	return nil
}

var rolePolicies = map[storage.Role][]string{
	storage.RoleAdmin:    {"*"},
	storage.RoleOperator: {},
	storage.RoleViewer:   {},
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
