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
		{
			name: "viewer allowed to read in-scope agent",
			subject: Subject{
				Role:             storage.RoleViewer,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionAgentsRead,
			resource: ResourceRef{TenantIDs: []string{"tenant-a"}},
			wantErr:  nil,
		},
		{
			name: "viewer denied agent write",
			subject: Subject{
				Role:             storage.RoleViewer,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionAgentsWrite,
			resource: ResourceRef{TenantIDs: []string{"tenant-a"}},
			wantErr:  ErrForbidden,
		},
		{
			name: "operator allowed via wildcard",
			subject: Subject{
				Role:             storage.RoleOperator,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionAgentsDelete,
			resource: ResourceRef{TenantIDs: []string{"tenant-a"}},
			wantErr:  nil,
		},
		{
			name: "operator denied admin action",
			subject: Subject{
				Role:             storage.RoleOperator,
				AllowedTenantIDs: []string{"tenant-a"},
			},
			action:   ActionUsersRead,
			resource: ResourceRef{},
			wantErr:  ErrForbidden,
		},
		{
			name: "viewer allowed logs",
			subject: Subject{
				Role: storage.RoleViewer,
			},
			action:   ActionLogsRead,
			resource: ResourceRef{},
			wantErr:  nil,
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
