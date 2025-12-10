package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	authz "printmaster/server/authz"
	packager "printmaster/server/packager"
	"printmaster/server/storage"

	"printmaster/common/logger"
)

// RegisterRoutes registers HTTP handlers for tenancy endpoints.
// dbStore, when set via RegisterRoutes, will be used for persistence. If nil,
// the package in-memory `store` is used (keeps tests and backwards compatibility).
var dbStore storage.Store

// tenancyEnabled controls whether administrator-facing tenancy features
// (tenant CRUD, join tokens, package generation) are active. The public
// token registration endpoint remains reachable even when disabled so
// agents can always onboard via the new flow.
var tenancyEnabled bool

// pkgLogger provides structured logging for the tenancy package.
var pkgLogger *logger.Logger

// SetLogger configures the structured logger for the tenancy package.
func SetLogger(l *logger.Logger) {
	pkgLogger = l
}

func logDebug(msg string, kv ...interface{}) {
	if pkgLogger != nil {
		pkgLogger.Debug(msg, kv...)
	}
}

func logInfo(msg string, kv ...interface{}) {
	if pkgLogger != nil {
		pkgLogger.Info(msg, kv...)
	}
}

func logWarn(msg string, kv ...interface{}) {
	if pkgLogger != nil {
		pkgLogger.Warn(msg, kv...)
	}
}

func logError(msg string, kv ...interface{}) {
	if pkgLogger != nil {
		pkgLogger.Error(msg, kv...)
	}
}

type installerBuilder interface {
	BuildInstaller(context.Context, packager.BuildRequest) (*storage.InstallerBundle, error)
	OpenBundle(context.Context, *storage.InstallerBundle) (*packager.BundleHandle, error)
}

// installerPackager orchestrates archive generation for tenant-scoped installers.
var installerPackager installerBuilder

// SetInstallerPackager allows the server to inject the shared packager.Manager instance.
func SetInstallerPackager(builder installerBuilder) {
	installerPackager = builder
}

// SetEnabled allows the main server to toggle tenancy feature flags at
// runtime (typically at startup based on configuration).
func SetEnabled(enabled bool) {
	tenancyEnabled = enabled
}

// agentEventSink, when configured, receives lifecycle events so the server can fan out
// updates (e.g., via SSE) to the UI without this package importing higher layers.
var agentEventSink func(eventType string, data map[string]interface{})

var agentSettingsBuilder func(context.Context, string) (string, interface{}, error)

var auditLogger func(*http.Request, *storage.AuditEntry)

var (
	releaseAssetBaseURL   = "https://github.com/mstrhakr/printmaster/releases/download"
	releaseDownloadClient = &http.Client{Timeout: 2 * time.Minute}
)

// SetAgentEventSink registers a callback invoked for agent lifecycle events.
func SetAgentEventSink(sink func(eventType string, data map[string]interface{})) {
	agentEventSink = sink
}

// SetAgentSettingsBuilder wires the callback that produces resolved settings snapshots for agents.
func SetAgentSettingsBuilder(builder func(context.Context, string) (string, interface{}, error)) {
	agentSettingsBuilder = builder
}

// SetAuditLogger wires an audit sink so tenancy actions appear in the central audit log.
func SetAuditLogger(logger func(*http.Request, *storage.AuditEntry)) {
	auditLogger = logger
}

func recordAudit(r *http.Request, entry *storage.AuditEntry) {
	if auditLogger == nil || entry == nil {
		return
	}
	auditLogger(r, entry)
}

func maskTokenValue(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return token
	}
	return token[:4] + "..." + token[len(token)-2:]
}

func tenantAuditMetadata(name, description, contactName, contactEmail, contactPhone, businessUnit, billingCode, address, loginDomain string) map[string]interface{} {
	return map[string]interface{}{
		"name":          name,
		"description":   description,
		"contact_name":  contactName,
		"contact_email": contactEmail,
		"contact_phone": contactPhone,
		"business_unit": businessUnit,
		"billing_code":  billingCode,
		"address":       address,
		"login_domain":  storage.NormalizeTenantDomain(loginDomain),
	}
}

func attachAgentSettings(resp map[string]interface{}, ctx context.Context, tenantID string) {
	if agentSettingsBuilder == nil || resp == nil {
		return
	}
	version, snapshot, err := agentSettingsBuilder(ctx, tenantID)
	if err != nil || version == "" || snapshot == nil {
		return
	}
	resp["settings_version"] = version
	resp["settings_snapshot"] = snapshot
}

// AuthMiddleware, when set by the main application, will be used to wrap
// tenancy handlers so they can enforce authentication/authorization.
// Set to nil to leave routes unprotected (not recommended).
var AuthMiddleware func(http.HandlerFunc) http.HandlerFunc

// installMap stores transient install scripts keyed by a short code.
type installEntry struct {
	Script    string
	Filename  string
	ExpiresAt time.Time
	OneTime   bool
}

type joinTokenInfo struct {
	ID        string
	ExpiresAt time.Time
	OneTime   bool
}

type tenantPayload struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
	BusinessUnit string `json:"business_unit,omitempty"`
	BillingCode  string `json:"billing_code,omitempty"`
	Address      string `json:"address,omitempty"`
	LoginDomain  string `json:"login_domain,omitempty"`
}

type packageRequest struct {
	TenantID      string `json:"tenant_id"`
	Platform      string `json:"platform"`
	InstallerType string `json:"installer_type"`
	TTLMinutes    int    `json:"ttl_minutes"`
	Format        string `json:"format"`
	Component     string `json:"component"`
	Version       string `json:"version"`
	Arch          string `json:"arch"`
}

var installStore = struct {
	mu sync.Mutex
	m  map[string]installEntry
}{
	m: make(map[string]installEntry),
}

var installCleanerOnce sync.Once

// serverVersion holds the running server semantic version. Main should set
// this via SetServerVersion so the download redirect can choose the matching
// agent release asset on GitHub.
var serverVersion string

var versionFileCandidates = []string{"server/VERSION", "VERSION"}

// SetServerVersion sets the server version (called from main at startup).
func SetServerVersion(v string) {
	serverVersion = v
}

func requireTenancyEnabled(w http.ResponseWriter, r *http.Request) bool {
	if tenancyEnabled {
		return true
	}
	http.NotFound(w, r)
	return false
}

// RegisterRoutes registers HTTP handlers for tenancy endpoints. If a
// non-nil storage.Store is provided, handlers will persist tenants and tokens
// in the server DB; otherwise the in-memory store is used.
func RegisterRoutes(s storage.Store) {
	// Allow RegisterRoutes to be called multiple times (tests may swap muxes).
	// If routes already registered, just update the dbStore reference and return
	// to avoid duplicate http.HandleFunc registration which panics.
	// Delegate to the mux-aware registration using the default mux
	RegisterRoutesOnMux(http.DefaultServeMux, s)
}

