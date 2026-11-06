package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// ReplyBounceInput describes one report recipient resolved from a DSN in a
// reply mailbox. Delivery is the pre-resolved target; it is re-resolved inside
// the transaction and must still match before any side effect is written.
type ReplyBounceInput struct {
	MailboxID  int
	MessageKey string
	Recipient  string
	References string
	Type       string // hard, soft or unknown
	Status     string
	Diagnostic string
	MessageID  string
	Sender     string
	Subject    string
	OccurredAt time.Time
	Delivery   ReplyAIDelivery
}

// ReplyBounceResult reports what the transaction did. Duplicate is true when
// the ledger already contained this mailbox/message/recipient tuple.
type ReplyBounceResult struct {
	EventID         int64
	Outcome         string // recorded, blocklisted or ignored
	ReasonCode      string
	ActionCount     int
	ActionThreshold int
	Duplicate       bool
}

type replyBounceAction struct {
	Count  int    `json:"count"`
	Action string `json:"action"`
}

// RecordReplyBounce applies the configured hard-bounce policy to one DSN
// recipient. Everything runs in one transaction: mailbox and workspace
// revalidation, re-resolution of the delivery, the ledger claim, the bounce
// row, the optional blocklist/exclusion, and the terminal ledger state. A
// database failure therefore leaves no partial change; a retry reuses the
// ledger key and is a no-op.
func (c *Core) RecordReplyBounce(input ReplyBounceInput) (ReplyBounceResult, error) {
	if input.MailboxID < 1 || strings.TrimSpace(input.MessageKey) == "" || strings.TrimSpace(input.Recipient) == "" {
		return ReplyBounceResult{}, fmt.Errorf("reply bounce requires a mailbox, message key and recipient")
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now()
	}
	if input.Type != "hard" && input.Type != "soft" {
		input.Type = "unknown"
	}

	tx, err := c.db.Beginx()
	if err != nil {
		return ReplyBounceResult{}, err
	}
	defer tx.Rollback()

	mailbox, err := lockReplyBounceMailbox(tx, input.MailboxID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ReplyBounceResult{Outcome: "ignored", ReasonCode: "workspace_not_writable"}, nil
		}
		return ReplyBounceResult{}, err
	}
	orgID := int64(0)
	if mailbox.OrganizationID.Valid {
		orgID = mailbox.OrganizationID.Int64
	}
	if !mailbox.ScanEnabled || !mailbox.BounceEnabled || mailbox.Status != "active" || !mailbox.VerifiedAt.Valid {
		return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
	}
	if int64(input.Delivery.OrganizationID) != orgID {
		return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
	}
	if orgID > 0 {
		var active bool
		if err := tx.Get(&active, `SELECT status='active' FROM organizations WHERE id=$1 FOR SHARE`, orgID); err != nil {
			if err == sql.ErrNoRows {
				return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
			}
			return ReplyBounceResult{}, err
		}
		if !active {
			return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
		}
		var allowed bool
		if err := tx.Get(&allowed, `SELECT EXISTS(SELECT 1 FROM organization_members
			WHERE organization_id=$1 AND user_id=$2 AND removed_at IS NULL)
			OR EXISTS(SELECT 1 FROM users WHERE id=$2 AND user_role_id=1 AND status='enabled')`,
			orgID, mailbox.UserID); err != nil {
			return ReplyBounceResult{}, err
		}
		if !allowed {
			return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
		}
	} else if mailbox.UserID != input.Delivery.UserID {
		return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
	}

	var bounceEnabled bool
	if err := tx.Get(&bounceEnabled, `SELECT COALESCE((SELECT value::BOOLEAN FROM settings WHERE key='bounce.enabled'), FALSE)`); err != nil {
		return ReplyBounceResult{}, err
	}
	if !bounceEnabled {
		return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "global_bounce_disabled")
	}
	hard, err := readReplyBouncePolicy(tx)
	if err != nil {
		return ReplyBounceResult{}, err
	}

	delivery, ok, err := findReplyAIDelivery(tx, input.MailboxID, input.Recipient, 0, input.References)
	if err != nil {
		return ReplyBounceResult{}, err
	}
	if !ok {
		return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "unmatched_delivery")
	}
	if delivery.CampaignID != input.Delivery.CampaignID ||
		delivery.CustomerID != input.Delivery.CustomerID ||
		delivery.PoolContactID != input.Delivery.PoolContactID ||
		delivery.PoolID != input.Delivery.PoolID ||
		delivery.AllocationID != input.Delivery.AllocationID ||
		delivery.OrganizationID != input.Delivery.OrganizationID {
		return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "delivery_changed")
	}

	meta, _ := json.Marshal(map[string]any{
		"mailbox_id":      input.MailboxID,
		"mailbox_email":   mailbox.Email,
		"message_id":      truncateReplyBounceText(input.MessageID, 512),
		"report_status":   truncateReplyBounceText(input.Status, 64),
		"diagnostic":      truncateReplyBounceText(input.Diagnostic, 2048),
		"classify_reason": input.Type,
		"channel":         "reply_mailbox",
	})

	if delivery.CustomerID > 0 {
		var customer struct {
			ID                int           `db:"id"`
			Status            string        `db:"status"`
			OrganizationID    sql.NullInt64 `db:"organization_id"`
			OwnerUserID       sql.NullInt64 `db:"owner_user_id"`
			TransferPendingAt sql.NullTime  `db:"transfer_pending_at"`
		}
		if err := tx.Get(&customer, `SELECT id, status, organization_id, owner_user_id, transfer_pending_at
			FROM customers WHERE id=$1 FOR UPDATE`, delivery.CustomerID); err != nil {
			if err == sql.ErrNoRows {
				return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
			}
			return ReplyBounceResult{}, err
		}
		customerOrg := int64(0)
		if customer.OrganizationID.Valid {
			customerOrg = customer.OrganizationID.Int64
		}
		if customerOrg != orgID || !customer.OwnerUserID.Valid || customer.TransferPendingAt.Valid {
			return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
		}
		var recipientStatus string
		if err := tx.Get(&recipientStatus, `SELECT status FROM campaign_recipients
			WHERE campaign_id=$1 AND customer_id=$2 FOR SHARE`, delivery.CampaignID, delivery.CustomerID); err != nil {
			if err == sql.ErrNoRows {
				return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "delivery_changed")
			}
			return ReplyBounceResult{}, err
		}
		if recipientStatus != "sent" {
			return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "delivery_changed")
		}
		if customer.Status == "blocklisted" {
			return c.finishReplyBounceIgnored(tx, input, delivery, "already_blocklisted")
		}

		eventID, inserted, err := insertReplyBounceEvent(tx, input, "recorded", "", delivery, 0, 0)
		if err != nil {
			return ReplyBounceResult{}, err
		}
		if !inserted {
			_ = tx.Rollback()
			return ReplyBounceResult{Duplicate: true, Outcome: "ignored", ReasonCode: "already_processed"}, nil
		}
		if _, err := tx.Exec(`INSERT INTO bounces(customer_id, campaign_id, type, source, meta, created_at)
			VALUES($1,$2,$3,'reply_mailbox',$4,$5)`,
			delivery.CustomerID, delivery.CampaignID, bounceRowType(input.Type), meta, input.OccurredAt); err != nil {
			return ReplyBounceResult{}, workspaceQueryError("recording reply bounce", err)
		}
		var count int
		if err := tx.Get(&count, `SELECT COUNT(*) FROM bounces WHERE customer_id=$1 AND type=$2`,
			delivery.CustomerID, bounceRowType(input.Type)); err != nil {
			return ReplyBounceResult{}, workspaceQueryError("counting reply bounces", err)
		}
		outcome, reason := replyBouncePolicyOutcome(input.Type, hard, count)
		if outcome == "blocklisted" {
			if _, err := tx.Exec(`UPDATE customers SET status='blocklisted', updated_at=NOW()
				WHERE id=$1 AND status <> 'blocklisted'`, delivery.CustomerID); err != nil {
				return ReplyBounceResult{}, workspaceQueryError("blocklisting reply bounce customer", err)
			}
		}
		if err := finishReplyBounceEvent(tx, eventID, outcome, reason, count, hard.Count); err != nil {
			return ReplyBounceResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ReplyBounceResult{}, err
		}
		return ReplyBounceResult{EventID: eventID, Outcome: outcome, ReasonCode: reason, ActionCount: count, ActionThreshold: hard.Count}, nil
	}

	if delivery.PoolContactID > 0 {
		var contactStatus string
		if err := tx.Get(&contactStatus, `SELECT status FROM pool_contacts WHERE id=$1 FOR UPDATE`, delivery.PoolContactID); err != nil {
			if err == sql.ErrNoRows {
				return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
			}
			return ReplyBounceResult{}, err
		}
		if contactStatus != "active" {
			return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "workspace_not_writable")
		}
		var recipientStatus string
		if err := tx.Get(&recipientStatus, `SELECT status FROM campaign_pool_recipients
			WHERE campaign_id=$1 AND pool_contact_id=$2 FOR SHARE`, delivery.CampaignID, delivery.PoolContactID); err != nil {
			if err == sql.ErrNoRows {
				return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "delivery_changed")
			}
			return ReplyBounceResult{}, err
		}
		if recipientStatus != "sent" {
			return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "delivery_changed")
		}
		var excluded bool
		if err := tx.Get(&excluded, `SELECT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions
			WHERE pool_id=$1 AND organization_id=$2 AND contact_id=$3 AND restored_at IS NULL)`,
			delivery.PoolID, delivery.OrganizationID, delivery.PoolContactID); err != nil {
			return ReplyBounceResult{}, err
		}
		if excluded {
			return c.finishReplyBounceIgnored(tx, input, delivery, "already_excluded")
		}

		eventID, inserted, err := insertReplyBounceEvent(tx, input, "recorded", "", delivery, 0, 0)
		if err != nil {
			return ReplyBounceResult{}, err
		}
		if !inserted {
			_ = tx.Rollback()
			return ReplyBounceResult{Duplicate: true, Outcome: "ignored", ReasonCode: "already_processed"}, nil
		}
		if _, err := tx.Exec(`INSERT INTO bounces(pool_contact_id, campaign_id, type, source, meta, created_at,
				source_pool_id, source_allocation_id, source_organization_id)
			VALUES($1,$2,$3,'reply_mailbox',$4,$5,NULLIF($6,0),NULLIF($7,0),$8)`,
			delivery.PoolContactID, delivery.CampaignID, bounceRowType(input.Type), meta, input.OccurredAt,
			delivery.PoolID, delivery.AllocationID, delivery.OrganizationID); err != nil {
			return ReplyBounceResult{}, workspaceQueryError("recording reply pool bounce", err)
		}
		var count int
		if err := tx.Get(&count, `SELECT COUNT(*) FROM bounces
			WHERE pool_contact_id=$1 AND type=$2 AND source_organization_id=$3`,
			delivery.PoolContactID, bounceRowType(input.Type), delivery.OrganizationID); err != nil {
			return ReplyBounceResult{}, workspaceQueryError("counting reply pool bounces", err)
		}
		outcome, reason := replyBouncePolicyOutcome(input.Type, hard, count)
		if outcome == "blocklisted" {
			if _, err := tx.Exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,source,removed_at,restored_at)
				VALUES($1,$2,$3,NULLIF($4,0),$5,'bounce',NOW(),NULL)
				ON CONFLICT(pool_id,organization_id,contact_id) DO UPDATE
				SET allocation_id=EXCLUDED.allocation_id,reason=EXCLUDED.reason,source='bounce',removed_at=NOW(),restored_at=NULL`,
				delivery.PoolID, delivery.OrganizationID, delivery.PoolContactID, delivery.AllocationID, bounceRowType(input.Type)); err != nil {
				return ReplyBounceResult{}, workspaceQueryError("excluding reply bounce pool contact", err)
			}
		}
		if err := finishReplyBounceEvent(tx, eventID, outcome, reason, count, hard.Count); err != nil {
			return ReplyBounceResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ReplyBounceResult{}, err
		}
		return ReplyBounceResult{EventID: eventID, Outcome: outcome, ReasonCode: reason, ActionCount: count, ActionThreshold: hard.Count}, nil
	}

	return c.finishReplyBounceIgnored(tx, input, ReplyAIDelivery{}, "unmatched_delivery")
}

// RecordReplyBounceIgnored records a DSN that could not be attributed or parsed.
// It never changes a customer. A duplicate message/recipient returns id 0.
func (c *Core) RecordReplyBounceIgnored(input ReplyBounceInput, reason string) (int64, error) {
	if input.MailboxID < 1 || strings.TrimSpace(input.MessageKey) == "" {
		return 0, nil
	}
	bounceType := input.Type
	if bounceType != "hard" && bounceType != "soft" {
		bounceType = "unknown"
	}
	var id int64
	err := c.db.Get(&id, `INSERT INTO reply_bounce_events(reply_mailbox_id,message_key,recipient,sender,subject,
		report_status,diagnostic,bounce_type,outcome,reason_code,processed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,'ignored',$9,NOW())
		ON CONFLICT(reply_mailbox_id,message_key,recipient) DO NOTHING RETURNING id`,
		input.MailboxID, truncateReplyBounceText(input.MessageKey, 512), truncateReplyBounceText(input.Recipient, 320),
		truncateReplyBounceText(input.Sender, 512), truncateReplyBounceText(input.Subject, 512),
		truncateReplyBounceText(input.Status, 64), truncateReplyBounceText(input.Diagnostic, 2048),
		bounceType, reason)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

type replyBounceMailbox struct {
	UserID         int           `db:"user_id"`
	OrganizationID sql.NullInt64 `db:"organization_id"`
	Email          string        `db:"email"`
	Status         string        `db:"status"`
	ScanEnabled    bool          `db:"scan_enabled"`
	BounceEnabled  bool          `db:"bounce_enabled"`
	VerifiedAt     sql.NullTime  `db:"verified_at"`
}

func lockReplyBounceMailbox(tx *sqlx.Tx, id int) (replyBounceMailbox, error) {
	var mailbox replyBounceMailbox
	err := tx.Get(&mailbox, `SELECT m.user_id, m.organization_id, m.email, m.status, m.scan_enabled, m.bounce_enabled,
		m.verified_at
		FROM reply_mailboxes m
		JOIN users u ON u.id=m.user_id AND u.status='enabled'
		WHERE m.id=$1
		FOR UPDATE OF m FOR SHARE OF u`, id)
	return mailbox, err
}

func readReplyBouncePolicy(tx *sqlx.Tx) (replyBounceAction, error) {
	var raw []byte
	if err := tx.Get(&raw, `SELECT value FROM settings WHERE key='bounce.actions'`); err != nil {
		if err == sql.ErrNoRows {
			return replyBounceAction{}, nil
		}
		return replyBounceAction{}, err
	}
	actions := map[string]replyBounceAction{}
	if err := json.Unmarshal(raw, &actions); err != nil {
		return replyBounceAction{}, err
	}
	hard := actions["hard"]
	if hard.Count < 1 {
		hard.Count = 1
	}
	return hard, nil
}

func replyBouncePolicyOutcome(bounceType string, hard replyBounceAction, count int) (string, string) {
	if bounceType != "hard" {
		return "recorded", "soft_recorded"
	}
	switch hard.Action {
	case "blocklist":
		if count >= hard.Count {
			return "blocklisted", "blocklisted"
		}
		return "recorded", "recorded"
	case "unsubscribe", "delete":
		return "recorded", "action_not_supported"
	default:
		return "recorded", "recorded"
	}
}

func bounceRowType(bounceType string) string {
	if bounceType == "hard" || bounceType == "soft" {
		return bounceType
	}
	return "soft"
}

func insertReplyBounceEvent(tx *sqlx.Tx, input ReplyBounceInput,
	outcome, reason string, delivery ReplyAIDelivery, count, threshold int) (int64, bool, error) {
	var id int64
	err := tx.Get(&id, `INSERT INTO reply_bounce_events(reply_mailbox_id,message_key,recipient,sender,subject,
		report_status,diagnostic,bounce_type,outcome,reason_code,campaign_id,customer_id,pool_contact_id,
		pool_id,source_allocation_id,source_organization_id,action_count,action_threshold,processed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,0),NULLIF($12,0),NULLIF($13,0),
		NULLIF($14,0),NULLIF($15,0),NULLIF($16,0),$17,$18,$19)
		ON CONFLICT(reply_mailbox_id,message_key,recipient) DO NOTHING RETURNING id`,
		input.MailboxID, truncateReplyBounceText(input.MessageKey, 512), truncateReplyBounceText(input.Recipient, 320),
		truncateReplyBounceText(input.Sender, 512), truncateReplyBounceText(input.Subject, 512),
		truncateReplyBounceText(input.Status, 64), truncateReplyBounceText(input.Diagnostic, 2048),
		input.Type, outcome, reason, delivery.CampaignID, delivery.CustomerID, delivery.PoolContactID,
		delivery.PoolID, delivery.AllocationID, delivery.OrganizationID, count, threshold,
		processedAtForReplyBounce(outcome))
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func finishReplyBounceEvent(tx *sqlx.Tx, id int64, outcome, reason string, count, threshold int) error {
	_, err := tx.Exec(`UPDATE reply_bounce_events
		SET outcome=$2, reason_code=$3, action_count=$4, action_threshold=$5, processed_at=NOW()
		WHERE id=$1`, id, outcome, reason, count, threshold)
	return err
}

func (c *Core) finishReplyBounceIgnored(tx *sqlx.Tx, input ReplyBounceInput,
	delivery ReplyAIDelivery, reason string) (ReplyBounceResult, error) {
	eventID, inserted, err := insertReplyBounceEvent(tx, input, "ignored", reason, delivery, 0, 0)
	if err != nil {
		return ReplyBounceResult{}, err
	}
	if !inserted {
		_ = tx.Rollback()
		return ReplyBounceResult{Duplicate: true, Outcome: "ignored", ReasonCode: "already_processed"}, nil
	}
	if err := tx.Commit(); err != nil {
		return ReplyBounceResult{}, err
	}
	return ReplyBounceResult{EventID: eventID, Outcome: "ignored", ReasonCode: reason}, nil
}

func processedAtForReplyBounce(outcome string) any {
	if outcome == "ignored" {
		return time.Now()
	}
	return nil
}

func truncateReplyBounceText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max < 1 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
