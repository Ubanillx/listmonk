package models

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/types"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	CustomerStatusEnabled     = "enabled"
	CustomerStatusDisabled    = "disabled"
	CustomerStatusBlockListed = "blocklisted"

	SubscriptionStatusUnconfirmed  = "unconfirmed"
	SubscriptionStatusConfirmed    = "confirmed"
	SubscriptionStatusUnsubscribed = "unsubscribed"
)

// Customers represents a slice of Customer.
type Customers []Customer

// Customer represents an e-mail customer.
type Customer struct {
	Base
	ResourceScope

	UUID          string         `db:"uuid" json:"uuid"`
	Email         string         `db:"email" json:"email" form:"email"`
	Name          string         `db:"name" json:"name" form:"name"`
	Attribs       JSON           `db:"attribs" json:"attribs"`
	Status        string         `db:"status" json:"status"`
	CustomerLists types.JSONText `db:"customer_lists" json:"customer_lists"`

	// CustomerCode is a business identifier assigned per customer. It is
	// required on the admin and import paths (validated at the application
	// layer) but optional for public subscriptions.
	CustomerCode string `db:"customer_code" json:"customer_code" form:"customer_code"`

	// Pseudofield for paginated workspace queries.
	Total int `db:"total" json:"-"`
}

type subLists struct {
	CustomerID    int            `db:"customer_id"`
	CustomerLists types.JSONText `db:"customer_lists"`
}

// GetIDs returns the customer_list of customer IDs.
func (subs Customers) GetIDs() []int {
	IDs := make([]int, len(subs))
	for i, c := range subs {
		IDs[i] = c.ID
	}

	return IDs
}

// LoadLists lazy loads the customer_lists for all the customers
// in the Customers slice and attaches them to their []CustomerLists property.
func (subs Customers) LoadLists(stmt *sqlx.Stmt) error {
	var sl []subLists
	err := stmt.Select(&sl, pq.Array(subs.GetIDs()))
	if err != nil {
		return err
	}

	if len(subs) != len(sl) {
		return errors.New("campaign stats count does not match")
	}

	for i, s := range sl {
		if s.CustomerID == subs[i].ID {
			subs[i].CustomerLists = s.CustomerLists
		}
	}

	return nil
}

// FirstName splits the name by spaces and returns the first chunk
// of the name that's greater than 2 characters in length, assuming
// that it is the customer's first name.
func (s Customer) FirstName() string {
	for _, s := range strings.Split(s.Name, " ") {
		if len(s) > 2 {
			return s
		}
	}

	return s.Name
}

// LastName splits the name by spaces and returns the last chunk
// of the name that's greater than 2 characters in length, assuming
// that it is the customer's last name.
func (s Customer) LastName() string {
	chunks := strings.Split(s.Name, " ")
	for i := len(chunks) - 1; i >= 0; i-- {
		chunk := chunks[i]
		if len(chunk) > 2 {
			return chunk
		}
	}

	return s.Name
}

// Subscription represents a customer_list attached to a customer.
type Subscription struct {
	CustomerList
	SubscriptionStatus    null.String     `db:"subscription_status" json:"subscription_status"`
	SubscriptionCreatedAt null.String     `db:"subscription_created_at" json:"subscription_created_at"`
	Meta                  json.RawMessage `db:"meta" json:"meta"`
}

// CustomerExport represents a customer record that is exported to raw data.
type CustomerExport struct {
	Base

	UUID    string `db:"uuid" json:"uuid"`
	Email   string `db:"email" json:"email"`
	Name    string `db:"name" json:"name"`
	Attribs string `db:"attribs" json:"attribs"`
	Status  string `db:"status" json:"status"`

	// CustomerCode is exported alongside the rest of the customer record.
	CustomerCode string `db:"customer_code" json:"customer_code"`
}

// CustomerExportProfile represents a customer's collated data in JSON for export.
type CustomerExportProfile struct {
	Email         string          `db:"email" json:"-"`
	Profile       json.RawMessage `db:"profile" json:"profile,omitempty"`
	Subscriptions json.RawMessage `db:"subscriptions" json:"subscriptions,omitempty"`
	CampaignViews json.RawMessage `db:"campaign_views" json:"campaign_views,omitempty"`
	LinkClicks    json.RawMessage `db:"link_clicks" json:"link_clicks,omitempty"`
}

// CustomerActivity represents a customer's campaign views and link clicks for the Activity tab.
type CustomerActivity struct {
	CampaignViews json.RawMessage `db:"campaign_views" json:"campaign_views"`
	LinkClicks    json.RawMessage `db:"link_clicks" json:"link_clicks"`
	ReplyAIEvents json.RawMessage `db:"reply_ai_events" json:"reply_ai_events"`
}
