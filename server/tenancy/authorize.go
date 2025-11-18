package tenancy

import (
	"errors"
	"net/http"

	"printmaster/server/authz"
)

var authorizer func(*http.Request, authz.Action, authz.ResourceRef) error

// SetAuthorizer allows the main server package to wire in its authorization helper.
func SetAuthorizer(fn func(*http.Request, authz.Action, authz.ResourceRef) error) {
	authorizer = fn
}

func authorizeOrReject(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceRef) bool {
	if authorizer == nil {
		http.Error(w, "authorization not configured", http.StatusInternalServerError)
		return false
	}
	if err := authorizer(r, action, resource); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, authz.ErrUnauthorized) {
			status = http.StatusUnauthorized
		}
		http.Error(w, http.StatusText(status), status)
		return false
	}
	return true
}
