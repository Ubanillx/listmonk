package auth

import (
	"encoding/json"
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

var ErrPermDenied = echo.NewHTTPError(http.StatusForbidden, "permission denied")

// PermType indicates a generic permission type which is either get (read) or manage (write).
type PermType uint8

const (
	PermTypeGet PermType = 1 << iota
	PermTypeManage
)

const (
	// UserHTTPCtxKey is the key on which the User profile is set on echo handlers.
	UserHTTPCtxKey             = "auth_user"
	SessionKey                 = "auth_session"
	IntegrationTokenHTTPCtxKey = "auth_integration_token"
)

const (
	// SuperAdminRoleID is the database ID of the primordial super admin role.
	SuperAdminRoleID = 1

	// User.
	UserTypeUser       = "user"
	UserTypeAPI        = "api"
	UserStatusEnabled  = "enabled"
	UserStatusDisabled = "disabled"

	// Role.
	RoleTypeUser = "user"
	RoleTypeList = "customer_list"
)

const (
	IntegrationTokenKindService  = "service"
	IntegrationTokenKindPersonal = "personal"
)

// CustomerList of all granular permissions.
const (
	PermListGetAll            = "customer_lists:get_all"
	PermListManageAll         = "customer_lists:manage_all"
	PermListManage            = "customer_list:manage"
	PermListGet               = "customer_list:get"
	PermCustomersGet          = "customers:get"
	PermCustomersGetAll       = "customers:get_all"
	PermCustomersManage       = "customers:manage"
	PermCustomersImport       = "customers:import"
	PermCustomersSqlQuery     = "customers:sql_query"
	PermTxSend                = "tx:send"
	PermCampaignsGet          = "campaigns:get"
	PermCampaignsGetAll       = "campaigns:get_all"
	PermCampaignsGetAnalytics = "campaigns:get_analytics"
	PermCampaignsManage       = "campaigns:manage"
	PermCampaignsManageAll    = "campaigns:manage_all"
	PermBouncesGet            = "bounces:get"
	PermBouncesManage         = "bounces:manage"
	PermWebhooksPostBounce    = "webhooks:post_bounce"
	PermMediaGet              = "media:get"
	PermMediaManage           = "media:manage"
	PermTemplatesGet          = "templates:get"
	PermTemplatesManage       = "templates:manage"
	PermUsersGet              = "users:get"
	PermUsersManage           = "users:manage"
	PermRolesGet              = "roles:get"
	PermRolesManage           = "roles:manage"
	PermSettingsGet           = "settings:get"
	PermSettingsManage        = "settings:manage"
	PermSettingsMaintain      = "settings:maintain"
	PermWorkspacesPersonal    = "workspaces:personal"
)

// Base holds common fields shared across models.
type Base struct {
	ID        int       `db:"id" json:"id"`
	CreatedAt null.Time `db:"created_at" json:"created_at"`
	UpdatedAt null.Time `db:"updated_at" json:"updated_at"`
}

// User represents an admin user.
type User struct {
	Base

	Username string `db:"username" json:"username"`

	// For API users, this is the plaintext API token.
	Password null.String `db:"password" json:"password,omitempty"`

	PasswordLogin bool        `db:"password_login" json:"password_login"`
	Email         null.String `db:"email" json:"email"`
	Name          string      `db:"name" json:"name"`
	// Attribs stores values for administrator-defined account fields. These
	// values belong to the sending account and are injected into campaign
	// templates at render time.
	Attribs               models.JSON      `db:"attribs" json:"attribs"`
	Type                  string           `db:"type" json:"type"`
	Status                string           `db:"status" json:"status"`
	Avatar                null.String      `db:"avatar" json:"avatar"`
	TwofaType             string           `db:"twofa_type" json:"twofa_type"`
	TwofaKey              null.String      `db:"twofa_key" json:"-"`
	LoggedInAt            null.Time        `db:"loggedin_at" json:"loggedin_at"`
	UserRoleID            int              `db:"user_role_id" json:"user_role_id,omitempty"`
	UserRoleName          string           `db:"user_role_name" json:"-"`
	CustomerListRoleID    *int             `db:"list_role_id" json:"customer_list_role_id,omitempty"`
	CustomerListRoleName  null.String      `db:"list_role_name" json:"-"`
	UserRolePerms         pq.StringArray   `db:"user_role_permissions" json:"-"`
	CustomerListsPermsRaw *json.RawMessage `db:"list_role_perms" json:"-"`

	// Non-DB fields filled post-retrieval.
	UserRole struct {
		ID          int      `db:"-" json:"id"`
		Name        string   `db:"-" json:"name"`
		Permissions []string `db:"-" json:"permissions"`
	} `db:"-" json:"user_role"`

	CustomerListRole           *CustomerListRolePermissions `db:"-" json:"customer_list_role"`
	PermissionsMap             map[string]struct{}          `db:"-" json:"-"`
	CustomerListPermissionsMap map[int]map[string]struct{}  `db:"-" json:"-"`
	GetCustomerListIDs         []int                        `db:"-" json:"-"`
	ManageCustomerListIDs      []int                        `db:"-" json:"-"`
	HasPassword                bool                         `db:"-" json:"-"`
}

// IsPlatformAdmin reports whether the user is the built-in platform
// administrator. Retrieved users keep the database role ID in UserRole.ID so
// it can be returned safely to clients, while users being created or updated
// still carry it in UserRoleID. Accept both representations at the boundary.
func (u User) IsPlatformAdmin() bool {
	return u.UserRole.ID == SuperAdminRoleID || u.UserRoleID == SuperAdminRoleID
}

type IntegrationToken struct {
	Base

	UserID                  int            `db:"user_id" json:"user_id"`
	Kind                    string         `db:"kind" json:"kind"`
	WorkspaceOrganizationID null.Int       `db:"workspace_organization_id" json:"workspace_organization_id"`
	Name                    string         `db:"name" json:"name"`
	TokenHash               string         `db:"token_hash" json:"-"`
	Scopes                  pq.StringArray `db:"scopes" json:"scopes"`
	ExpiresAt               null.Time      `db:"expires_at" json:"expires_at"`
	LastUsedAt              null.Time      `db:"last_used_at" json:"last_used_at"`
	RevokedAt               null.Time      `db:"revoked_at" json:"revoked_at"`

	// Non-DB field filled post-retrieval for auth cache lookups.
	User User `db:"-" json:"-"`
}

type CustomerListPermission struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Permissions pq.StringArray `json:"permissions"`
}

