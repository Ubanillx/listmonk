package core

import (
	"database/sql"
	"net/http"
	"sort"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// UpdateCampaignInWorkspace performs the normal campaign update while the
// campaign row and all selected related resources are protected by the same
// workspace mutation transaction. The existing prepared statement remains the
// source of truth for campaign/list/media relationship behavior.
func (c *Core) UpdateCampaignInWorkspace(access models.WorkspaceAccess, id int, o models.Campaign, listIDs, mediaIDs []int, visibility string) (models.Campaign, error) {
	err := c.withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}, func(tx *sqlx.Tx) error {
		// A campaign with a recipient snapshot has started (or has been
		// processed previously) and its audience must remain immutable.  Perform
		// this check while the campaign row is locked by
		// withWorkspaceResourceMutation; the handler's read-time check alone
		// would allow a concurrent scheduler/member update to change lists
		// between authorization and the write.
		var hasRecipients bool
		if err := tx.Get(&hasRecipients,
			`SELECT EXISTS(SELECT 1 FROM campaign_recipients WHERE campaign_id = $1)`, id); err != nil {
			return workspaceQueryError("checking campaign recipients", err)
		}
		if hasRecipients {
			var currentListIDs []int
			if err := tx.Select(&currentListIDs, `
				SELECT COALESCE(list_id, 0) AS id
				FROM campaign_lists
				WHERE campaign_id = $1
				ORDER BY list_id NULLS FIRST`, id); err != nil {
				return workspaceQueryError("fetching campaign lists", err)
			}
			if !sameIntIDs(currentListIDs, listIDs) {
				return echo.NewHTTPError(http.StatusBadRequest,
					c.i18n.T("campaigns.cantUpdateListsAfterStart"))
			}
		}
		if visibility != "" {
			if err := validateResourceVisibility(resourceCampaigns, visibility); err != nil {
				return err
			}
		}
		// Visual templates are imported into the campaign body rather than
		// retained as a template dependency. When the editor sends the source
		// visual template ID, snapshot all selected template media into the
		// campaign owner's workspace and rewrite body/CID references while the
		// campaign row is locked. This is what lets a member use a shared
		// template containing the author's private images without leaving a
		// cross-owner media reference behind.
		if o.ContentType == models.CampaignContentTypeVisual && o.TemplateID.Valid && o.TemplateID.Int > 0 {
			var targetScope models.ResourceScope
			if err := tx.Get(&targetScope, `
				SELECT organization_id, owner_user_id, original_owner_user_id,
					visibility, transfer_pending_at
				FROM campaigns WHERE id = $1`, id); err != nil {
				return workspaceQueryError("reading campaign workspace", err)
			}
			snapshot, err := c.snapshotVisualCampaignMedia(tx, access, targetScope,
				int(o.TemplateID.Int), mediaIDs, o.Body, o.BodySource, o.AltBody)
			if err != nil {
				return err
			}
			o.Body = snapshot.Body
			o.BodySource = snapshot.BodySource
			o.AltBody = snapshot.AltBody
			mediaIDs = snapshot.MediaIDs
			// update-campaign already clears template_id for visual content;
			// clear the in-memory value as well so no later related-resource
			// check can accidentally treat the imported source as a saved link.
			o.TemplateID = null.Int{}
		}
		// Related resources are locked in a deterministic order. Campaign
		// updates already hold the campaign row via the outer helper; taking
		// template before media avoids the inverse order used by template
		// updates and eliminates a common deadlock cycle.
		if o.TemplateID.Valid {
			if err := c.lockWorkspaceUsableResources(tx, access, resourceTemplates, []int{int(o.TemplateID.Int)}); err != nil {
				return err
			}
		}
		if o.ArchiveTemplateID.Valid {
			if err := c.lockWorkspaceUsableResources(tx, access, resourceTemplates, []int{int(o.ArchiveTemplateID.Int)}); err != nil {
				return err
			}
		}
		if err := c.lockWorkspaceUsableResources(tx, access, resourceMedia, mediaIDs); err != nil {
			return err
		}
		if err := c.lockWorkspaceMutationResources(tx, access, resourceLists, listIDs); err != nil {
			return err
		}
		_, err := tx.Stmtx(c.q.UpdateCampaign).Exec(id,
			o.Name,
			o.Subject,
			o.FromEmail,
			o.Body,
			o.AltBody,
			o.ContentType,
			o.DailySendLimit,
			o.DailyResumeTime,
			o.SendAt,
			o.Headers,
			o.Attribs,
			pq.StringArray(normalizeTags(o.Tags)),
			o.Messenger,
			o.TemplateID,
			pq.Array(listIDs),
			o.Archive,
			o.ArchiveSlug,
			o.ArchiveTemplateID,
			o.ArchiveMeta,
			pq.Array(mediaIDs),
			o.BodySource,
			o.AutoTrackLinks)
		if err != nil {
			return workspaceQueryError("updating campaign", err)
		}
		if visibility != "" {
			if _, err := tx.Exec("UPDATE campaigns SET visibility = $2, updated_at = NOW() WHERE id = $1", id, visibility); err != nil {
				return workspaceQueryError("updating campaign visibility", err)
			}
		}
		return nil
	})
	if err != nil {
		return models.Campaign{}, err
	}
	return c.GetWorkspaceCampaign(access, id)
}

