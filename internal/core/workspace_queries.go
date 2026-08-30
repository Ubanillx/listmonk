package core

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx/types"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gopkg.in/volatiletech/null.v6"
)

// workspaceReadPredicate emits a fixed SQL predicate for a resource alias.
// User supplied filters are always appended outside this predicate, so an
// arbitrary search condition cannot widen a workspace boundary.
func activeOrganizationPredicate(alias string) string {
	field := "organization_id"
	if alias != "" {
		field = alias + ".organization_id"
	}
	return fmt.Sprintf(`(%s IS NULL OR EXISTS (
		SELECT 1 FROM organizations workspace_org
		WHERE workspace_org.id = %s AND workspace_org.status = 'active'
	))`, field, field)
}

func withActiveOrganizationPredicate(scope, alias string) string {
	return fmt.Sprintf("(%s) AND %s", scope, activeOrganizationPredicate(alias))
}

func workspaceReadPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	field := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	arg := func(offset int) string { return fmt.Sprintf("$%d", firstArg+offset) }
	if access.PlatformAdmin {
		return "TRUE", nil
	}
	if !access.IsOrganization() {
		scope := fmt.Sprintf("((%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL) OR (%svisibility = 'global' AND %stransfer_pending_at IS NULL))",
			field(""), field(""), arg(0), field(""), field(""), field(""))
		return withActiveOrganizationPredicate(scope, alias), []any{access.UserID}
	}
	if access.IsOrganizationManager() {
		scope := fmt.Sprintf("(%sorganization_id = %s OR (%svisibility = 'global' AND %stransfer_pending_at IS NULL))",
			field(""), arg(0), field(""), field(""))
		return withActiveOrganizationPredicate(scope, alias), []any{access.OrganizationID}
	}
	scope := fmt.Sprintf(`((%sorganization_id = %s AND (
		(%sowner_user_id = %s AND %stransfer_pending_at IS NULL) OR (%stransfer_pending_at IS NULL AND %svisibility IN ('organization', 'global'))
	)) OR (%svisibility = 'global' AND %stransfer_pending_at IS NULL))`,
		field(""), arg(0), field(""), arg(1), field(""), field(""), field(""), field(""), field(""))
	return withActiveOrganizationPredicate(scope, alias),
		[]any{access.OrganizationID, access.UserID}
}

// workspaceOwnerScopedReadPredicate is used for lists and subscribers. An
// owner audience cannot be shared with ordinary organization members; only an
// organization manager can inspect another member's records in that org.
func workspaceOwnerScopedReadPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	field := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	arg := func(offset int) string { return fmt.Sprintf("$%d", firstArg+offset) }
	if access.PlatformAdmin {
		return "TRUE", nil
	}
	if !access.IsOrganization() {
		scope := fmt.Sprintf("(%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
			field(""), field(""), arg(0), field(""))
		return withActiveOrganizationPredicate(scope, alias), []any{access.UserID}
	}
	if access.IsOrganizationManager() {
		scope := fmt.Sprintf("%sorganization_id = %s", field(""), arg(0))
		return withActiveOrganizationPredicate(scope, alias), []any{access.OrganizationID}
	}
	scope := fmt.Sprintf("(%sorganization_id = %s AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
		field(""), arg(0), field(""), arg(1), field(""))
	return withActiveOrganizationPredicate(scope, alias), []any{access.OrganizationID, access.UserID}
}

func workspaceSort(orderBy, order string, fields map[string]string, fallback string) string {
	field, ok := fields[orderBy]
	if !ok {
		field = fallback
	}
	if strings.EqualFold(order, SortAsc) {
		return field + " ASC"
	}
	return field + " DESC"
}

// QueryWorkspaceLists returns only lists visible in the selected workspace.
func (c *Core) QueryWorkspaceLists(access models.WorkspaceAccess, search, typ, optin, status string, tags []string, orderBy, order string, offset, limit int) ([]models.List, int, error) {
	if tags == nil {
		tags = []string{}
	}
	_ = c.refreshCache(matListSubStats, false)

	scope, args := workspaceOwnerScopedReadPredicate(access, "l", 1)
	first := len(args) + 1
	search = strings.TrimSpace(search)
	stmt := fmt.Sprintf(`
		WITH ls AS (
			SELECT COUNT(*) OVER() AS total, l.*,
				COALESCE(u.username, '') AS owner_username,
				COALESCE(u.name, '') AS owner_name
			FROM lists l
			LEFT JOIN users u ON u.id = COALESCE(l.owner_user_id, l.original_owner_user_id)
			WHERE (%s)
				AND ($%d = '' OR l.name ILIKE $%d)
				AND ($%d = '' OR l.type = $%d::list_type)
				AND ($%d = '' OR l.optin = $%d::list_optin)
				AND ($%d = '' OR l.status = $%d::list_status)
				AND (CARDINALITY($%d::VARCHAR(100)[]) = 0 OR $%d <@ l.tags)
			ORDER BY %s
			OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)
		), statuses AS (
			SELECT list_id,
				COALESCE(JSONB_OBJECT_AGG(status, subscriber_count) FILTER (WHERE status IS NOT NULL), '{}') AS subscriber_statuses,
				SUM(subscriber_count) AS subscriber_count
			FROM mat_list_subscriber_stats GROUP BY list_id
		)
		SELECT ls.*, COALESCE(ss.subscriber_statuses, '{}') AS subscriber_statuses,
			COALESCE(ss.subscriber_count, 0) AS subscriber_count
		FROM ls LEFT JOIN statuses ss ON ss.list_id = ls.id
		ORDER BY %s`,
		scope,
		first, first,
		first+1, first+1,
		first+2, first+2,
		first+3, first+3,
		first+4, first+4,
		workspaceSort(orderBy, order, map[string]string{
			"name":             "l.name",
			"status":           "l.status",
			"created_at":       "l.created_at",
			"updated_at":       "l.updated_at",
			"subscriber_count": "l.id",
		}, "l.created_at"),
		first+5, first+6, first+6,
		workspaceSort(orderBy, order, map[string]string{
			"name":             "ls.name",
			"status":           "ls.status",
			"created_at":       "ls.created_at",
			"updated_at":       "ls.updated_at",
			"subscriber_count": "subscriber_count",
		}, "ls.created_at"),
	)
	args = append(args,
		searchString(search), typ, optin, status, pq.StringArray(tags), offset, limit,
	)

	var out []models.List
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching lists", err)
	}
	for i := range out {
		if out[i].Tags == nil {
			out[i].Tags = []string{}
		}
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

// GetWorkspaceLists is the lightweight list selector equivalent of the
// paginated list query.
func (c *Core) GetWorkspaceLists(access models.WorkspaceAccess, typ, status string) ([]models.List, error) {
	out, _, err := c.QueryWorkspaceLists(access, "", typ, "", status, nil, "name", SortAsc, 0, 0)
	return out, err
}

// GetPublicSubscriptionLists returns active public lists that can accept an
// unauthenticated subscription. Unlike the legacy GetLists helper, this query
// excludes resources awaiting transfer and returns enough scope metadata to
// bind the submitted e-mail to the list owner's workspace.
func (c *Core) GetPublicSubscriptionLists(uuids []string) ([]models.List, error) {
	var (
		stmt string
		args []any
	)
	if len(uuids) == 0 {
		stmt = `
			SELECT l.* FROM lists l
			LEFT JOIN organizations o ON o.id = l.organization_id
			WHERE l.type = 'public' AND l.status = 'active'
				AND owner_user_id IS NOT NULL AND transfer_pending_at IS NULL
				AND (l.organization_id IS NULL OR o.status = 'active')
			ORDER BY l.name`
	} else {
		stmt = `
			SELECT l.* FROM lists l
			LEFT JOIN organizations o ON o.id = l.organization_id
			WHERE l.uuid = ANY($1::UUID[]) AND l.type = 'public' AND l.status = 'active'
				AND l.owner_user_id IS NOT NULL AND l.transfer_pending_at IS NULL
				AND (l.organization_id IS NULL OR o.status = 'active')
			ORDER BY l.name`
		args = []any{pq.Array(uuids)}
	}
	var out []models.List
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching public subscription lists", err)
	}
	return out, nil
}

