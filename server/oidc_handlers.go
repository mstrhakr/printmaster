package main

import (
	context "context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	oidclib "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"printmaster/server/storage"
)

var (
	oidcCache = struct {
		mu        sync.Mutex
		providers map[string]*oidclib.Provider
	}{providers: make(map[string]*oidclib.Provider)}

	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)
)

type oidcClaims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
}

type oidcProviderPayload struct {
	Slug         string   `json:"slug"`
	DisplayName  string   `json:"display_name"`
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes"`
	Icon         string   `json:"icon"`
	ButtonText   string   `json:"button_text"`
	ButtonStyle  string   `json:"button_style"`
	AutoLogin    bool     `json:"auto_login"`
	TenantID     string   `json:"tenant_id"`
	DefaultRole  string   `json:"default_role"`
}

func handleAuthOptions(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))

	providers, err := serverStore.ListOIDCProviders(ctx, "")
	if err != nil {
		http.Error(w, "failed to load providers", http.StatusInternalServerError)
		return
	}

	visible := make([]map[string]interface{}, 0, len(providers))
	for _, p := range providers {
		if tenantID == "" {
			if p.TenantID != "" {
				continue
			}
		} else if p.TenantID != "" && p.TenantID != tenantID {
			continue
		}
		visible = append(visible, publicProviderDTO(p))
	}

	resp := map[string]interface{}{
		"local_login": true,
		"providers":   visible,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleOIDCProviders(w http.ResponseWriter, r *http.Request) {
	principal := getPrincipal(r)
	if principal == nil || !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		ctx := context.Background()
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
		providers, err := serverStore.ListOIDCProviders(ctx, tenantID)
		if err != nil {
			http.Error(w, "failed to list providers", http.StatusInternalServerError)
			return
		}
		list := make([]map[string]interface{}, 0, len(providers))
		for _, p := range providers {
			list = append(list, adminProviderDTO(p))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	case http.MethodPost:
		var payload oidcProviderPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		provider, err := buildProviderFromPayload(&payload, nil, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := context.Background()
		if err := serverStore.CreateOIDCProvider(ctx, provider); err != nil {
			http.Error(w, fmt.Sprintf("failed to create provider: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adminProviderDTO(provider))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleOIDCProvider(w http.ResponseWriter, r *http.Request) {
	principal := getPrincipal(r)
	if principal == nil || !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/sso/providers/")
	slug := strings.Trim(rest, "/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	ctx := context.Background()
	existing, err := serverStore.GetOIDCProvider(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adminProviderDTO(existing))
	case http.MethodPut:
		var payload oidcProviderPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		updated, err := buildProviderFromPayload(&payload, existing, slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := serverStore.UpdateOIDCProvider(ctx, updated); err != nil {
			http.Error(w, fmt.Sprintf("failed to update provider: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adminProviderDTO(updated))
	case http.MethodDelete:
		if err := serverStore.DeleteOIDCProvider(ctx, slug); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete provider: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/auth/oidc/start/")
	slug := strings.Trim(rest, "/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	ctx := context.Background()
	provider, err := serverStore.GetOIDCProvider(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	effectiveTenant := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if provider.TenantID != "" {
		if effectiveTenant == "" {
			effectiveTenant = provider.TenantID
		} else if effectiveTenant != provider.TenantID {
			http.Error(w, "tenant mismatch", http.StatusForbidden)
			return
		}
	}

	redirectURL := sanitizeRedirectTarget(r.URL.Query().Get("redirect"))
	state, err := randomURLSafe(24)
	if err != nil {
		http.Error(w, "failed to create state", http.StatusInternalServerError)
		return
	}
	nonce, err := randomURLSafe(24)
	if err != nil {
		http.Error(w, "failed to create nonce", http.StatusInternalServerError)
		return
	}

	op, err := cachedOIDCProvider(ctx, provider.Issuer)
	if err != nil {
		http.Error(w, "failed to load issuer", http.StatusBadGateway)
		return
	}

	oauthConfig := buildOAuthConfig(r, provider, op)

	sess := &storage.OIDCSession{
		ID:           state,
		ProviderSlug: provider.Slug,
		TenantID:     effectiveTenant,
		Nonce:        nonce,
		State:        state,
		RedirectURL:  redirectURL,
		CreatedAt:    time.Now().UTC(),
	}
	if err := serverStore.CreateOIDCSession(ctx, sess); err != nil {
		http.Error(w, "failed to persist state", http.StatusInternalServerError)
		return
	}

	authURL := oauthConfig.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Redirect(w, r, "/login?error=oidc_invalid", http.StatusFound)
		return
	}

	ctx := context.Background()
	sess, err := serverStore.GetOIDCSession(ctx, state)
	if err != nil {
		http.Redirect(w, r, "/login?error=oidc_state", http.StatusFound)
		return
	}
	defer serverStore.DeleteOIDCSession(ctx, sess.ID)

	provider, err := serverStore.GetOIDCProvider(ctx, sess.ProviderSlug)
	if err != nil {
		http.Redirect(w, r, "/login?error=oidc_provider", http.StatusFound)
		return
	}

	op, err := cachedOIDCProvider(ctx, provider.Issuer)
	if err != nil {
		http.Redirect(w, r, "/login?error=oidc_discovery", http.StatusFound)
		return
	}

	oauthConfig := buildOAuthConfig(r, provider, op)
	token, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		http.Redirect(w, r, "/login?error=oidc_exchange", http.StatusFound)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Redirect(w, r, "/login?error=oidc_token", http.StatusFound)
		return
	}

	verifier := op.Verifier(&oidclib.Config{ClientID: provider.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Redirect(w, r, "/login?error=oidc_verify", http.StatusFound)
		return
	}
	if idToken.Nonce != sess.Nonce {
		http.Redirect(w, r, "/login?error=oidc_nonce", http.StatusFound)
		return
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		http.Redirect(w, r, "/login?error=oidc_claims", http.StatusFound)
		return
	}
	claims.Subject = idToken.Subject

	user, err := resolveOIDCUser(ctx, provider, &claims)
	if err != nil {
		logError("OIDC user resolution failed", "error", err, "provider", provider.Slug)
		http.Redirect(w, r, "/login?error=oidc_user", http.StatusFound)
		return
	}

	if _, err := createSessionCookie(w, r, user.ID); err != nil {
		logError("OIDC session creation failed", "error", err)
		http.Redirect(w, r, "/login?error=oidc_session", http.StatusFound)
		return
	}

	redirectURL := sess.RedirectURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func adminProviderDTO(p *storage.OIDCProvider) map[string]interface{} {
	return map[string]interface{}{
		"slug":         p.Slug,
		"display_name": p.DisplayName,
		"issuer":       p.Issuer,
		"client_id":    p.ClientID,
		"has_secret":   p.ClientSecret != "",
		"scopes":       p.Scopes,
		"icon":         p.Icon,
		"button_text":  p.ButtonText,
		"button_style": p.ButtonStyle,
		"auto_login":   p.AutoLogin,
		"tenant_id":    p.TenantID,
		"default_role": p.DefaultRole,
		"created_at":   p.CreatedAt,
		"updated_at":   p.UpdatedAt,
	}
}

func publicProviderDTO(p *storage.OIDCProvider) map[string]interface{} {
	return map[string]interface{}{
		"slug":         p.Slug,
		"display_name": p.DisplayName,
		"button_text":  firstNonEmpty(p.ButtonText, p.DisplayName),
		"button_style": p.ButtonStyle,
		"icon":         p.Icon,
		"auto_login":   p.AutoLogin,
		"tenant_id":    p.TenantID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func buildProviderFromPayload(payload *oidcProviderPayload, existing *storage.OIDCProvider, forcedSlug string) (*storage.OIDCProvider, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload required")
	}

	result := &storage.OIDCProvider{}
	if existing != nil {
		*result = *existing
	}

	slug := forcedSlug
	if slug == "" {
		slug = strings.ToLower(strings.TrimSpace(payload.Slug))
	}
	if slug == "" {
		slug = slugify(payload.DisplayName)
	}
	slug = strings.TrimSpace(slug)
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("slug must be 3-64 characters (lowercase letters, numbers, hyphen)")
	}
	result.Slug = slug

	display := strings.TrimSpace(payload.DisplayName)
	if display == "" {
		return nil, fmt.Errorf("display_name required")
	}
	result.DisplayName = display

	issuer := strings.TrimSpace(payload.Issuer)
	if existing == nil && issuer == "" {
		return nil, fmt.Errorf("issuer required")
	}
	if issuer != "" {
		result.Issuer = issuer
	}

	clientID := strings.TrimSpace(payload.ClientID)
	if existing == nil && clientID == "" {
		return nil, fmt.Errorf("client_id required")
	}
	if clientID != "" {
		result.ClientID = clientID
	}

	secret := strings.TrimSpace(payload.ClientSecret)
	if secret != "" {
		result.ClientSecret = secret
	} else if existing == nil {
		return nil, fmt.Errorf("client_secret required")
	}

	result.Scopes = normalizeScopes(payload.Scopes)
	result.Icon = strings.TrimSpace(payload.Icon)
	result.ButtonStyle = strings.TrimSpace(payload.ButtonStyle)
	result.ButtonText = firstNonEmpty(strings.TrimSpace(payload.ButtonText), result.DisplayName)
	result.AutoLogin = payload.AutoLogin
	result.TenantID = strings.TrimSpace(payload.TenantID)

	role := strings.ToLower(strings.TrimSpace(payload.DefaultRole))
	if role != "admin" {
		role = "user"
	}
	result.DefaultRole = storage.NormalizeRole(role)

	return result, nil
}

func slugify(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "provider"
	}
	if len(s) < 3 {
		s = fmt.Sprintf("%s-%s", s, "oidc")
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"openid", "profile", "email"}
	}
	set := map[string]struct{}{}
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	set["openid"] = struct{}{}
	ordered := make([]string, 0, len(set))
	if _, ok := set["openid"]; ok {
		ordered = append(ordered, "openid")
		delete(set, "openid")
	}
	for _, candidate := range []string{"profile", "email"} {
		if _, ok := set[candidate]; ok {
			ordered = append(ordered, candidate)
			delete(set, candidate)
		}
	}
	for scope := range set {
		ordered = append(ordered, scope)
	}
	return ordered
}

func cachedOIDCProvider(ctx context.Context, issuer string) (*oidclib.Provider, error) {
	oidcCache.mu.Lock()
	if provider, ok := oidcCache.providers[issuer]; ok {
		oidcCache.mu.Unlock()
		return provider, nil
	}
	oidcCache.mu.Unlock()

	provider, err := oidclib.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	oidcCache.mu.Lock()
	oidcCache.providers[issuer] = provider
	oidcCache.mu.Unlock()
	return provider, nil
}

func buildOAuthConfig(r *http.Request, provider *storage.OIDCProvider, op *oidclib.Provider) *oauth2.Config {
	redirect := buildExternalURL(r) + "/auth/oidc/callback"
	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		RedirectURL:  redirect,
		Endpoint:     op.Endpoint(),
		Scopes:       provider.Scopes,
	}
}

func buildExternalURL(r *http.Request) string {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func sanitizeRedirectTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() {
		return "/"
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func resolveOIDCUser(ctx context.Context, provider *storage.OIDCProvider, claims *oidcClaims) (*storage.User, error) {
	link, err := serverStore.GetOIDCLink(ctx, provider.Slug, claims.Subject)
	if err == nil {
		return serverStore.GetUserByID(ctx, link.UserID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email != "" {
		user, err := serverStore.GetUserByEmail(ctx, email)
		if err == nil {
			_ = serverStore.CreateOIDCLink(ctx, &storage.OIDCLink{ProviderSlug: provider.Slug, Subject: claims.Subject, Email: email, UserID: user.ID})
			return user, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	username := deriveUsername(claims)
	role := provider.DefaultRole
	if role != storage.RoleAdmin && role != storage.RoleOperator {
		role = storage.RoleViewer
	}

	var user *storage.User
	for i := 0; i < 5; i++ {
		candidate := username
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", username, i)
		}
		candidate = strings.Trim(candidate, "-")
		if candidate == "" {
			candidate = fmt.Sprintf("%s-user", provider.Slug)
		}
		tempUser := &storage.User{
			Username:  candidate,
			Role:      role,
			TenantIDs: []string{},
			Email:     email,
		}
		if provider.TenantID != "" {
			tempUser.TenantIDs = []string{provider.TenantID}
			tempUser.TenantID = provider.TenantID
		}
		pass, err := randomURLSafe(18)
		if err != nil {
			return nil, err
		}
		if err := serverStore.CreateUser(ctx, tempUser, pass); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				continue
			}
			return nil, err
		}
		user = tempUser
		break
	}
	if user == nil {
		return nil, fmt.Errorf("failed to create unique username")
	}

	if err := serverStore.CreateOIDCLink(ctx, &storage.OIDCLink{ProviderSlug: provider.Slug, Subject: claims.Subject, Email: email, UserID: user.ID}); err != nil {
		return nil, err
	}

	logInfo("OIDC auto-provisioned user", "username", user.Username, "provider", provider.Slug, "tenant_id", user.TenantID)

	return user, nil
}

func deriveUsername(claims *oidcClaims) string {
	if claims == nil {
		return "user"
	}
	if claims.PreferredUsername != "" {
		return normalizeUsername(claims.PreferredUsername)
	}
	if claims.Email != "" {
		parts := strings.Split(claims.Email, "@")
		return normalizeUsername(parts[0])
	}
	if claims.Name != "" {
		return normalizeUsername(strings.ReplaceAll(claims.Name, " ", "."))
	}
	return "user"
}

func normalizeUsername(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(raw, "")
	if raw == "" {
		return "user"
	}
	if len(raw) > 64 {
		raw = raw[:64]
	}
	return raw
}
