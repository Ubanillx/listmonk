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
}
