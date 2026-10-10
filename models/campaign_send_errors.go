package models

import (
	null "gopkg.in/volatiletech/null.v6"
	"time"
)

// CampaignSendFailure is a single failed attempt, with the recipient as it was
// at send time. It is written together with the campaign's cumulative count.
type CampaignSendFailure struct {
	CampaignID              int
	CampaignOwnerUserID     null.Int
	CampaignOrganizationID  null.Int
	RecipientType           string
	RecipientID             int64
	RecipientOrganizationID int
	CustomerCode            string
	Name                    string
	Email                   string
	Stage                   string
	Category                string
	SMTPCode                int
	Error                   string
}

type CampaignSendErrorRow struct {
	RecipientType  string    `db:"recipient_type" json:"recipient_type"`
	RecipientID    int64     `db:"recipient_id" json:"recipient_id"`
	OrganizationID int       `db:"recipient_organization_id" json:"organization_id"`
	CustomerCode   string    `db:"customer_code" json:"customer_code"`
	Name           string    `db:"name" json:"name"`
	Email          string    `db:"email" json:"email"`
	Stage          string    `db:"stage" json:"stage"`
	Category       string    `db:"category" json:"category"`
	SMTPCode       int       `db:"smtp_code" json:"smtp_code"`
	Error          string    `db:"error" json:"error"`
	Count          int       `db:"count" json:"count"`
	FirstAt        time.Time `db:"first_at" json:"first_at"`
	LastAt         time.Time `db:"last_at" json:"last_at"`
}

type CampaignSendErrorReason struct {
	Category string `db:"category" json:"category"`
	Count    int    `db:"count" json:"count"`
}

type CampaignSendErrorFilters struct {
	Search           string
	Category         string
	IncludePrivate   bool
	IncludePool      bool
	SensitivePrivate bool
	SensitivePool    bool
}

type CampaignSendErrorReport struct {
	Results          []CampaignSendErrorRow    `json:"results"`
	Reasons          []CampaignSendErrorReason `json:"reasons"`
	RecordedErrors   int                       `db:"recorded_errors" json:"recorded_errors"`
	HistoricalErrors int                       `db:"historical_errors" json:"historical_errors"`
	Total            int                       `db:"total" json:"total"`
	Page             int                       `json:"page"`
	PerPage          int                       `json:"per_page"`
	HasPrivate       bool                      `db:"has_private" json:"-"`
	HasPool          bool                      `db:"has_pool" json:"-"`
	CanExport        bool                      `json:"can_export"`
}
