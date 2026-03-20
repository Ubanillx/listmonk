package models

import null "gopkg.in/volatiletech/null.v6"

type CampaignSubscriber struct {
	Subscriber
	RecipientStatus string    `db:"recipient_status" json:"recipient_status"`
	SentAt          null.Time `db:"sent_at" json:"sent_at"`
}