// QueryWorkspaceCampaigns returns campaigns visible in the active workspace.
func (c *Core) QueryWorkspaceCampaigns(access models.WorkspaceAccess, search string, statuses, tags []string, orderBy, order string, offset, limit int) (models.Campaigns, int, error) {
	if statuses == nil {
		statuses = []string{}
	}
	if tags == nil {
		tags = []string{}
	}

	scope, args := workspaceReadPredicate(access, "c", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT c.*, COALESCE(rm.email, '') AS reply_mailbox_email, COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			CASE WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = c.id)
				THEN (SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = c.id
					AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[]))
				ELSE GREATEST(c.to_send - c.sent, 0) END AS unsent_count,
			COUNT(*) OVER() AS total,
			(SELECT COALESCE(ARRAY_TO_JSON(ARRAY_AGG(l)), '[]') FROM (
				SELECT COALESCE(cl.list_id, 0) AS id, cl.list_name AS name
				FROM campaign_lists cl
				LEFT JOIN lists cl_list ON cl_list.id = cl.list_id
				WHERE cl.campaign_id = c.id AND (%s)
			) l) AS lists
		FROM campaigns c
		LEFT JOIN users u ON u.id = COALESCE(c.owner_user_id, c.original_owner_user_id)
		LEFT JOIN reply_mailboxes rm ON rm.id = c.reply_mailbox_id
		WHERE (%s)
			AND (CARDINALITY($%d::campaign_status[]) = 0 OR c.status = ANY($%d::campaign_status[]))
			AND (CARDINALITY($%d::VARCHAR(100)[]) = 0 OR $%d <@ c.tags)
			AND ($%d = '' OR c.name ILIKE $%d OR c.subject ILIKE $%d)
		ORDER BY %s OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
		workspaceCampaignListPredicate(access, "c", "cl_list", "cl"),
		scope,
		first, first,
		first+1, first+1,
		first+2, first+2, first+2,
		workspaceSort(orderBy, order, map[string]string{
			"name":       "c.name",
			"status":     "c.status",
			"created_at": "c.created_at",
			"updated_at": "c.updated_at",
		}, "c.created_at"),
		first+3, first+4, first+4,
	)
	args = append(args, pq.StringArray(statuses), pq.StringArray(tags), searchString(search), offset, limit)

	var out models.Campaigns
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching campaigns", err)
	}
	for i := range out {
		if out[i].Tags == nil {
			out[i].Tags = []string{}
		}
	}
	if err := c.loadWorkspaceCampaignStats(access, out); err != nil {
		return nil, 0, workspaceQueryError("fetching campaign statistics", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

// GetWorkspaceCampaign returns one campaign through the same workspace
// predicate used by QueryWorkspaceCampaigns.  The legacy GetCampaign helper is
// intentionally left available for bearer-token/public delivery paths, but an
// authenticated endpoint must not read a row after a separate authorization
// check and then fall back to an unscoped SELECT: a concurrent transfer could
// otherwise turn that check into a stale read.
func (c *Core) GetWorkspaceCampaign(access models.WorkspaceAccess, id int) (models.Campaign, error) {
	if id < 1 {
		return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}

	scope, args := workspaceReadPredicate(access, "campaigns", 1)
	// The campaign row and its optional template must be constrained by the
	// same active workspace.  A plain LEFT JOIN on template_id would otherwise
	// leak the body of a private/cross-organization template whenever the
	// campaign itself is public or organization-shared.  Keep the template
	// arguments ahead of the campaign ID so all generated predicates retain
	// stable PostgreSQL placeholders.
	templateScope, templateArgs := workspaceReadPredicate(access, "templates", len(args)+1)
	args = append(args, templateArgs...)
	idArg := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT campaigns.*, COALESCE(rm.email, '') AS reply_mailbox_email,
			COALESCE(campaign_owner.username, '') AS owner_username,
			COALESCE(campaign_owner.name, '') AS owner_name,
			CASE
				WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
					SELECT COUNT(*) FROM campaign_recipients cr
					WHERE cr.campaign_id = campaigns.id
						AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
				)
				ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
			END AS unsent_count,
			COALESCE(templates.body, (
				SELECT fallback.body FROM templates fallback
				WHERE fallback.is_default = TRUE
					AND fallback.transfer_pending_at IS NULL
					AND (fallback.organization_id IS NULL OR EXISTS (
						SELECT 1 FROM organizations fallback_org
						WHERE fallback_org.id = fallback.organization_id
							AND fallback_org.status = 'active'
					))
				AND (
					(fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
						AND (fallback.owner_user_id = campaigns.owner_user_id
							OR fallback.visibility = 'organization'))
					OR fallback.visibility = 'global'
				)
				ORDER BY CASE WHEN fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
					AND fallback.owner_user_id = campaigns.owner_user_id THEN 0
					WHEN fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
						AND fallback.visibility = 'organization' THEN 1
					ELSE 2 END, fallback.id
				LIMIT 1
			), '') AS template_body
		FROM campaigns
		LEFT JOIN users campaign_owner
			ON campaign_owner.id = COALESCE(campaigns.owner_user_id, campaigns.original_owner_user_id)
		LEFT JOIN reply_mailboxes rm ON rm.id = campaigns.reply_mailbox_id
		LEFT JOIN templates ON templates.id = campaigns.template_id AND (%s)
		WHERE campaigns.id = $%d AND (%s)
		LIMIT 1`, templateScope, idArg, scope)
	args = append(args, id)

	var out models.Campaign
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
				c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
		}
		return models.Campaign{}, workspaceQueryError("fetching campaign", err)
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	// Keep the API shape identical to GetCampaign, including lazy statistics,
	// while the campaign row itself remains scope constrained.
	stats := models.Campaigns{out}
	if err := c.loadWorkspaceCampaignStats(access, stats); err != nil {
		return models.Campaign{}, workspaceQueryError("fetching campaign statistics", err)
	}
	out = stats[0]
	return out, nil
}

// GetWorkspaceCampaignForPreview is the scope-checked counterpart of
// GetCampaignForPreview.  The optional template ID is checked in the same SQL
// statement as the campaign, so a template moved between the initial handler
// check and this read cannot be substituted into a preview.
func (c *Core) GetWorkspaceCampaignForPreview(access models.WorkspaceAccess, id, tplID int) (models.Campaign, error) {
	if id < 1 {
		return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}

	campaignScope, args := workspaceReadPredicate(access, "campaigns", 1)
	templateScope, templateArgs := workspaceReadPredicate(access, "templates", len(args)+1)
	args = append(args, templateArgs...)
	campaignArg := len(args) + 1
	templateArg := campaignArg + 1
	stmt := fmt.Sprintf(`
		SELECT campaigns.*, COALESCE(rm.email, '') AS reply_mailbox_email,
			COALESCE(campaign_owner.username, '') AS owner_username,
			COALESCE(campaign_owner.name, '') AS owner_name,
			CASE
				WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
					SELECT COUNT(*) FROM campaign_recipients cr
					WHERE cr.campaign_id = campaigns.id
						AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
				)
				ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
			END AS unsent_count,
			COALESCE(templates.body, '') AS template_body,
			COALESCE((
				SELECT ARRAY_AGG(DISTINCT x.media_id ORDER BY x.media_id)::INT[]
				FROM (
					SELECT cm.media_id FROM campaign_media cm
					LEFT JOIN media cm_media ON cm_media.id = cm.media_id
					WHERE cm.campaign_id = campaigns.id AND cm.media_id IS NOT NULL
						AND (%s)
					UNION
					SELECT tm.media_id FROM template_media tm
					LEFT JOIN media tm_media ON tm_media.id = tm.media_id
					WHERE templates.id IS NOT NULL
						AND tm.template_id = COALESCE(NULLIF($%d, 0), campaigns.template_id)
						AND tm.media_id IS NOT NULL
						AND (%s)
				) x
			), '{}') AS media_id,
			(
				SELECT COALESCE(ARRAY_TO_JSON(ARRAY_AGG(l)), '[]') FROM (
				SELECT COALESCE(cl.list_id, 0) AS id, cl.list_name AS name
				FROM campaign_lists cl
				LEFT JOIN lists cl_list ON cl_list.id = cl.list_id
				WHERE cl.campaign_id = campaigns.id AND (%s)
			) l
			) AS lists
		FROM campaigns
		LEFT JOIN users campaign_owner
			ON campaign_owner.id = COALESCE(campaigns.owner_user_id, campaigns.original_owner_user_id)
		LEFT JOIN reply_mailboxes rm ON rm.id = campaigns.reply_mailbox_id
		LEFT JOIN templates ON templates.id = (CASE WHEN $%d = 0 THEN campaigns.template_id ELSE $%d END)
		WHERE campaigns.id = $%d
			AND (%s)
			-- An explicitly requested template must exist and be readable. A
			-- missing saved template is allowed for visual/legacy campaigns.
			AND ($%d = 0 OR templates.id IS NOT NULL)
			AND (templates.id IS NULL OR (%s))
		LIMIT 1`,
		workspaceCampaignMediaPredicate(access, "campaigns", "cm_media"),
		templateArg,
		workspaceTemplateMediaPredicate(access, "templates", "tm_media"),
		workspaceCampaignListPredicate(access, "campaigns", "cl_list", "cl"),
		templateArg, templateArg,
		campaignArg, campaignScope,
		templateArg, templateScope)
	args = append(args, id, tplID)

	var out models.Campaign
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
				c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
		}
		return models.Campaign{}, workspaceQueryError("fetching campaign preview", err)
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return out, nil
}

// GetWorkspaceList returns one list with subscriber counters through the
// owner-scoped predicate.  Managers may inspect another member's list, while
// a personal request can only ever resolve its own row.
func (c *Core) GetWorkspaceList(access models.WorkspaceAccess, id int) (models.List, error) {
	if id < 1 {
		return models.List{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.list}"))
	}
	_ = c.refreshCache(matListSubStats, false)
	scope, args := workspaceOwnerScopedReadPredicate(access, "l", 1)
	idArg := len(args) + 1
	stmt := fmt.Sprintf(`
		WITH statuses AS (
			SELECT list_id,
				COALESCE(JSONB_OBJECT_AGG(status, subscriber_count) FILTER (WHERE status IS NOT NULL), '{}') AS subscriber_statuses,
				SUM(subscriber_count) AS subscriber_count
			FROM mat_list_subscriber_stats GROUP BY list_id
		)
		SELECT l.*, COALESCE(u.username, '') AS owner_username,
			COALESCE(u.name, '') AS owner_name,
			COALESCE(s.subscriber_statuses, '{}') AS subscriber_statuses,
			COALESCE(s.subscriber_count, 0) AS subscriber_count
		FROM lists l
		LEFT JOIN users u ON u.id = COALESCE(l.owner_user_id, l.original_owner_user_id)
		LEFT JOIN statuses s ON s.list_id = l.id
		WHERE l.id = $%d AND (%s)
		LIMIT 1`, idArg, scope)
	args = append(args, id)
	var out models.List
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return models.List{}, echo.NewHTTPError(http.StatusBadRequest,
				c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.list}"))
		}
		return models.List{}, workspaceQueryError("fetching list", err)
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	for _, n := range out.SubscriberCounts {
		out.SubscriberCount += n
	}
	return out, nil
}

// GetWorkspaceTemplate resolves one template through the active workspace.
// It mirrors GetWorkspaceTemplates' media metadata so editor and preview
// responses do not need a second unscoped lookup.
func (c *Core) GetWorkspaceTemplate(access models.WorkspaceAccess, id int, noBody bool) (models.Template, error) {
	if id < 1 {
		return models.Template{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.template}"))
	}
	scope, args := workspaceReadPredicate(access, "t", 1)
	idArg := len(args) + 1
	bodyArg := idArg + 1
	stmt := fmt.Sprintf(`
		SELECT t.id, t.name, t.type, t.subject,
			(CASE WHEN $%d THEN '' ELSE t.body END) AS body,
			(CASE WHEN $%d THEN NULL ELSE t.body_source END) AS body_source,
			t.is_default, t.created_at, t.updated_at,
			t.organization_id, t.owner_user_id, t.original_owner_user_id,
			t.visibility, t.transfer_pending_at,
			COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			COALESCE((SELECT ARRAY_AGG(tm.media_id ORDER BY tm.media_id)::INT[] FROM template_media tm
				LEFT JOIN media tm_media ON tm_media.id = tm.media_id
				WHERE tm.template_id = t.id AND tm.media_id IS NOT NULL
					AND (%s)), '{}') AS media_id,
			COALESCE((SELECT JSON_AGG(JSON_BUILD_OBJECT('id', tm.media_id, 'filename', tm.filename)
				ORDER BY tm.media_id) FROM template_media tm
				LEFT JOIN media tm_media ON tm_media.id = tm.media_id
				WHERE tm.template_id = t.id AND (%s)), '[]') AS media
		FROM templates t
		LEFT JOIN users u ON u.id = COALESCE(t.owner_user_id, t.original_owner_user_id)
		WHERE t.id = $%d AND (%s)
		LIMIT 1`, bodyArg, bodyArg,
		workspaceTemplateMediaPredicate(access, "t", "tm_media"),
		workspaceTemplateMediaPredicate(access, "t", "tm_media"),
		idArg, scope)
	args = append(args, id, noBody)
	var out models.Template
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return models.Template{}, echo.NewHTTPError(http.StatusBadRequest,
				c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.template}"))
		}
		return models.Template{}, workspaceQueryError("fetching template", err)
	}
	return out, nil
}

// GetWorkspaceTemplates returns only templates visible in the active
// workspace, including global templates from other organizations.
func (c *Core) GetWorkspaceTemplates(access models.WorkspaceAccess, status string, noBody bool) ([]models.Template, error) {
	scope, args := workspaceReadPredicate(access, "t", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT t.id, t.name, t.type, t.subject,
			(CASE WHEN $%d THEN '' ELSE t.body END) AS body,
			(CASE WHEN $%d THEN NULL ELSE t.body_source END) AS body_source,
			t.is_default, t.created_at, t.updated_at,
			t.organization_id, t.owner_user_id, t.original_owner_user_id, t.visibility, t.transfer_pending_at,
			COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			COALESCE((SELECT ARRAY_AGG(tm.media_id ORDER BY tm.media_id)::INT[] FROM template_media tm
				LEFT JOIN media tm_media ON tm_media.id = tm.media_id
				WHERE tm.template_id = t.id AND tm.media_id IS NOT NULL
					AND (%s)), '{}') AS media_id,
			COALESCE((SELECT JSON_AGG(JSON_BUILD_OBJECT('id', tm.media_id, 'filename', tm.filename)
				ORDER BY tm.media_id) FROM template_media tm
				LEFT JOIN media tm_media ON tm_media.id = tm.media_id
				WHERE tm.template_id = t.id AND (%s)), '[]') AS media
		FROM templates t
		LEFT JOIN users u ON u.id = COALESCE(t.owner_user_id, t.original_owner_user_id)
		WHERE (%s) AND ($%d = '' OR t.type = $%d::template_type)
		ORDER BY t.created_at`, first, first,
		workspaceTemplateMediaPredicate(access, "t", "tm_media"),
		workspaceTemplateMediaPredicate(access, "t", "tm_media"),
		scope, first+1, first+1)
	args = append(args, noBody, status)

	var out []models.Template
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching templates", err)
	}
	return out, nil
}

