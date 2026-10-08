package core

import (
	"database/sql"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
)

// ReplyAIDelivery binds an inbound sender to a sent recipient in the actual
// receiving mailbox. An organization mailbox is shared, but it does not grant
// permission to mutate arbitrary customers belonging to its creator.
type ReplyAIDelivery struct {
	CampaignID     int    `db:"campaign_id"`
	CampaignUUID   string `db:"campaign_uuid"`
	CustomerID     int    `db:"customer_id"`
	UserID         int    `db:"user_id"`
	PoolContactID  int64  `db:"pool_contact_id"`
	PoolID         int    `db:"pool_id"`
	AllocationID   int64  `db:"allocation_id"`
	OrganizationID int    `db:"organization_id"`
}

const replyAIDeliverySQL = `
	SELECT c.id AS campaign_id, c.uuid::TEXT AS campaign_uuid, s.id AS customer_id, s.owner_user_id AS user_id,
		0::BIGINT AS pool_contact_id, 0 AS pool_id, 0::BIGINT AS allocation_id,
		COALESCE(s.organization_id,0) AS organization_id
	FROM reply_mailboxes m
	JOIN campaigns c ON c.organization_id IS NOT DISTINCT FROM m.organization_id
	JOIN campaign_recipients cr ON cr.campaign_id=c.id AND cr.status='sent'
	JOIN customers s ON s.id=cr.customer_id
	WHERE m.id=$1 AND LOWER(COALESCE(cr.email_snapshot,s.email))=LOWER($2)
		AND ((cr.reply_to_snapshot IS NOT NULL AND LOWER(cr.reply_to_snapshot)=LOWER(m.email))
			OR (cr.reply_to_snapshot IS NULL AND c.reply_mailbox_id=m.id))
		AND s.organization_id IS NOT DISTINCT FROM m.organization_id
		AND c.organization_id IS NOT DISTINCT FROM m.organization_id
		AND s.transfer_pending_at IS NULL AND s.owner_user_id IS NOT NULL
		AND (m.organization_id IS NOT NULL OR s.owner_user_id=m.user_id)
		AND ($3=0 OR c.id=$3)
	UNION ALL
	SELECT cpr.campaign_id, c.uuid::TEXT, 0, m.user_id, pc.id, cpr.pool_id,
		COALESCE(cpr.allocation_id,0), cpr.organization_id
	FROM reply_mailboxes m
	JOIN campaign_pool_recipients cpr ON cpr.organization_id=m.organization_id
		AND LOWER(cpr.reply_to_snapshot)=LOWER(m.email)
	JOIN pool_contacts pc ON pc.id=cpr.pool_contact_id AND pc.status='active'
	JOIN campaigns c ON c.id=cpr.campaign_id
	WHERE m.id=$1 AND cpr.status='sent' AND LOWER(cpr.email_snapshot)=LOWER($2)
		AND LOWER(cpr.reply_to_snapshot)=LOWER(m.email)
		AND ($3=0 OR cpr.campaign_id=$3)
	ORDER BY campaign_id DESC`

func findReplyAIDelivery(db sqlx.Queryer, mailboxID int, email string, campaignID int, references string) (ReplyAIDelivery, bool, error) {
	var rows []ReplyAIDelivery
	if err := sqlx.Select(db, &rows, replyAIDeliverySQL, mailboxID, strings.TrimSpace(email), campaignID); err != nil {
		return ReplyAIDelivery{}, false, err
	}
	if len(rows) == 0 {
		return ReplyAIDelivery{}, false, nil
	}
	refs := models.ParseReplyReferences(references)
	if len(refs) > 0 {
		var matched []ReplyAIDelivery
		for _, ref := range refs {
			for _, row := range rows {
				if strings.EqualFold(row.CampaignUUID, ref.CampaignUUID) && row.CustomerID == ref.CustomerID && row.PoolContactID == ref.PoolContactID {
					matched = append(matched, row)
				}
			}
			// In-Reply-To is placed first by ingestion, followed by References
			// from newest to oldest. Never fall back to sender-only matching if
			// a listmonk reference identifies a different mailbox or recipient.
			if len(matched) > 0 {
				break
			}
		}
		rows = matched
		if len(rows) == 0 {
			return ReplyAIDelivery{}, false, nil
		}
	}
	first := rows[0]
	for _, row := range rows[1:] {
		if row.CustomerID != first.CustomerID || row.PoolContactID != first.PoolContactID ||
			row.OrganizationID != first.OrganizationID || row.PoolID != first.PoolID || row.AllocationID != first.AllocationID {
			return ReplyAIDelivery{}, false, nil
		}
	}
	return first, true, nil
}

