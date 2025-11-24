package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	authz "printmaster/server/authz"
	"printmaster/server/storage"
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

func tenantAuditMetadata(name, description, contactName, contactEmail, contactPhone, businessUnit, billingCode, address string) map[string]interface{} {
	return map[string]interface{}{
		"name":          name,
		"description":   description,
		"contact_name":  contactName,
		"contact_email": contactEmail,
		"contact_phone": contactPhone,
		"business_unit": businessUnit,
		"billing_code":  billingCode,
		"address":       address,
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
	// Package generation (bootstrap script / archive) - admin only
	mux.HandleFunc("/api/v1/packages", wrap(handleGeneratePackage))
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
	if !requireTenancyEnabled(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !authorizeOrReject(w, r, authz.ActionTenantsRead, authz.ResourceRef{}) {
			return
		}
		if dbStore != nil {
			list, err := dbStore.ListTenants(r.Context())
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"failed to list tenants"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
			return
		}
		list := store.ListTenants()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	case http.MethodPost:
		if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{}) {
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
			}
			if tn.ID == "" {
				// Let storage layer generate ID via SQL default
			}
			if err := dbStore.CreateTenant(r.Context(), tn); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"failed to create tenant"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tn)
			recordAudit(r, &storage.AuditEntry{
				Action:     "tenant.create",
				TargetType: "tenant",
				TargetID:   tn.ID,
				TenantID:   tn.ID,
				Details:    fmt.Sprintf("Created tenant %s", tn.Name),
				Metadata:   tenantAuditMetadata(tn.Name, tn.Description, tn.ContactName, tn.ContactEmail, tn.ContactPhone, tn.BusinessUnit, tn.BillingCode, tn.Address),
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
			Metadata:   tenantAuditMetadata(t.Name, t.Description, t.ContactName, t.ContactEmail, t.ContactPhone, t.BusinessUnit, t.BillingCode, t.Address),
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
				"before": tenantAuditMetadata(before.Name, before.Description, before.ContactName, before.ContactEmail, before.ContactPhone, before.BusinessUnit, before.BillingCode, before.Address),
				"after":  tenantAuditMetadata(tn.Name, tn.Description, tn.ContactName, tn.ContactEmail, tn.ContactPhone, tn.BusinessUnit, tn.BillingCode, tn.Address),
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
			"before": tenantAuditMetadata(existing.Name, existing.Description, existing.ContactName, existing.ContactEmail, existing.ContactPhone, existing.BusinessUnit, existing.BillingCode, existing.Address),
			"after":  tenantAuditMetadata(res.Name, res.Description, res.ContactName, res.ContactEmail, res.ContactPhone, res.BusinessUnit, res.BillingCode, res.Address),
		},
	})
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
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
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
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if in.Token == "" || in.AgentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"token and agent_id required"}`))
		return
	}
	if dbStore != nil {
		jt, err := dbStore.ValidateJoinToken(r.Context(), in.Token)
		if err != nil {
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
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to register agent"}`))
			return
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
	var in struct {
		TenantID      string `json:"tenant_id"`
		Platform      string `json:"platform"`
		InstallerType string `json:"installer_type"` // script or archive
		TTLMinutes    int    `json:"ttl_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if in.TTLMinutes <= 0 {
		in.TTLMinutes = 10
	}
	if !authorizeOrReject(w, r, authz.ActionPackagesGenerate, authz.ResourceRef{TenantIDs: []string{strings.TrimSpace(in.TenantID)}}) {
		return
	}
	// Ensure tenant exists
	if dbStore != nil {
		if _, err := dbStore.GetTenant(r.Context(), in.TenantID); err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"tenant not found"}`))
			return
		}
	} else {
		// In-memory fallback - check tenants map under lock
		store.mu.Lock()
		_, ok := store.tenants[in.TenantID]
		store.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"tenant not found"}`))
			return
		}
	}

	// Create a short-lived one-time join token for the package
	var rawToken string
	if dbStore != nil {
		if _, rt, err := dbStore.CreateJoinToken(r.Context(), in.TenantID, in.TTLMinutes, true); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create token"}`))
			return
		} else {
			rawToken = rt
		}
	} else {
		jt, err := store.CreateJoinToken(in.TenantID, in.TTLMinutes, true)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create token"}`))
			return
		}
		rawToken = jt.Token
	}

	// Build server URL from request
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	serverURL := scheme + "://" + r.Host

	// Provide simple script templates for platforms (script installer MVP)
	installerType := strings.ToLower(in.InstallerType)
	if installerType == "" {
		installerType = "script"
	}
	platform := strings.ToLower(in.Platform)
	recordAudit(r, &storage.AuditEntry{
		Action:     "package.generate",
		TargetType: "install_package",
		TargetID:   strings.TrimSpace(in.TenantID),
		TenantID:   strings.TrimSpace(in.TenantID),
		Details:    "Bootstrap package generated",
		Metadata: map[string]interface{}{
			"platform":       platform,
			"installer_type": installerType,
			"ttl_minutes":    in.TTLMinutes,
		},
	})

	if installerType == "script" {
		var script string
		filename := "bootstrap"
		switch platform {
		case "windows", "win", "windows_nt":
			filename = "install.ps1"
			pwTemplate := `# PowerShell bootstrap for PrintMaster
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
			script = fmt.Sprintf(pwTemplate, serverURL, rawToken)
		default:
			// linux / darwin
			filename = "install.sh"
			shTemplate := `#!/bin/sh
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
			script = fmt.Sprintf(shTemplate, serverURL, rawToken)
		}

		// Create a short-lived install code and store the script for hosting
		code := randomHex(12) // 24 hex chars
		oneTimeDownload := true
		if inOneTime, ok := r.URL.Query()["one_time_download"]; ok && len(inOneTime) > 0 {
			// allow override via query param (string values like "false")
			if strings.ToLower(inOneTime[0]) == "false" || strings.ToLower(inOneTime[0]) == "0" {
				oneTimeDownload = false
			}
		}
		installStore.mu.Lock()
		installStore.m[code] = installEntry{Script: script, Filename: filename, ExpiresAt: time.Now().UTC().Add(time.Duration(in.TTLMinutes) * time.Minute), OneTime: oneTimeDownload}
		installStore.mu.Unlock()

		downloadURL := fmt.Sprintf("%s/install/%s/%s", serverURL, code, filename)

		// Respond with JSON containing script and hosted URL for convenience
		w.Header().Set("Content-Type", "application/json")
		// Provide a short one-line command that admins can paste into a shell
		// to fetch and execute the hosted install script. For Windows we emit
		// an Invoke-RestMethod/Invoke-Expression pattern (`irm <url> | iex`) and
		// for Unix-like systems we emit `curl -fsSL <url> | sh`.
		oneLiner := ""
		switch platform {
		case "windows":
			oneLiner = fmt.Sprintf("irm %q | iex", downloadURL)
		default:
			oneLiner = fmt.Sprintf("curl -fsSL %q | sh", downloadURL)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"script":       script,
			"filename":     filename,
			"download_url": downloadURL,
			"one_liner":    oneLiner,
		})
		return
	}

	// For archive type or others, simply respond not implemented for MVP
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error":"archive generation not implemented yet"}`))
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
