package releases

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authz "printmaster/server/authz"
	"printmaster/server/storage"
)

type stubManager struct {
	keys        []*storage.SigningKey
	manifests   []*storage.ReleaseManifest
	rotateNotes string
	rotateKey   *storage.SigningKey
	regenCount  int
}

func (s *stubManager) ListSigningKeys(ctx context.Context, limit int) ([]*storage.SigningKey, error) {
	return s.keys, nil
}

func (s *stubManager) RotateSigningKey(ctx context.Context, notes string) (*storage.SigningKey, error) {
	s.rotateNotes = notes
	return s.rotateKey, nil
}

func (s *stubManager) RegenerateManifests(ctx context.Context) (int, error) {
	return s.regenCount, nil
}

func (s *stubManager) ListManifests(ctx context.Context, component string, limit int) ([]*storage.ReleaseManifest, error) {
	return s.manifests, nil
}

func (s *stubManager) GetManifest(ctx context.Context, component, version, platform, arch string) (*storage.ReleaseManifest, error) {
	if len(s.manifests) == 0 {
		return nil, sql.ErrNoRows
	}
	return s.manifests[0], nil
}

func TestReleaseAPIListSigningKeys(t *testing.T) {
	mgr := &stubManager{
		keys: []*storage.SigningKey{
			{ID: "k1", Algorithm: "ed25519", PublicKey: "pub", Active: true, CreatedAt: time.Unix(0, 0)},
		},
	}
	api := NewAPI(mgr, APIOptions{
		AuthMiddleware: func(next http.HandlerFunc) http.HandlerFunc { return next },
		Authorizer:     func(*http.Request, authz.Action, authz.ResourceRef) error { return nil },
	})
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/releases/signing-keys", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("k1")) {
		t.Fatalf("response missing key id")
	}
}

func TestReleaseAPIRotateSigningKey(t *testing.T) {
	mgr := &stubManager{
		rotateKey:  &storage.SigningKey{ID: "k2", Algorithm: "ed25519", PublicKey: "pub2", CreatedAt: time.Unix(10, 0)},
		regenCount: 3,
	}
	api := NewAPI(mgr, APIOptions{
		AuthMiddleware: func(next http.HandlerFunc) http.HandlerFunc { return next },
		Authorizer:     func(*http.Request, authz.Action, authz.ResourceRef) error { return nil },
	})
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	payload := []byte(`{"notes":"rotate","regenerate_manifests":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/releases/signing-keys/rotate", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if mgr.rotateNotes != "rotate" {
		t.Fatalf("notes not forwarded to manager")
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("regenerated_count")) {
		t.Fatalf("response missing regenerated count")
	}
}
