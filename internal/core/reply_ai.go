package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// ErrReplyAILeaseLost signals that the claimed queue row was re-leased by
// another worker (its lease expired while the classifier was running). The
// caller must not apply any customer mutation or mark the row failed.
var ErrReplyAILeaseLost = errors.New("reply AI event lease lost")

// ReplyAIAction is the bounded, already-classified mutation request emitted by
// the inbound-reply worker. It intentionally contains no reply body.
type ReplyAIAction struct {
	EventID              int
	LeaseToken           string
	CustomerID           int
	Intent               string
	Confidence           float64
	ReasonCode           string
	Model                string
	OccurredAt           time.Time
	PoolContactID        int64
	PoolID               int
	SourceAllocationID   int64
	SourceOrganizationID int64
}

func (c *Core) FindReplyAIPoolContact(access models.WorkspaceAccess, email string) (models.PoolContact, PublicPoolRecipient, bool, error) {
	if !access.IsOrganization() || access.OrganizationID <= 0 {
		return models.PoolContact{}, PublicPoolRecipient{}, false, nil
	}
	var row struct {
		models.PoolContact
		PublicPoolRecipient
	}
	err := c.db.Get(&row, `SELECT pc.id,pc.uuid,pc.customer_code,pc.company_name,pc.email,pc.name,pc.attribs,pc.status,pc.created_at,pc.updated_at,cpr.campaign_id,cpr.pool_contact_id,cpr.organization_id,cpr.allocation_id FROM pool_contacts pc JOIN campaign_pool_recipients cpr ON cpr.pool_contact_id=pc.id WHERE LOWER(pc.email)=LOWER($1) AND cpr.organization_id=$2 AND pc.status='active' AND cpr.status IN ('pending','queued','sent') ORDER BY cpr.created_at DESC LIMIT 1`, email, access.OrganizationID)
	if err == sql.ErrNoRows {
		return models.PoolContact{}, PublicPoolRecipient{}, false, nil
	}
	if err != nil {
		return models.PoolContact{}, PublicPoolRecipient{}, false, err
	}
	return row.PoolContact, row.PublicPoolRecipient, true, nil
}

// FindReplyAIWorkspaceCustomer resolves a sender only inside the reply
// mailbox owner's active resource boundary. A caller must treat false as a
// no-action condition; it must never fall back to an unscoped email search.
func (c *Core) FindReplyAIWorkspaceCustomer(access models.WorkspaceAccess, email string) (models.Customer, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return models.Customer{}, false, nil
	}

	scope, args := workspaceManagedCustomerPredicate(access, "s", 1)
	stmt := fmt.Sprintf(`
		SELECT s.id, s.uuid, s.email, s.status
		FROM customers s
		WHERE (%s) AND LOWER(s.email) = $%d
		ORDER BY s.id
		LIMIT 2`, scope, len(args)+1)
	args = append(args, email)

	var rows []models.Customer
	if err := c.db.Select(&rows, stmt, args...); err != nil {
		return models.Customer{}, false, workspaceQueryError("resolving reply AI customer", err)
	}
	if len(rows) != 1 {
		return models.Customer{}, false, nil
	}
	return rows[0], true, nil
}

