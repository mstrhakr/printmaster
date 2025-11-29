package main

import (
	"net/http"
	"strings"
	"time"

	"printmaster/server/storage"
)

const (
	tenantHintCookieName   = "pm_tenant_hint"
	tenantHintCookieMaxAge = 90 * 24 * time.Hour
)

// rememberUserTenantHint stores the user's primary tenant ID in a scoped cookie so the
// login page can automatically pre-select the right tenant during the next visit.
func rememberUserTenantHint(w http.ResponseWriter, r *http.Request, user *storage.User) {
	if user == nil {
		setTenantHintCookie(w, r, "")
		return
	}
	tenantID := strings.TrimSpace(primaryTenantID(user))
	setTenantHintCookie(w, r, tenantID)
}

func primaryTenantID(user *storage.User) string {
	if user == nil {
		return ""
	}
	if len(user.TenantIDs) > 0 {
		return strings.TrimSpace(user.TenantIDs[0])
	}
	return strings.TrimSpace(user.TenantID)
}

func setTenantHintCookie(w http.ResponseWriter, r *http.Request, tenantID string) {
	secure := requestIsHTTPS(r)
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		http.SetCookie(w, &http.Cookie{
			Name:     tenantHintCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     tenantHintCookieName,
		Value:    tenantID,
		Path:     "/",
		Expires:  time.Now().Add(tenantHintCookieMaxAge),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