// sameIntIDs compares relationship ID slices as sets.  API clients are free
// to submit list IDs in any order, while campaign_lists has a deterministic
// database order; duplicate request IDs are ignored by the relationship
// query and should not make an otherwise unchanged audience look different.
func sameIntIDs(a, b []int) bool {
	left := uniqueMutationIDs(a)
	right := uniqueMutationIDs(b)
	if len(left) != len(right) {
		return false
	}
	sort.Ints(left)
	sort.Ints(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// UpdateCampaignStatusInWorkspace rechecks the campaign state while holding
// the organization lock. Cancellation of queued recipients is included in the
// transaction so a member removal cannot interleave between the status change
// and its recipient cleanup.
func (c *Core) UpdateCampaignStatusInWorkspace(access models.WorkspaceAccess, id int, status string) (models.Campaign, error) {
	err := c.withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}, func(tx *sqlx.Tx) error {
		var state struct {
			Status string       `db:"status"`
			SendAt sql.NullTime `db:"send_at"`
		}
		if err := tx.Get(&state, `SELECT status, send_at FROM campaigns WHERE id = $1`, id); err != nil {
			return workspaceMutationError()
		}
		if err := c.validateCampaignStatusTransition(state.Status, state.SendAt.Valid, status); err != nil {
			return err
		}
		res, err := tx.Stmtx(c.q.UpdateCampaignStatus).Exec(id, status)
		if err != nil {
			return workspaceQueryError("updating campaign status", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return workspaceMutationError()
		}
		if status == models.CampaignStatusCancelled {
			if _, err := tx.Stmtx(c.q.UpdateCampaignRecipientStatuses).Exec(id,
				models.CampaignRecipientStatusCancelled, pq.Array([]string{
					models.CampaignRecipientStatusPending,
					models.CampaignRecipientStatusDeferred,
					models.CampaignRecipientStatusQueued,
				})); err != nil {
				return workspaceQueryError("cancelling campaign recipients", err)
			}
		}
		return nil
	})
	if err != nil {
		return models.Campaign{}, err
	}
	return c.GetWorkspaceCampaign(access, id)
}

func (c *Core) validateCampaignStatusTransition(current string, sendAt bool, status string) error {
	errMsg := ""
	switch status {
	case models.CampaignStatusDraft:
		if current != models.CampaignStatusScheduled {
			errMsg = c.i18n.T("campaigns.onlyScheduledAsDraft")
		}
	case models.CampaignStatusScheduled:
		if current != models.CampaignStatusDraft && current != models.CampaignStatusPaused && current != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyDraftAsScheduled")
		}
		if !sendAt {
			errMsg = c.i18n.T("campaigns.needsSendAt")
		}
	case models.CampaignStatusRunning:
		if current != models.CampaignStatusPaused && current != models.CampaignStatusDraft && current != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyPausedDraft")
		}
	case models.CampaignStatusPaused:
		if current != models.CampaignStatusRunning && current != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyActivePause")
		}
	case models.CampaignStatusCancelled:
		if current != models.CampaignStatusRunning && current != models.CampaignStatusPaused && current != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyActiveCancel")
		}
	default:
		errMsg = c.i18n.T("globals.messages.invalidData")
	}
	if errMsg != "" {
		return echo.NewHTTPError(http.StatusBadRequest, errMsg)
	}
	return nil
}

func (c *Core) UpdateCampaignArchiveInWorkspace(access models.WorkspaceAccess, id int, enabled bool, tplID int, meta models.JSON, archiveSlug string) error {
	return c.withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}, func(tx *sqlx.Tx) error {
		if tplID > 0 {
			if err := c.lockWorkspaceUsableResources(tx, access, resourceTemplates, []int{tplID}); err != nil {
				return err
			}
		}
		if _, err := tx.Stmtx(c.q.UpdateCampaignArchive).Exec(id, enabled, archiveSlug, tplID, meta); err != nil {
			return workspaceQueryError("updating campaign archive", err)
		}
		return nil
	})
}

