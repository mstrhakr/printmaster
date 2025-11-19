package tenancy

import (
	"net/http"
	"strings"
	"sync"
)

// TenantSubresourceHandler handles nested routes such as /api/v1/tenants/{id}/settings.
type TenantSubresourceHandler func(http.ResponseWriter, *http.Request, string, string)

var (
	tenantSubresourceHandlers   = make(map[string]TenantSubresourceHandler)
	tenantSubresourceHandlersMu sync.RWMutex
)

// RegisterTenantSubresource installs a handler for a named tenant subresource (e.g., "settings").
// Pass a nil handler to remove an existing registration.
func RegisterTenantSubresource(name string, handler TenantSubresourceHandler) {
	key := normalizeSubresourceName(name)
	if key == "" {
		return
	}
	tenantSubresourceHandlersMu.Lock()
	defer tenantSubresourceHandlersMu.Unlock()
	if handler == nil {
		delete(tenantSubresourceHandlers, key)
		return
	}
	tenantSubresourceHandlers[key] = handler
}

func getTenantSubresourceHandler(name string) TenantSubresourceHandler {
	key := normalizeSubresourceName(name)
	if key == "" {
		return nil
	}
	tenantSubresourceHandlersMu.RLock()
	defer tenantSubresourceHandlersMu.RUnlock()
	return tenantSubresourceHandlers[key]
}

func normalizeSubresourceName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "/")
	return strings.ToLower(name)
}
