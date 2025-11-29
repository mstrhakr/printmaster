package tenancy

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"printmaster/server/storage"
)

// InMemoryStore is a simple, process-local store for tenants and join tokens.
// This is intentionally basic and intended as a safe, non-breaking skeleton for
// implementing tenancy before adding DB-backed persistence.
type InMemoryStore struct {
	mu      sync.Mutex
	tokens  map[string]JoinToken
	tenants map[string]Tenant
}

var store *InMemoryStore

// NewInMemoryStore creates and initializes a new in-memory store
func NewInMemoryStore() *InMemoryStore {
	s := &InMemoryStore{
		tokens:  make(map[string]JoinToken),
		tenants: make(map[string]Tenant),
	}
	// Ensure a default tenant exists
	if _, ok := s.tenants["default"]; !ok {
		s.tenants["default"] = Tenant{ID: "default", Name: "default", CreatedAt: time.Now().UTC()}
	}
	return s
}

func init() {
	store = NewInMemoryStore()
}

// CreateTenant registers a new tenant. If ID is empty a random one is generated.
func (s *InMemoryStore) CreateTenant(t Tenant) (Tenant, error) {
	if t.ID == "" {
		t.ID = randomHex(8)
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	t.LoginDomain = storage.NormalizeTenantDomain(t.LoginDomain)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureUniqueDomainLocked(t.ID, t.LoginDomain); err != nil {
		return Tenant{}, err
	}
	s.tenants[t.ID] = t
	return t, nil
}

// UpdateTenant updates an existing tenant's metadata.
func (s *InMemoryStore) UpdateTenant(t Tenant) (Tenant, error) {
	if t.ID == "" {
		return Tenant{}, ErrTenantNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tenants[t.ID]
	if !ok {
		return Tenant{}, ErrTenantNotFound
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = existing.CreatedAt
	}
	t.LoginDomain = storage.NormalizeTenantDomain(t.LoginDomain)
	if err := s.ensureUniqueDomainLocked(t.ID, t.LoginDomain); err != nil {
		return Tenant{}, err
	}
	s.tenants[t.ID] = t
	return t, nil
}

// ListTenants returns all tenants
func (s *InMemoryStore) ListTenants() []Tenant {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		res = append(res, t)
	}
	return res
}

func (s *InMemoryStore) ensureUniqueDomainLocked(id, domain string) error {
	if domain == "" {
		return nil
	}
	for existingID, existing := range s.tenants {
		if existing.LoginDomain == "" || existingID == id {
			continue
		}
		if existing.LoginDomain == domain {
			return ErrTenantDomainConflict
		}
	}
	return nil
}

// CreateJoinToken issues a new join token for a tenant with ttl in minutes
func (s *InMemoryStore) CreateJoinToken(tenantID string, ttlMinutes int, oneTime bool) (JoinToken, error) {
	// Validate tenant exists
	s.mu.Lock()
	if _, ok := s.tenants[tenantID]; !ok {
		s.mu.Unlock()
		return JoinToken{}, ErrTenantNotFound
	}
	s.mu.Unlock()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return JoinToken{}, err
	}
	token := hex.EncodeToString(b)
	exp := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)
	jt := JoinToken{Token: token, TenantID: tenantID, ExpiresAt: exp, OneTime: oneTime, CreatedAt: time.Now().UTC()}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = jt
	return jt, nil
}

// ValidateToken returns the token if valid. If one-time it is consumed.
func (s *InMemoryStore) ValidateToken(token string) (JoinToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jt, ok := s.tokens[token]
	if !ok {
		return JoinToken{}, ErrTokenNotFound
	}
	if time.Now().UTC().After(jt.ExpiresAt) {
		delete(s.tokens, token)
		return JoinToken{}, ErrTokenExpired
	}
	if jt.OneTime {
		// consume
		delete(s.tokens, token)
	}
	return jt, nil
}

// helper
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "runtoken"
	}
	return hex.EncodeToString(b)
}

// Errors
var (
	ErrTenantNotFound       = &StoreError{"tenant not found"}
	ErrTokenNotFound        = &StoreError{"token not found"}
	ErrTokenExpired         = &StoreError{"token expired"}
	ErrTenantDomainConflict = &StoreError{"tenant login domain already exists"}
)

// StoreError is a simple sentinel error type
type StoreError struct{ msg string }

func (s *StoreError) Error() string { return s.msg }