func (c *Core) DeleteCampaignInWorkspace(access models.WorkspaceAccess, id int) error {
	return c.withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}, func(tx *sqlx.Tx) error {
		res, err := tx.Stmtx(c.q.DeleteCampaign).Exec(id)
		if err != nil {
			return workspaceQueryError("deleting campaign", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return workspaceMutationError()
		}
		return nil
	})
}

func (c *Core) DeleteCampaignsInWorkspace(access models.WorkspaceAccess, ids []int) error {
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	sort.Ints(ids)
	return c.withWorkspaceResourceMutation(access, resourceCampaigns, ids, func(tx *sqlx.Tx) error {
		if _, err := tx.Stmtx(c.q.DeleteCampaigns).Exec(pq.Array(ids), "", true, pq.Array(nil)); err != nil {
			return workspaceQueryError("deleting campaigns", err)
		}
		return nil
	})
}

func (c *Core) UpdateListInWorkspace(access models.WorkspaceAccess, id int, l models.List, visibility string) (models.List, error) {
	err := c.withWorkspaceResourceMutation(access, resourceLists, []int{id}, func(tx *sqlx.Tx) error {
		if visibility != "" {
			if err := validateResourceVisibility(resourceLists, visibility); err != nil {
				return err
			}
		}
		if _, err := tx.Stmtx(c.q.UpdateList).Exec(id, l.Name, l.Type, l.Optin, l.Status,
			pq.StringArray(normalizeTags(l.Tags)), l.Description); err != nil {
			return workspaceQueryError("updating list", err)
		}
		if visibility != "" {
			if _, err := tx.Exec("UPDATE lists SET visibility = $2, updated_at = NOW() WHERE id = $1", id, visibility); err != nil {
				return workspaceQueryError("updating list visibility", err)
			}
		}
		return nil
	})
	if err != nil {
		return models.List{}, err
	}
	return c.GetWorkspaceList(access, id)
}

func (c *Core) DeleteListsInWorkspace(access models.WorkspaceAccess, ids []int) error {
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	return c.withWorkspaceResourceMutation(access, resourceLists, ids, func(tx *sqlx.Tx) error {
		if _, err := tx.Stmtx(c.q.DeleteLists).Exec(pq.Array(ids), "", true, pq.Array(nil)); err != nil {
			return workspaceQueryError("deleting lists", err)
		}
		return nil
	})
}

// SetWorkspaceResourceVisibility is the write-safe counterpart to
// SetResourceVisibility. Visibility is a resource mutation too: without this
// transaction a stale owner request could publish a resource just after its
// organization membership was revoked.
func (c *Core) SetWorkspaceResourceVisibility(access models.WorkspaceAccess, resource string, id int, visibility string) error {
	if _, ok := workspaceResourceTables[resource]; !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "unknown workspace resource")
	}
	if err := validateResourceVisibility(resource, visibility); err != nil {
		return err
	}
	return c.withWorkspaceResourceMutation(access, resource, []int{id}, func(tx *sqlx.Tx) error {
		table := workspaceResourceTables[resource]
		if _, err := tx.Exec("UPDATE "+table+" SET visibility = $2, updated_at = NOW() WHERE id = $1", id, visibility); err != nil {
			return workspaceQueryError("updating resource visibility", err)
		}
		return nil
	})
}

func (c *Core) lockWorkspaceUsableResources(tx *sqlx.Tx, access models.WorkspaceAccess, resource string, ids []int) error {
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	table, ok := workspaceResourceTables[resource]
	if !ok {
		return workspaceMutationError()
	}
	where := `id = ANY($1::INT[]) AND transfer_pending_at IS NULL
		AND (organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations usable_organization
			WHERE usable_organization.id = organization_id
				AND usable_organization.status = 'active'
		))`
	args := []any{pq.Array(ids)}
	if !access.PlatformAdmin {
		if access.IsOrganization() {
			where += " AND ((organization_id = $2 AND (owner_user_id = $3 OR visibility = 'organization')) OR visibility = 'global')"
			args = append(args, access.OrganizationID, access.UserID)
		} else {
			where += " AND ((organization_id IS NULL AND owner_user_id = $2) OR visibility = 'global')"
			args = append(args, access.UserID)
		}
	}
	var locked []int
	if err := tx.Select(&locked, "SELECT id FROM "+table+" WHERE "+where+" ORDER BY id FOR UPDATE", args...); err != nil {
		return workspaceQueryError("locking usable workspace resources", err)
	}
	if len(locked) != len(ids) {
		return workspaceMutationError()
	}
	return nil
}

func int64IDs(ids pq.Int64Array) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			out = append(out, int(id))
		}
	}
	return out
}
