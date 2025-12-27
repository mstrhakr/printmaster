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
	u := &User{Username: "tester", Role: RoleOperator, Email: "t@example.com"}
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

func TestDeleteSessionByHashWithCount(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a user
	u := &User{Username: "tester", Role: RoleOperator, Email: "t@example.com"}
	if err := s.CreateUser(ctx, u, "password123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create a session
	ses, err := s.CreateSession(ctx, u.ID, 60)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Get the stored hash from ListSessions
	list, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var storedHash string
	for _, ss := range list {
		if ss.UserID == u.ID {
			storedHash = ss.Token
			break
		}
	}
	if storedHash == "" {
		t.Fatalf("expected to find stored hash")
	}

	// Delete with count - should return 1
	count, err := s.DeleteSessionByHashWithCount(ctx, storedHash)
	if err != nil {
		t.Fatalf("DeleteSessionByHashWithCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count=1, got=%d", count)
	}

	// Verify session is gone
	if _, err := s.GetSessionByToken(ctx, ses.Token); err == nil {
		t.Fatalf("expected session to be deleted")
	}

	// Delete again - should return 0
	count, err = s.DeleteSessionByHashWithCount(ctx, storedHash)
	if err != nil {
		t.Fatalf("DeleteSessionByHashWithCount (second): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected count=0 on second delete, got=%d", count)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a user
	u := &User{Username: "tester", Role: RoleOperator, Email: "t@example.com"}
	if err := s.CreateUser(ctx, u, "password123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create an expired session
	_, err = s.CreateSession(ctx, u.ID, -1) // Already expired
	if err != nil {
		t.Fatalf("CreateSession expired: %v", err)
	}

	// Create a valid session
	validSes, err := s.CreateSession(ctx, u.ID, 60)
	if err != nil {
		t.Fatalf("CreateSession valid: %v", err)
	}

	// List all sessions (should include expired one since ListSessions doesn't filter)
	list, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}

	// Delete expired sessions
	count, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 expired session deleted, got %d", count)
	}

	// Verify only valid session remains
	list, err = s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after cleanup: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session after cleanup, got %d", len(list))
	}

	// Verify valid session is still accessible
	if _, err := s.GetSessionByToken(ctx, validSes.Token); err != nil {
		t.Fatalf("expected valid session to still exist: %v", err)
	}
}
