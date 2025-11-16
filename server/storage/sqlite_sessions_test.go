package storage

import (
	"context"
	"testing"
)

func TestSessionLifecycle(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a user
	u := &User{Username: "tester", Role: "user", Email: "t@example.com"}
	if err := s.CreateUser(ctx, u, "password123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Fatalf("expected user ID to be set")
	}

	// Create a session
	ses, err := s.CreateSession(ctx, u.ID, 60)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if ses.Token == "" {
		t.Fatalf("expected non-empty token")
	}

	// Retrieve by raw token
	got, err := s.GetSessionByToken(ctx, ses.Token)
	if err != nil {
		t.Fatalf("GetSessionByToken: %v", err)
	}
	if got.UserID != u.ID {
		t.Fatalf("unexpected user id: got=%d want=%d", got.UserID, u.ID)
	}

	// List sessions and ensure username is present
	list, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected at least one session in list")
	}
	var found bool
	var storedHash string
	for _, ss := range list {
		if ss.UserID == u.ID {
			found = true
			storedHash = ss.Token
			if ss.Username != "tester" {
				t.Fatalf("expected username in session list, got=%q", ss.Username)
			}
			break
		}
	}
	if !found {
		t.Fatalf("created session not found in ListSessions")
	}
	if storedHash == "" {
		t.Fatalf("expected stored hash present")
	}

	// Revoke via DeleteSession (uses raw token)
	if err := s.DeleteSession(ctx, ses.Token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSessionByToken(ctx, ses.Token); err == nil {
		t.Fatalf("expected session to be deleted")
	}

	// Create another session and revoke by stored hash
	ses2, err := s.CreateSession(ctx, u.ID, 60)
	if err != nil {
		t.Fatalf("CreateSession2: %v", err)
	}
	list2, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after create: %v", err)
	}
	var hash2 string
	for _, ss := range list2 {
		if ss.UserID == u.ID {
			hash2 = ss.Token
			break
		}
	}
	if hash2 == "" {
		t.Fatalf("expected to find stored hash for new session")
	}
	if err := s.DeleteSessionByHash(ctx, hash2); err != nil {
		t.Fatalf("DeleteSessionByHash: %v", err)
	}
	if _, err := s.GetSessionByToken(ctx, ses2.Token); err == nil {
		t.Fatalf("expected session2 to be deleted")
	}

	// Expired session should be rejected and removed
	ses3, err := s.CreateSession(ctx, u.ID, -1) // negative ttl -> already expired
	if err != nil {
		t.Fatalf("CreateSession expired: %v", err)
	}
	if _, err := s.GetSessionByToken(ctx, ses3.Token); err == nil {
		t.Fatalf("expected expired session to be invalid")
	}
}
