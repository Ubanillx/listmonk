package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
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
		{
			name:   "archived organization resource is not a copy source",
			access: manager,
			scope: models.ResourceScope{
				OrganizationID:       null.Int{Int: 7, Valid: true},
				OwnerUserID:          null.Int{Int: 10, Valid: true},
				Visibility:           models.ResourceVisibilityGlobal,
				OrganizationArchived: true,
			},
			wantRead: false,
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

func TestCampaignCopyAllowsManagerInspection(t *testing.T) {
	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, Role: models.OrganizationMemberRoleManager},
		UserID:    30,
	}
	scope := permissionTestScope(7, 20, models.ResourceVisibilityPrivate, false)
	if !canCopyWorkspaceCampaign(manager, scope) {
		t.Fatal("organization manager should be able to copy a readable member campaign")
	}
	if canCopyWorkspaceResource(manager, scope) {
		t.Fatal("generic resource copy rule unexpectedly widened")
	}
}

func TestLegacyPermissionsRemainNarrowingGuards(t *testing.T) {
	readOnly := permissionTestUser(auth.PermCustomersGet, auth.PermListGet)
	readOnly.GetCustomerListIDs = []int{3}
	readOnly.CustomerListPermissionsMap = map[int]map[string]struct{}{
		3: {auth.PermListGet: {}},
	}

	if err := requireLegacyPermission(readOnly, auth.PermCustomersManage); err == nil {
		t.Fatal("customer write permission unexpectedly granted")
	}
	if err := requireLegacyPermission(readOnly, auth.PermTxSend); err == nil {
		t.Fatal("send permission unexpectedly granted")
	}
	if err := requireLegacyListPermission(readOnly, 3, true); err == nil {
		t.Fatal("customer_list write permission unexpectedly granted from customer_list:get")
	}
	if err := requireLegacyListPermission(readOnly, 3, false); err != nil {
		t.Fatalf("customer_list read permission = %v, want allowed", err)
	}

	all, ids := legacyReadableCustomerListIDs(readOnly)
	if all || len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("legacyReadableCustomerListIDs() = (%v, %v), want (false, [3])", all, ids)
	}
}

func TestActionSpecificPermissionsRemainIndependent(t *testing.T) {
	tests := []struct {
		name       string
		permission string
	}{
		{name: "customer delete", permission: auth.PermCustomersDelete},
		{name: "customer blocklist", permission: auth.PermCustomersBlocklist},
		{name: "customer membership", permission: auth.PermCustomersMembershipManage},
		{name: "customer export", permission: auth.PermCustomersExport},
		{name: "customer sensitive read", permission: auth.PermCustomersSensitiveRead},
		{name: "campaign send", permission: auth.PermCampaignsSend},
		{name: "campaign test", permission: auth.PermCampaignsTest},
		{name: "campaign schedule", permission: auth.PermCampaignsSchedule},
		{name: "campaign control", permission: auth.PermCampaignsControl},
		{name: "campaign recipients", permission: auth.PermCampaignsRecipients},
		{name: "bounce delete", permission: auth.PermBouncesDelete},
		{name: "bounce blocklist", permission: auth.PermBouncesBlocklist},
		{name: "user tokens", permission: auth.PermUsersTokens},
		{name: "organization platform management", permission: auth.PermOrganizationsPlatformManage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allowed := permissionTestUser(test.permission)
			if err := requireLegacyPermission(allowed, test.permission); err != nil {
				t.Fatalf("specific permission %q was rejected: %v", test.permission, err)
			}
			denied := permissionTestUser(auth.PermCustomersGet)
			if err := requireLegacyPermission(denied, test.permission); err == nil {
				t.Fatalf("unrelated permission unexpectedly granted %q", test.permission)
			}
		})
	}
}

func TestBusinessActionsDoNotInheritBroadPermissions(t *testing.T) {
	tests := []struct {
		name   string
		broad  string
		action string
	}{
		{name: "customer delete", broad: auth.PermCustomersManage, action: auth.PermCustomersDelete},
		{name: "customer blocklist", broad: auth.PermCustomersManage, action: auth.PermCustomersBlocklist},
		{name: "customer membership", broad: auth.PermCustomersManage, action: auth.PermCustomersMembershipManage},
		{name: "bounce delete", broad: auth.PermBouncesManage, action: auth.PermBouncesDelete},
		{name: "bounce blocklist", broad: auth.PermBouncesManage, action: auth.PermBouncesBlocklist},
		{name: "campaign test", broad: auth.PermCampaignsManage, action: auth.PermCampaignsTest},
		{name: "campaign schedule", broad: auth.PermCampaignsManage, action: auth.PermCampaignsSchedule},
		{name: "campaign control", broad: auth.PermCampaignsManage, action: auth.PermCampaignsControl},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := requireLegacyPermission(permissionTestUser(test.broad), test.action); err == nil {
				t.Fatalf("broad permission %q unexpectedly granted %q", test.broad, test.action)
			}
		})
	}
}

