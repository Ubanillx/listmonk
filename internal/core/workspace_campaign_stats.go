package core

import (
	"fmt"

	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// workspaceCampaignListPredicate limits a campaign-list snapshot to the
// campaign's own workspace/owner graph. A NULL list_id is a historical
// snapshot of a list that has since been deleted and is intentionally kept.
// Platform administrators already have global access; keeping the predicate
// open for them also preserves their ability to inspect malformed legacy rows
// while ordinary callers remain strictly tenant-scoped.
func workspaceCampaignListPredicate(access models.WorkspaceAccess, campaignAlias, listAlias, associationAlias string) string {
	if access.PlatformAdmin {
		return "TRUE"
	}
	pendingPredicate := fmt.Sprintf("%s.transfer_pending_at IS NULL", listAlias)
	if access.IsOrganizationManager() {
		// Managers inspect the transfer queue while ordinary members must never
		// receive a pending list ID through a campaign snapshot.
		pendingPredicate = "TRUE"
	}
	return fmt.Sprintf(`
		%s.list_id IS NULL OR (
			%s.id IS NOT NULL
			AND %s.organization_id IS NOT DISTINCT FROM %s.organization_id
			AND %s.owner_user_id IS NOT DISTINCT FROM %s.owner_user_id
			AND (%s)
			AND (%s.organization_id IS NULL OR EXISTS (
				SELECT 1 FROM organizations campaign_list_org
				WHERE campaign_list_org.id = %s.organization_id
					AND campaign_list_org.status = 'active'
			))
		)`,
		associationAlias,
		listAlias,
		listAlias, campaignAlias,
		listAlias, campaignAlias,
		pendingPredicate,
		listAlias, listAlias)
}

// workspaceCampaignMediaPredicate validates a direct campaign-media edge.
// Shared/global campaigns may carry a private binary owned by the campaign
// author; arbitrary private binaries from another owner or organization are
// never made visible by a forged association.
func workspaceCampaignMediaPredicate(access models.WorkspaceAccess, campaignAlias, mediaAlias string) string {
	if access.PlatformAdmin {
		return "TRUE"
	}
	return fmt.Sprintf(`
		%s.id IS NOT NULL
		AND %s.transfer_pending_at IS NULL
		AND (%s.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations campaign_media_org
			WHERE campaign_media_org.id = %s.organization_id
				AND campaign_media_org.status = 'active'
		))
		AND (
			%s.visibility = 'global'
			OR (
				%s.organization_id IS NOT DISTINCT FROM %s.organization_id
				AND %s.visibility = 'organization'
			)
			OR (
				%s.organization_id IS NOT DISTINCT FROM %s.organization_id
				AND %s.owner_user_id IS NOT NULL
				AND %s.owner_user_id IS NOT NULL
				AND %s.owner_user_id = %s.owner_user_id
				AND %s.visibility <> 'organization'
			)
		)`,
		mediaAlias,
		mediaAlias,
		mediaAlias, mediaAlias,
		mediaAlias,
		mediaAlias, campaignAlias, mediaAlias,
		mediaAlias, campaignAlias, mediaAlias, campaignAlias,
		mediaAlias, campaignAlias, mediaAlias)
}

// workspaceTemplateMediaPredicate validates a template-media edge. It is
// deliberately parallel to workspaceCampaignMediaPredicate so a shared
// template can publish its author's private inline image without turning an
// unrelated private media row into a readable/sendable asset.
func workspaceTemplateMediaPredicate(access models.WorkspaceAccess, templateAlias, mediaAlias string) string {
	if access.PlatformAdmin {
		return "TRUE"
	}
	return fmt.Sprintf(`
		%s.id IS NOT NULL
		AND %s.transfer_pending_at IS NULL
		AND (%s.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations template_media_org
			WHERE template_media_org.id = %s.organization_id
				AND template_media_org.status = 'active'
		))
		AND (
			%s.visibility = 'global'
			OR (
				%s.organization_id IS NOT DISTINCT FROM %s.organization_id
				AND %s.visibility = 'organization'
			)
			OR (
				%s.organization_id IS NOT DISTINCT FROM %s.organization_id
				AND %s.owner_user_id IS NOT NULL
				AND %s.owner_user_id IS NOT NULL
				AND %s.owner_user_id = %s.owner_user_id
				AND %s.visibility <> 'organization'
			)
		)`,
		mediaAlias,
		mediaAlias,
		mediaAlias, mediaAlias,
		mediaAlias,
		mediaAlias, templateAlias, mediaAlias,
		mediaAlias, templateAlias, mediaAlias, templateAlias,
		mediaAlias, templateAlias, mediaAlias)
}

// loadWorkspaceCampaignStats is the workspace-safe counterpart of the legacy
// lazy get-campaign-stats query. The campaign IDs are scoped in a CTE and all
// denormalized association metadata is joined back through the campaign's
// owner/workspace graph. Event counts are also joined to that CTE, so a stale
// ID list cannot retrieve statistics after a transfer or archive.
func (c *Core) loadWorkspaceCampaignStats(access models.WorkspaceAccess, camps models.Campaigns) error {
	if len(camps) == 0 {
		return nil
	}

	campaignScope, scopeArgs := workspaceReadPredicate(access, "sc", 2)
	listPredicate := workspaceCampaignListPredicate(access, "sc", "cl_list", "cl")
	mediaPredicate := workspaceCampaignMediaPredicate(access, "sc", "cm_media")
	stmt := fmt.Sprintf(`
		WITH scoped_campaigns AS (
			SELECT sc.id, sc.organization_id, sc.owner_user_id
			FROM campaigns sc
			WHERE sc.id = ANY($1::INT[]) AND (%s)
		), lists AS (
			SELECT cl.campaign_id,
				JSON_AGG(JSON_BUILD_OBJECT('id', cl.list_id, 'name', cl.list_name)
					ORDER BY cl.list_id NULLS LAST, cl.list_name) AS lists
			FROM campaign_lists cl
			JOIN scoped_campaigns sc ON sc.id = cl.campaign_id
			LEFT JOIN lists cl_list ON cl_list.id = cl.list_id
			WHERE (%s)
			GROUP BY cl.campaign_id
		), media AS (
			SELECT cm.campaign_id,
				JSON_AGG(JSON_BUILD_OBJECT('id', cm.media_id, 'filename', cm.filename)
					ORDER BY cm.media_id NULLS LAST, cm.filename) AS media
			FROM campaign_media cm
			JOIN scoped_campaigns sc ON sc.id = cm.campaign_id
			LEFT JOIN media cm_media ON cm_media.id = cm.media_id
			WHERE cm.media_id IS NULL OR (%s)
			GROUP BY cm.campaign_id
		), views AS (
			SELECT cv.campaign_id, COUNT(*) AS num
			FROM campaign_views cv JOIN scoped_campaigns sc ON sc.id = cv.campaign_id
			GROUP BY cv.campaign_id
		), clicks AS (
			SELECT lc.campaign_id, COUNT(*) AS num
			FROM link_clicks lc JOIN scoped_campaigns sc ON sc.id = lc.campaign_id
			GROUP BY lc.campaign_id
		), bounces AS (
			SELECT b.campaign_id, COUNT(*) AS num
			FROM bounces b JOIN scoped_campaigns sc ON sc.id = b.campaign_id
			GROUP BY b.campaign_id
		)
		SELECT requested.id AS campaign_id,
			COALESCE(v.num, 0) AS views,
			COALESCE(clicks.num, 0) AS clicks,
			COALESCE(b.num, 0) AS bounces,
			COALESCE(l.lists, '[]') AS lists,
			COALESCE(m.media, '[]') AS media
		FROM UNNEST($1::INT[]) AS requested(id)
		LEFT JOIN lists l ON l.campaign_id = requested.id
		LEFT JOIN media m ON m.campaign_id = requested.id
		LEFT JOIN views v ON v.campaign_id = requested.id
		LEFT JOIN clicks ON clicks.campaign_id = requested.id
		LEFT JOIN bounces b ON b.campaign_id = requested.id
		ORDER BY ARRAY_POSITION($1::INT[], requested.id)`,
		campaignScope, listPredicate, mediaPredicate)

	args := []any{pq.Array(camps.GetIDs())}
	args = append(args, scopeArgs...)
	var meta []models.CampaignMeta
	if err := c.db.Select(&meta, stmt, args...); err != nil {
		return err
	}
	if len(meta) != len(camps) {
		return fmt.Errorf("campaign stats count does not match")
	}
	for i, row := range meta {
		if row.CampaignID != camps[i].ID {
			return fmt.Errorf("campaign stats order does not match")
		}
		camps[i].Lists = row.Lists
		camps[i].Views = row.Views
		camps[i].Clicks = row.Clicks
		camps[i].Bounces = row.Bounces
		camps[i].Media = row.Media
	}
	return nil
}
