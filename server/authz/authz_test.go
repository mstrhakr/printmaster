package authz

import (
	"errors"
	"testing"

	"printmaster/server/storage"
)

func TestAuthorizeRolePolicies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		subject  Subject
		action   Action
		resource ResourceRef
		wantErr  error
	}{
		{
			name: "admin allowed",
			subject: Subject{
				Role:             storage.RoleAdmin,
				AllowedTenantIDs: nil,
				IsAdmin:          true,
			},
			action:   ActionTenantsWrite,
			resource: ResourceRef{},
			wantErr:  nil,
		},
		{
			name: "operator denied",
			subject: Subject{
				Role:             storage.RoleOperator,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionTenantsRead,
			resource: ResourceRef{TenantIDs: []string{"tenant-a"}},
			wantErr:  ErrForbidden,
		},
		{
			name: "viewer denied",
			subject: Subject{
				Role:             storage.RoleViewer,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionJoinTokensRead,
			resource: ResourceRef{TenantIDs: []string{"tenant-a"}},
			wantErr:  ErrForbidden,
		},
		{
			name: "tenant mismatch",
			subject: Subject{
				Role:             storage.RoleAdmin,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionTenantsRead,
			resource: ResourceRef{TenantIDs: []string{"tenant-b"}},
			wantErr:  ErrForbidden,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Authorize(tc.subject, tc.action, tc.resource)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
			}
		})
	}
}
