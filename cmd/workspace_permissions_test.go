package main

import (
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

func permissionTestScope(orgID, ownerID int, visibility string, pending bool) models.ResourceScope {
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

func permissionTestUser(perms ...string) auth.User {
	permissions := make(map[string]struct{}, len(perms))
	for _, permission := range perms {
		permissions[permission] = struct{}{}
	}
	return auth.User{PermissionsMap: permissions}
}

func TestWorkspaceShareAndCopyExceptions(t *testing.T) {
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
		name     string
		access   models.WorkspaceAccess
		scope    models.ResourceScope
		wantRead bool
		wantCopy bool
	}{
		{
			name:     "global campaign is readable and copyable by a signed in user",
			access:   personalUser,
			scope:    permissionTestScope(7, 10, models.ResourceVisibilityGlobal, false),
			wantRead: true,
			wantCopy: true,
		},
		{
			name:     "organization shared campaign is copyable by a member",
			access:   member,
			scope:    permissionTestScope(7, 10, models.ResourceVisibilityOrganization, false),
			wantRead: true,
			wantCopy: true,
		},
		{
			name:     "organization manager only inspects another members private resource",
			access:   manager,
			scope:    permissionTestScope(7, 10, models.ResourceVisibilityPrivate, false),
			wantRead: true,
			wantCopy: false,
		},
		{
			name:     "pending transfer resource cannot be copied",
			access:   manager,
			scope:    permissionTestScope(7, 0, models.ResourceVisibilityOrganization, true),
			wantRead: true,
			wantCopy: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := workspaceReadException(test.access, test.scope); got != test.wantRead {
				t.Fatalf("workspaceReadException() = %v, want %v", got, test.wantRead)
			}
			if got := canCopyWorkspaceResource(test.access, test.scope); got != test.wantCopy {
				t.Fatalf("canCopyWorkspaceResource() = %v, want %v", got, test.wantCopy)
			}
		})
	}
}

func TestLegacyPermissionsRemainNarrowingGuards(t *testing.T) {
	readOnly := permissionTestUser(auth.PermSubscribersGet, auth.PermListGet)
	readOnly.GetListIDs = []int{3}
	readOnly.ListPermissionsMap = map[int]map[string]struct{}{
		3: {auth.PermListGet: {}},
	}

	if err := requireLegacyPermission(readOnly, auth.PermSubscribersManage); err == nil {
		t.Fatal("subscriber write permission unexpectedly granted")
	}
	if err := requireLegacyPermission(readOnly, auth.PermTxSend); err == nil {
		t.Fatal("send permission unexpectedly granted")
	}
	if err := requireLegacyListPermission(readOnly, 3, true); err == nil {
		t.Fatal("list write permission unexpectedly granted from list:get")
	}
	if err := requireLegacyListPermission(readOnly, 3, false); err != nil {
		t.Fatalf("list read permission = %v, want allowed", err)
	}

	all, ids := legacyReadableListIDs(readOnly)
	if all || len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("legacyReadableListIDs() = (%v, %v), want (false, [3])", all, ids)
	}
}

func TestManagedListIntersectionNeverFallsBackToAllLists(t *testing.T) {
	if got := intersectManagedWorkspaceLegacyListIDs([]int{2, 5}, false, []int{5}); len(got) != 1 || got[0] != 5 {
		t.Fatalf("managed list intersection = %v, want [5]", got)
	}
	if got := intersectManagedWorkspaceLegacyListIDs([]int{2, 5}, false, []int{9}); len(got) != 1 || got[0] != -1 {
		t.Fatalf("empty managed list intersection = %v, want [-1]", got)
	}
	if got := intersectManagedWorkspaceLegacyListIDs(nil, true, nil); len(got) != 1 || got[0] != -1 {
		t.Fatalf("empty global managed list set = %v, want [-1]", got)
	}
}