// RegisterRoutesOnMux registers tenancy routes on the provided ServeMux.
// This is useful for tests which create their own muxes to avoid global
// DefaultServeMux races. It will always register the routes on the given
// mux; callers are responsible for ensuring they don't register the same
// routes multiple times on the same mux.
func RegisterRoutesOnMux(mux *http.ServeMux, s storage.Store) {
	dbStore = s
	RegisterTenantSubresource("bundles", handleTenantBundlesSubresource)
	// Wrap handlers with AuthMiddleware when provided
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		if AuthMiddleware != nil {
			return AuthMiddleware(h)
		}
		return h
	}

	mux.HandleFunc("/api/v1/tenants", wrap(handleTenants))
	mux.HandleFunc("/api/v1/tenants/", wrap(handleTenantRoute))
	mux.HandleFunc("/api/v1/join-token", wrap(handleCreateJoinToken))
	mux.HandleFunc("/api/v1/agents/register-with-token", handleRegisterWithToken) // registration must remain public
	mux.HandleFunc("/api/v1/join-tokens", wrap(handleListJoinTokens))             // GET (admin)
	mux.HandleFunc("/api/v1/join-token/revoke", wrap(handleRevokeJoinToken))      // POST {"id":"..."}
	// Pending agent registrations (expired token capture)
	mux.HandleFunc("/api/v1/pending-registrations", wrap(handlePendingRegistrations))   // GET (admin)
	mux.HandleFunc("/api/v1/pending-registrations/", wrap(handlePendingRegistrationByID)) // GET/POST/DELETE by ID
	// Package generation (bootstrap script / archive) - admin only
	mux.HandleFunc("/api/v1/packages", wrap(handleGeneratePackage))
	mux.HandleFunc("/api/v1/packages/", wrap(handlePackageRoute))
	// Public redirect to latest agent binary on GitHub releases. This chooses
	// the release based on the running server version (set by main via
	// SetServerVersion) and redirects to the appropriate asset for the
	// requested platform/arch.
	mux.HandleFunc("/api/v1/agents/download/latest", handleAgentDownloadLatest)
	// Hosted install scripts (transient codes)
	mux.HandleFunc("/install/", handleInstall)

	// Start background cleanup for transient installs (runs once)
	installCleanerOnce.Do(func() { go installCleanupLoop() })
}

// handleTenants supports GET (list) and POST (create)
func handleTenants(w http.ResponseWriter, r *http.Request) {
	logDebug("handleTenants called", "method", r.Method, "tenancyEnabled", tenancyEnabled)
	if !requireTenancyEnabled(w, r) {
		logDebug("handleTenants: tenancy not enabled, returning 404")
		return
	}
	switch r.Method {
	case http.MethodGet:
		logDebug("handleTenants: GET request, checking authorization")
		if !authorizeOrReject(w, r, authz.ActionTenantsRead, authz.ResourceRef{}) {
			logDebug("handleTenants: authorization rejected for TenantsRead")
			return
		}
		logDebug("handleTenants: authorized, checking dbStore", "dbStore", dbStore != nil)
		if dbStore != nil {
			logDebug("handleTenants: querying database for tenants")
			list, err := dbStore.ListTenants(r.Context())
			if err != nil {
				logError("handleTenants: failed to list tenants from database", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"failed to list tenants"}`))
				return
			}
			logDebug("handleTenants: successfully listed tenants from database", "count", len(list))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
			return
		}
		logDebug("handleTenants: using in-memory store")
		list := store.ListTenants()
		logDebug("handleTenants: listed tenants from memory", "count", len(list))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	case http.MethodPost:
		logDebug("handleTenants: POST request, checking authorization")
		if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{}) {
			logWarn("handleTenants: authorization rejected for TenantsWrite")
			return
		}
		var in tenantPayload
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid json"}`))
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"name required"}`))
			return
		}
		if dbStore != nil {
			tn := &storage.Tenant{
				ID:           in.ID,
				Name:         in.Name,
				Description:  in.Description,
				ContactName:  in.ContactName,
				ContactEmail: in.ContactEmail,
				ContactPhone: in.ContactPhone,
				BusinessUnit: in.BusinessUnit,
				BillingCode:  in.BillingCode,
				Address:      in.Address,
				LoginDomain:  storage.NormalizeTenantDomain(in.LoginDomain),
			}
			if tn.ID == "" {
				// Let storage layer generate ID via SQL default
			}
			if err := dbStore.CreateTenant(r.Context(), tn); err != nil {
				logError("handleTenants: failed to create tenant in database", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"failed to create tenant"}`))
				return
			}
			logInfo("handleTenants: tenant created successfully", "id", tn.ID, "name", tn.Name)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tn)
			recordAudit(r, &storage.AuditEntry{
				Action:     "tenant.create",
				TargetType: "tenant",
				TargetID:   tn.ID,
				TenantID:   tn.ID,
				Details:    fmt.Sprintf("Created tenant %s", tn.Name),
				Metadata:   tenantAuditMetadata(tn.Name, tn.Description, tn.ContactName, tn.ContactEmail, tn.ContactPhone, tn.BusinessUnit, tn.BillingCode, tn.Address, tn.LoginDomain),
			})
			return
		}
		t, err := store.CreateTenant(Tenant{
			ID:           in.ID,
			Name:         in.Name,
			Description:  in.Description,
			ContactName:  in.ContactName,
			ContactEmail: in.ContactEmail,
			ContactPhone: in.ContactPhone,
			BusinessUnit: in.BusinessUnit,
			BillingCode:  in.BillingCode,
			Address:      in.Address,
			LoginDomain:  storage.NormalizeTenantDomain(in.LoginDomain),
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create tenant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(t)
		recordAudit(r, &storage.AuditEntry{
			Action:     "tenant.create",
			TargetType: "tenant",
			TargetID:   t.ID,
			TenantID:   t.ID,
			Details:    fmt.Sprintf("Created tenant %s", t.Name),
			Metadata:   tenantAuditMetadata(t.Name, t.Description, t.ContactName, t.ContactEmail, t.ContactPhone, t.BusinessUnit, t.BillingCode, t.Address, t.LoginDomain),
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleTenantRoute dispatches /api/v1/tenants/{id} and nested subresources like /settings.
func handleTenantRoute(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	tenantID := strings.TrimSpace(parts[0])
	if tenantID == "" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 {
		handleTenantByID(w, r)
		return
	}
	subPath := strings.Trim(parts[1], "/")
	if subPath == "" {
		handleTenantByID(w, r)
		return
	}
	subParts := strings.SplitN(subPath, "/", 2)
	resource := subParts[0]
	remainder := ""
	if len(subParts) == 2 {
		remainder = subParts[1]
	}
	if handler := getTenantSubresourceHandler(resource); handler != nil {
		handler(w, r, tenantID, remainder)
		return
	}
	http.NotFound(w, r)
}

func handleTenantByID(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"tenant id required"}`))
		return
	}
	if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{TenantIDs: []string{id}}) {
		return
	}
	var in tenantPayload
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"name required"}`))
		return
	}
	if dbStore != nil {
		tn, err := dbStore.GetTenant(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"tenant not found"}`))
			return
		}
		before := *tn
		tn.Name = in.Name
		tn.Description = in.Description
		tn.ContactName = in.ContactName
		tn.ContactEmail = in.ContactEmail
		tn.ContactPhone = in.ContactPhone
		tn.BusinessUnit = in.BusinessUnit
		tn.BillingCode = in.BillingCode
		tn.Address = in.Address
		tn.LoginDomain = storage.NormalizeTenantDomain(in.LoginDomain)
		if err := dbStore.UpdateTenant(r.Context(), tn); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to update tenant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tn)
		recordAudit(r, &storage.AuditEntry{
			Action:     "tenant.update",
			TargetType: "tenant",
			TargetID:   tn.ID,
			TenantID:   tn.ID,
			Details:    fmt.Sprintf("Updated tenant %s", tn.Name),
			Metadata: map[string]interface{}{
				"before": tenantAuditMetadata(before.Name, before.Description, before.ContactName, before.ContactEmail, before.ContactPhone, before.BusinessUnit, before.BillingCode, before.Address, before.LoginDomain),
				"after":  tenantAuditMetadata(tn.Name, tn.Description, tn.ContactName, tn.ContactEmail, tn.ContactPhone, tn.BusinessUnit, tn.BillingCode, tn.Address, tn.LoginDomain),
			},
		})
		return
	}
	existing, ok := store.tenants[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"tenant not found"}`))
		return
	}
	updated := Tenant{
		ID:           existing.ID,
		Name:         in.Name,
		Description:  in.Description,
		ContactName:  in.ContactName,
		ContactEmail: in.ContactEmail,
		ContactPhone: in.ContactPhone,
		BusinessUnit: in.BusinessUnit,
		BillingCode:  in.BillingCode,
		Address:      in.Address,
		LoginDomain:  storage.NormalizeTenantDomain(in.LoginDomain),
		CreatedAt:    existing.CreatedAt,
	}
	res, err := store.UpdateTenant(updated)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to update tenant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
	recordAudit(r, &storage.AuditEntry{
		Action:     "tenant.update",
		TargetType: "tenant",
		TargetID:   res.ID,
		TenantID:   res.ID,
		Details:    fmt.Sprintf("Updated tenant %s", res.Name),
		Metadata: map[string]interface{}{
			"before": tenantAuditMetadata(existing.Name, existing.Description, existing.ContactName, existing.ContactEmail, existing.ContactPhone, existing.BusinessUnit, existing.BillingCode, existing.Address, existing.LoginDomain),
			"after":  tenantAuditMetadata(res.Name, res.Description, res.ContactName, res.ContactEmail, res.ContactPhone, res.BusinessUnit, res.BillingCode, res.Address, res.LoginDomain),
		},
	})
}