// ApplyReplyAIAction rechecks and locks the mailbox owner's workspace before
// applying a global customer blocklist. Complaint actions additionally create
// exactly one bounce row tied to the durable queue event, making retries safe.
// The terminal queue update runs in the same transaction, so a crash cannot
// leave a completed blocklist action eligible for another complaint attempt.
// Pool replies follow the same rule: the leased event row is locked before any
// side effect is written, and a terminal update that does not affect exactly
// one row rolls everything back and leaves the event for the next retry.
func (c *Core) ApplyReplyAIAction(access models.WorkspaceAccess, action ReplyAIAction) error {
	if action.EventID < 1 || (action.CustomerID < 1 && action.PoolContactID < 1) {
		return fmt.Errorf("reply AI action requires an event and customer")
	}
	if action.Intent != models.ReplyAIIntentUnsubscribe && action.Intent != models.ReplyAIIntentComplaint {
		return fmt.Errorf("reply AI action has unsupported intent %q", action.Intent)
	}
	if action.OccurredAt.IsZero() {
		action.OccurredAt = time.Now()
	}

	if action.PoolContactID > 0 {
		if action.PoolID <= 0 || !access.IsOrganization() || access.OrganizationID <= 0 || action.SourceOrganizationID != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "pool reply is outside the active organization")
		}
		tx, err := c.db.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// Lock the leased event row before writing any side effect. Under READ
		// COMMITTED a locking read re-checks the predicate against the newest
		// row version, so a concurrent reclaim that rotated the lease token
		// while the classifier ran fails this claim instead of letting both
		// workers commit.
		var held bool
		if err := tx.Get(&held, `
			SELECT EXISTS(
				SELECT 1 FROM reply_ai_events
				WHERE id = $1 AND status = 'processing' AND lease_token = $2::UUID
				FOR UPDATE)`,
			action.EventID, action.LeaseToken); err != nil {
			return err
		}
		if !held {
			return ErrReplyAILeaseLost
		}
		meta, _ := json.Marshal(map[string]any{"event_id": action.EventID, "intent": action.Intent, "confidence": action.Confidence, "reason_code": action.ReasonCode, "model": action.Model})
		if action.Intent == models.ReplyAIIntentComplaint {
			// The event id is the idempotency key, exactly as in the customer
			// branch: complaint counts must not double when the same durable
			// event is applied more than once.
			if _, err := tx.Exec(`INSERT INTO bounces(pool_contact_id,type,source,meta,created_at,source_pool_id,source_allocation_id,source_organization_id,reply_ai_event_id) VALUES($1,'complaint',$2,$3,$4,$5,NULLIF($6,0),$7,$8) ON CONFLICT (reply_ai_event_id) DO NOTHING`, action.PoolContactID, models.ReplyAISource, meta, action.OccurredAt, action.PoolID, action.SourceAllocationID, action.SourceOrganizationID, action.EventID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,source) VALUES($1,$2,$3,NULLIF($4,0),$5,'reply_ai') ON CONFLICT(pool_id,organization_id,contact_id) DO UPDATE SET reason=EXCLUDED.reason,source='reply_ai',removed_at=NOW(),restored_at=NULL`, action.PoolID, action.SourceOrganizationID, action.PoolContactID, action.SourceAllocationID, action.Intent); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE reply_ai_events SET pool_contact_id=$2,pool_id=$3,source_allocation_id=$4,source_organization_id=$5,intent=$6,confidence=$7,reason_code=$8,model=$9,action='blocklisted',status='processed',body='',classified_at=NOW(),actioned_at=NOW(),lease_expires_at=NULL,lease_token=NULL,updated_at=NOW() WHERE id=$1 AND status='processing' AND lease_token=$10::UUID`, action.EventID, action.PoolContactID, action.PoolID, action.SourceAllocationID, action.SourceOrganizationID, action.Intent, action.Confidence, action.ReasonCode, action.Model, action.LeaseToken)
		if err != nil {
			return err
		}
		// The terminal transition is the commit gate: if it did not update the
		// leased row, this worker lost the lease, so the side effects written
		// above must roll back and the event stays claimable.
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrReplyAILeaseLost
		}
		return tx.Commit()
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, []int{action.CustomerID}, func(tx *sqlx.Tx) error {
		// Re-check the lease inside the transaction that will perform the
		// mutation. A claim whose lease expired while the external classifier
		// ran must not blocklist or record a complaint twice.
		var held bool
		if err := tx.Get(&held, `
			SELECT EXISTS(
				SELECT 1 FROM reply_ai_events
				WHERE id = $1 AND status = 'processing' AND lease_token = $2::UUID
				FOR UPDATE)`,
			action.EventID, action.LeaseToken); err != nil {
			return workspaceQueryError("verifying reply AI event lease", err)
		}
		if !held {
			return ErrReplyAILeaseLost
		}

		if action.Intent == models.ReplyAIIntentComplaint {
			meta, err := json.Marshal(map[string]any{
				"event_id":    action.EventID,
				"intent":      action.Intent,
				"confidence":  action.Confidence,
				"reason_code": action.ReasonCode,
				"model":       action.Model,
			})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`
				INSERT INTO bounces (customer_id, type, source, meta, reply_ai_event_id, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (reply_ai_event_id) DO NOTHING`,
				action.CustomerID, models.BounceTypeComplaint, models.ReplyAISource,
				meta, action.EventID, action.OccurredAt); err != nil {
				return workspaceQueryError("recording reply AI complaint", err)
			}
		}

		if _, err := tx.Stmtx(c.q.BlocklistCustomers).Exec(pq.Array([]int{action.CustomerID})); err != nil {
			return workspaceQueryError("blocklisting reply AI customer", err)
		}
		res, err := tx.Exec(`
			UPDATE reply_ai_events
			SET customer_id = $2,
				intent = $3,
				confidence = $4,
				reason_code = $5,
				model = $6,
				action = 'blocklisted',
				status = 'processed',
				body = '',
				last_error = '',
				classified_at = NOW(),
				actioned_at = NOW(),
				lease_expires_at = NULL,
				lease_token = NULL,
				updated_at = NOW()
			WHERE id = $1 AND status = 'processing' AND lease_token = $7::UUID`,
			action.EventID, action.CustomerID, action.Intent, action.Confidence, action.ReasonCode, action.Model, action.LeaseToken)
		if err != nil {
			return workspaceQueryError("finishing reply AI event", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrReplyAILeaseLost
		}
		return nil
	})
}
