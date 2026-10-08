package core

import (
	"database/sql"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// The helpers in this file are the transaction-aware form of the pool SQL in
// pools.go. A campaign create/update writes its campaign row, its
// customer_list/media relations, its reply mailbox and its pool audience in one
// transaction, so every statement below must run on the caller's *sqlx.Tx and
// never on c.db.
//
// The SQL itself exists exactly once. Each single-call helper in pools.go
// (ensurePool, resolvePoolRecipients, organizationReplyMailboxID,
// refreshPoolCampaignRecipients, refreshAllOrgPoolCampaignRecipients,
// AttachPoolToCampaign) now opens its own transaction and delegates here, which
// keeps the exported signatures and the statement text of every existing caller
// unchanged.

// withPoolTx runs fn inside one database transaction. It is the small bridge
// that lets the existing single-call helpers share the *Tx implementations
// instead of keeping a second copy of their SQL.
func (c *Core) withPoolTx(fn func(*sqlx.Tx) error) error {
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ensurePoolTx is the transaction-aware form of ensurePool.
func (c *Core) ensurePoolTx(tx *sqlx.Tx, poolID int) error {
	var typ string
	if err := tx.Get(&typ, `SELECT type::text FROM customer_lists WHERE id=$1`, poolID); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "pool not found")
		}
		return err
	}
	if typ != models.CustomerListTypePool {
		return echo.NewHTTPError(http.StatusBadRequest, "customer list is not a public pool")
	}
	return nil
}

// resolvePoolRecipientsTx is the transaction-aware form of
// resolvePoolRecipients: it reads the deliverable members of one pool audience,
// where a nil allocationID accepts every pool allocation the organization owns
// in the pool.
func (c *Core) resolvePoolRecipientsTx(tx *sqlx.Tx, poolID int, organizationID int64, allocationID *int64) ([]PoolRecipient, error) {
	if err := c.ensurePoolTx(tx, poolID); err != nil {
		return nil, err
	}
	var out []PoolRecipient
	err := tx.Select(&out, poolRecipientSelectSQL, poolID, organizationID, allocationID)
	return out, err
}

// organizationReplyMailboxIDTx is the transaction-aware form of
// organizationReplyMailboxID: the organization's single unified reply mailbox,
// but only while the mailbox row is active and verified.
func (c *Core) organizationReplyMailboxIDTx(tx *sqlx.Tx, organizationID int64) (*int64, error) {
	var mailboxID int64
	if err := tx.Get(&mailboxID, `
		SELECT o.reply_mailbox_id
		FROM organizations o
		JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id
			AND rm.status='active' AND (NOT rm.ai_enabled OR rm.verified_at IS NOT NULL)
		WHERE o.id=$1`, organizationID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &mailboxID, nil
}

// refreshAllOrgPoolCampaignRecipientsTx is the transaction-aware form of
// refreshAllOrgPoolCampaignRecipients: it refreshes the platform-level snapshot
// of one first-level pool audience.
func (c *Core) refreshAllOrgPoolCampaignRecipientsTx(tx *sqlx.Tx, campaignID, poolID int) error {
	var ids []int64
	if err := tx.Select(&ids, poolRecipientAllOrgSelectSQL, poolID, campaignID); err != nil {
		return err
	}
	if _, err := tx.Exec(poolRecipientAllOrgSnapshotPruneSQL, campaignID, poolID, pq.Int64Array(ids)); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(poolRecipientAllOrgSnapshotUpsertSQL, poolID, campaignID)
	return err
}

// refreshPoolCampaignRecipientsTx is the transaction-aware form of
// refreshPoolCampaignRecipients: it makes the campaign snapshot of one pool
// audience equal the deliverable members of that audience.
func (c *Core) refreshPoolCampaignRecipientsTx(tx *sqlx.Tx, campaignID int, aud poolAudience) error {
	recipients, err := c.resolvePoolRecipientsTx(tx, aud.poolID, aud.organizationID, aud.allocationID)
	if err != nil {
		return err
	}
	ids := make(pq.Int64Array, 0, len(recipients))
	for _, r := range recipients {
		ids = append(ids, r.ID)
	}
	if _, err := tx.Exec(poolRecipientSnapshotPruneSQL, campaignID, aud.poolID, aud.organizationID, aud.allocationID, ids); err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}
	_, err = tx.Exec(poolRecipientSnapshotUpsertSQL, aud.poolID, aud.organizationID, aud.allocationID, campaignID, aud.mailboxID)
	return err
}