func handleTenantBundlesSubresource(w http.ResponseWriter, r *http.Request, tenantID, rest string) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		http.NotFound(w, r)
		return
	}
	if strings.TrimSpace(rest) != "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if dbStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "installer bundles unavailable")
		return
	}
	if !authorizeOrReject(w, r, authz.ActionPackagesGenerate, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
		return
	}
	limit := parseBundleListLimit(r.URL.Query().Get("limit"))
	bundles, err := dbStore.ListInstallerBundles(r.Context(), tenantID, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list bundles")
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host
	response := make([]map[string]interface{}, 0, len(bundles))
	for _, bundle := range bundles {
		if bundle == nil {
			continue
		}
		item := map[string]interface{}{
			"id":           bundle.ID,
			"tenant_id":    bundle.TenantID,
			"component":    bundle.Component,
			"version":      bundle.Version,
			"platform":     bundle.Platform,
			"arch":         bundle.Arch,
			"format":       bundle.Format,
			"size_bytes":   bundle.SizeBytes,
			"created_at":   bundle.CreatedAt,
			"expires_at":   bundle.ExpiresAt,
			"expired":      bundleExpired(bundle),
			"download_url": fmt.Sprintf("%s/api/v1/packages/%d/download", baseURL, bundle.ID),
		}
		if meta, err := decodeBundleMetadata(bundle.MetadataJSON); err == nil && len(meta) > 0 {
			item["metadata"] = meta
		}
		response = append(response, item)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleCreateJoinToken issues a join token. Body: {"tenant_id":"...","ttl_minutes":60,"one_time":false}
func handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		TenantID   string `json:"tenant_id"`
		TTLMinutes int    `json:"ttl_minutes"`
		OneTime    bool   `json:"one_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		pkgLogger.Warn("create-join-token: invalid JSON", "remote_addr", r.RemoteAddr, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if pkgLogger != nil {
		pkgLogger.Info("create-join-token: request received", "tenant_id", in.TenantID, "ttl_minutes", in.TTLMinutes, "one_time", in.OneTime, "remote_addr", r.RemoteAddr)
	}
	if in.TTLMinutes <= 0 {
		in.TTLMinutes = 60
	}
	resource := authz.ResourceRef{}
	if strings.TrimSpace(in.TenantID) != "" {
		resource.TenantIDs = []string{strings.TrimSpace(in.TenantID)}
	}
	if !authorizeOrReject(w, r, authz.ActionJoinTokensWrite, resource) {
		return
	}
	if dbStore != nil {
		jt, raw, err := dbStore.CreateJoinToken(r.Context(), in.TenantID, in.TTLMinutes, in.OneTime)
		if err != nil {
			// Map storage errors to HTTP codes
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      raw,
			"tenant_id":  jt.TenantID,
			"expires_at": jt.ExpiresAt.Format(time.RFC3339),
		})
		recordAudit(r, &storage.AuditEntry{
			Action:     "join_token.create",
			TargetType: "join_token",
			TargetID:   jt.ID,
			TenantID:   jt.TenantID,
			Details:    fmt.Sprintf("Join token created for tenant %s", jt.TenantID),
			Metadata: map[string]interface{}{
				"ttl_minutes": in.TTLMinutes,
				"one_time":    in.OneTime,
				"expires_at":  jt.ExpiresAt.Format(time.RFC3339),
			},
		})
		return
	}
	jt, err := store.CreateJoinToken(in.TenantID, in.TTLMinutes, in.OneTime)
	if err != nil {
		if err == ErrTenantNotFound {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"tenant not found"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to create token"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      jt.Token,
		"tenant_id":  jt.TenantID,
		"expires_at": jt.ExpiresAt.Format(time.RFC3339),
	})
	recordAudit(r, &storage.AuditEntry{
		Action:     "join_token.create",
		TargetType: "join_token",
		TargetID:   maskTokenValue(jt.Token),
		TenantID:   jt.TenantID,
		Details:    fmt.Sprintf("Join token created for tenant %s", jt.TenantID),
		Metadata: map[string]interface{}{
			"ttl_minutes": in.TTLMinutes,
			"one_time":    in.OneTime,
			"expires_at":  jt.ExpiresAt.Format(time.RFC3339),
		},
	})
}

// handleListJoinTokens returns a list of join tokens for a tenant. Query param: tenant_id
func handleListJoinTokens(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"tenant_id required"}`))
		return
	}
	if !authorizeOrReject(w, r, authz.ActionJoinTokensRead, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
		return
	}
	if dbStore != nil {
		list, err := dbStore.ListJoinTokens(r.Context(), tenantID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to list tokens"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
		return
	}
	// fallback: filter in-memory store
	all := make([]JoinToken, 0)
	for _, jt := range store.tokens {
		if jt.TenantID == tenantID {
			all = append(all, jt)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(all)
}

// handleRevokeJoinToken revokes a token by id. Body: {"id":"..."}
func handleRevokeJoinToken(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionJoinTokensWrite, authz.ResourceRef{}) {
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if in.ID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"id required"}`))
		return
	}
	if dbStore != nil {
		if err := dbStore.RevokeJoinToken(r.Context(), in.ID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to revoke token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		recordAudit(r, &storage.AuditEntry{
			Action:     "join_token.revoke",
			TargetType: "join_token",
			TargetID:   in.ID,
			Details:    "Join token revoked",
		})
		return
	}
	// fallback: remove from in-memory store
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.tokens[in.ID]; ok {
		delete(store.tokens, in.ID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		recordAudit(r, &storage.AuditEntry{
			Action:     "join_token.revoke",
			TargetType: "join_token",
			TargetID:   maskTokenValue(in.ID),
			Details:    "Join token revoked",
		})
		return
	}
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"error":"token not found"}`))
}