// QueryWorkspaceMedia returns media files visible in the active workspace.
func (c *Core) QueryWorkspaceMedia(access models.WorkspaceAccess, provider string, s media.Store, query string, offset, limit int) ([]media.Media, int, error) {
	scope, args := workspaceReadPredicate(access, "m", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT COUNT(*) OVER() AS total, m.*, COALESCE(u.username, '') AS owner_username,
			COALESCE(u.name, '') AS owner_name
		FROM media m
		LEFT JOIN users u ON u.id = COALESCE(m.owner_user_id, m.original_owner_user_id)
		WHERE (%s) AND ($%d = '' OR m.filename ILIKE $%d) AND m.provider = $%d
		-- paginator encodes per_page=all as a zero limit. PostgreSQL's
		-- literal LIMIT 0 would incorrectly return an empty media library, so
		-- normalize non-positive limits to an unbounded LIMIT just like the
		-- other workspace list queries.
		ORDER BY m.created_at DESC OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
		scope, first, first, first+1, first+2, first+3, first+3)
	query = strings.TrimSpace(query)
	if query != "" {
		query = "%" + query + "%"
	}
	args = append(args, query, provider, offset, limit)

	var out []media.Media
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching media", err)
	}
	for i := range out {
		out[i].URL = s.GetURL(out[i].Filename)
		if out[i].Thumb != "" {
			out[i].ThumbURL.String = s.GetURL(out[i].Thumb)
			out[i].ThumbURL.Valid = true
		}
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

// GetWorkspaceMediaByID resolves one exact media row through the active
// workspace. Besides a directly visible media record, an image may be readable
// because it is attached to a readable template or campaign. That derived
// access is essential for organization/global templates whose media records
// remain private to the template creator while the template itself is shared.
//
// New editor URLs include the media ID so cloned records that intentionally
// retain the same provider filename cannot be confused with one another.
func (c *Core) GetWorkspaceMediaByID(access models.WorkspaceAccess, id int) (media.Media, error) {
	if id < 1 {
		return media.Media{}, ErrNotFound
	}
	mediaScope, args := workspaceReadPredicate(access, "m", 1)
	templateScope, templateArgs := workspaceReadPredicate(access, "t", len(args)+1)
	args = append(args, templateArgs...)
	campaignScope, campaignArgs := workspaceReadPredicate(access, "c", len(args)+1)
	args = append(args, campaignArgs...)
	first := len(args) + 1
	var out media.Media
	stmt := fmt.Sprintf(`
		SELECT m.* FROM media m
		WHERE m.id = $%d
			AND (
				(%s)
				-- A template/campaign association is a derived read grant.  It
				-- must never resurrect a binary which has been marked for transfer
				-- (or whose organization has been archived).  Organization
				-- managers can still inspect such a row through the direct media
				-- predicate above when the selected workspace is the owning org;
				-- the derived path is intentionally limited to live binaries.
				OR (m.transfer_pending_at IS NULL
					AND (m.organization_id IS NULL OR EXISTS (
						SELECT 1 FROM organizations media_organization
						WHERE media_organization.id = m.organization_id
							AND media_organization.status = 'active'
					))
					AND EXISTS (
					SELECT 1 FROM template_media tm
					JOIN templates t ON t.id = tm.template_id
					WHERE tm.media_id = m.id AND (%s)
						AND t.transfer_pending_at IS NULL
						AND (t.organization_id IS NULL OR EXISTS (
							SELECT 1 FROM organizations template_organization
							WHERE template_organization.id = t.organization_id
								AND template_organization.status = 'active'
						))
						-- A template may carry a private image only from its own
						-- workspace/owner graph.  Without this check, a forged
						-- template_media row would make any private binary readable
						-- to the template audience merely by association.
						AND (
							m.visibility = 'global'
							OR (
								m.organization_id IS NOT DISTINCT FROM t.organization_id
								AND m.visibility = 'organization'
							)
							OR (
								m.organization_id IS NOT DISTINCT FROM t.organization_id
								AND m.owner_user_id IS NOT NULL
								AND t.owner_user_id IS NOT NULL
								AND m.owner_user_id = t.owner_user_id
								AND m.visibility <> 'organization'
							)
						)
					))
				OR (m.transfer_pending_at IS NULL
					AND (m.organization_id IS NULL OR EXISTS (
						SELECT 1 FROM organizations media_organization
						WHERE media_organization.id = m.organization_id
							AND media_organization.status = 'active'
					))
					AND EXISTS (
					SELECT 1 FROM campaign_media cm
					JOIN campaigns c ON c.id = cm.campaign_id
					WHERE cm.media_id = m.id AND (%s)
					))
			)
		LIMIT 1`, first, mediaScope, templateScope, campaignScope)
	args = append(args, id)
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return out, ErrNotFound
		}
		return out, workspaceQueryError("fetching workspace media", err)
	}
	return out, nil
}

