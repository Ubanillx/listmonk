package models

import (
	"strings"
	"time"

	null "gopkg.in/volatiletech/null.v6"
)

// PoolContact is a contact imported into a first-class public customer pool.
// CustomerCode is intentionally not unique; the contact ID is the stable key.
// AllocationDepartment stores the validated active organization name supplied
// by the source template. When a matching pool allocation exists, the import
// path also creates the corresponding org-pool-allocation membership.
type PoolContact struct {
	ID                   int64                  `db:"id" json:"id"`
	UUID                 string                 `db:"uuid" json:"uuid"`
	CustomerCode         string                 `db:"customer_code" json:"customer_code"`
	Email                string                 `db:"email" json:"email"`
	Name                 string                 `db:"name" json:"name"`
	AllocationDepartment string                 `db:"allocation_department" json:"allocation_department"`
	Attribs              JSON                   `db:"attribs" json:"attribs"`
	Status               string                 `db:"status" json:"status"`
	CreatedAt            time.Time              `db:"created_at" json:"created_at"`
	UpdatedAt            time.Time              `db:"updated_at" json:"updated_at"`
	Exclusions           []PoolExclusionSummary `db:"-" json:"exclusions,omitempty"`
}

// PoolExclusionSummary is safe audit metadata for a highest administrator.
// It deliberately contains no customer contact data.
type PoolExclusionSummary struct {
	OrganizationID   int64  `json:"organization_id"`
	OrganizationName string `json:"organization_name,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Source           string `json:"source,omitempty"`
}

// SafePoolContact is returned to non-highest-admin users. E-mail addresses are
// always masked; the contact name is visible inside the organization's own
// allocation so members can identify the row.
type SafePoolContact struct {
	ID                   int64     `json:"id"`
	CustomerCode         string    `json:"customer_code"`
	Email                string    `json:"email"`
	Name                 string    `json:"name"`
	AllocationDepartment string    `json:"allocation_department,omitempty"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	Excluded             bool      `json:"excluded,omitempty"`
	ExclusionReason      string    `json:"exclusion_reason,omitempty"`
}

type OrgPoolAllocation struct {
	ID                int64    `db:"id" json:"id"`
	ListID            int      `db:"list_id" json:"list_id"`
	ListName          string   `db:"list_name" json:"list_name,omitempty"`
	PoolID            null.Int `db:"pool_id" json:"pool_id"`
	OrganizationID    int64    `db:"organization_id" json:"organization_id"`
	OrganizationName  string   `db:"organization_name" json:"organization_name,omitempty"`
	ReplyMailboxID    *int     `db:"reply_mailbox_id" json:"reply_mailbox_id,omitempty"`
	ReplyMailboxEmail string   `db:"reply_mailbox_email" json:"reply_mailbox_email,omitempty"`
}

// PoolImportRow is one customer_code/email pair from a pool-allocation
// allocation file. Row is the 1-based source row (including the header) so
// callers can fix rejected records in the original file.
type PoolImportRow struct {
	Row          int
	CustomerCode string
	Email        string
}

type PoolImportIssue struct {
	Row          int    `json:"row"`
	CustomerCode string `json:"customer_code,omitempty"`
	Reason       string `json:"reason"`
}

// PoolContactImportRow is one row from the unified public-pool import. The
// source template may contain additional columns; only these four fields are
// persisted by the pool import path.
type PoolContactImportRow struct {
	Row                  int
	CustomerCode         string
	Name                 string
	Email                string
	AllocationDepartment string
}

// PoolContactImportIssue contains safe, row-level validation information. It
// deliberately excludes the uploaded email address; the department value is a
// non-sensitive diagnostic that helps the operator correct the source row.
type PoolContactImportIssue struct {
	Row                  int    `json:"row"`
	CustomerCode         string `json:"customer_code,omitempty"`
	AllocationDepartment string `json:"allocation_department,omitempty"`
	Reason               string `json:"reason"`
}

// PoolContactImportResult is returned synchronously by the unified import
// endpoint when its target list is a first-level public pool.
type PoolContactImportResult struct {
	Target     string                   `json:"target"`
	PoolID     int                      `json:"pool_id"`
	Total      int                      `json:"total"`
	Valid      int                      `json:"valid"`
	Created    int                      `json:"created"`
	Existing   int                      `json:"existing"`
	Conflicts  int                      `json:"conflicts"`
	Invalid    int                      `json:"invalid"`
	Duplicates int                      `json:"duplicates"`
	Issues     []PoolContactImportIssue `json:"issues,omitempty"`
}

// PoolImportResult deliberately contains no email values. This keeps the
// import response safe for organization users even though the uploaded file
// itself contains real addresses.
type PoolImportResult struct {
	Total           int               `json:"total"`
	Valid           int               `json:"valid"`
	Created         int               `json:"created"`
	Reactivated     int               `json:"reactivated"`
	AlreadyAssigned int               `json:"already_assigned"`
	Unmatched       int               `json:"unmatched"`
	Ambiguous       int               `json:"ambiguous"`
	Invalid         int               `json:"invalid"`
	Duplicates      int               `json:"duplicates"`
	Issues          []PoolImportIssue `json:"issues,omitempty"`
}

// PoolManagementTarget is the independent target-organization context used by
// highest administrators when allocating a first-level public pool. It only
// identifies the target and its bound pool allocation; the target organization
// owns its reply-mailbox configuration. Selecting it does not create an
// organization membership or change the active workspace.
type PoolManagementTarget struct {
	OrganizationID   int                `json:"organization_id"`
	OrganizationName string             `json:"organization_name"`
	Allocation       *OrgPoolAllocation `json:"allocation,omitempty"`
}

type PoolExclusion struct {
	PoolID         int    `db:"pool_id" json:"pool_id"`
	OrganizationID int64  `db:"organization_id" json:"organization_id"`
	ContactID      int64  `db:"contact_id" json:"contact_id"`
	AllocationID   *int64 `db:"allocation_id" json:"allocation_id,omitempty"`
	Reason         string `db:"reason" json:"reason"`
	Source         string `db:"source" json:"source"`
}

func MaskPoolEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 3 {
		return strings.Repeat("x", len(local)) + domain
	}
	return local[:3] + strings.Repeat("x", len(local)-3) + domain
}

func (p PoolContact) Safe() SafePoolContact {
	// The name and masked address let members identify a contact inside their
	// own allocation; the raw address and internal identifiers stay hidden.
	return SafePoolContact{
		ID:                   p.ID,
		CustomerCode:         p.CustomerCode,
		Email:                MaskPoolEmail(p.Email),
		Name:                 p.Name,
		AllocationDepartment: p.AllocationDepartment,
		Status:               p.Status,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
	}
}