// handlePendingRegistrations handles GET for listing pending registrations
func handlePendingRegistrations(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionAgentsRead, authz.ResourceRef{}) {
		return
	}
	if dbStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"database not available"}`))
		return
	}
	
	// Optional status filter
	status := r.URL.Query().Get("status")
	
	list, err := dbStore.ListPendingAgentRegistrations(r.Context(), status)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to list pending registrations"}`))
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// handlePendingRegistrationByID handles GET (single), POST (approve/reject), DELETE
func handlePendingRegistrationByID(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if dbStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"database not available"}`))
		return
	}
	
	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pending-registrations/")
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil || id <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid registration id"}`))
		return
	}
	
	switch r.Method {
	case http.MethodGet:
		if !authorizeOrReject(w, r, authz.ActionAgentsRead, authz.ResourceRef{}) {
			return
		}
		reg, err := dbStore.GetPendingAgentRegistration(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"registration not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reg)
		
	case http.MethodPost:
		// Approve or reject
		if !authorizeOrReject(w, r, authz.ActionAgentsWrite, authz.ResourceRef{}) {
			return
		}
		var in struct {
			Action   string `json:"action"` // "approve" or "reject"
			TenantID string `json:"tenant_id,omitempty"` // For approve
			Notes    string `json:"notes,omitempty"`     // For reject
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid json"}`))
			return
		}
		
		username := "admin" // TODO: Extract from auth context when available
		
		switch in.Action {
		case "approve":
			if in.TenantID == "" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"tenant_id required for approval"}`))
				return
			}
			if err := dbStore.ApprovePendingRegistration(r.Context(), id, in.TenantID, username); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"failed to approve registration"}`))
				return
			}
			// Create a new join token for this agent to use
			jt, rawToken, err := dbStore.CreateJoinToken(r.Context(), in.TenantID, 60*24, true) // 24hr one-time token
			if err != nil {
				logWarn("handlePendingRegistrationByID: failed to create approval token", "error", err)
			}
			recordAudit(r, &storage.AuditEntry{
				Action:     "pending_registration.approve",
				TargetType: "pending_registration",
				TargetID:   strconv.FormatInt(id, 10),
				Details:    fmt.Sprintf("Pending registration approved for tenant %s", in.TenantID),
			})
			resp := map[string]interface{}{"success": true}
			if jt != nil {
				resp["join_token"] = rawToken
				resp["token_expires"] = jt.ExpiresAt
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			
		case "reject":
			if err := dbStore.RejectPendingRegistration(r.Context(), id, username, in.Notes); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"failed to reject registration"}`))
				return
			}
			recordAudit(r, &storage.AuditEntry{
				Action:     "pending_registration.reject",
				TargetType: "pending_registration",
				TargetID:   strconv.FormatInt(id, 10),
				Details:    "Pending registration rejected",
			})
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
			
		default:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"action must be 'approve' or 'reject'"}`))
		}
		
	case http.MethodDelete:
		if !authorizeOrReject(w, r, authz.ActionAgentsWrite, authz.ResourceRef{}) {
			return
		}
		if err := dbStore.DeletePendingAgentRegistration(r.Context(), id); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to delete registration"}`))
			return
		}
		recordAudit(r, &storage.AuditEntry{
			Action:     "pending_registration.delete",
			TargetType: "pending_registration",
			TargetID:   strconv.FormatInt(id, 10),
			Details:    "Pending registration deleted",
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleRegisterWithToken accepts {"token":"...","agent_id":"..."}
// Validates token and returns a placeholder agent token and tenant assignment.
func handleRegisterWithToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Token           string `json:"token"`
		AgentID         string `json:"agent_id"`
		Name            string `json:"name,omitempty"`
		AgentVersion    string `json:"agent_version,omitempty"`
		ProtocolVersion string `json:"protocol_version,omitempty"`
		Hostname        string `json:"hostname,omitempty"`
		IP              string `json:"ip,omitempty"`
		Platform        string `json:"platform,omitempty"`
		OSVersion       string `json:"os_version,omitempty"`
		GoVersion       string `json:"go_version,omitempty"`
		Architecture    string `json:"architecture,omitempty"`
		NumCPU          int    `json:"num_cpu,omitempty"`
		TotalMemoryMB   int64  `json:"total_memory_mb,omitempty"`
		BuildType       string `json:"build_type,omitempty"`
		GitCommit       string `json:"git_commit,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if pkgLogger != nil {
			pkgLogger.Warn("register-with-token: invalid JSON", "remote_addr", r.RemoteAddr, "error", err)
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if pkgLogger != nil {
		pkgLogger.Info("register-with-token: request received", "agent_id", in.AgentID, "name", in.Name, "hostname", in.Hostname, "platform", in.Platform, "version", in.AgentVersion, "token_length", len(in.Token), "remote_addr", r.RemoteAddr)
	}
	if in.Token == "" || in.AgentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"token and agent_id required"}`))
		return
	}
	if dbStore != nil {
		if pkgLogger != nil {
			pkgLogger.Debug("register-with-token: validating with dbStore", "agent_id", in.AgentID)
		}
		jt, err := dbStore.ValidateJoinToken(r.Context(), in.Token)
		if err != nil {
			// Check if this is an expired (but known) token - we can capture these for admin review
			if storage.IsExpiredToken(err) {
				var tve *storage.TokenValidationError
				if errors.As(err, &tve) && tve.TenantID != "" {
					if pkgLogger != nil {
						pkgLogger.Info("register-with-token: capturing expired token registration", 
							"agent_id", in.AgentID, "expired_tenant_id", tve.TenantID, "token_id", tve.TokenID)
					}
					// Create pending registration for admin review
					pending := &storage.PendingAgentRegistration{
						AgentID:         in.AgentID,
						Name:            in.Name,
						Hostname:        in.Hostname,
						IP:              r.RemoteAddr,
						Platform:        in.Platform,
						AgentVersion:    in.AgentVersion,
						ProtocolVersion: in.ProtocolVersion,
						ExpiredTokenID:  tve.TokenID,
						ExpiredTenantID: tve.TenantID,
						Status:          storage.PendingStatusPending,
					}
					if _, createErr := dbStore.CreatePendingAgentRegistration(r.Context(), pending); createErr != nil {
						if pkgLogger != nil {
							pkgLogger.Warn("register-with-token: failed to create pending registration", "error", createErr)
						}
					}
					recordAudit(r, &storage.AuditEntry{
						ActorType: storage.AuditActorAgent,
						ActorID:   strings.TrimSpace(in.AgentID),
						ActorName: strings.TrimSpace(in.Name),
						Action:    "agent.register.pending",
						Severity:  storage.AuditSeverityInfo,
						Details:   "Agent registration captured: expired token - pending admin review",
						Metadata: map[string]interface{}{
							"token_prefix":      maskTokenValue(in.Token),
							"hostname":          strings.TrimSpace(in.Hostname),
							"platform":          strings.TrimSpace(in.Platform),
							"expired_tenant_id": tve.TenantID,
						},
					})
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error":"token expired - registration pending admin approval"}`))
					return
				}
			}
			
			// Unknown or invalid token - don't capture, just reject
			if pkgLogger != nil {
				pkgLogger.Warn("register-with-token: token validation failed", "agent_id", in.AgentID, "error", err, "remote_addr", r.RemoteAddr)
			}
			recordAudit(r, &storage.AuditEntry{
				ActorType: storage.AuditActorAgent,
				ActorID:   strings.TrimSpace(in.AgentID),
				ActorName: strings.TrimSpace(in.Name),
				Action:    "agent.register.token",
				Severity:  storage.AuditSeverityWarn,
				Details:   "Agent registration denied: invalid or unknown token",
				Metadata: map[string]interface{}{
					"token_prefix": maskTokenValue(in.Token),
					"hostname":     strings.TrimSpace(in.Hostname),
					"platform":     strings.TrimSpace(in.Platform),
				},
			})
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid or expired token"}`))
			return
		}

		if pkgLogger != nil {
			pkgLogger.Info("register-with-token: token validated successfully", "agent_id", in.AgentID, "tenant_id", jt.TenantID, "token_id", jt.ID)
		}

		// Create or update agent in server DB with tenant assignment and issue a secure token
		// Generate secure random token (256 bits -> base64url)
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to generate agent token"}`))
			return
		}
		token := base64.URLEncoding.EncodeToString(b)

		// Persist agent with tenant assignment using storage.Store.RegisterAgent
		ag := &storage.Agent{
			AgentID:         in.AgentID,
			Name:            in.Name,
			Hostname:        in.Hostname,
			IP:              in.IP,
			Platform:        in.Platform,
			Version:         in.AgentVersion,
			Token:           token,
			RegisteredAt:    time.Now().UTC(),
			LastSeen:        time.Now().UTC(),
			Status:          "active",
			OSVersion:       in.OSVersion,
			GoVersion:       in.GoVersion,
			Architecture:    in.Architecture,
			NumCPU:          in.NumCPU,
			TotalMemoryMB:   in.TotalMemoryMB,
			BuildType:       in.BuildType,
			GitCommit:       in.GitCommit,
			ProtocolVersion: in.ProtocolVersion,
			TenantID:        jt.TenantID,
		}
		if err := dbStore.RegisterAgent(r.Context(), ag); err != nil {
			if pkgLogger != nil {
				pkgLogger.Error("register-with-token: failed to register agent", "agent_id", in.AgentID, "error", err)
			}
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to register agent"}`))
			return
		}

		if pkgLogger != nil {
			pkgLogger.Info("register-with-token: agent registered successfully", "agent_id", ag.AgentID, "tenant_id", ag.TenantID, "name", ag.Name)
		}

		emitAgentEvent("agent_registered", ag)

		resp := map[string]interface{}{
			"success":     true,
			"tenant_id":   jt.TenantID,
			"agent_token": token,
		}
		attachAgentSettings(resp, r.Context(), jt.TenantID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		recordAudit(r, &storage.AuditEntry{
			ActorType: storage.AuditActorAgent,
			ActorID:   in.AgentID,
			ActorName: in.Name,
			TenantID:  jt.TenantID,
			Action:    "agent.register.token",
			Details:   "Agent registered via join token",
			Metadata: map[string]interface{}{
				"tenant_id":        jt.TenantID,
				"protocol_version": strings.TrimSpace(in.ProtocolVersion),
				"platform":         strings.TrimSpace(in.Platform),
				"hostname":         strings.TrimSpace(in.Hostname),
				"agent_version":    strings.TrimSpace(in.AgentVersion),
			},
		})
		return
	}

	jt, err := store.ValidateToken(in.Token)
	if err != nil {
		if err == ErrTokenNotFound || err == ErrTokenExpired {
			recordAudit(r, &storage.AuditEntry{
				ActorType: storage.AuditActorAgent,
				ActorID:   strings.TrimSpace(in.AgentID),
				ActorName: strings.TrimSpace(in.Name),
				Action:    "agent.register.token",
				Severity:  storage.AuditSeverityWarn,
				Details:   "Agent registration denied: invalid or expired join token",
				Metadata: map[string]interface{}{
					"token_prefix": maskTokenValue(in.Token),
					"hostname":     strings.TrimSpace(in.Hostname),
					"platform":     strings.TrimSpace(in.Platform),
				},
			})
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid or expired token"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"token validation failed"}`))
		return
	}

	// For non-DB (in-memory) fallback, generate a secure token and return it
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to generate agent token"}`))
		return
	}
	placeholder := base64.URLEncoding.EncodeToString(b)
	now := time.Now().UTC()
	emitAgentEvent("agent_registered", &storage.Agent{
		AgentID:         in.AgentID,
		Name:            in.Name,
		Hostname:        in.Hostname,
		IP:              in.IP,
		Platform:        in.Platform,
		Version:         in.AgentVersion,
		ProtocolVersion: in.ProtocolVersion,
		Status:          "active",
		RegisteredAt:    now,
		LastSeen:        now,
		OSVersion:       in.OSVersion,
		GoVersion:       in.GoVersion,
		Architecture:    in.Architecture,
		NumCPU:          in.NumCPU,
		TotalMemoryMB:   in.TotalMemoryMB,
		BuildType:       in.BuildType,
		GitCommit:       in.GitCommit,
		TenantID:        jt.TenantID,
	})
	resp := map[string]interface{}{
		"success":     true,
		"tenant_id":   jt.TenantID,
		"agent_token": placeholder,
	}
	attachAgentSettings(resp, r.Context(), jt.TenantID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	recordAudit(r, &storage.AuditEntry{
		ActorType: storage.AuditActorAgent,
		ActorID:   in.AgentID,
		ActorName: in.Name,
		TenantID:  jt.TenantID,
		Action:    "agent.register.token",
		Details:   "Agent registered via join token",
		Metadata: map[string]interface{}{
			"tenant_id":        jt.TenantID,
			"protocol_version": strings.TrimSpace(in.ProtocolVersion),
			"platform":         strings.TrimSpace(in.Platform),
			"hostname":         strings.TrimSpace(in.Hostname),
			"agent_version":    strings.TrimSpace(in.AgentVersion),
		},
	})
}