// GetWorkspaceMediaByFilename resolves a stored media name through the active
// workspace. It is retained for old /uploads and /api/media/file/:filename
// links. New links should use GetWorkspaceMediaByID because filenames can be
// shared by cloned records.
func (c *Core) GetWorkspaceMediaByFilename(access models.WorkspaceAccess, filename string) (media.Media, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return media.Media{}, ErrNotFound
	}
	mediaScope, args := workspaceReadPredicate(access, "m", 1)
	templateScope, templateArgs := workspaceReadPredicate(access, "t", len(args)+1)
	args = append(args, templateArgs...)
	campaignScope, campaignArgs := workspaceReadPredicate(access, "c", len(args)+1)
	args = append(args, campaignArgs...)
	first := len(args) + 1
	var out media.Media
	stmt := fmt.Sprintf(`
		SELECT m.* FROM media m
		WHERE (m.filename = $%d OR m.thumb = $%d)
			AND (
				(%s)
				OR (m.transfer_pending_at IS NULL
					AND (m.organization_id IS NULL OR EXISTS (
						SELECT 1 FROM organizations media_organization
						WHERE media_organization.id = m.organization_id
							AND media_organization.status = 'active'
					))
					AND EXISTS (
					SELECT 1 FROM template_media tm
					JOIN templates t ON t.id = tm.template_id
					WHERE tm.media_id = m.id AND (%s)
						AND t.transfer_pending_at IS NULL
						AND (t.organization_id IS NULL OR EXISTS (
							SELECT 1 FROM organizations template_organization
							WHERE template_organization.id = t.organization_id
								AND template_organization.status = 'active'
						))
						AND (
							m.visibility = 'global'
							OR (
								m.organization_id IS NOT DISTINCT FROM t.organization_id
								AND m.visibility = 'organization'
							)
							OR (
								m.organization_id IS NOT DISTINCT FROM t.organization_id
								AND m.owner_user_id IS NOT NULL
								AND t.owner_user_id IS NOT NULL
								AND m.owner_user_id = t.owner_user_id
								AND m.visibility <> 'organization'
							)
						)
					))
				OR (m.transfer_pending_at IS NULL
					AND (m.organization_id IS NULL OR EXISTS (
						SELECT 1 FROM organizations media_organization
						WHERE media_organization.id = m.organization_id
							AND media_organization.status = 'active'
					))
					AND EXISTS (
					SELECT 1 FROM campaign_media cm
					JOIN campaigns c ON c.id = cm.campaign_id
					WHERE cm.media_id = m.id AND (%s)
					))
			)
		ORDER BY (m.filename = $%d) DESC, m.id ASC
		LIMIT 1`, first, first, mediaScope, templateScope, campaignScope, first)
	args = append(args, filename)
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return out, ErrNotFound
		}
		return out, workspaceQueryError("fetching workspace media", err)
	}
	return out, nil
}

