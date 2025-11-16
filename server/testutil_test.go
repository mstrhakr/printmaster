package main

import (
	"context"
	"net/http"

	"printmaster/server/storage"
)

// NewTestAdminUser returns a reusable admin user for tests.
func NewTestAdminUser() *storage.User {
	return &storage.User{ID: 1, Username: "test-admin", Role: "admin"}
}

// InjectTestAdmin attaches a test admin user into the request context and returns
// the modified request.
func InjectTestAdmin(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), userContextKey, NewTestAdminUser())
	return req.WithContext(ctx)
}

// WrapWithAdmin wraps an http handler so that every incoming request has the
// test admin user injected into its context before the handler is invoked.
func WrapWithAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userContextKey, NewTestAdminUser())
		h(w, r.WithContext(ctx))
	}
}
