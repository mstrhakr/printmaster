package tenancy

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"printmaster/server/storage"
)

// RegisterRoutes registers HTTP handlers for tenancy endpoints.
// dbStore, when set via RegisterRoutes, will be used for persistence. If nil,
// the package in-memory `store` is used (keeps tests and backwards compatibility).
var dbStore storage.Store

// AuthMiddleware, when set by the main application, will be used to wrap
// tenancy handlers so they can enforce authentication/authorization.
// Set to nil to leave routes unprotected (not recommended).
var AuthMiddleware func(http.HandlerFunc) http.HandlerFunc

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
	mux.HandleFunc("/api/v1/join-token", wrap(handleCreateJoinToken))
	mux.HandleFunc("/api/v1/agents/register-with-token", handleRegisterWithToken) // registration must remain public
	mux.HandleFunc("/api/v1/join-tokens", wrap(handleListJoinTokens))             // GET (admin)
	mux.HandleFunc("/api/v1/join-token/revoke", wrap(handleRevokeJoinToken))      // POST {"id":"..."}
}

// handleTenants supports GET (list) and POST (create)
func handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
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
		var in struct {
			ID          string `json:"id,omitempty"`
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid json"}`))
			return
		}
		if dbStore != nil {
			tn := &storage.Tenant{ID: in.ID, Name: in.Name, Description: in.Description}
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
			return
		}
		t, err := store.CreateTenant(in.ID, in.Name, in.Description)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to create tenant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(t)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleCreateJoinToken issues a join token. Body: {"tenant_id":"...","ttl_minutes":60,"one_time":false}
func handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
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
}

// handleListJoinTokens returns a list of join tokens for a tenant. Query param: tenant_id
func handleListJoinTokens(w http.ResponseWriter, r *http.Request) {
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
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
		return
	}
	// fallback: remove from in-memory store
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.tokens[in.ID]; ok {
		delete(store.tokens, in.ID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"tenant_id":   jt.TenantID,
			"agent_token": token,
		})
		return
	}

	jt, err := store.ValidateToken(in.Token)
	if err != nil {
		if err == ErrTokenNotFound || err == ErrTokenExpired {
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"tenant_id":   jt.TenantID,
		"agent_token": placeholder,
	})
}