type CustomerListRolePermissions struct {
	ID            int                      `db:"-" json:"id"`
	Name          string                   `db:"-" json:"name"`
	CustomerLists []CustomerListPermission `db:"-" json:"customer_lists"`
}

type Role struct {
	Base

	Type        string         `db:"type" json:"type"`
	Name        null.String    `db:"name" json:"name"`
	Permissions pq.StringArray `db:"permissions" json:"permissions"`

	CustomerListID   null.Int                 `db:"customer_list_id" json:"-"`
	ParentID         null.Int                 `db:"parent_id" json:"-"`
	CustomerListsRaw json.RawMessage          `db:"list_permissions" json:"-"`
	CustomerLists    []CustomerListPermission `db:"-" json:"customer_lists"`
}

type CustomerListRole struct {
	Base

	Name null.String `db:"name" json:"name"`

	CustomerListID   null.Int                 `db:"customer_list_id" json:"-"`
	ParentID         null.Int                 `db:"parent_id" json:"-"`
	CustomerListsRaw json.RawMessage          `db:"list_permissions" json:"-"`
	CustomerLists    []CustomerListPermission `db:"-" json:"customer_lists"`
}

// HasPerm checks if the user has a specific permission.
func (u *User) HasPerm(perm string) bool {
	// Short-circuit if the user is the primordial super admin.
	if u.IsPlatformAdmin() {
		return true
	}

	_, ok := u.PermissionsMap[perm]
	return ok
}

