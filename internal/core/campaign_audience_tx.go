package core

import (
	"net/http"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// CampaignAudience is one pool audience a campaign selects: a first-level
// public pool and, when the requester picked one explicitly, that pool's
// allocation in the target organization. It mirrors the shape the campaign API
// already separates out of the requested customer_list IDs.
type CampaignAudience struct {
	PoolID       int
	AllocationID *int64
}

// CampaignAudiencePlan is the audience decision the caller already made. The
// campaign API owns this decision because it depends on whether the campaign
// already has a recipient snapshot — which makes its audience immutable — and on
// whether the request still selects a pool. Core applies the plan verbatim and
// never re-derives it, so a transaction cannot rebuild an audience the caller
// meant to keep, or prune one it meant to rebuild.
//
// The three branches of the campaign update path map onto it exactly:
//
//	RebuildAll=true,  Audiences empty     — no pool is selected (or the campaign
//	                                       never left draft): every pool
//	                                       association is wiped and nothing is
//	                                       attached.
//	RebuildAll=true,  Audiences non-empty — a draft rebuild: every pool
//	                                       association is wiped, then the
//	                                       selected audiences are attached.
//	RebuildAll=false, Audiences non-empty — the campaign already has a recipient
//	                                       snapshot: only the pool associations it
//	                                       no longer selects are removed, so
//	                                       queued/sent delivery history survives,
//	                                       then the selected audiences are
//	                                       attached.
//
// AllOrganizations marks a platform-level (all-organization) public pool
// campaign, whose audiences carry no target organization, allocation or
// mailbox.
type CampaignAudiencePlan struct {
	Audiences        []CampaignAudience
	RebuildAll       bool
	AllOrganizations bool
}

// syncCampaignAudienceTx makes the campaign's pool audience equal the caller's
// plan inside the caller's transaction: it removes the pool associations the
// plan does not keep and attaches every selected audience.
//
// Every statement runs on tx. The audience rebuild, the audience rows and their
// recipient snapshots therefore commit or roll back with the campaign row they
// belong to, instead of leaving a partially updated audience behind when one
// pool attachment fails.
func (c *Core) syncCampaignAudienceTx(tx *sqlx.Tx, campaignID int, access models.WorkspaceAccess, plan CampaignAudiencePlan) error {
	poolIDs := make([]int, 0, len(plan.Audiences))
	for _, audience := range plan.Audiences {
		poolIDs = append(poolIDs, audience.PoolID)
	}
	if plan.RebuildAll {
		// The campaign has no immutable send snapshot yet. Rebuild all pool
		// associations so removing a pool allocation (or replacing it with a
		// different one under the same first-level pool) cannot leave stale
		// audience metadata or recipients behind.
		if _, err := tx.Exec(`DELETE FROM campaign_pool_recipients WHERE campaign_id=$1`, campaignID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL`, campaignID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM campaign_pool_org_orders WHERE campaign_id=$1`, campaignID); err != nil {
			return err
		}
	} else if len(poolIDs) > 0 {
		if _, err := tx.Exec(`DELETE FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_id <> ALL($2::INT[])`, campaignID, pq.Array(poolIDs)); err != nil {
			return err
		}
	}
	if len(poolIDs) > 0 {
		if _, err := tx.Exec(`DELETE FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL AND pool_id <> ALL($2::INT[])`, campaignID, pq.Array(poolIDs)); err != nil {
			return err
		}
	}
	// Pool audience rows are maintained separately from legacy customer-list
	// rows. Attach is idempotent and resolves a first-level pool to the target
	// organization's pool allocation at send time.
	for _, audience := range plan.Audiences {
		if err := c.attachPoolToCampaignTx(tx, campaignID, audience.PoolID, audience.AllocationID, int64(access.OrganizationID), plan.AllOrganizations); err != nil {
			return err
		}
	}
	return nil
}

// updateCampaignReplyMailboxTx attaches or clears a campaign's reply mailbox on
// the caller's transaction. It is the transaction-aware form of the campaign
// API's former post-commit write: the same statement and the same owner scoping,
// with a NULL parameter when the selection is cleared.
func (c *Core) updateCampaignReplyMailboxTx(tx *sqlx.Tx, campaignID, ownerUserID int, mailboxID null.Int) error {
	// This helper intentionally accepts the null.Int shape without exposing it
	// in the request layer. A NULL value clears an existing selection.
	if campaignID < 1 || ownerUserID < 1 {
		return nil
	}
	var id any
	if mailboxID.Valid && mailboxID.Int > 0 {
		id = mailboxID.Int
	}
	if _, err := tx.Exec(`UPDATE campaigns SET reply_mailbox_id = $1, updated_at = NOW() WHERE id = $2 AND owner_user_id = $3`, id, campaignID, ownerUserID); err != nil {
		return err
	}
	return nil
}

// UpdateCampaignWithAudienceInWorkspace updates a campaign together with its
// reply mailbox and its pool audience in one workspace mutation transaction.
//
// It replaces the previous sequence of independent writes (campaign update,
// mailbox update, raw audience deletes, one AttachPoolToCampaign transaction per
// selected audience), in which any failing step left the campaign partially
// updated. The campaign row and its customer_list/media/visibility relations,
// the reply mailbox and the pool audience rows with their recipient snapshots now
// commit or roll back together: a pool attachment that fails leaves the campaign
// row, its lists and its mailbox exactly as they were, and the reverse holds too.
//
// The plan is the caller's branch decision; see CampaignAudiencePlan.
func (c *Core) UpdateCampaignWithAudienceInWorkspace(access models.WorkspaceAccess, id int, o models.Campaign, customerListIDs, mediaIDs []int, visibility string, audience CampaignAudiencePlan) (models.Campaign, error) {
	err := c.withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}, func(tx *sqlx.Tx) error {
		if err := c.updateCampaignTx(tx, access, id, o, customerListIDs, mediaIDs, visibility); err != nil {
			return err
		}
		if err := c.updateCampaignReplyMailboxTx(tx, id, access.UserID, o.ReplyMailboxID); err != nil {
			return err
		}
		return c.syncCampaignAudienceTx(tx, id, access, audience)
	})
	if err != nil {
		return models.Campaign{}, err
	}
	return c.GetWorkspaceCampaign(access, id)
}

