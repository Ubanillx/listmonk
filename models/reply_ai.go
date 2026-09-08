package models

import null "gopkg.in/volatiletech/null.v6"

const (
	ReplyAIIntentUnsubscribe = "unsubscribe"
	ReplyAIIntentComplaint   = "complaint"
	ReplyAIIntentOther       = "other"

	ReplyAIEventStatusPending    = "pending"
	ReplyAIEventStatusProcessing = "processing"
	ReplyAIEventStatusProcessed  = "processed"
	ReplyAIEventStatusIgnored    = "ignored"
	ReplyAIEventStatusFailed     = "failed"

	ReplyAIActionPending     = "pending"
	ReplyAIActionIgnored     = "ignored"
	ReplyAIActionBlocklisted = "blocklisted"

	ReplyAISource = "reply_ai"
)

// ReplyAISettings configures the OpenAI-compatible classifier shared by
// explicitly enabled customer-reply mailboxes. APIKey is always masked before
// it is returned by the settings API.
type ReplyAISettings struct {
	Enabled       bool    `json:"enabled"`
	BaseURL       string  `json:"base_url"`
	APIKey        string  `json:"api_key,omitempty"`
	Model         string  `json:"model"`
	Timeout       string  `json:"timeout"`
	MinConfidence float64 `json:"min_confidence"`
}

// ReplyAIEvent is an auditable inbound reply classification. Body is retained
// only while a queued item needs retrying and is never returned through APIs.
type ReplyAIEvent struct {
	Base

	ReplyMailboxID       int      `db:"reply_mailbox_id" json:"reply_mailbox_id"`
	CustomerID           null.Int `db:"customer_id" json:"customer_id"`
	PoolContactID        null.Int `db:"pool_contact_id" json:"pool_contact_id"`
	PoolID               null.Int `db:"pool_id" json:"pool_id"`
	SourceSegmentID      null.Int `db:"source_segment_id" json:"source_segment_id"`
	SourceOrganizationID null.Int `db:"source_organization_id" json:"source_organization_id"`
	FromEmail            string   `db:"from_email" json:"from_email"`
	Subject              string   `db:"subject" json:"subject"`
	MessageKey           string   `db:"message_key" json:"-"`
	Body                 string   `db:"body" json:"-"`
	BodyHash             string   `db:"body_hash" json:"body_hash"`

	Intent     string  `db:"intent" json:"intent"`
	Confidence float64 `db:"confidence" json:"confidence"`
	ReasonCode string  `db:"reason_code" json:"reason_code"`
	Model      string  `db:"model" json:"model"`
	Action     string  `db:"action" json:"action"`
	Status     string  `db:"status" json:"status"`
	Attempts   int     `db:"attempts" json:"attempts"`
	LastError  string  `db:"last_error" json:"-"`

	ReceivedAt     null.Time `db:"received_at" json:"received_at"`
	ClassifiedAt   null.Time `db:"classified_at" json:"classified_at"`
	ActionedAt     null.Time `db:"actioned_at" json:"actioned_at"`
	NextAttemptAt  null.Time `db:"next_attempt_at" json:"-"`
	LeaseExpiresAt null.Time `db:"lease_expires_at" json:"-"`
	LeaseToken     string    `db:"lease_token" json:"-"`
}