// HasListPerm checks if the user has get or manage access to the given customer_list.
// perm is either PermGet or PermManage.
func (u *User) HasListPerm(types PermType, customerListIDs ...int) error {
	var permAll, perm string

	if types == 0 {
		return ErrPermDenied
	}

	if types&PermTypeGet != 0 {
		permAll = PermListGetAll
		perm = PermListGet
	} else if types&PermTypeManage != 0 {
		permAll = PermListManageAll
		perm = PermListManage
	}

	// Check if the user has permissions for all customer_lists or the specific customer_list.
	if u.HasPerm(permAll) {
		return nil
	}

	for _, id := range customerListIDs {
		if id > 0 {
			if !u.hasListPerm(perm, id) {
				return ErrPermDenied
			}
		}
	}

	return nil
}

func (u *User) hasListPerm(perm string, customerListID int) bool {
	// Short-circuit if the user is the primordial super admin.
	if u.IsPlatformAdmin() {
		return true
	}

	if _, ok := u.CustomerListPermissionsMap[customerListID]; !ok {
		return false
	}

	_, ok := u.CustomerListPermissionsMap[customerListID][perm]
	return ok
}

// GetPermittedLists returns a customer_list of IDs the user has access to based on
// the given get / manage permissions. If the user has the blanket "*_all"
// permission (or the user is a super admin), then the bool is set to true and
// the customer_list is nil as all customer_lists are permitted.
func (u *User) GetPermittedLists(types PermType) (bool, []int) {
	if types == 0 {
		return false, nil
	}

	// Short-circuit if the user is the primordial super admin.
	if u.IsPlatformAdmin() {
		return true, nil
	}

	var (
		get    = types&PermTypeGet != 0
		manage = types&PermTypeManage != 0
	)

	// If the user has the customer_list:get_all or customer_list:manage_all permission, no
	// further checks are required.
	if get {
		if _, ok := u.PermissionsMap[PermListGetAll]; ok {
			return true, nil
		}
	}
	if manage {
		if _, ok := u.PermissionsMap[PermListManageAll]; ok {
			return true, nil
		}
	}

	if get {
		// If the user has per-customer_list permissions, return that. Otherwise, let the
		// 'manage' permission check run.
		if len(u.GetCustomerListIDs) > 0 {
			out := make([]int, len(u.GetCustomerListIDs))
			copy(out, u.GetCustomerListIDs)
			return false, out
		}
	}

	if manage {
		// User has per-customer_list permissions.
		out := make([]int, len(u.ManageCustomerListIDs))
		copy(out, u.ManageCustomerListIDs)
		return false, out
	}

	return false, nil
}

// FilterListsByPerm returns customer_list IDs filtered by either of the given perms.
func (u *User) FilterListsByPerm(types PermType, customerListIDs []int) []int {
	if types == 0 {
		return nil
	}
	if u.IsPlatformAdmin() {
		return customerListIDs
	}

	var (
		get    = types&PermTypeGet != 0
		manage = types&PermTypeManage != 0
	)

	// If the user has full customer_list management permission,
	// no further checks are required.
	if get {
		if _, ok := u.PermissionsMap[PermListGetAll]; ok {
			return customerListIDs
		}
	}
	if manage {
		if _, ok := u.PermissionsMap[PermListManageAll]; ok {
			return customerListIDs
		}
	}

	out := make([]int, 0, len(customerListIDs))
	for _, id := range customerListIDs {
		// Check if it exists in the map.
		if l, ok := u.CustomerListPermissionsMap[id]; ok {
			// Check if any of the given permission exists for it.
			if get {
				if _, ok := l[PermListGet]; ok {
					out = append(out, id)
				}
			} else if manage {
				if _, ok := l[PermListManage]; ok {
					out = append(out, id)
				}
			}
		}
	}

	return out
}