func emitAgentEvent(eventType string, agent *storage.Agent) {
	if agentEventSink == nil || agent == nil {
		return
	}
	registeredAt := agent.RegisteredAt
	if registeredAt.IsZero() {
		registeredAt = time.Now().UTC()
	}
	lastSeen := agent.LastSeen
	if lastSeen.IsZero() {
		lastSeen = registeredAt
	}
	payload := map[string]interface{}{
		"agent_id":         agent.AgentID,
		"name":             agent.Name,
		"hostname":         agent.Hostname,
		"ip":               agent.IP,
		"platform":         agent.Platform,
		"version":          agent.Version,
		"protocol_version": agent.ProtocolVersion,
		"status":           agent.Status,
		"registered_at":    registeredAt,
		"last_seen":        lastSeen,
		"connection_type":  "none",
	}
	if agent.TenantID != "" {
		payload["tenant_id"] = agent.TenantID
	}
	if agent.OSVersion != "" {
		payload["os_version"] = agent.OSVersion
	}
	if agent.Architecture != "" {
		payload["architecture"] = agent.Architecture
	}
	if agent.BuildType != "" {
		payload["build_type"] = agent.BuildType
	}
	if agent.GitCommit != "" {
		payload["git_commit"] = agent.GitCommit
	}
	if agent.GoVersion != "" {
		payload["go_version"] = agent.GoVersion
	}
	if agent.NumCPU > 0 {
		payload["num_cpu"] = agent.NumCPU
	}
	if agent.TotalMemoryMB > 0 {
		payload["total_memory_mb"] = agent.TotalMemoryMB
	}
	agentEventSink(eventType, payload)
}

