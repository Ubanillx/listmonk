package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx/types"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// workspaceReadPredicate emits a fixed SQL predicate for a resource alias.
// User supplied filters are always appended outside this predicate, so an
// arbitrary search condition cannot widen a workspace boundary.
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
		return fmt.Sprintf("((%sorganization_id IS NULL AND %sowner_user_id = %s) OR (%svisibility = 'global' AND %stransfer_pending_at IS NULL))",
			field(""), field(""), arg(0), field(""), field("")), []any{access.UserID}
	}
	if access.IsOrganizationManager() {
		return fmt.Sprintf("(%sorganization_id = %s OR (%svisibility = 'global' AND %stransfer_pending_at IS NULL))",
			field(""), arg(0), field(""), field("")), []any{access.OrganizationID}
	}
	return fmt.Sprintf(`((%sorganization_id = %s AND (
		%sowner_user_id = %s OR (%stransfer_pending_at IS NULL AND %svisibility IN ('organization', 'global'))
	)) OR (%svisibility = 'global' AND %stransfer_pending_at IS NULL))`,
			field(""), arg(0), field(""), arg(1), field(""), field(""), field(""), field("")),
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
		return fmt.Sprintf("(%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
			field(""), field(""), arg(0), field("")), []any{access.UserID}
	}
	if access.IsOrganizationManager() {
		return fmt.Sprintf("%sorganization_id = %s", field(""), arg(0)), []any{access.OrganizationID}
	}
	return fmt.Sprintf("(%sorganization_id = %s AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
		field(""), arg(0), field(""), arg(1), field("")), []any{access.OrganizationID, access.UserID}
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
		SELECT c.*, COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			CASE WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = c.id)
				THEN (SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = c.id
					AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[]))
				ELSE GREATEST(c.to_send - c.sent, 0) END AS unsent_count,
			COUNT(*) OVER() AS total,
			(SELECT COALESCE(ARRAY_TO_JSON(ARRAY_AGG(l)), '[]') FROM (
				SELECT COALESCE(cl.list_id, 0) AS id, cl.list_name AS name
				FROM campaign_lists cl WHERE cl.campaign_id = c.id
			) l) AS lists
		FROM campaigns c
		LEFT JOIN users u ON u.id = COALESCE(c.owner_user_id, c.original_owner_user_id)
		WHERE (%s)
			AND (CARDINALITY($%d::campaign_status[]) = 0 OR c.status = ANY($%d::campaign_status[]))
			AND (CARDINALITY($%d::VARCHAR(100)[]) = 0 OR $%d <@ c.tags)
			AND ($%d = '' OR c.name ILIKE $%d OR c.subject ILIKE $%d)
		ORDER BY %s OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
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
	if err := out.LoadStats(c.q.GetCampaignStats); err != nil {
		return nil, 0, workspaceQueryError("fetching campaign statistics", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
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
				WHERE tm.template_id = t.id AND tm.media_id IS NOT NULL), '{}') AS media_id,
			COALESCE((SELECT JSON_AGG(JSON_BUILD_OBJECT('id', tm.media_id, 'filename', tm.filename)
				ORDER BY tm.media_id) FROM template_media tm WHERE tm.template_id = t.id), '[]') AS media
		FROM templates t
		LEFT JOIN users u ON u.id = COALESCE(t.owner_user_id, t.original_owner_user_id)
		WHERE (%s) AND ($%d = '' OR t.type = $%d::template_type)
		ORDER BY t.created_at`, first, first, scope, first+1, first+1)
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
		ORDER BY m.created_at DESC OFFSET $%d LIMIT $%d`,
		scope, first, first, first+1, first+2, first+3)
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

func workspaceSubscriberReadPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	return workspaceOwnerScopedReadPredicate(access, alias, firstArg)
}

type workspaceSubscriberLists struct {
	SubscriberID int            `db:"subscriber_id"`
	Lists        types.JSONText `db:"lists"`
}

// loadWorkspaceSubscriberLists deliberately does not use the legacy lazy
// query. A malformed subscriber_lists row must not expose a list that belongs
// to a different owner or organization just because the subscriber itself is
// visible to an organization manager.
func (c *Core) loadWorkspaceSubscriberLists(subscribers models.Subscribers) error {
	if len(subscribers) == 0 {
		return nil
	}

	var rows []workspaceSubscriberLists
	if err := c.db.Select(&rows, `
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
				AND l.transfer_pending_at IS NULL
			GROUP BY sl.subscriber_id
		)
		SELECT requested.id AS subscriber_id, COALESCE(associated_lists.lists, '[]') AS lists
		FROM UNNEST($1::INT[]) AS requested(id)
		LEFT JOIN associated_lists ON associated_lists.subscriber_id = requested.id
		ORDER BY ARRAY_POSITION($1::INT[], requested.id)`, pq.Array(subscribers.GetIDs())); err != nil {
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
	if err := c.loadWorkspaceSubscriberLists(out); err != nil {
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
	if err := c.loadWorkspaceSubscriberLists(out); err != nil {
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
	if err := c.loadWorkspaceSubscriberLists(out); err != nil {
		return nil, workspaceQueryError("fetching subscriber lists", err)
	}
	return out, nil
}

// ExportWorkspaceSubscribers batches export rows without ever preparing an
// unscoped query. Managers are rejected at the handler boundary before this
// helper is called.
func (c *Core) ExportWorkspaceSubscribers(access models.WorkspaceAccess, search string, listIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.SubscriberExport, error), error) {
	if batchSize < 1 {
		batchSize = 1000
	}
	if listIDs == nil {
		listIDs = []int{}
	}
	if requestedIDs == nil {
		requestedIDs = []int{-1}
	}
	scope, args := workspaceSubscriberReadPredicate(access, "s", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT s.id, s.uuid, s.email, s.name, s.attribs, s.status, s.created_at, s.updated_at
		FROM subscribers s
		WHERE (%s) AND s.id > $%d
			AND s.id = ANY($%d::INT[])
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
			))
		ORDER BY s.id ASC LIMIT $%d`,
		scope,
		first,
		first+1,
		first+2, first+2, first+2,
		first+3, first+3, first+4, first+4,
		first+5)
	baseArgs := append([]any{}, args...)
	baseArgs = append(baseArgs, 0, pq.Array(requestedIDs), strings.TrimSpace(search), pq.Array(listIDs), subscriptionStatus, batchSize)
	lastID := 0
	return func() ([]models.SubscriberExport, error) {
		callArgs := append([]any{}, baseArgs...)
		callArgs[len(args)] = lastID
		var out []models.SubscriberExport
		if err := c.db.Select(&out, stmt, callArgs...); err != nil {
			return nil, workspaceQueryError("exporting subscribers", err)
		}
		if len(out) > 0 {
			lastID = out[len(out)-1].ID
		}
		return out, nil
	}, nil
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
	out, err := c.GetSubscriber(id, "", "")
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

	out, err := c.GetSubscriber(subscriberID, "", "")
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
