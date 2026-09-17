package models

import (
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	CustomerListTypePrivate           = "private"
	CustomerListTypePublic            = "public"
	CustomerListTypePool              = "pool"
	CustomerListTypeOrgPoolAllocation = "org_pool_allocation"
	CustomerListOptinSingle           = "single"
	CustomerListOptinDouble           = "double"
	CustomerListStatusActive          = "active"
	CustomerListStatusArchived        = "archived"
)

// CustomerList represents a mailing customer_list.
type CustomerList struct {
	Base
	ResourceScope

	UUID               string         `db:"uuid" json:"uuid"`
	Name               string         `db:"name" json:"name"`
	Type               string         `db:"type" json:"type"`
	Optin              string         `db:"optin" json:"optin"`
	Status             string         `db:"status" json:"status"`
	Tags               pq.StringArray `db:"tags" json:"tags"`
	Description        string         `db:"description" json:"description"`
	MaskEmails         bool           `db:"mask_emails" json:"mask_emails"`
	PoolParentID       null.Int       `db:"pool_parent_id" json:"pool_parent_id,omitempty"`
	PoolReplyMailboxID null.Int       `db:"pool_reply_mailbox_id" json:"pool_reply_mailbox_id,omitempty"`
	// PoolDeliveryAllowed marks a first-level/pool-allocation public-pool list that
	// the active organization may select for campaign delivery. It is a
	// capability flag, not permission to inspect pool contacts.
	PoolDeliveryAllowed bool         `db:"-" json:"pool_delivery_allowed,omitempty"`
	CustomerCount       int          `db:"customer_count" json:"customer_count"`
	CustomerCounts      StringIntMap `db:"customer_statuses" json:"customer_statuses"`
	CustomerID          int          `db:"customer_id" json:"-"`

	// This is only relevant when querying the customer_lists of a customer.
	SubscriptionStatus    string    `db:"subscription_status" json:"subscription_status,omitempty"`
	SubscriptionCreatedAt null.Time `db:"subscription_created_at" json:"subscription_created_at,omitempty"`
	SubscriptionUpdatedAt null.Time `db:"subscription_updated_at" json:"subscription_updated_at,omitempty"`

	// Pseudofield for getting the total number of customers
	// in searches and queries.
	Total int `db:"total" json:"-"`
}
