package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

func workspaceTestScope(orgID, ownerID int, visibility string, pending bool) models.ResourceScope {
	scope := models.ResourceScope{Visibility: visibility}
	if orgID > 0 {
		scope.OrganizationID = null.Int{Int: orgID, Valid: true}
	}
	if ownerID > 0 {
		scope.OwnerUserID = null.Int{Int: ownerID, Valid: true}
	}
	if pending {
		scope.TransferPendingAt.Valid = true
	}
	return scope
}

func TestWorkspaceResourceAccessBoundaries(t *testing.T) {
	c := &Core{}
	personalOwner := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    10,
	}
	otherPersonalUser := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    20,
	}
	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7},
		UserID:    20,
	}
	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, Role: models.OrganizationMemberRoleManager},
		UserID:    30,
	}
	platformAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true, PlatformAdmin: true},
		UserID:    1,
	}
	archivedPlatformAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, PlatformAdmin: true, Archived: true},
		UserID:    1,
	}

	tests := []struct {
		name       string
		access     models.WorkspaceAccess
		scope      models.ResourceScope
		wantRead   bool
		wantManage bool
	}{
		{
			name:       "personal owner manages private resource",
			access:     personalOwner,
			scope:      workspaceTestScope(0, 10, models.ResourceVisibilityPrivate, false),
			wantRead:   true,
			wantManage: true,
		},
		{
			name:       "other personal user cannot read private resource",
			access:     otherPersonalUser,
			scope:      workspaceTestScope(0, 10, models.ResourceVisibilityPrivate, false),
			wantRead:   false,
			wantManage: false,
		},
		{
			name:       "global resource is readable outside owner workspace",
			access:     otherPersonalUser,
			scope:      workspaceTestScope(0, 10, models.ResourceVisibilityGlobal, false),
			wantRead:   true,
			wantManage: false,
		},
		{
			name:       "member reads own organization private resource",
			access:     member,
			scope:      workspaceTestScope(7, 20, models.ResourceVisibilityPrivate, false),
			wantRead:   true,
			wantManage: true,
		},
		{
			name:       "member reads organization shared member resource",
			access:     member,
			scope:      workspaceTestScope(7, 10, models.ResourceVisibilityOrganization, false),
			wantRead:   true,
			wantManage: false,
		},
		{
			name:       "member cannot read another members private resource",
			access:     member,
			scope:      workspaceTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			wantRead:   false,
			wantManage: false,
		},
		{
			name:       "manager can inspect but not change member resource",
			access:     manager,
			scope:      workspaceTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			wantRead:   true,
			wantManage: false,
		},
		{
			name:       "only manager can inspect pending transfer resource",
			access:     member,
			scope:      workspaceTestScope(7, 0, models.ResourceVisibilityOrganization, true),
			wantRead:   false,
			wantManage: false,
		},
		{
			name:       "manager can inspect pending transfer resource",
			access:     manager,
			scope:      workspaceTestScope(7, 0, models.ResourceVisibilityOrganization, true),
			wantRead:   true,
			wantManage: false,
		},
		{
			name:       "platform admin has full access",
			access:     platformAdmin,
			scope:      workspaceTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			wantRead:   true,
			wantManage: true,
		},
		{
			name:       "archived workspace is readable but never writable",
			access:     archivedPlatformAdmin,
			scope:      workspaceTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			wantRead:   true,
			wantManage: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := c.CanReadResource(test.access, test.scope); got != test.wantRead {
				t.Fatalf("CanReadResource() = %v, want %v", got, test.wantRead)
			}
			if got := c.CanManageResource(test.access, test.scope); got != test.wantManage {
				t.Fatalf("CanManageResource() = %v, want %v", got, test.wantManage)
			}
		})
	}
}

func TestOwnerScopedResourceAccessBoundaries(t *testing.T) {
	c := &Core{}
	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7},
		UserID:    20,
	}
	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, Role: models.OrganizationMemberRoleManager},
		UserID:    30,
	}
	otherOrganizationMember := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 8},
		UserID:    20,
	}

	tests := []struct {
		name   string
		access models.WorkspaceAccess
		scope  models.ResourceScope
		want   bool
	}{
		{
			name:   "member reads own organization list",
			access: member,
			scope:  workspaceTestScope(7, 20, models.ResourceVisibilityPrivate, false),
			want:   true,
		},
		{
			name:   "member cannot read another members old shared list",
			access: member,
			scope:  workspaceTestScope(7, 10, models.ResourceVisibilityOrganization, false),
			want:   false,
		},
		{
			name:   "manager can inspect another members list",
			access: manager,
			scope:  workspaceTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			want:   true,
		},
		{
			name:   "member cannot inspect pending list",
			access: member,
			scope:  workspaceTestScope(7, 0, models.ResourceVisibilityPrivate, true),
			want:   false,
		},
		{
			name:   "manager can inspect pending list for transfer",
			access: manager,
			scope:  workspaceTestScope(7, 0, models.ResourceVisibilityPrivate, true),
			want:   true,
		},
		{
			name:   "same user cannot cross organization boundary",
			access: otherOrganizationMember,
			scope:  workspaceTestScope(7, 20, models.ResourceVisibilityPrivate, false),
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := c.CanReadOwnerScopedResource(test.access, test.scope); got != test.want {
				t.Fatalf("CanReadOwnerScopedResource() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWorkspaceResourceUseBoundaries(t *testing.T) {
	c := &Core{}
	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7},
		UserID:    20,
	}
	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, Role: models.OrganizationMemberRoleManager},
		UserID:    30,
	}
	personalUser := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    40,
	}

	tests := []struct {
		name   string
		access models.WorkspaceAccess
		scope  models.ResourceScope
		want   bool
	}{
		{
			name:   "member uses own private organization template",
			access: member,
			scope:  workspaceTestScope(7, 20, models.ResourceVisibilityPrivate, false),
			want:   true,
		},
		{
			name:   "member uses organization shared template",
			access: member,
			scope:  workspaceTestScope(7, 10, models.ResourceVisibilityOrganization, false),
			want:   true,
		},
		{
			name:   "manager cannot send with member private template",
			access: manager,
			scope:  workspaceTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			want:   false,
		},
		{
			name:   "global template is usable in personal space",
			access: personalUser,
			scope:  workspaceTestScope(7, 10, models.ResourceVisibilityGlobal, false),
			want:   true,
		},
		{
			name:   "pending transfer resource cannot be sent",
			access: manager,
			scope:  workspaceTestScope(7, 10, models.ResourceVisibilityOrganization, true),
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := c.CanUseResource(test.access, test.scope); got != test.want {
				t.Fatalf("CanUseResource() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestApplyWorkspaceScope(t *testing.T) {
	personal := ApplyWorkspaceScope(models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    10,
	}, models.ResourceVisibilityOrganization)
	if personal.OrganizationID.Valid || personal.Visibility != models.ResourceVisibilityPrivate {
		t.Fatalf("personal scope = %+v, want a private personal resource", personal)
	}

	organization := ApplyWorkspaceScope(models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7},
		UserID:    10,
	}, models.ResourceVisibilityOrganization)
	if !organization.OrganizationID.Valid || organization.OrganizationID.Int != 7 ||
		!organization.OwnerUserID.Valid || organization.OwnerUserID.Int != 10 ||
		organization.Visibility != models.ResourceVisibilityOrganization {
		t.Fatalf("organization scope = %+v, want organization 7 owned by user 10", organization)
	}
}
