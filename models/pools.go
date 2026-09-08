package models

import (
	"strings"
	"time"

	null "gopkg.in/volatiletech/null.v6"
)

// PoolContact is a contact imported into a first-class public customer pool.
// CustomerCode is intentionally not unique; the contact ID is the stable key.
type PoolContact struct {
	ID           int64                  `db:"id" json:"id"`
	UUID         string                 `db:"uuid" json:"uuid"`
	CustomerCode string                 `db:"customer_code" json:"customer_code"`
	CompanyName  string                 `db:"company_name" json:"company_name"`
	Email        string                 `db:"email" json:"email"`
	Name         string                 `db:"name" json:"name"`
	Attribs      JSON                   `db:"attribs" json:"attribs"`
	Status       string                 `db:"status" json:"status"`
	CreatedAt    time.Time              `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time              `db:"updated_at" json:"updated_at"`
	Exclusions   []PoolExclusionSummary `db:"-" json:"exclusions,omitempty"`
}

// PoolExclusionSummary is safe audit metadata for a highest administrator.
// It deliberately contains no customer contact data.
type PoolExclusionSummary struct {
	OrganizationID   int64  `json:"organization_id"`
	OrganizationName string `json:"organization_name,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Source           string `json:"source,omitempty"`
}

// SafePoolContact is returned to non-highest-admin users. Email is always masked.
type SafePoolContact struct {
	ID              int64  `json:"id"`
	CustomerCode    string `json:"customer_code"`
	CompanyName     string `json:"company_name"`
	Email           string `json:"email"`
	Name            string `json:"name,omitempty"`
	Status          string `json:"status"`
	Excluded        bool   `json:"excluded,omitempty"`
	ExclusionReason string `json:"exclusion_reason,omitempty"`
}

type PoolSegment struct {
	ID                int64    `db:"id" json:"id"`
	ListID            int      `db:"list_id" json:"list_id"`
	ListName          string   `db:"list_name" json:"list_name,omitempty"`
	PoolID            null.Int `db:"pool_id" json:"pool_id"`
	OrganizationID    int64    `db:"organization_id" json:"organization_id"`
	OrganizationName  string   `db:"organization_name" json:"organization_name,omitempty"`
	ReplyMailboxID    *int     `db:"reply_mailbox_id" json:"reply_mailbox_id,omitempty"`
	ReplyMailboxEmail string   `db:"reply_mailbox_email" json:"reply_mailbox_email,omitempty"`
}

// PoolImportRow is one customer_code/email pair from a secondary-list
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
// identifies the target and its bound secondary list; the target organization
// owns its reply-mailbox configuration. Selecting it does not create an
// organization membership or change the active workspace.
type PoolManagementTarget struct {
	OrganizationID   int          `json:"organization_id"`
	OrganizationName string       `json:"organization_name"`
	Segment          *PoolSegment `json:"segment,omitempty"`
}

type PoolExclusion struct {
	PoolID         int    `db:"pool_id" json:"pool_id"`
	OrganizationID int64  `db:"organization_id" json:"organization_id"`
	ContactID      int64  `db:"contact_id" json:"contact_id"`
	SegmentID      *int64 `db:"segment_id" json:"segment_id,omitempty"`
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
	// Contact person names are customer PII as well; non-highest administrators
	// receive only the imported code, company name, status and masked address.
	return SafePoolContact{ID: p.ID, CustomerCode: p.CustomerCode, CompanyName: p.CompanyName, Email: MaskPoolEmail(p.Email), Status: p.Status}
}
