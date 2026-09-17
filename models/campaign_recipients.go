package models

import null "gopkg.in/volatiletech/null.v6"

type CampaignCustomer struct {
	Customer
	RecipientStatus string    `db:"recipient_status" json:"recipient_status"`
	SentAt          null.Time `db:"sent_at" json:"sent_at"`
	// PoolContactID is set for a first-class public-pool recipient. Pool
	// recipients do not have a legacy customers row and are tracked in the
	// campaign_pool_recipients table.
	PoolContactID         int64    `db:"pool_contact_id" json:"-"`
	PoolID                int      `db:"pool_id" json:"-"`
	OrgPoolAllocationID   int64    `db:"org_pool_allocation_id" json:"-"`
	ReplyMailboxID        null.Int `db:"pool_reply_mailbox_id" json:"-"`
	PoolReplyMailboxEmail string   `db:"pool_reply_mailbox_email" json:"-"`

	// PoolOrganizationID is the target organization the recipient resolves to.
	// It is filled for platform-level ('all_organizations') campaigns, whose
	// pool rows carry the per-contact target organization instead of the
	// campaign workspace's organization.
	PoolOrganizationID int64 `db:"pool_organization_id" json:"-"`

	// PoolSenderSMTPUUID / PoolSenderUserID / PoolSenderFromEmail carry the
	// SMTP assignment made by the organization pool allocator. Empty for
	// legacy organization-scope campaigns, which send through the campaign
	// owner's personal SMTP pool.
	PoolSenderSMTPUUID string `db:"pool_sender_smtp_uuid" json:"-"`
	PoolSenderUserID   int64  `db:"pool_sender_user_id" json:"-"`
	PoolSenderFrom     string `db:"pool_sender_from_email" json:"-"`
}