// publicArchiveMediaGraph is the complete authorization graph for an
// unauthenticated archive image. A campaign being public is not sufficient on
// its own: the campaign/template relation must be real, every referenced row
// must still be live, and a private binary must belong to the same owner and
// workspace as the campaign (or the template that publishes it). Keeping this
// predicate in one place prevents the filename and ID routes from drifting.
const publicArchiveMediaGraph = `
	m.transfer_pending_at IS NULL
	AND (m.organization_id IS NULL OR EXISTS (
		SELECT 1 FROM organizations media_org
		WHERE media_org.id = m.organization_id AND media_org.status = 'active'
	))
	AND EXISTS (
		SELECT 1
		FROM campaigns c
		LEFT JOIN organizations campaign_org ON campaign_org.id = c.organization_id
		WHERE c.archive = TRUE
			AND c.type = 'regular'
			AND c.status = ANY('{running, paused, deferred, finished}'::campaign_status[])
			AND c.visibility = 'global'
			AND c.owner_user_id IS NOT NULL
			AND c.transfer_pending_at IS NULL
			AND (c.organization_id IS NULL OR campaign_org.status = 'active')
			AND (
				EXISTS (
					SELECT 1
					FROM campaign_media cm
					WHERE cm.campaign_id = c.id
						AND cm.media_id = m.id
						AND (
							m.visibility = 'global'
							OR (
								m.organization_id IS NOT DISTINCT FROM c.organization_id
								AND (m.owner_user_id = c.owner_user_id OR m.visibility = 'organization')
							)
						)
				)
				OR EXISTS (
					SELECT 1
					FROM template_media tm
					JOIN templates t ON t.id = tm.template_id
					LEFT JOIN organizations template_org ON template_org.id = t.organization_id
					WHERE tm.media_id = m.id
						AND (c.template_id = t.id OR c.archive_template_id = t.id)
						AND t.transfer_pending_at IS NULL
						AND (t.organization_id IS NULL OR template_org.status = 'active')
						AND (
							t.visibility = 'global'
							OR (
								t.organization_id IS NOT DISTINCT FROM c.organization_id
								AND (t.owner_user_id = c.owner_user_id OR t.visibility = 'organization')
							)
						)
						AND (
							m.visibility = 'global'
							OR (
								m.organization_id IS NOT DISTINCT FROM t.organization_id
								AND (m.owner_user_id = t.owner_user_id OR m.visibility = 'organization')
							)
						)
				)
			)
	)`

// getPublicArchiveMedia resolves one authorized media row. The same query is
// used for legacy filename URLs and ID-qualified URLs, so a boolean check
// cannot race with a second unscoped lookup (or select a different clone that
// happens to share a provider filename).
func (c *Core) getPublicArchiveMedia(where string, arg any, s media.Store) (media.Media, error) {
	var out media.Media
	stmt := fmt.Sprintf(`SELECT m.* FROM media m WHERE %s AND (%s)
		ORDER BY m.id ASC LIMIT 1`, where, publicArchiveMediaGraph)
	if err := c.db.Get(&out, stmt, arg); err != nil {
		if err == sql.ErrNoRows {
			return out, ErrNotFound
		}
		return out, workspaceQueryError("checking public archive media", err)
	}
	if s != nil {
		out.URL = s.GetURL(out.Filename)
		if out.Thumb != "" {
			out.ThumbURL = null.String{Valid: true, String: s.GetURL(out.Thumb)}
		}
	}
	return out, nil
}

// GetPublicArchiveMediaByFilename returns a media row only when it is part of
// an active public archive. It is used by the legacy filename-only route.
func (c *Core) GetPublicArchiveMediaByFilename(filename string, s media.Store) (media.Media, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return media.Media{}, ErrNotFound
	}
	return c.getPublicArchiveMedia("(m.filename = $1 OR m.thumb = $1)", filename, s)
}

// GetPublicArchiveMediaByID is the exact-ID counterpart used by the protected
// media route. Filename-only archive checks are kept for legacy links, but an
// ID-qualified query prevents cloned records from being confused.
func (c *Core) GetPublicArchiveMediaByID(id int, s media.Store) (media.Media, error) {
	if id < 1 {
		return media.Media{}, ErrNotFound
	}
	return c.getPublicArchiveMedia("m.id = $1", id, s)
}

// IsPublicArchiveMedia reports whether a media binary is referenced by an
// active public archive campaign. Public archives render stored HTML directly,
// so this narrow exception preserves archive images without making the whole
// upload directory readable to unauthenticated callers.
func (c *Core) IsPublicArchiveMedia(filename string) (bool, error) {
	_, err := c.GetPublicArchiveMediaByFilename(filename, nil)
	if err == ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

// IsPublicArchiveMediaID is the exact-ID counterpart used by the protected
// media route.
func (c *Core) IsPublicArchiveMediaID(id int) (bool, error) {
	_, err := c.GetPublicArchiveMediaByID(id, nil)
	if err == ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

func workspaceSubscriberReadPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	return workspaceOwnerScopedReadPredicate(access, alias, firstArg)
}

// workspaceSensitiveSubscriberPredicate is the immutable owner boundary for
// operations that serialize recipient identity (CSV/privacy exports). An
// organization manager may inspect a member's subscriber row through the
// normal read predicate, but that inspection grant must never be enough to
// export the member's e-mail address or subscriptions. Keep this predicate in
// Core as well as the HTTP checks so a non-HTTP caller cannot bypass the
// privacy boundary by invoking an export helper directly.
func workspaceSensitiveSubscriberPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	field := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	arg := func(offset int) string { return fmt.Sprintf("$%d", firstArg+offset) }
	if access.PlatformAdmin {
		return "TRUE", nil
	}
	if access.IsOrganization() {
		return fmt.Sprintf("%sorganization_id = %s AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL",
				field(""), arg(0), field(""), arg(1), field("")),
			[]any{access.OrganizationID, access.UserID}
	}
	return fmt.Sprintf("%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL",
		field(""), field(""), arg(0), field("")), []any{access.UserID}
}

type workspaceSubscriberLists struct {
	SubscriberID int            `db:"subscriber_id"`
	Lists        types.JSONText `db:"lists"`
}

// loadWorkspaceSubscriberLists deliberately does not use the legacy lazy
// query. A malformed subscriber_lists row must not expose a list that belongs
// to a different owner or organization just because the subscriber itself is
// visible to an organization manager.
func (c *Core) loadWorkspaceSubscriberLists(access models.WorkspaceAccess, subscribers models.Subscribers) error {
	if len(subscribers) == 0 {
		return nil
	}

	// Pending-transfer lists are intentionally hidden from ordinary members,
	// but organization managers (and platform administrators) must be able to
	// inspect the complete audience before transferring it.  The owner/org
	// equality predicates below remain mandatory in both cases, so a malformed
	// subscriber_lists row can never expose another workspace's list.
	listStatePredicate := "l.transfer_pending_at IS NULL"
	if access.PlatformAdmin || access.IsOrganizationManager() {
		listStatePredicate = "TRUE"
	}
	organizationPredicate := "TRUE"
	if !access.PlatformAdmin {
		organizationPredicate = `(l.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations list_org
			WHERE list_org.id = l.organization_id AND list_org.status = 'active'
		))`
	}

	var rows []workspaceSubscriberLists
	if err := c.db.Select(&rows, fmt.Sprintf(`
		WITH associated_lists AS (
			SELECT sl.subscriber_id, JSON_AGG(
				ROW_TO_JSON((SELECT list_row FROM (
					SELECT
						sl.status AS subscription_status,
						sl.created_at AS subscription_created_at,
						sl.updated_at AS subscription_updated_at,
						sl.meta AS subscription_meta,
						l.*
				) list_row)) ORDER BY l.id
			) AS lists
			FROM subscriber_lists sl
			JOIN subscribers s ON s.id = sl.subscriber_id
			JOIN lists l ON l.id = sl.list_id
			WHERE sl.subscriber_id = ANY($1::INT[])
				AND l.organization_id IS NOT DISTINCT FROM s.organization_id
				AND l.owner_user_id IS NOT DISTINCT FROM s.owner_user_id
				AND %s
				AND %s
			GROUP BY sl.subscriber_id
		)
		SELECT requested.id AS subscriber_id, COALESCE(associated_lists.lists, '[]') AS lists
		FROM UNNEST($1::INT[]) AS requested(id)
		LEFT JOIN associated_lists ON associated_lists.subscriber_id = requested.id
		ORDER BY ARRAY_POSITION($1::INT[], requested.id)`, listStatePredicate, organizationPredicate), pq.Array(subscribers.GetIDs())); err != nil {
		return err
	}

	loaded := make(map[int]types.JSONText, len(rows))
	for _, row := range rows {
		loaded[row.SubscriberID] = row.Lists
	}
	for i := range subscribers {
		if lists, ok := loaded[subscribers[i].ID]; ok {
			subscribers[i].Lists = lists
		} else {
			subscribers[i].Lists = types.JSONText([]byte("[]"))
		}
	}
	return nil
}

