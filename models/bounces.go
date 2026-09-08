package models

import (
	"encoding/json"
	"time"

	null "gopkg.in/volatiletech/null.v6"
)

const (
	BounceTypeHard      = "hard"
	BounceTypeSoft      = "soft"
	BounceTypeComplaint = "complaint"
)

// Bounce represents a single bounce event.
type Bounce struct {
	ID        int             `db:"id" json:"id"`
	Type      string          `db:"type" json:"type"`
	Source    string          `db:"source" json:"source"`
	Meta      json.RawMessage `db:"meta" json:"meta"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`

	// One of these should be provided.
	Email                string   `db:"email" json:"email,omitempty"`
	CustomerUUID         string   `db:"customer_uuid" json:"customer_uuid,omitempty"`
	CustomerID           int      `db:"customer_id" json:"customer_id,omitempty"`
	PoolContactID        int64    `db:"pool_contact_id" json:"pool_contact_id,omitempty"`
	SourcePoolID         int      `db:"source_pool_id" json:"source_pool_id,omitempty"`
	SourceSegmentID      int64    `db:"source_segment_id" json:"source_segment_id,omitempty"`
	SourceOrganizationID null.Int `db:"source_organization_id" json:"source_organization_id,omitempty"`
	CustomerStatus       string   `db:"customer_status" json:"customer_status"`
	// Bounce records inherit their access boundary from the customer. These
	// fields let organization managers inspect a member's bounce history while
	// the UI keeps destructive controls limited to records they own.
	OrganizationID    null.Int  `db:"organization_id" json:"organization_id"`
	OwnerUserID       null.Int  `db:"owner_user_id" json:"owner_user_id"`
	TransferPendingAt null.Time `db:"transfer_pending_at" json:"transfer_pending_at"`

	CampaignUUID string           `db:"campaign_uuid" json:"campaign_uuid,omitempty"`
	Campaign     *json.RawMessage `db:"campaign" json:"campaign"`

	// Pseudofield for getting the total number of bounces
	// in searches and queries.
	Total int `db:"total" json:"-"`
}
