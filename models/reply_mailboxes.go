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
	LastSyncAt     *time.Time `db:"last_sync_at" json:"last_sync_at"`
	LastSyncErr    string     `db:"last_sync_error" json:"last_sync_error"`
	ForwardCount   int        `db:"forward_count" json:"forward_count"`
	// Manageable is true only when the caller owns the mailbox. In an
	// organization workspace the listing also returns mailboxes the caller may
	// select for a campaign but cannot manage.
	Manageable bool `db:"manageable" json:"manageable"`
	// Deletable mirrors the right the delete endpoint enforces for this caller:
	// the owner may always remove their own mailbox, and a manager of the
	// mailbox's organization may remove a stale one (a platform administrator
	// counts as a manager of the organization the request selected). The listing
	// handler computes it, so it carries no database column.
	Deletable bool `db:"-" json:"deletable"`
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