// GetWorkspaceSubscriber returns one subscriber after applying the same
// owner/workspace predicate used by list and bulk queries.  The legacy
// GetSubscriber method intentionally remains available to public bearer-token
// flows; authenticated handlers must use this method so the embedded Lists
// payload cannot reveal cross-workspace associations.
func (c *Core) GetWorkspaceSubscriber(access models.WorkspaceAccess, id int) (models.Subscriber, error) {
	if id < 1 {
		return models.Subscriber{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("{globals.terms.subscriber} (%d)", id)))
	}

	scope, args := workspaceSubscriberReadPredicate(access, "s", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT s.*, COALESCE(u.username, '') AS owner_username,
			COALESCE(u.name, '') AS owner_name
		FROM subscribers s
		LEFT JOIN users u ON u.id = COALESCE(s.owner_user_id, s.original_owner_user_id)
		WHERE s.id = $%d AND (%s)
		LIMIT 1`, first, scope)
	args = append(args, id)

	var out models.Subscriber
	if err := c.db.Get(&out, stmt, args...); err != nil {
		if err == sql.ErrNoRows {
			return models.Subscriber{}, echo.NewHTTPError(http.StatusBadRequest,
				c.i18n.Ts("globals.messages.notFound", "name",
					fmt.Sprintf("{globals.terms.subscriber} (%d)", id)))
		}
		return models.Subscriber{}, workspaceQueryError("fetching subscriber", err)
	}

	// loadWorkspaceSubscriberLists mutates the slice elements in place. Keep a
	// stable slice here and copy the hydrated element back; passing a temporary
	// literal would otherwise discard the Lists field before returning it.
	hydrated := models.Subscribers{out}
	if err := c.loadWorkspaceSubscriberLists(access, hydrated); err != nil {
		return models.Subscriber{}, workspaceQueryError("fetching subscriber lists", err)
	}
	out = hydrated[0]
	return out, nil
}

// campaignActivityScope returns a conservative relationship predicate for
// campaign analytics attached to a subscriber.  Campaign recipients are
// created only for matching organization/owner pairs; enforcing that same
// invariant here prevents forged historical rows from crossing workspaces.
func campaignActivityScope(access models.WorkspaceAccess, campaignAlias, subscriberAlias string) string {
	predicate := fmt.Sprintf(`%s.organization_id IS NOT DISTINCT FROM %s.organization_id
		AND %s.owner_user_id IS NOT DISTINCT FROM %s.owner_user_id`, campaignAlias, subscriberAlias, campaignAlias, subscriberAlias)
	if access.PlatformAdmin || access.IsOrganizationManager() {
		return predicate
	}
	return predicate + fmt.Sprintf(`
		AND %s.transfer_pending_at IS NULL
		AND (%s.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations activity_org
			WHERE activity_org.id = %s.organization_id AND activity_org.status = 'active'
		))`, campaignAlias, campaignAlias, campaignAlias)
}

// GetWorkspaceSubscriberActivity returns campaign views and link clicks for
// a subscriber while constraining every joined campaign to the subscriber's
// own organization/owner boundary. Organization managers may inspect member
// statistics (including resources awaiting transfer), but no unrelated
// campaign identifiers are returned.
func (c *Core) GetWorkspaceSubscriberActivity(access models.WorkspaceAccess, id int) (models.SubscriberActivity, error) {
	if id < 1 {
		return models.SubscriberActivity{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("{globals.terms.subscriber} (%d)", id)))
	}

	subscriberScope, args := workspaceSubscriberReadPredicate(access, "s", 1)
	first := len(args) + 1
	activityScope := campaignActivityScope(access, "c", "s")
	stmt := fmt.Sprintf(`
		WITH target AS (
			SELECT s.id, s.organization_id, s.owner_user_id
			FROM subscribers s
			WHERE s.id = $%d AND (%s)
		), views AS (
			SELECT c.id, c.uuid, c.name, c.subject,
				COUNT(*) AS view_count, MAX(cv.created_at) AS last_viewed_at
			FROM campaign_views cv
			JOIN target s ON s.id = cv.subscriber_id
			JOIN campaigns c ON c.id = cv.campaign_id
			WHERE %s
			GROUP BY c.id, c.uuid, c.name, c.subject
			ORDER BY last_viewed_at DESC
		), clicks AS (
			SELECT l.id AS link_id, l.url,
				c.id AS campaign_id, c.uuid AS campaign_uuid,
				c.name AS campaign_name, c.subject AS campaign_subject,
				COUNT(*) AS click_count, MAX(lc.created_at) AS last_clicked_at
			FROM link_clicks lc
			JOIN target s ON s.id = lc.subscriber_id
			JOIN links l ON l.id = lc.link_id
			JOIN campaigns c ON c.id = lc.campaign_id
			WHERE %s
			GROUP BY l.id, l.url, c.id, c.uuid, c.name, c.subject
			ORDER BY last_clicked_at DESC
		)
		SELECT
			COALESCE((SELECT JSON_AGG(v) FROM views v), '[]') AS campaign_views,
			COALESCE((SELECT JSON_AGG(c) FROM clicks c), '[]') AS link_clicks`,
		first, subscriberScope, activityScope, activityScope)
	args = append(args, id)

	// Aggregates always produce one row, so explicitly verify that the target
	// subscriber matched the workspace predicate before returning an empty
	// activity payload for a hidden ID.
	var exists bool
	existsStmt := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM subscribers s WHERE s.id = $%d AND (%s))`, first, subscriberScope)
	if err := c.db.Get(&exists, existsStmt, args...); err != nil {
		return models.SubscriberActivity{}, workspaceQueryError("checking subscriber", err)
	}
	if !exists {
		return models.SubscriberActivity{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("{globals.terms.subscriber} (%d)", id)))
	}

	var out models.SubscriberActivity
	if err := c.db.Get(&out, stmt, args...); err != nil {
		return models.SubscriberActivity{}, workspaceQueryError("fetching subscriber activity", err)
	}
	return out, nil
}