// CreateCampaignWithAudienceInWorkspace creates a campaign together with its
// reply mailbox and its pool audience in one workspace creation transaction. It
// is the creation counterpart of UpdateCampaignWithAudienceInWorkspace: the
// INSERT, the mailbox assignment and the selected pool audiences (with their
// recipient snapshots) commit or roll back as one unit.
//
// A brand-new campaign owns no pool association yet, so a create always passes
// RebuildAll=false: the plan's prune step matches no row and the attach step
// writes the selected audiences, which is exactly what the previous
// insert-then-attach sequence did.
func (c *Core) CreateCampaignWithAudienceInWorkspace(access models.WorkspaceAccess, o models.Campaign, customerListIDs, mediaIDs []int, scope models.ResourceScope, audience CampaignAudiencePlan) (models.Campaign, error) {
	uuidValue, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	var newID int
	err = c.withWorkspaceCreation(access, func(tx *sqlx.Tx) error {
		id, err := c.createCampaignTx(tx, access, o, customerListIDs, mediaIDs, scope, uuidValue)
		if err != nil {
			return err
		}
		newID = id
		if err := c.updateCampaignReplyMailboxTx(tx, newID, access.UserID, o.ReplyMailboxID); err != nil {
			return err
		}
		return c.syncCampaignAudienceTx(tx, newID, access, audience)
	})
	if err != nil {
		return models.Campaign{}, err
	}
	return c.GetWorkspaceCampaign(access, newID)
}