func TestCampaignStatusPermissionMapping(t *testing.T) {
	tests := map[string]string{
		models.CampaignStatusScheduled: auth.PermCampaignsSchedule,
		models.CampaignStatusRunning:   auth.PermCampaignsSend,
		models.CampaignStatusDraft:     auth.PermCampaignsControl,
		models.CampaignStatusPaused:    auth.PermCampaignsControl,
		models.CampaignStatusCancelled: auth.PermCampaignsControl,
		models.CampaignStatusDeferred:  "",
		models.CampaignStatusFinished:  "",
	}
	for status, want := range tests {
		if got := campaignStatusPermission(status); got != want {
			t.Fatalf("campaignStatusPermission(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestOrganizationManagementPathsAreExplicitlyWhitelisted(t *testing.T) {
	for _, path := range []string{
		"/api/organizations/members",
		"/api/organizations/members/:user_id",
		"/api/organizations/resources/transfer",
		"/api/organizations/templates/:id/transfer",
		"/api/organizations/templates/:id/unpublish",
		"/api/organizations/reply-forwarding",
		"/api/organizations/reply-forwarding/:id",
		"/api/organizations/invites",
		"/api/organizations/invites/:id",
	} {
		if !isOrganizationManagementPath(path) {
			t.Fatalf("management path %q was not whitelisted", path)
		}
	}
	for _, path := range []string{"/api/customers", "/api/organizations", "/api/users"} {
		if isOrganizationManagementPath(path) {
			t.Fatalf("unrelated path %q was whitelisted", path)
		}
	}
}

func TestManagedListIntersectionNeverFallsBackToAllLists(t *testing.T) {
	if got := intersectManagedWorkspaceLegacyCustomerListIDs([]int{2, 5}, false, []int{5}); len(got) != 1 || got[0] != 5 {
		t.Fatalf("managed customer_list intersection = %v, want [5]", got)
	}
	if got := intersectManagedWorkspaceLegacyCustomerListIDs([]int{2, 5}, false, []int{9}); len(got) != 1 || got[0] != -1 {
		t.Fatalf("empty managed customer_list intersection = %v, want [-1]", got)
	}
	if got := intersectManagedWorkspaceLegacyCustomerListIDs(nil, true, nil); len(got) != 1 || got[0] != -1 {
		t.Fatalf("empty global managed customer_list set = %v, want [-1]", got)
	}
}

func TestCanUsePersonalWorkspace(t *testing.T) {
	tests := []struct {
		name string
		user auth.User
		want bool
	}{
		{
			name: "platform administrator always has personal workspace",
			user: auth.User{UserRoleID: auth.SuperAdminRoleID},
			want: true,
		},
		{
			name: "user with workspaces:personal permission has personal workspace",
			user: permissionTestUser(auth.PermWorkspacesPersonal),
			want: true,
		},
		{
			name: "user without the permission loses personal workspace",
			user: permissionTestUser(auth.PermCampaignsGet),
			want: false,
		},
		{
			name: "user with no permissions has no personal workspace",
			user: auth.User{},
			want: false,
		},
		{
			name: "platform admin with additional permissions still has personal workspace",
			user: auth.User{UserRoleID: auth.SuperAdminRoleID, PermissionsMap: map[string]struct{}{"some:other": {}}},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canUsePersonalWorkspace(test.user); got != test.want {
				t.Fatalf("canUsePersonalWorkspace() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRequirePersonalWorkspace(t *testing.T) {
	admin := auth.User{UserRoleID: auth.SuperAdminRoleID}
	if err := requirePersonalWorkspace(admin); err != nil {
		t.Fatalf("requirePersonalWorkspace(admin) = %v, want nil", err)
	}
	permitted := permissionTestUser(auth.PermWorkspacesPersonal)
	if err := requirePersonalWorkspace(permitted); err != nil {
		t.Fatalf("requirePersonalWorkspace(permitted) = %v, want nil", err)
	}
	denied := permissionTestUser(auth.PermCampaignsGet)
	if err := requirePersonalWorkspace(denied); err == nil {
		t.Fatal("requirePersonalWorkspace(denied) = nil, want error")
	}
}

func TestPersonalMigrationListReadExemption(t *testing.T) {
	e := echo.New()

	mkCtx := func(method, path string) echo.Context {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath(path)
		return c
	}

	// Only read-only methods on the four migration customer_list endpoints are exempt.
	for _, path := range []string{"/api/customer-lists", "/api/templates", "/api/campaigns", "/api/media"} {
		c := mkCtx(http.MethodGet, path)
		if !isReadOnlyMethod(c) || !isPersonalMigrationListPath(c.Path()) {
			t.Fatalf("GET %s should be a read-only migration customer_list path", path)
		}
		c = mkCtx(http.MethodPost, path)
		if isReadOnlyMethod(c) {
			t.Fatalf("POST %s must not count as read-only", path)
		}
	}

	// Detail reads, exports, and other endpoints are not exempt.
	for _, path := range []string{
		"/api/customer-lists/3", "/api/templates/3", "/api/campaigns/3", "/api/media/3",
		"/api/customers/export", "/api/dashboard/counts", "/api/workspace",
	} {
		c := mkCtx(http.MethodGet, path)
		if isPersonalMigrationListPath(c.Path()) {
			t.Fatalf("GET %s must not be an exempt migration customer_list path", path)
		}
	}
}