// handleGeneratePackage creates a bootstrap package/script for an agent to
// download and register using a join token. Request (POST) JSON:
// {"tenant_id":"...","platform":"linux|windows|darwin","installer_type":"script|archive","ttl_minutes":10}
// Response: attachment (script) or JSON with download_url depending on request.
func handleGeneratePackage(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in packageRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	in.TenantID = strings.TrimSpace(in.TenantID)
	if in.TenantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"tenant_id required"}`))
		return
	}
	if in.TTLMinutes <= 0 {
		in.TTLMinutes = 10
	}
	platform := normalizePlatform(in.Platform)
	installerType := strings.ToLower(strings.TrimSpace(in.InstallerType))
	if installerType == "" {
		installerType = "script"
	}
	if !authorizeOrReject(w, r, authz.ActionPackagesGenerate, authz.ResourceRef{TenantIDs: []string{in.TenantID}}) {
		return
	}

	var tenantRecord *storage.Tenant
	if dbStore != nil {
		tenant, err := dbStore.GetTenant(r.Context(), in.TenantID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"tenant not found"}`))
			return
		}
		tenantRecord = tenant
	} else {
		store.mu.Lock()
		_, ok := store.tenants[in.TenantID]
		store.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"tenant not found"}`))
			return
		}
	}

	var rawToken string
	var tokenMeta joinTokenInfo
	if dbStore != nil {
		jt, rt, err := dbStore.CreateJoinToken(r.Context(), in.TenantID, in.TTLMinutes, true)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create token"}`))
			return
		}
		rawToken = rt
		if jt != nil {
			tokenMeta = joinTokenInfo{ID: strings.TrimSpace(jt.ID), ExpiresAt: jt.ExpiresAt, OneTime: jt.OneTime}
		}
	} else {
		jt, err := store.CreateJoinToken(in.TenantID, in.TTLMinutes, true)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create token"}`))
			return
		}
		rawToken = jt.Token
		tokenMeta = joinTokenInfo{ExpiresAt: jt.ExpiresAt, OneTime: jt.OneTime}
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	serverURL := scheme + "://" + r.Host

	recordAudit(r, &storage.AuditEntry{
		Action:     "package.generate",
		TargetType: "install_package",
		TargetID:   in.TenantID,
		TenantID:   in.TenantID,
		Details:    "Bootstrap package generated",
		Metadata: map[string]interface{}{
			"platform":       platform,
			"installer_type": installerType,
			"format":         strings.ToLower(strings.TrimSpace(in.Format)),
			"ttl_minutes":    in.TTLMinutes,
		},
	})

	switch installerType {
	case "script":
		script, filename := buildBootstrapScript(platform, serverURL, rawToken)
		if script == "" || filename == "" {
			writeJSONError(w, http.StatusInternalServerError, "unable to build bootstrap script")
			return
		}
		code := randomHex(12)
		oneTimeDownload := true
		if inOneTime, ok := r.URL.Query()["one_time_download"]; ok && len(inOneTime) > 0 {
			value := strings.ToLower(strings.TrimSpace(inOneTime[0]))
			if value == "false" || value == "0" {
				oneTimeDownload = false
			}
		}
		expiresAt := time.Now().UTC().Add(time.Duration(in.TTLMinutes) * time.Minute)
		installStore.mu.Lock()
		installStore.m[code] = installEntry{Script: script, Filename: filename, ExpiresAt: expiresAt, OneTime: oneTimeDownload}
		installStore.mu.Unlock()

		downloadURL := fmt.Sprintf("%s/install/%s/%s", serverURL, code, filename)
		w.Header().Set("Content-Type", "application/json")
		oneLiner := fmt.Sprintf("curl -fsSL %q | sh", downloadURL)
		if platform == "windows" {
			oneLiner = fmt.Sprintf("irm %q | iex", downloadURL)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"script":       script,
			"filename":     filename,
			"download_url": downloadURL,
			"one_liner":    oneLiner,
		})
		return
	case "archive":
		if dbStore == nil || installerPackager == nil || tenantRecord == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "installer packaging unavailable")
			return
		}
		bundle, metadata, err := generateInstallerBundle(r.Context(), r, in, tenantRecord, platform, rawToken, serverURL, tokenMeta)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp := map[string]interface{}{
			"bundle_id":    bundle.ID,
			"tenant_id":    bundle.TenantID,
			"component":    bundle.Component,
			"version":      bundle.Version,
			"platform":     bundle.Platform,
			"arch":         bundle.Arch,
			"format":       bundle.Format,
			"size_bytes":   bundle.SizeBytes,
			"expires_at":   bundle.ExpiresAt,
			"download_url": fmt.Sprintf("%s/api/v1/packages/%d/download", serverURL, bundle.ID),
		}
		if len(metadata) > 0 {
			resp["metadata"] = metadata
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported installer_type")
		return
	}
}

// handlePackageRoute dispatches bundle metadata and download requests under /api/v1/packages/{id}
func handlePackageRoute(w http.ResponseWriter, r *http.Request) {
	if !requireTenancyEnabled(w, r) {
		return
	}
	if dbStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "installer bundles unavailable")
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/packages/"), "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid package id")
		return
	}
	bundle, err := dbStore.GetInstallerBundle(r.Context(), id)
	if err != nil || bundle == nil {
		http.NotFound(w, r)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionPackagesGenerate, authz.ResourceRef{TenantIDs: []string{bundle.TenantID}}) {
		return
	}
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		respondWithBundleMetadata(w, r, bundle)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(parts[1]))
	switch sub {
	case "download":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		serveInstallerBundle(w, r, bundle)
		return
	default:
		http.NotFound(w, r)
		return
	}
}

func respondWithBundleMetadata(w http.ResponseWriter, r *http.Request, bundle *storage.InstallerBundle) {
	if bundle == nil {
		http.NotFound(w, r)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host
	expired := bundleExpired(bundle)
	resp := map[string]interface{}{
		"bundle_id":    bundle.ID,
		"tenant_id":    bundle.TenantID,
		"component":    bundle.Component,
		"version":      bundle.Version,
		"platform":     bundle.Platform,
		"arch":         bundle.Arch,
		"format":       bundle.Format,
		"size_bytes":   bundle.SizeBytes,
		"expires_at":   bundle.ExpiresAt,
		"expired":      expired,
		"download_url": fmt.Sprintf("%s/api/v1/packages/%d/download", baseURL, bundle.ID),
	}
	if meta, err := decodeBundleMetadata(bundle.MetadataJSON); err == nil && len(meta) > 0 {
		resp["metadata"] = meta
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func serveInstallerBundle(w http.ResponseWriter, r *http.Request, bundle *storage.InstallerBundle) {
	if bundle == nil {
		http.NotFound(w, r)
		return
	}
	if bundleExpired(bundle) {
		writeJSONError(w, http.StatusGone, "installer bundle expired")
		return
	}
	if installerPackager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "installer packager unavailable")
		return
	}
	handle, err := installerPackager.OpenBundle(r.Context(), bundle)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	defer handle.Close()
	filename := handle.Name()
	if strings.TrimSpace(filename) == "" {
		filename = filepath.Base(bundle.BundlePath)
	}
	if strings.TrimSpace(filename) == "" {
		filename = fmt.Sprintf("installer-%d", bundle.ID)
	}
	modTime := handle.ModTime()
	if modTime.IsZero() {
		modTime = bundle.UpdatedAt
	}
	w.Header().Set("Content-Type", contentTypeForFormat(bundle.Format))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, modTime, handle)
	recordAudit(r, &storage.AuditEntry{
		Action:     "package.download",
		TargetType: "install_package",
		TargetID:   bundle.TenantID,
		TenantID:   bundle.TenantID,
		Details:    "Installer bundle downloaded",
		Metadata: map[string]interface{}{
			"bundle_id": bundle.ID,
			"format":    bundle.Format,
			"component": bundle.Component,
			"version":   bundle.Version,
		},
	})
}

func contentTypeForFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		return "application/zip"
	case "tar.gz", "tgz":
		return "application/gzip"
	case "msi":
		return "application/x-msi"
	default:
		return "application/octet-stream"
	}
}

// handleInstall serves hosted install scripts by short code. URL: /install/{code}/{filename}
func handleInstall(w http.ResponseWriter, r *http.Request) {
	// expect path /install/{code}/{filename}
	p := strings.TrimPrefix(r.URL.Path, "/install/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	code := parts[0]

	installStore.mu.Lock()
	entry, ok := installStore.m[code]
	// If one-time, remove immediately (we'll serve below)
	if ok && entry.OneTime {
		delete(installStore.m, code)
	}
	installStore.mu.Unlock()
	if !ok || time.Now().UTC().After(entry.ExpiresAt) {
		http.NotFound(w, r)
		return
	}

	// Serve script with appropriate content-type based on filename
	contentType := "text/plain; charset=utf-8"
	if strings.HasSuffix(entry.Filename, ".sh") {
		contentType = "application/x-sh"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+entry.Filename+"\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write([]byte(entry.Script))
}

// installCleanupLoop periodically removes expired install entries.
func installCleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().UTC()
		installStore.mu.Lock()
		for k, v := range installStore.m {
			if now.After(v.ExpiresAt) {
				delete(installStore.m, k)
			}
		}
		installStore.mu.Unlock()
	}
}

func generateInstallerBundle(ctx context.Context, r *http.Request, req packageRequest, tenant *storage.Tenant, platform, rawToken, serverURL string, tokenMeta joinTokenInfo) (*storage.InstallerBundle, map[string]interface{}, error) {
	if installerPackager == nil {
		return nil, nil, fmt.Errorf("installer packager unavailable")
	}
	if tenant == nil {
		return nil, nil, fmt.Errorf("tenant context required")
	}
	resolvedPlatform := normalizePlatform(platform)
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = defaultFormatForPlatform(resolvedPlatform)
	}
	if format == "" {
		return nil, nil, fmt.Errorf("installer format required")
	}
	component := strings.TrimSpace(req.Component)
	if component == "" {
		component = "agent"
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = defaultBundleVersion()
	}
	if version == "" {
		return nil, nil, fmt.Errorf("version required")
	}
	arch := normalizeArch(req.Arch, resolvedPlatform)
	if arch == "" {
		return nil, nil, fmt.Errorf("arch required")
	}
	ttl := time.Duration(req.TTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	effectiveServer := strings.TrimSpace(serverURL)
	generatedAt := time.Now().UTC()
	overlayMeta := map[string]interface{}{
		"tenant_id":           tenant.ID,
		"tenant_name":         tenant.Name,
		"platform":            resolvedPlatform,
		"arch":                arch,
		"format":              format,
		"component":           component,
		"version":             version,
		"server_url":          effectiveServer,
		"generated_at":        generatedAt,
		"server_version":      defaultBundleVersion(),
		"masked_join_token":   maskTokenValue(rawToken),
		"join_token_one_time": tokenMeta.OneTime,
	}
	if tokenMeta.ID != "" {
		overlayMeta["join_token_id"] = tokenMeta.ID
	}
	if !tokenMeta.ExpiresAt.IsZero() {
		overlayMeta["join_token_expires_at"] = tokenMeta.ExpiresAt
	}
	if r != nil {
		if addr := strings.TrimSpace(r.RemoteAddr); addr != "" {
			overlayMeta["request_ip"] = addr
		}
		if ua := strings.TrimSpace(r.UserAgent()); ua != "" {
			overlayMeta["user_agent"] = ua
		}
	}
	overlays, err := buildOverlayFiles(effectiveServer, rawToken, overlayMeta)
	if err != nil {
		return nil, nil, err
	}
	requestMetadata := map[string]interface{}{
		"tenant_id":   tenant.ID,
		"tenant_name": tenant.Name,
		"platform":    resolvedPlatform,
		"arch":        arch,
		"format":      format,
		"component":   component,
		"version":     version,
		"server_url":  effectiveServer,
	}
	if tokenMeta.ID != "" {
		requestMetadata["join_token_id"] = tokenMeta.ID
	}
	if !tokenMeta.ExpiresAt.IsZero() {
		requestMetadata["join_token_expires_at"] = tokenMeta.ExpiresAt
	}
	requestMetadata["join_token_one_time"] = tokenMeta.OneTime
	bundle, err := installerPackager.BuildInstaller(ctx, packager.BuildRequest{
		TenantID:     tenant.ID,
		Component:    component,
		Version:      version,
		Platform:     resolvedPlatform,
		Arch:         arch,
		Format:       format,
		OverlayFiles: overlays,
		Metadata:     requestMetadata,
		TTL:          ttl,
	})
	if err != nil {
		return nil, nil, err
	}
	metadata, _ := decodeBundleMetadata(bundle.MetadataJSON)
	return bundle, metadata, nil
}

func buildOverlayFiles(serverURL, rawToken string, meta map[string]interface{}) ([]packager.OverlayFile, error) {
	config := buildBootstrapConfig(serverURL, rawToken)
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	files := []packager.OverlayFile{
		{Path: "config/bootstrap.toml", Mode: 0o600, Data: config},
		{Path: "config/metadata.json", Mode: 0o640, Data: metaBytes},
	}
	return files, nil
}

func buildBootstrapConfig(serverURL, token string) []byte {
	var b strings.Builder
	b.WriteString("[server]\n")
	b.WriteString("enabled = true\n")
	b.WriteString(fmt.Sprintf("url = %q\n", serverURL))
	b.WriteString("name = \"\"\n")
	b.WriteString(fmt.Sprintf("token = %q\n", token))
	b.WriteString("insecure_skip_verify = true\n")
	return []byte(b.String())
}

func buildBootstrapScript(platform, serverURL, token string) (string, string) {
	switch normalizePlatform(platform) {
	case "windows":
		return fmt.Sprintf(windowsBootstrapScript, serverURL, token), "install.ps1"
	default:
		return fmt.Sprintf(unixBootstrapScript, serverURL, token), "install.sh"
	}
}