func (c *Core) FindReplyAIDelivery(mailboxID int, email string, campaignID int, references ...string) (ReplyAIDelivery, bool, error) {
	refs := ""
	if len(references) > 0 {
		refs = references[0]
	}
	return findReplyAIDelivery(c.db, mailboxID, email, campaignID, refs)
}

// lockReplyAIAuthority runs after the workspace lock and before any side effect.
// Mailbox edits serialize on its row; user/member changes are also rechecked.
func (c *Core) lockReplyAIAuthority(tx *sqlx.Tx, access models.WorkspaceAccess, action ReplyAIAction) error {
	var enabled bool
	if err := tx.Get(&enabled, `SELECT COALESCE((value->>'enabled')::BOOLEAN,FALSE) FROM settings WHERE key='reply_ai' FOR SHARE`); err != nil {
		return replyAIAuthorityReadError(err)
	}
	if !enabled {
		return workspaceMutationError()
	}
	var event struct {
		MailboxID  int    `db:"reply_mailbox_id"`
		Email      string `db:"from_email"`
		References string `db:"reply_references"`
	}
	if err := tx.Get(&event, `SELECT reply_mailbox_id,from_email,reply_references FROM reply_ai_events
		WHERE id=$1 AND status='processing' AND lease_token=$2::UUID FOR UPDATE`, action.EventID, action.LeaseToken); err != nil {
		if err == sql.ErrNoRows {
			return ErrReplyAILeaseLost
		}
		return err
	}
	var mailbox struct {
		UserID         int           `db:"user_id"`
		OrganizationID sql.NullInt64 `db:"organization_id"`
	}
	if err := tx.Get(&mailbox, `SELECT m.user_id,m.organization_id FROM reply_mailboxes m
		JOIN users u ON u.id=m.user_id AND u.status='enabled'
		WHERE m.id=$1 AND m.status='active' AND m.ai_enabled AND m.verified_at IS NOT NULL
		FOR UPDATE OF m FOR SHARE OF u`, event.MailboxID); err != nil {
		if err == sql.ErrNoRows {
			return workspaceMutationError()
		}
		return err
	}
	if int(mailbox.OrganizationID.Int64) != access.OrganizationID {
		return workspaceMutationError()
	}
	if access.IsOrganization() {
		var allowed bool
		if err := tx.Get(&allowed, `SELECT EXISTS(SELECT 1 FROM organization_members
			WHERE organization_id=$1 AND user_id=$2 AND removed_at IS NULL)
			OR EXISTS(SELECT 1 FROM users WHERE id=$2 AND user_role_id=1 AND status='enabled')`, access.OrganizationID, mailbox.UserID); err != nil {
			return err
		}
		if !allowed {
			return workspaceMutationError()
		}
	} else if mailbox.UserID != access.UserID {
		return workspaceMutationError()
	}
	var locked int
	if action.CustomerID > 0 {
		if err := tx.Get(&locked, `SELECT customer_id FROM campaign_recipients WHERE campaign_id=$1 AND customer_id=$2 AND status='sent' FOR SHARE`, action.CampaignID, action.CustomerID); err != nil {
			return replyAIAuthorityReadError(err)
		}
	} else {
		if err := tx.Get(&locked, `SELECT pool_contact_id FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2 AND status='sent' FOR SHARE`, action.CampaignID, action.PoolContactID); err != nil {
			return replyAIAuthorityReadError(err)
		}
		if err := tx.Get(&locked, `SELECT id FROM pool_contacts WHERE id=$1 AND status='active' FOR SHARE`, action.PoolContactID); err != nil {
			return replyAIAuthorityReadError(err)
		}
	}
	delivery, ok, err := findReplyAIDelivery(tx, event.MailboxID, event.Email, 0, event.References)
	if err != nil {
		return err
	}
	if !ok || delivery.CampaignID != action.CampaignID || delivery.CustomerID != action.CustomerID || delivery.PoolContactID != action.PoolContactID ||
		delivery.PoolID != action.PoolID || delivery.AllocationID != action.SourceAllocationID {
		return workspaceMutationError()
	}
	if action.CustomerID > 0 {
		var active bool
		if err := tx.Get(&active, `SELECT status='enabled' FROM users WHERE id=$1 FOR SHARE`, delivery.UserID); err != nil {
			return replyAIAuthorityReadError(err)
		}
		if !active {
			return workspaceMutationError()
		}
	}
	return nil
}

func replyAIAuthorityReadError(err error) error {
	if err == sql.ErrNoRows {
		return workspaceMutationError()
	}
	// A database outage must retry the queue event, not discard its body as
	// an authorization failure.
	return err
}
