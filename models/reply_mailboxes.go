package models

import (
	"time"

	null "gopkg.in/volatiletech/null.v6"
)

const (
	ReplyMailboxStatusPending  = "pending"
	ReplyMailboxStatusActive   = "active"
	ReplyMailboxStatusRetained = "retained"
	ReplyMailboxStatusDisabled = "disabled"

	ReplyForwardStatusActive   = "active"
	ReplyForwardStatusDisabled = "disabled"
)

// ReplyMailbox is a dedicated 263 mailbox used as the Reply-To destination
// for customer replies. IMAP credentials are never returned by API methods.
type ReplyMailbox struct {
	Base
	UserID         int        `db:"user_id" json:"user_id"`
	OrganizationID null.Int   `db:"organization_id" json:"organization_id"`
	Email          string     `db:"email" json:"email"`
	Name           string     `db:"name" json:"name"`
	Username       string     `db:"username" json:"username"`
	IMAPHost       string     `db:"imap_host" json:"imap_host"`
	IMAPPort       int        `db:"imap_port" json:"imap_port"`
	IMAPTLS        bool       `db:"imap_tls" json:"imap_tls"`
	Folder         string     `db:"folder" json:"folder"`
	Status         string     `db:"status" json:"status"`
	VerifiedAt     *time.Time `db:"verified_at" json:"verified_at"`
	IsDefault      bool       `db:"is_default" json:"is_default"`
	AIEnabled      bool       `db:"ai_enabled" json:"ai_enabled"`
	BounceEnabled  bool       `db:"bounce_enabled" json:"bounce_enabled"`
	HasPassword    bool       `db:"has_password" json:"has_password"`
	LastSyncAt     *time.Time `db:"last_sync_at" json:"last_sync_at"`
	LastSyncErr    string     `db:"last_sync_error" json:"last_sync_error"`
	ForwardCount   int        `db:"forward_count" json:"forward_count"`
	// BounceProcessingEnabled and BouncePolicy are computed by the handler from
	// the current settings. They let the mailbox card explain the effective
	// policy without exposing any other setting or credential.
	BounceProcessingEnabled bool              `db:"-" json:"bounce_processing_enabled"`
	BouncePolicy            ReplyBouncePolicy `db:"-" json:"bounce_policy"`
	// Manageable is true only when the caller owns the mailbox. In an
	// organization workspace the listing also returns mailboxes the caller may
	// select for a campaign but cannot manage.
	Manageable bool `db:"manageable" json:"manageable"`
	// Inspectable is true for the owner, an organization manager or a platform
	// administrator: the read-only inspection and processing history use the
	// same boundary as the backend, while editing remains owner-only.
	Inspectable bool `db:"-" json:"inspectable"`
	// Deletable mirrors the right the delete endpoint enforces for this caller:
	// the owner may always remove their own mailbox, and a manager of the
	// mailbox's organization may remove a stale one (a platform administrator
	// counts as a manager of the organization the request selected). The listing
	// handler computes it, so it carries no database column.
	Deletable bool `db:"-" json:"deletable"`
}

// ReplyBouncePolicy is the effective hard-bounce policy shown in the mailbox
// card. Only the threshold and action are exposed; the policy itself remains
// owned by Settings -> Bounces.
type ReplyBouncePolicy struct {
	Enabled   bool   `json:"enabled"`
	Threshold int    `json:"threshold"`
	Action    string `json:"action"`
}

// ReplyBounceEvent is the safe projection of reply_bounce_events used by the
// per-mailbox processing history. The DSN body, raw headers and diagnostic text
// are intentionally not exposed.
type ReplyBounceEvent struct {
	ID            int64     `db:"id" json:"id"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	Recipient     string    `db:"recipient" json:"recipient"`
	BounceType    string    `db:"bounce_type" json:"bounce_type"`
	Outcome       string    `db:"outcome" json:"outcome"`
	ReasonCode    string    `db:"reason_code" json:"reason_code"`
	CampaignID    int       `db:"campaign_id" json:"campaign_id"`
	CustomerID    int       `db:"customer_id" json:"customer_id"`
	PoolContactID int64     `db:"pool_contact_id" json:"pool_contact_id"`
}

// ReplyForwardRule describes server-side application forwarding that is
// enabled after an organization member leaves. The source mailbox remains
// usable by the former member; only the forwarding worker changes state.
type ReplyForwardRule struct {
	Base
	ReplyMailboxID int        `db:"reply_mailbox_id" json:"reply_mailbox_id"`
	OrganizationID int        `db:"organization_id" json:"organization_id"`
	TargetUserID   int        `db:"target_user_id" json:"target_user_id"`
	TargetEmail    string     `db:"target_email" json:"target_email"`
	Status         string     `db:"status" json:"status"`
	DisabledAt     *time.Time `db:"disabled_at" json:"disabled_at"`
	DisabledBy     *int       `db:"disabled_by" json:"disabled_by"`
	LastError      string     `db:"last_error" json:"last_error"`
	LastForwardAt  *time.Time `db:"last_forward_at" json:"last_forward_at"`
}