// GetWorkspaceSubscriberProfileForExport returns the privacy export payload
// for an authenticated owner. Every relation is matched to the target
// subscriber's organization and owner, preventing a manager/platform request
// from accidentally serializing unrelated lists or campaign analytics.
func (c *Core) GetWorkspaceSubscriberProfileForExport(access models.WorkspaceAccess, id int) (models.SubscriberExportProfile, error) {
	if id < 1 {
		return models.SubscriberExportProfile{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("{globals.terms.subscriber} (%d)", id)))
	}

	// Privacy exports follow the immutable owner boundary, not the broader
	// organization-manager inspection predicate. Keep this check in Core so a
	// direct caller cannot turn a manager's read access into an export.
	subscriberScope, args := workspaceSensitiveSubscriberPredicate(access, "s", 1)
	first := len(args) + 1
	listState := "l.transfer_pending_at IS NULL"
	listOrganizationState := `(l.organization_id IS NULL OR EXISTS (
		SELECT 1 FROM organizations export_list_org
		WHERE export_list_org.id = l.organization_id AND export_list_org.status = 'active'
	))`
	campaignState := `c.transfer_pending_at IS NULL
		AND (c.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations export_campaign_org
			WHERE export_campaign_org.id = c.organization_id AND export_campaign_org.status = 'active'
		))`
	if access.PlatformAdmin || access.IsOrganizationManager() {
		listState = "TRUE"
		listOrganizationState = "TRUE"
		campaignState = "TRUE"
	}
	stmt := fmt.Sprintf(`
		WITH prof AS (
			SELECT s.id, s.uuid, s.email, s.name, s.attribs, s.status,
				s.created_at, s.updated_at, s.organization_id, s.owner_user_id
			FROM subscribers s
			WHERE s.id = $%d AND (%s)
		), subs AS (
			SELECT sl.status AS subscription_status,
				(CASE WHEN l.type = 'private' THEN 'Private list' ELSE l.name END) AS name,
				l.type, sl.created_at
			FROM prof p
			JOIN subscriber_lists sl ON sl.subscriber_id = p.id
			JOIN lists l ON l.id = sl.list_id
			WHERE l.organization_id IS NOT DISTINCT FROM p.organization_id
				AND l.owner_user_id IS NOT DISTINCT FROM p.owner_user_id
				AND %s
				AND %s
		), views AS (
			SELECT c.subject AS campaign, COUNT(cv.subscriber_id) AS views
			FROM prof p
			JOIN campaign_views cv ON cv.subscriber_id = p.id
			JOIN campaigns c ON c.id = cv.campaign_id
			WHERE c.organization_id IS NOT DISTINCT FROM p.organization_id
				AND c.owner_user_id IS NOT DISTINCT FROM p.owner_user_id
				AND %s
			GROUP BY c.id, c.subject ORDER BY c.id
		), clicks AS (
			SELECT l.url, COUNT(lc.subscriber_id) AS clicks
			FROM prof p
			JOIN link_clicks lc ON lc.subscriber_id = p.id
			JOIN links l ON l.id = lc.link_id
			JOIN campaigns c ON c.id = lc.campaign_id
			WHERE c.organization_id IS NOT DISTINCT FROM p.organization_id
				AND c.owner_user_id IS NOT DISTINCT FROM p.owner_user_id
				AND %s
			GROUP BY l.id, l.url ORDER BY l.id
		)
		SELECT (SELECT email FROM prof) AS email,
			COALESCE((SELECT JSON_AGG(t) FROM prof t), '{}') AS profile,
			COALESCE((SELECT JSON_AGG(t) FROM subs t), '[]') AS subscriptions,
			COALESCE((SELECT JSON_AGG(t) FROM views t), '[]') AS campaign_views,
			COALESCE((SELECT JSON_AGG(t) FROM clicks t), '[]') AS link_clicks`,
		first, subscriberScope, listState, listOrganizationState, campaignState, campaignState)
	args = append(args, id)

	var out models.SubscriberExportProfile
	if err := c.db.Get(&out, stmt, args...); err != nil {
		return models.SubscriberExportProfile{}, workspaceQueryError("fetching subscriber export data", err)
	}
	if out.Email == "" {
		return models.SubscriberExportProfile{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("{globals.terms.subscriber} (%d)", id)))
	}
	return out, nil
}

// QueryWorkspaceSubscribers keeps subscriber identifiers and personal data
// inside the active owner/workspace boundary. Organization managers may read
// member records but never gain a writable result set from this query.
func (c *Core) QueryWorkspaceSubscribers(access models.WorkspaceAccess, search string, listIDs []int, subscriptionStatus, order, orderBy string, offset, limit int) (models.Subscribers, int, error) {
	if listIDs == nil {
		listIDs = []int{}
	}
	fields := map[string]string{
		"email":      "s.email",
		"status":     "s.status",
		"name":       "s.name",
		"created_at": "s.created_at",
		"updated_at": "s.updated_at",
	}
	if _, ok := fields[orderBy]; !ok {
		orderBy = "created_at"
	}
	scope, args := workspaceSubscriberReadPredicate(access, "s", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT s.*, COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			COUNT(*) OVER() AS total
		FROM subscribers s
		LEFT JOIN users u ON u.id = COALESCE(s.owner_user_id, s.original_owner_user_id)
		WHERE (%s)
			AND ($%d = '' OR s.name ~* $%d OR s.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM subscriber_lists sl
				JOIN lists l ON l.id = sl.list_id
				WHERE sl.subscriber_id = s.id AND sl.list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM s.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM s.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))
		ORDER BY %s OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
		scope,
		first, first, first,
		first+1, first+1, first+2, first+2,
		workspaceSort(orderBy, order, fields, "s.created_at"), first+3, first+4, first+4)
	args = append(args, strings.TrimSpace(search), pq.Array(listIDs), subscriptionStatus, offset, limit)

	var out models.Subscribers
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching subscribers", err)
	}
	if err := c.loadWorkspaceSubscriberLists(access, out); err != nil {
		return nil, 0, workspaceQueryError("fetching subscriber lists", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

// GetWorkspaceSubscriberIDs resolves bulk targets through the same scope
// predicate used for list views. It intentionally does not accept arbitrary
// SQL expressions; raw expressions are unsafe for workspace bulk writes.
func (c *Core) GetWorkspaceSubscriberIDs(access models.WorkspaceAccess, search string, listIDs []int, subscriptionStatus string) ([]int, error) {
	if listIDs == nil {
		listIDs = []int{}
	}
	scope, args := workspaceSubscriberReadPredicate(access, "s", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT s.id FROM subscribers s
		WHERE (%s)
			AND ($%d = '' OR s.name ~* $%d OR s.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM subscriber_lists sl
				JOIN lists l ON l.id = sl.list_id
				WHERE sl.subscriber_id = s.id
					AND sl.list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM s.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM s.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))`,
		scope,
		first, first, first,
		first+1, first+1, first+2, first+2)
	args = append(args, strings.TrimSpace(search), pq.Array(listIDs), subscriptionStatus)
	var ids []int
	if err := c.db.Select(&ids, stmt, args...); err != nil {
		return nil, workspaceQueryError("selecting subscribers", err)
	}
	return ids, nil
}

// GetWorkspaceSubscribersByEmails is used by campaign test sends. Email
// addresses are only unique within an owner/workspace, so the legacy global
// email lookup cannot be used here.
func (c *Core) GetWorkspaceSubscribersByEmails(access models.WorkspaceAccess, emails []string) (models.Subscribers, error) {
	if emails == nil {
		emails = []string{}
	}
	scope, args := workspaceSubscriberReadPredicate(access, "s", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT s.*
		FROM subscribers s
		WHERE (%s) AND LOWER(s.email) = ANY($%d::TEXT[])
		ORDER BY s.id`, scope, first)
	for i := range emails {
		emails[i] = strings.ToLower(strings.TrimSpace(emails[i]))
	}
	args = append(args, pq.Array(emails))
	var out models.Subscribers
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching subscribers", err)
	}
	if len(out) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("campaigns.noKnownSubsToTest"))
	}
	if err := c.loadWorkspaceSubscriberLists(access, out); err != nil {
		return nil, workspaceQueryError("fetching subscriber lists", err)
	}
	return out, nil
}

// GetManagedWorkspaceSubscribersByEmails is the test-send counterpart of the
// read helper above. Organization managers can inspect another member's
// subscriber records, but test sending must remain within their own owner
// boundary.
func (c *Core) GetManagedWorkspaceSubscribersByEmails(access models.WorkspaceAccess, emails []string) (models.Subscribers, error) {
	if emails == nil {
		emails = []string{}
	}
	var (
		scope string
		args  []any
	)
	if access.PlatformAdmin {
		scope = "TRUE"
	} else if access.IsOrganization() {
		scope = "s.organization_id = $1 AND s.owner_user_id = $2 AND s.transfer_pending_at IS NULL"
		args = []any{access.OrganizationID, access.UserID}
	} else {
		scope = "s.organization_id IS NULL AND s.owner_user_id = $1 AND s.transfer_pending_at IS NULL"
		args = []any{access.UserID}
	}
	for i := range emails {
		emails[i] = strings.ToLower(strings.TrimSpace(emails[i]))
	}
	stmt := fmt.Sprintf(`
		SELECT s.* FROM subscribers s
		WHERE (%s) AND LOWER(s.email) = ANY($%d::TEXT[])
		ORDER BY s.id`, scope, len(args)+1)
	args = append(args, pq.Array(emails))
	var out models.Subscribers
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching subscribers", err)
	}
	if len(out) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("campaigns.noKnownSubsToTest"))
	}
	if err := c.loadWorkspaceSubscriberLists(access, out); err != nil {
		return nil, workspaceQueryError("fetching subscriber lists", err)
	}
	return out, nil
}

