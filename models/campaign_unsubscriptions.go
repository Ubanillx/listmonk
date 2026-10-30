package models

import "time"

type CampaignUnsubscription struct {
	RecipientType  string    `db:"recipient_type" json:"recipient_type"`
	RecipientID    int64     `db:"recipient_id" json:"recipient_id"`
	OrganizationID int       `db:"organization_id" json:"organization_id"`
	CustomerCode   string    `db:"customer_code" json:"customer_code"`
	Name           string    `db:"name" json:"name"`
	Email          string    `db:"email" json:"email"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
}

type CampaignUnsubscriptionFilters struct {
	Search                          string
	IncludePrivate, IncludePool     bool
	SensitivePrivate, SensitivePool bool
}
