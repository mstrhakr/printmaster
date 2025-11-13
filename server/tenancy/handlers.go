package tenancy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"printmaster/server/storage"
)

// RegisterRoutes registers HTTP handlers for tenancy endpoints.
// dbStore, when set via RegisterRoutes, will be used for persistence. If nil,
// the package in-memory `store` is used (keeps tests and backwards compatibility).
var dbStore storage.Store

// RegisterRoutes registers HTTP handlers for tenancy endpoints. If a
// non-nil storage.Store is provided, handlers will persist tenants and tokens
// in the server DB; otherwise the in-memory store is used.
func RegisterRoutes(s storage.Store) {
	dbStore = s
	http.HandleFunc("/api/v1/tenants", handleTenants)
	http.HandleFunc("/api/v1/join-token", handleCreateJoinToken)
	http.HandleFunc("/api/v1/agents/register-with-token", handleRegisterWithToken)
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

// handleRegisterWithToken accepts {"token":"...","agent_id":"..."}
// Validates token and returns a placeholder agent token and tenant assignment.
func handleRegisterWithToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Token   string `json:"token"`
		AgentID string `json:"agent_id"`
		Name    string `json:"name,omitempty"`
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

		// Create or update agent in server DB with tenant assignment and issue placeholder token
		// Generate placeholder agent token
		placeholder := "agent_token_" + strconv.FormatInt(time.Now().Unix(), 10)

		// Persist agent with tenant assignment using storage.Store.RegisterAgent
		ag := &storage.Agent{
			AgentID:         in.AgentID,
			Name:            in.Name,
			Token:           placeholder,
			RegisteredAt:    time.Now().UTC(),
			LastSeen:        time.Now().UTC(),
			Status:          "active",
			ProtocolVersion: "1",
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
			"agent_token": placeholder,
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

	// For now, we don't persist the agent with tenant to DB from here. Instead,
	// return the tenant assignment and a placeholder bearer token the agent can
	// use. Later this handler should create the agent record in server DB and
	// return a real token.

	placeholder := "agent_token_" + strconv.FormatInt(time.Now().Unix(), 10)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"tenant_id":   jt.TenantID,
		"agent_token": placeholder,
	})
}