// ExportWorkspaceSubscribers batches export rows without ever preparing an
// unscoped query. Managers are rejected at the handler boundary before this
// helper is called.
func (c *Core) ExportWorkspaceSubscribers(access models.WorkspaceAccess, search string, listIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.SubscriberExport, error), error) {
	return c.exportWorkspaceSubscribers(access, search, "", listIDs, requestedIDs, subscriptionStatus, batchSize)
}

// InsertWorkspaceSubscriber creates an owner-scoped subscriber and its list
// subscriptions atomically. The caller validates target lists first; the
// additional database predicates keep a forged request from crossing scope.
func (c *Core) InsertWorkspaceSubscriber(access models.WorkspaceAccess, sub models.Subscriber, listIDs []int, preconfirm, assertOptin bool) (models.Subscriber, bool, error) {
	if listIDs == nil {
		listIDs = []int{}
	}
	uu, err := uuid.NewV4()
	if err != nil {
		return models.Subscriber{}, false, echo.NewHTTPError(http.StatusInternalServerError, c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	if sub.Status == "" {
		sub.Status = models.SubscriberStatusEnabled
	}
	scope := ApplyWorkspaceScope(access, models.ResourceVisibilityPrivate)
	subStatus := models.SubscriptionStatusUnconfirmed
	if preconfirm {
		subStatus = models.SubscriptionStatusConfirmed
	}

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.Subscriber{}, false, workspaceQueryError("starting subscriber creation", err)
	}
	defer tx.Rollback()
	var id int
	err = tx.Get(&id, `
		INSERT INTO subscribers (
			uuid, email, name, status, attribs,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`, uu, sub.Email, strings.TrimSpace(sub.Name), sub.Status, sub.Attribs,
		scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return models.Subscriber{}, false, echo.NewHTTPError(http.StatusConflict, c.i18n.T("subscribers.emailExists"))
		}
		return models.Subscriber{}, false, workspaceQueryError("creating subscriber", err)
	}

	if len(listIDs) > 0 {
		var inserted int
		if access.IsOrganization() {
			err = tx.Get(&inserted, `
				WITH lists_in_scope AS (
					SELECT id FROM lists WHERE id = ANY($1::INT[])
						AND organization_id = $2 AND owner_user_id = $3 AND transfer_pending_at IS NULL
				), ins AS (
					INSERT INTO subscriber_lists (subscriber_id, list_id, status)
					SELECT $4, id, $5::subscription_status FROM lists_in_scope
					ON CONFLICT (subscriber_id, list_id) DO UPDATE SET status = EXCLUDED.status, updated_at = NOW()
					RETURNING 1
				) SELECT COUNT(*) FROM ins`, pq.Array(listIDs), access.OrganizationID, access.UserID, id, subStatus)
		} else {
			err = tx.Get(&inserted, `
				WITH lists_in_scope AS (
					SELECT id FROM lists WHERE id = ANY($1::INT[])
						AND organization_id IS NULL AND owner_user_id = $2 AND transfer_pending_at IS NULL
				), ins AS (
					INSERT INTO subscriber_lists (subscriber_id, list_id, status)
					SELECT $3, id, $4::subscription_status FROM lists_in_scope
					ON CONFLICT (subscriber_id, list_id) DO UPDATE SET status = EXCLUDED.status, updated_at = NOW()
					RETURNING 1
				) SELECT COUNT(*) FROM ins`, pq.Array(listIDs), access.UserID, id, subStatus)
		}
		if err != nil {
			return models.Subscriber{}, false, workspaceQueryError("adding subscriber lists", err)
		}
		if inserted != len(listIDs) {
			return models.Subscriber{}, false, echo.NewHTTPError(http.StatusForbidden, "a selected list is outside the active workspace")
		}
	}
	if err := tx.Commit(); err != nil {
		return models.Subscriber{}, false, workspaceQueryError("committing subscriber creation", err)
	}

	sub.ID = id
	out, err := c.GetWorkspaceSubscriber(access, id)
	if err != nil {
		return models.Subscriber{}, false, err
	}
	hasOptin := false
	if !preconfirm && c.consts.SendOptinConfirmation {
		num, err := c.h.SendOptinConfirmation(out, listIDs)
		if assertOptin && err != nil {
			return out, hasOptin, err
		}
		hasOptin = num > 0
	}
	return out, hasOptin, nil
}

// UpsertPublicWorkspaceSubscriber subscribes an unauthenticated address to
// public lists that have already been proven to belong to one owner/workspace.
// This is deliberately separate from the administrator create flow: public
// requests retain the existing subscriber profile and only add subscriptions.
func (c *Core) UpsertPublicWorkspaceSubscriber(access models.WorkspaceAccess, sub models.Subscriber, listIDs []int) (models.Subscriber, bool, error) {
	if access.UserID < 1 {
		return models.Subscriber{}, false, echo.NewHTTPError(http.StatusBadRequest, "public list has no resource owner")
	}
	if len(listIDs) == 0 {
		return models.Subscriber{}, false, echo.NewHTTPError(http.StatusBadRequest, "at least one public list is required")
	}

	uu, err := uuid.NewV4()
	if err != nil {
		return models.Subscriber{}, false, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	scope := ApplyWorkspaceScope(access, models.ResourceVisibilityPrivate)
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.Subscriber{}, false, workspaceQueryError("starting public subscription", err)
	}
	defer tx.Rollback()

	var (
		subscriberID int
		status       string
	)
	if err := tx.QueryRowx(`
		INSERT INTO subscribers AS s (
			uuid, email, name, status, attribs,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES ($1, $2, $3, 'enabled', $4, $5, $6, $7, 'private')
		ON CONFLICT ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
			WHERE owner_user_id IS NOT NULL
		DO UPDATE SET updated_at = NOW()
		RETURNING id, status`, uu, strings.ToLower(strings.TrimSpace(sub.Email)), strings.TrimSpace(sub.Name),
		sub.Attribs, scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID).
		Scan(&subscriberID, &status); err != nil {
		return models.Subscriber{}, false, workspaceQueryError("creating public subscription", err)
	}

	var inserted int
	if err := tx.Get(&inserted, `
		WITH lists_in_scope AS (
			SELECT l.id FROM lists l
			LEFT JOIN organizations organization ON organization.id = l.organization_id
			WHERE l.id = ANY($1::INT[]) AND l.type = 'public' AND l.status = 'active'
				AND l.transfer_pending_at IS NULL
				AND l.organization_id IS NOT DISTINCT FROM $2::BIGINT
				AND l.owner_user_id = $3
				AND (l.organization_id IS NULL OR organization.status = 'active')
		), subscriptions AS (
			INSERT INTO subscriber_lists (subscriber_id, list_id, status)
			SELECT $4, id,
				CASE WHEN $5 = 'blocklisted' THEN 'unsubscribed'::subscription_status
				ELSE 'unconfirmed'::subscription_status END
			FROM lists_in_scope
			ON CONFLICT (subscriber_id, list_id) DO UPDATE
				SET updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM subscriptions`, pq.Array(listIDs), scope.OrganizationID, access.UserID, subscriberID, status); err != nil {
		return models.Subscriber{}, false, workspaceQueryError("adding public list subscriptions", err)
	}
	if inserted != len(listIDs) {
		return models.Subscriber{}, false, echo.NewHTTPError(http.StatusForbidden, "a selected public list is outside the owner's workspace")
	}
	if err := tx.Commit(); err != nil {
		return models.Subscriber{}, false, workspaceQueryError("committing public subscription", err)
	}

	out, err := c.GetWorkspaceSubscriber(access, subscriberID)
	if err != nil {
		return models.Subscriber{}, false, err
	}
	hasOptin := false
	if c.consts.SendOptinConfirmation {
		num, err := c.h.SendOptinConfirmation(out, listIDs)
		if err != nil {
			return out, false, err
		}
		hasOptin = num > 0
	}
	return out, hasOptin, nil
}

func searchString(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	return "%" + in + "%"
}

func workspaceQueryError(action string, err error) error {
	return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("error %s: %s", action, pqErrMsg(err)))
}
