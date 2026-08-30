package models

import (
	"time"

	null "gopkg.in/volatiletech/null.v6"
)

const (
	OrganizationStatusActive   = "active"
	OrganizationStatusArchived = "archived"

	OrganizationRequestPending   = "pending"
	OrganizationRequestApproved  = "approved"
	OrganizationRequestRejected  = "rejected"
	OrganizationRequestWithdrawn = "withdrawn"

	OrganizationMemberRoleMember  = "member"
	OrganizationMemberRoleManager = "manager"

	ResourceVisibilityPrivate      = "private"
	ResourceVisibilityOrganization = "organization"
	ResourceVisibilityGlobal       = "global"
)

// Organization is a tenant that can contain member-owned mailing resources.
// An archived organization remains available to platform administrators for
// resource transfers, but cannot be selected as an active workspace.
type Organization struct {
	Base

	Name            string    `db:"name" json:"name"`
	Description     string    `db:"description" json:"description"`
	Status          string    `db:"status" json:"status"`
	CreatedByUserID int       `db:"created_by_user_id" json:"created_by_user_id"`
	ArchivedAt      null.Time `db:"archived_at" json:"archived_at"`

	MemberCount int    `db:"member_count" json:"member_count"`
	MyRole      string `db:"my_role" json:"my_role"`
}

// OrganizationMember is an active or former relationship between a user and
// an organization. Roles here are intentionally separate from global roles.
type OrganizationMember struct {
	OrganizationID  int       `db:"organization_id" json:"organization_id"`
	UserID          int       `db:"user_id" json:"user_id"`
	Role            string    `db:"role" json:"role"`
	JoinedAt        null.Time `db:"joined_at" json:"joined_at"`
	RemovedAt       null.Time `db:"removed_at" json:"removed_at"`
	RemovedByUserID null.Int  `db:"removed_by_user_id" json:"removed_by_user_id"`

	Username string `db:"username" json:"username"`
	Name     string `db:"name" json:"name"`
	Email    string `db:"email" json:"email"`
}

// OrganizationJoinRequest is a request to establish a new organization. The
// table name is retained so the flow can later support organization join
// requests without another schema migration; invite based joining is direct.
type OrganizationJoinRequest struct {
	Base

	RequestedName     string    `db:"requested_name" json:"requested_name"`
	Description       string    `db:"description" json:"description"`
	Status            string    `db:"status" json:"status"`
	RequestedByUserID int       `db:"requested_by_user_id" json:"requested_by_user_id"`
	ReviewedByUserID  null.Int  `db:"reviewed_by_user_id" json:"reviewed_by_user_id"`
	ReviewedAt        null.Time `db:"reviewed_at" json:"reviewed_at"`
	ReviewNote        string    `db:"review_note" json:"review_note"`
	OrganizationID    null.Int  `db:"organization_id" json:"organization_id"`

	RequestedByName string `db:"requested_by_name" json:"requested_by_name"`
}

// OrganizationInvite stores only a hash of the invitation code. The plaintext
// code is returned once at creation time and never written to the database.
type OrganizationInvite struct {
	Base

	OrganizationID  int       `db:"organization_id" json:"organization_id"`
	Name            string    `db:"name" json:"name"`
	CodeHash        string    `db:"code_hash" json:"-"`
	CreatedByUserID int       `db:"created_by_user_id" json:"created_by_user_id"`
	ExpiresAt       null.Time `db:"expires_at" json:"expires_at"`
	RevokedAt       null.Time `db:"revoked_at" json:"revoked_at"`
	MaxUses         null.Int  `db:"max_uses" json:"max_uses"`
	UseCount        int       `db:"use_count" json:"use_count"`

	OrganizationName string `db:"organization_name" json:"organization_name"`
	Code             string `db:"-" json:"code,omitempty"`
}

// ResourceScope is embedded by user-owned models. organization_id is NULL for
// personal resources. owner_user_id becomes NULL only after a member leaves;
// original_owner_user_id is retained for audit and transfer labels.
type ResourceScope struct {
	OrganizationID      null.Int  `db:"organization_id" json:"organization_id"`
	OwnerUserID         null.Int  `db:"owner_user_id" json:"owner_user_id"`
	OriginalOwnerUserID null.Int  `db:"original_owner_user_id" json:"original_owner_user_id"`
	Visibility          string    `db:"visibility" json:"visibility"`
	TransferPendingAt   null.Time `db:"transfer_pending_at" json:"transfer_pending_at"`
	// OrganizationArchived is loaded for single-resource authorization checks.
	// It is intentionally omitted from API responses; list queries enforce the
	// same condition directly in SQL.
	OrganizationArchived bool `db:"organization_archived" json:"-"`

	OwnerUsername string `db:"owner_username" json:"owner_username"`
	OwnerName     string `db:"owner_name" json:"owner_name"`
}

// Workspace identifies the personal or organization workspace used for an
// authenticated request. It is intentionally serializable for the frontend.
type Workspace struct {
	OrganizationID   int    `json:"organization_id"`
	OrganizationName string `json:"organization_name,omitempty"`
	Role             string `json:"role,omitempty"`
	Personal         bool   `json:"personal"`
	PlatformAdmin    bool   `json:"platform_admin"`
	// Archived is only surfaced to a platform administrator while it performs
	// the constrained transfer and cleanup work required before a purge. It
	// must never make an archived workspace writable through ordinary APIs.
	Archived bool `json:"archived,omitempty"`
}

// WorkspaceAccess pairs a selected workspace with the authenticated user. It
// is passed to core methods rather than inferred from process-global state so
// concurrent requests and API-token clients remain isolated.
type WorkspaceAccess struct {
	Workspace
	UserID int `json:"-"`
}

func (a WorkspaceAccess) IsOrganization() bool {
	return a.OrganizationID > 0
}

func (a WorkspaceAccess) IsOrganizationManager() bool {
	return a.PlatformAdmin || a.Role == OrganizationMemberRoleManager
}

// InviteExpiry is the small request shape used by organization handlers.
// Keeping this in models avoids leaking a time parsing convention into views.
type InviteExpiry struct {
	ExpiresAt *time.Time `json:"expires_at"`
}
