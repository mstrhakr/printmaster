package tenancy

import (
	"encoding/json"
	"net/http"
	"strings"

	authz "printmaster/server/authz"
	"printmaster/server/storage"
)

func init() {
	// Register the sites subresource handler: /api/v1/tenants/{id}/sites
	RegisterTenantSubresource("sites", handleSitesSubresource)
}

// handleSitesSubresource handles /api/v1/tenants/{tenantID}/sites[/{siteID}[/...]]
func handleSitesSubresource(w http.ResponseWriter, r *http.Request, tenantID string, rest string) {
	if !requireTenancyEnabled(w, r) {
		return
	}

	// Parse the rest of the path
	rest = strings.Trim(rest, "/")

	if rest == "" {
		// /api/v1/tenants/{id}/sites - list or create
		handleSitesCollection(w, r, tenantID)
		return
	}

	// /api/v1/tenants/{id}/sites/{siteID}[/...]
	parts := strings.SplitN(rest, "/", 2)
	siteID := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}

	if subPath == "" {
		// /api/v1/tenants/{id}/sites/{siteID}
		handleSiteByID(w, r, tenantID, siteID)
		return
	}

	if subPath == "agents" {
		// /api/v1/tenants/{id}/sites/{siteID}/agents
		handleSiteAgents(w, r, tenantID, siteID)
		return
	}

	http.NotFound(w, r)
}

// handleSitesCollection handles GET (list) and POST (create) on /api/v1/tenants/{id}/sites
func handleSitesCollection(w http.ResponseWriter, r *http.Request, tenantID string) {
	switch r.Method {
	case http.MethodGet:
		// List sites for this tenant
		if !authorizeOrReject(w, r, authz.ActionTenantsRead, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}
		sites, err := dbStore.ListSitesByTenant(r.Context(), tenantID)
		if err != nil {
			logError("Failed to list sites", "tenant_id", tenantID, "error", err)
			http.Error(w, `{"error":"failed to list sites"}`, http.StatusInternalServerError)
			return
		}

		// Enrich with agent counts
		for _, site := range sites {
			agentIDs, _ := dbStore.GetSiteAgentIDs(r.Context(), site.ID)
			site.AgentCount = len(agentIDs)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sites)

	case http.MethodPost:
		// Create a new site
		if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}

		var payload struct {
			Name        string                   `json:"name"`
			Description string                   `json:"description"`
			Address     string                   `json:"address"`
			FilterRules []storage.SiteFilterRule `json:"filter_rules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.Name) == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}

		site := &storage.Site{
			TenantID:    tenantID,
			Name:        strings.TrimSpace(payload.Name),
			Description: strings.TrimSpace(payload.Description),
			Address:     strings.TrimSpace(payload.Address),
			FilterRules: payload.FilterRules,
		}

		if err := dbStore.CreateSite(r.Context(), site); err != nil {
			logError("Failed to create site", "tenant_id", tenantID, "error", err)
			http.Error(w, `{"error":"failed to create site"}`, http.StatusInternalServerError)
			return
		}

		logInfo("Site created", "site_id", site.ID, "tenant_id", tenantID, "name", site.Name)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(site)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleSiteByID handles GET, PUT, DELETE on /api/v1/tenants/{id}/sites/{siteID}
func handleSiteByID(w http.ResponseWriter, r *http.Request, tenantID, siteID string) {
	switch r.Method {
	case http.MethodGet:
		if !authorizeOrReject(w, r, authz.ActionTenantsRead, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}
		site, err := dbStore.GetSite(r.Context(), siteID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if site.TenantID != tenantID {
			http.NotFound(w, r)
			return
		}

		// Enrich with agent IDs
		agentIDs, _ := dbStore.GetSiteAgentIDs(r.Context(), site.ID)
		site.AgentCount = len(agentIDs)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(site)

	case http.MethodPut:
		if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}

		// Verify site exists and belongs to tenant
		existing, err := dbStore.GetSite(r.Context(), siteID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if existing.TenantID != tenantID {
			http.NotFound(w, r)
			return
		}

		var payload struct {
			Name        string                   `json:"name"`
			Description string                   `json:"description"`
			Address     string                   `json:"address"`
			FilterRules []storage.SiteFilterRule `json:"filter_rules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.Name) == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}

		existing.Name = strings.TrimSpace(payload.Name)
		existing.Description = strings.TrimSpace(payload.Description)
		existing.Address = strings.TrimSpace(payload.Address)
		existing.FilterRules = payload.FilterRules

		if err := dbStore.UpdateSite(r.Context(), existing); err != nil {
			logError("Failed to update site", "site_id", siteID, "error", err)
			http.Error(w, `{"error":"failed to update site"}`, http.StatusInternalServerError)
			return
		}

		logInfo("Site updated", "site_id", siteID, "tenant_id", tenantID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(existing)

	case http.MethodDelete:
		if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}

		// Verify site exists and belongs to tenant
		existing, err := dbStore.GetSite(r.Context(), siteID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if existing.TenantID != tenantID {
			http.NotFound(w, r)
			return
		}

		if err := dbStore.DeleteSite(r.Context(), siteID); err != nil {
			logError("Failed to delete site", "site_id", siteID, "error", err)
			http.Error(w, `{"error":"failed to delete site"}`, http.StatusInternalServerError)
			return
		}

		logInfo("Site deleted", "site_id", siteID, "tenant_id", tenantID)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleSiteAgents handles GET and PUT on /api/v1/tenants/{id}/sites/{siteID}/agents
func handleSiteAgents(w http.ResponseWriter, r *http.Request, tenantID, siteID string) {
	// Verify site exists and belongs to tenant
	site, err := dbStore.GetSite(r.Context(), siteID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if site.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !authorizeOrReject(w, r, authz.ActionTenantsRead, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}
		agentIDs, err := dbStore.GetSiteAgentIDs(r.Context(), siteID)
		if err != nil {
			logError("Failed to get site agents", "site_id", siteID, "error", err)
			http.Error(w, `{"error":"failed to get agents"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"site_id":   siteID,
			"agent_ids": agentIDs,
		})

	case http.MethodPut:
		// Replace all agent assignments for this site
		if !authorizeOrReject(w, r, authz.ActionTenantsWrite, authz.ResourceRef{TenantIDs: []string{tenantID}}) {
			return
		}

		var payload struct {
			AgentIDs []string `json:"agent_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}

		// Update each agent's site assignments
		// First, get current agents assigned to this site
		currentAgentIDs, _ := dbStore.GetSiteAgentIDs(r.Context(), siteID)

		// Find agents to remove
		newAgentSet := make(map[string]bool)
		for _, id := range payload.AgentIDs {
			newAgentSet[id] = true
		}
		for _, agentID := range currentAgentIDs {
			if !newAgentSet[agentID] {
				dbStore.UnassignAgentFromSite(r.Context(), agentID, siteID)
			}
		}

		// Add new agents
		currentSet := make(map[string]bool)
		for _, id := range currentAgentIDs {
			currentSet[id] = true
		}
		for _, agentID := range payload.AgentIDs {
			if !currentSet[agentID] {
				dbStore.AssignAgentToSite(r.Context(), agentID, siteID)
			}
		}

		logInfo("Site agents updated", "site_id", siteID, "tenant_id", tenantID, "agent_count", len(payload.AgentIDs))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"site_id":   siteID,
			"agent_ids": payload.AgentIDs,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