// attachPoolToCampaignTx is the transaction-aware form of AttachPoolToCampaign:
// it records a pool/allocation audience on a campaign and refreshes that
// audience's recipient snapshot from the shared membership rule. Both the
// relation row and its snapshot therefore belong to the caller's transaction,
// which is what lets a failed campaign update leave neither of them behind.
func (c *Core) attachPoolToCampaignTx(tx *sqlx.Tx, campaignID, poolID int, allocationID *int64, organizationID int64, allOrganizations bool) error {
	var err error
	if err := c.ensurePoolTx(tx, poolID); err != nil {
		return err
	}
	if allOrganizations {
		// Platform-level audience: permission to send to every organization
		// is checked by the campaign API (campaigns:public_pool_send), not by
		// a per-organization pool delivery grant. The relation carries no
		// target organization, allocation or mailbox: recipients resolve per
		// contact to every active organization's allocation and its unified
		// reply mailbox. A previous rotation is discarded so the snapshot
		// refresh recomputes membership for the newly selected pool.
		var name string
		if err := tx.Get(&name, `SELECT name FROM customer_lists WHERE id=$1`, poolID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM campaign_pool_org_orders WHERE campaign_id=$1`, campaignID); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
			VALUES($1,NULL,$2,$3,NULL,NULL,NULL) ON CONFLICT DO NOTHING`, campaignID, name, poolID); err != nil {
			return err
		}
		return c.refreshAllOrgPoolCampaignRecipientsTx(tx, campaignID, poolID)
	}
	if organizationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "organization is required for pool audiences")
	}
	var permitted bool
	if err := tx.Get(&permitted, `SELECT EXISTS(SELECT 1 FROM pool_organization_permissions WHERE pool_id=$1 AND organization_id=$2) OR EXISTS(SELECT 1 FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2)`, poolID, organizationID); err != nil {
		return err
	}
	if !permitted {
		return echo.NewHTTPError(http.StatusForbidden, "pool delivery access has not been granted to organization")
	}
	var name string
	if err := tx.Get(&name, `SELECT name FROM customer_lists WHERE id=$1`, poolID); err != nil {
		return err
	}
	mailbox, err := c.organizationReplyMailboxIDTx(tx, organizationID)
	if err != nil {
		return err
	}
	// Selecting a first-level pool resolves to the single effective pool allocation
	// for the target organization. If none (or more than one) exists, retain an
	// unresolved relation so drafts can be saved but preview/send will be blocked
	// until an administrator fixes the assignment.
	// Keep the audience selection semantics (pool vs explicit allocation) separate
	// from the resolved delivery allocation used for recipients. This lets an
	// activity remain editable with the original first-level pool ID even after
	// a unique pool allocation is resolved.
	selectedAllocationID := allocationID
	if allocationID == nil {
		var candidates []struct {
			ID int64 `db:"id"`
		}
		if err := tx.Select(&candidates, `SELECT id FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2 ORDER BY id`, poolID, organizationID); err != nil {
			return err
		}
		if len(candidates) == 1 {
			id := candidates[0].ID
			allocationID = &id
		}
	} else {
		var exists bool
		if err := tx.Get(&exists, `SELECT EXISTS(SELECT 1 FROM org_pool_allocations WHERE id=$1 AND pool_id=$2 AND organization_id=$3)`, *allocationID, poolID, organizationID); err != nil {
			return err
		}
		if !exists {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid pool allocation")
		}
	}
	_, err = tx.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
		VALUES($1,NULL,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, campaignID, name, poolID, selectedAllocationID, organizationID, mailbox)
	if err != nil {
		return err
	}
	if allocationID != nil {
		// The snapshot of every row carries the resolved organization mailbox.
		if err := c.refreshPoolCampaignRecipientsTx(tx, campaignID, poolAudience{poolID: poolID, organizationID: organizationID, allocationID: allocationID, mailboxID: mailbox}); err != nil {
			return err
		}
	}
	return nil
}
