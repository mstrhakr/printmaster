package storage

import (
	"context"
	"testing"
)

func TestOIDCProviderLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create OIDC provider
	provider := &OIDCProvider{
		Slug:         "google",
		DisplayName:  "Google",
		Issuer:       "https://accounts.google.com",
		ClientID:     "client-id-123",
		ClientSecret: "client-secret-456",
		Scopes:       []string{"openid", "profile", "email"},
		Icon:         "google-icon",
		ButtonText:   "Sign in with Google",
		AutoLogin:    false,
		DefaultRole:  RoleViewer,
	}

	err = s.CreateOIDCProvider(ctx, provider)
	if err != nil {
		t.Fatalf("CreateOIDCProvider: %v", err)
	}
	if provider.ID == 0 {
		t.Fatal("expected non-zero provider ID")
	}

	// Get provider by slug
	got, err := s.GetOIDCProvider(ctx, "google")
	if err != nil {
		t.Fatalf("GetOIDCProvider: %v", err)
	}
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if got.DisplayName != "Google" {
		t.Errorf("display_name mismatch: got=%q", got.DisplayName)
	}
	if got.Issuer != "https://accounts.google.com" {
		t.Errorf("issuer mismatch: got=%q", got.Issuer)
	}
	if len(got.Scopes) != 3 {
		t.Errorf("scopes count mismatch: got=%d", len(got.Scopes))
	}

	// Update provider
	provider.DisplayName = "Google OAuth"
	provider.AutoLogin = true
	err = s.UpdateOIDCProvider(ctx, provider)
	if err != nil {
		t.Fatalf("UpdateOIDCProvider: %v", err)
	}

	got, _ = s.GetOIDCProvider(ctx, "google")
	if got.DisplayName != "Google OAuth" {
		t.Errorf("display_name not updated: got=%q", got.DisplayName)
	}
	if !got.AutoLogin {
		t.Error("auto_login not updated")
	}

	// Delete provider
	err = s.DeleteOIDCProvider(ctx, "google")
	if err != nil {
		t.Fatalf("DeleteOIDCProvider: %v", err)
	}

	got, err = s.GetOIDCProvider(ctx, "google")
	if err != nil {
		t.Fatalf("GetOIDCProvider after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestListOIDCProviders(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create multiple providers
	providers := []struct {
		slug string
		name string
	}{
		{"google", "Google"},
		{"github", "GitHub"},
		{"microsoft", "Microsoft"},
	}

	for _, p := range providers {
		provider := &OIDCProvider{
			Slug:         p.slug,
			DisplayName:  p.name,
			Issuer:       "https://" + p.slug + ".example.com",
			ClientID:     p.slug + "-client-id",
			ClientSecret: p.slug + "-secret",
			Scopes:       []string{"openid"},
			DefaultRole:  RoleViewer,
		}
		err := s.CreateOIDCProvider(ctx, provider)
		if err != nil {
			t.Fatalf("CreateOIDCProvider %s: %v", p.slug, err)
		}
	}

	// List all providers (empty tenant = global)
	list, err := s.ListOIDCProviders(ctx, "")
	if err != nil {
		t.Fatalf("ListOIDCProviders: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 providers, got %d", len(list))
	}
}

func TestOIDCProviderWithTenant(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create tenant
	tenant := &Tenant{
		ID:   "tenant-oidc",
		Name: "OIDC Test Tenant",
	}
	err = s.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Create provider for tenant
	provider := &OIDCProvider{
		Slug:         "okta-tenant",
		DisplayName:  "Okta",
		Issuer:       "https://okta.example.com",
		ClientID:     "okta-client",
		ClientSecret: "okta-secret",
		Scopes:       []string{"openid", "profile"},
		TenantID:     "tenant-oidc",
		DefaultRole:  RoleOperator,
	}
	err = s.CreateOIDCProvider(ctx, provider)
	if err != nil {
		t.Fatalf("CreateOIDCProvider: %v", err)
	}

	// List providers for tenant
	list, err := s.ListOIDCProviders(ctx, "tenant-oidc")
	if err != nil {
		t.Fatalf("ListOIDCProviders: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 provider for tenant, got %d", len(list))
	}
	if list[0].TenantID != "tenant-oidc" {
		t.Errorf("tenant_id mismatch: got=%q", list[0].TenantID)
	}
}

func TestOIDCSessionLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create provider first
	provider := &OIDCProvider{
		Slug:         "google-sess",
		DisplayName:  "Google",
		Issuer:       "https://accounts.google.com",
		ClientID:     "client-id",
		ClientSecret: "secret",
		Scopes:       []string{"openid"},
		DefaultRole:  RoleViewer,
	}
	err = s.CreateOIDCProvider(ctx, provider)
	if err != nil {
		t.Fatalf("CreateOIDCProvider: %v", err)
	}

	// Create OIDC session
	session := &OIDCSession{
		ID:           "session-123",
		ProviderSlug: "google-sess",
		Nonce:        "nonce-abc",
		State:        "state-xyz",
		RedirectURL:  "https://example.com/callback",
	}

	err = s.CreateOIDCSession(ctx, session)
	if err != nil {
		t.Fatalf("CreateOIDCSession: %v", err)
	}

	// Get session
	got, err := s.GetOIDCSession(ctx, "session-123")
	if err != nil {
		t.Fatalf("GetOIDCSession: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.Nonce != "nonce-abc" {
		t.Errorf("nonce mismatch: got=%q", got.Nonce)
	}
	if got.State != "state-xyz" {
		t.Errorf("state mismatch: got=%q", got.State)
	}

	// Delete session
	err = s.DeleteOIDCSession(ctx, "session-123")
	if err != nil {
		t.Fatalf("DeleteOIDCSession: %v", err)
	}

	got, err = s.GetOIDCSession(ctx, "session-123")
	if err != nil {
		t.Fatalf("GetOIDCSession after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestOIDCLinkLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user
	user := &User{
		Username: "oidcuser",
		Role:     RoleAdmin,
		Email:    "oidcuser@example.com",
	}
	err = s.CreateUser(ctx, user, "password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create provider
	provider := &OIDCProvider{
		Slug:         "google-link",
		DisplayName:  "Google",
		Issuer:       "https://accounts.google.com",
		ClientID:     "client-id",
		ClientSecret: "secret",
		Scopes:       []string{"openid"},
		DefaultRole:  RoleViewer,
	}
	err = s.CreateOIDCProvider(ctx, provider)
	if err != nil {
		t.Fatalf("CreateOIDCProvider: %v", err)
	}

	// Create OIDC link
	link := &OIDCLink{
		ProviderSlug: "google-link",
		Subject:      "google-subject-12345",
		Email:        "oidcuser@gmail.com",
		UserID:       user.ID,
	}

	err = s.CreateOIDCLink(ctx, link)
	if err != nil {
		t.Fatalf("CreateOIDCLink: %v", err)
	}
	if link.ID == 0 {
		t.Fatal("expected non-zero link ID")
	}

	// Get link
	got, err := s.GetOIDCLink(ctx, "google-link", "google-subject-12345")
	if err != nil {
		t.Fatalf("GetOIDCLink: %v", err)
	}
	if got == nil {
		t.Fatal("expected link, got nil")
	}
	if got.UserID != user.ID {
		t.Errorf("user_id mismatch: got=%d", got.UserID)
	}
	if got.Email != "oidcuser@gmail.com" {
		t.Errorf("email mismatch: got=%q", got.Email)
	}

	// List links for user
	links, err := s.ListOIDCLinksForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListOIDCLinksForUser: %v", err)
	}
	if len(links) != 1 {
		t.Errorf("expected 1 link, got %d", len(links))
	}

	// Delete link
	err = s.DeleteOIDCLink(ctx, "google-link", "google-subject-12345")
	if err != nil {
		t.Fatalf("DeleteOIDCLink: %v", err)
	}

	got, err = s.GetOIDCLink(ctx, "google-link", "google-subject-12345")
	if err != nil {
		t.Fatalf("GetOIDCLink after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestMultipleOIDCLinksForUser(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user
	user := &User{
		Username: "multilink",
		Role:     RoleAdmin,
	}
	err = s.CreateUser(ctx, user, "password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create multiple providers
	providers := []string{"google", "github", "microsoft"}
	for _, slug := range providers {
		provider := &OIDCProvider{
			Slug:         slug + "-multi",
			DisplayName:  slug,
			Issuer:       "https://" + slug + ".example.com",
			ClientID:     slug + "-client",
			ClientSecret: slug + "-secret",
			Scopes:       []string{"openid"},
			DefaultRole:  RoleViewer,
		}
		err := s.CreateOIDCProvider(ctx, provider)
		if err != nil {
			t.Fatalf("CreateOIDCProvider %s: %v", slug, err)
		}

		// Link to user
		link := &OIDCLink{
			ProviderSlug: slug + "-multi",
			Subject:      slug + "-subject",
			UserID:       user.ID,
		}
		err = s.CreateOIDCLink(ctx, link)
		if err != nil {
			t.Fatalf("CreateOIDCLink %s: %v", slug, err)
		}
	}

	// List all links for user
	links, err := s.ListOIDCLinksForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListOIDCLinksForUser: %v", err)
	}
	if len(links) != 3 {
		t.Errorf("expected 3 links, got %d", len(links))
	}
}