const windowsBootstrapScript = `# PowerShell bootstrap for PrintMaster
$ErrorActionPreference = "Stop"
$server = "%s"
$token = "%s"

function Assert-Administrator {
	$current = [Security.Principal.WindowsIdentity]::GetCurrent()
	$principal = New-Object Security.Principal.WindowsPrincipal($current)
	if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
		Write-Error "This installer must be run from an elevated PowerShell session."
		exit 1
	}
}

function Set-RelaxedCertificatePolicy {
	try {
		[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12
		[System.Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
	} catch {
		Write-Warning "Unable to relax certificate validation: $_"
	}
}

Assert-Administrator
Set-RelaxedCertificatePolicy

$programFiles = ${env:ProgramFiles}
if ([string]::IsNullOrWhiteSpace($programFiles)) {
	$programFiles = "C:\\Program Files"
}
$programData = ${env:ProgramData}
if ([string]::IsNullOrWhiteSpace($programData)) {
	$programData = "C:\\ProgramData"
}

$agentDir = Join-Path $programFiles "PrintMaster"
$agentExe = Join-Path $agentDir "printmaster-agent.exe"
$dataRoot = Join-Path $programData "PrintMaster"
$configDir = Join-Path $dataRoot "agent"
$configPath = Join-Path $configDir "config.toml"

Write-Host "Preparing directories..."
New-Item -ItemType Directory -Force -Path $agentDir | Out-Null
New-Item -ItemType Directory -Force -Path $configDir | Out-Null

Write-Host "Downloading agent binary..."
try {
	$downloadParams = @{
			Uri = "$server/api/v1/agents/download/latest?platform=windows&arch=amd64&proxy=1"
		OutFile = $agentExe
		ErrorAction = 'Stop'
	}
	try {
		$invokeCmd = Get-Command Invoke-WebRequest -ErrorAction Stop
		if ($invokeCmd.Parameters.Keys -contains 'UseBasicParsing') {
			$downloadParams.UseBasicParsing = $true
		}
		if ($invokeCmd.Parameters.Keys -contains 'SkipCertificateCheck') {
			$downloadParams.SkipCertificateCheck = $true
		}
	} catch {
		# Fall back to relaxed certificate policy only
	}
	Invoke-WebRequest @downloadParams
} catch {
	Write-Error "Failed to download agent: $_"
	exit 1
}

if (-not (Test-Path $agentExe)) {
	Write-Error "Agent binary missing after download."
	exit 1
}

try {
	Unblock-File -Path $agentExe -ErrorAction SilentlyContinue
} catch {
	# Ignore if Unblock-File is unavailable
}

$agentName = $env:COMPUTERNAME
if ([string]::IsNullOrWhiteSpace($agentName)) {
	$agentName = "windows-agent"
}

Write-Host "Writing configuration to $configPath"
$configContent = @"
[server]
enabled = true
url = "$server"
name = "$agentName"
token = "$token"
insecure_skip_verify = true
"@
Set-Content -Path $configPath -Value $configContent -Encoding UTF8

Write-Host "Installing PrintMaster Agent service..."
& $agentExe --service install --quiet
if ($LASTEXITCODE -ne 0) {
	Write-Error "Service installation failed with exit code $LASTEXITCODE"
	exit $LASTEXITCODE
}

Write-Host "Starting PrintMaster Agent service..."
& $agentExe --service start --quiet
if ($LASTEXITCODE -ne 0) {
	Write-Warning "Service installed but failed to start (exit code $LASTEXITCODE). Use 'Get-Service PrintMasterAgent' for status."
} else {
	Write-Host "PrintMaster Agent service is running."
	Write-Host "Configuration: $configPath"
	Write-Host "Logs:        $(Join-Path $configDir 'logs')"
}
`

const unixBootstrapScript = `#!/bin/sh
SERVER="%s"
TOKEN="%s"
set -e
echo "Downloading agent..."
curl -fsSL "$SERVER/api/v1/agents/download/latest?proxy=1" -o /usr/local/bin/pm-agent || exit 1
chmod +x /usr/local/bin/pm-agent
mkdir -p /etc/printmaster
cat > /etc/printmaster/pm-config.json <<EOF
{"server_url":"$SERVER","join_token":"$TOKEN"}
EOF
# Try to install systemd unit if available (best-effort)
if command -v systemctl >/dev/null 2>&1; then
	cat >/etc/systemd/system/printmaster-agent.service <<EOL
[Unit]
Description=PrintMaster Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/pm-agent --config /etc/printmaster/pm-config.json
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOL
	systemctl daemon-reload || true
	systemctl enable --now printmaster-agent || true
else
	/usr/local/bin/pm-agent --config /etc/printmaster/pm-config.json &
fi
`

func normalizePlatform(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "win", "windows", "windows_nt":
		return "windows"
	case "mac", "darwin", "osx":
		return "darwin"
	case "linux", "":
		return "linux"
	default:
		return strings.ToLower(strings.TrimSpace(input))
	}
}

func normalizeArch(input, platform string) string {
	arch := strings.ToLower(strings.TrimSpace(input))
	switch arch {
	case "", "x86_64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	case "armv7":
		arch = "armv7"
	}
	if arch == "" {
		switch normalizePlatform(platform) {
		case "darwin":
			arch = "arm64"
		default:
			arch = "amd64"
		}
	}
	return arch
}

func defaultFormatForPlatform(platform string) string {
	switch normalizePlatform(platform) {
	case "windows":
		return "zip"
	case "darwin":
		return "tar.gz"
	default:
		return "tar.gz"
	}
}

func defaultBundleVersion() string {
	version := strings.TrimSpace(serverVersion)
	if version != "" && !isDevVersion(version) {
		return version
	}
	for _, candidate := range versionFileCandidates {
		if data, err := os.ReadFile(candidate); err == nil {
			fileVersion := strings.TrimSpace(string(data))
			if fileVersion != "" {
				return fileVersion
			}
		}
	}
	return version
}

func isDevVersion(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "dev", "development", "dirty", "local":
		return true
	default:
		return false
	}
}

func parseBundleListLimit(raw string) int {
	const (
		defaultLimit = 50
		maxLimit     = 200
	)
	if strings.TrimSpace(raw) == "" {
		return defaultLimit
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return defaultLimit
	}
	if value > maxLimit {
		return maxLimit
	}
	return value
}

func bundleExpired(bundle *storage.InstallerBundle) bool {
	if bundle == nil {
		return false
	}
	return !bundle.ExpiresAt.IsZero() && time.Now().UTC().After(bundle.ExpiresAt)
}

func decodeBundleMetadata(raw string) (map[string]interface{}, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// handleAgentDownloadLatest redirects to the latest compatible agent binary
// on GitHub Releases. Query params accepted: ?platform=linux|windows|darwin&arch=amd64|arm64
// If server version was supplied by main via SetServerVersion, that is used;
// otherwise the handler attempts to read `server/VERSION` from the working
// directory. If no version can be determined, a 404 is returned.
func handleAgentDownloadLatest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	platform := strings.ToLower(q.Get("platform"))
	arch := strings.ToLower(q.Get("arch"))
	proxyParam := strings.ToLower(q.Get("proxy"))
	proxyDownload := proxyParam == "1" || proxyParam == "true" || proxyParam == "yes"
	if platform == "" {
		platform = "linux"
	}
	if arch == "" {
		arch = "amd64"
	}
	switch platform {
	case "win", "windows", "windows_nt":
		platform = "windows"
	case "mac", "darwin", "osx":
		platform = "darwin"
	default:
		platform = "linux"
	}

	ver := serverVersion
	if ver == "" {
		if b, err := os.ReadFile("server/VERSION"); err == nil {
			ver = strings.TrimSpace(string(b))
		}
	}
	if ver == "" {
		http.Error(w, "server version unknown", http.StatusNotFound)
		return
	}

	tag := ver
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	releaseTag := "agent-" + tag

	ext := ""
	if platform == "windows" {
		ext = ".exe"
	}

	asset := fmt.Sprintf("printmaster-agent-%s-%s-%s%s", tag, platform, arch, ext)
	redirectURL := fmt.Sprintf("%s/%s/%s", releaseAssetBaseURL, releaseTag, asset)

	if !proxyDownload {
		// Use 302/Found to allow capable clients to follow to GitHub directly.
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Fall back to proxying the download through the server for older clients
	// (notably legacy PowerShell) that cannot negotiate GitHub's TLS/SNI
	// requirements.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, redirectURL, nil)
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", fmt.Sprintf("printmaster-server/%s", ver))
	resp, err := releaseDownloadClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream download failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream responded with %s", resp.Status), http.StatusBadGateway)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", asset))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// We cannot change the response at this point; best-effort copy only.
		return
	}
}
