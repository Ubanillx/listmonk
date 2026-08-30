package core

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// workspaceManagedCampaignPredicate is the sensitive-data counterpart to the
// normal campaign read predicate.  Aggregate analytics may be inspected by an
// organization manager, but recipient reports contain e-mail addresses and
// therefore stay inside the campaign owner's mutable boundary.
func workspaceManagedCampaignPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
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
			field(""), arg(0), field(""), arg(1), field("")), []any{access.OrganizationID, access.UserID}
	}
	return fmt.Sprintf("%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL",
		field(""), field(""), arg(0), field("")), []any{access.UserID}
}

func validateWorkspaceAnalyticsDates(c *Core, fromDate, toDate string) error {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}
	return nil
}

func workspaceCampaignScopeCTE(scope string) string {
	return fmt.Sprintf("scoped_campaigns AS (SELECT id FROM campaigns scoped_campaign WHERE (%s))", scope)
}

// GetWorkspaceCampaignAnalyticsCounts applies the workspace predicate in the
// analytics SQL itself.  Handler-level ID checks remain useful for legacy list
// permissions, but a campaign moved or archived between those checks and this
// statement cannot leak an event count.
func (c *Core) GetWorkspaceCampaignAnalyticsCounts(access models.WorkspaceAccess, campIDs []int, typ, fromDate, toDate string) ([]models.CampaignAnalyticsCount, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return nil, err
	}
	if len(campIDs) == 0 {
		return []models.CampaignAnalyticsCount{}, nil
	}

	var table string
	switch typ {
	case CampaignAnalyticsViews:
		table = "campaign_views"
	case CampaignAnalyticsClicks:
		table = "link_clicks"
	case CampaignAnalyticsBounces:
		table = "bounces"
	default:
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("globals.messages.invalidData"))
	}

	scope, scopeArgs := workspaceReadPredicate(access, "scoped_campaign", 4)
	stmt := fmt.Sprintf(c.q.GetCampaignAnalyticsCounts, table)
	// Both the normal and unique-count templates begin with this CTE.  Keep the
	// original event query intact and add a scope relation that uses parameters
	// after its existing IDs/date arguments.
	stmt = strings.Replace(stmt, "WITH intval AS (",
		"WITH "+workspaceCampaignScopeCTE(scope)+", intval AS (", 1)
	stmt = strings.ReplaceAll(stmt, "campaign_id=ANY($1)",
		"campaign_id=ANY($1) AND campaign_id IN (SELECT id FROM scoped_campaigns)")

	args := []any{pq.Array(campIDs), fromDate, toDate}
	args = append(args, scopeArgs...)
	var out []models.CampaignAnalyticsCount
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching campaign analytics", err)
	}
	return out, nil
}

// GetWorkspaceCampaignAnalyticsLinks returns link aggregates constrained by
// the same campaign scope.  unique controls the individual-tracking variant
// that the prepared legacy query normally selects at application startup.
func (c *Core) GetWorkspaceCampaignAnalyticsLinks(access models.WorkspaceAccess, campIDs []int, fromDate, toDate string, unique bool) ([]models.CampaignAnalyticsLink, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return nil, err
	}
	if len(campIDs) == 0 {
		return []models.CampaignAnalyticsLink{}, nil
	}
	scope, scopeArgs := workspaceReadPredicate(access, "scoped_campaign", 4)
	countExpr := "COUNT(*)"
	if unique {
		countExpr = "COUNT(DISTINCT lc.subscriber_id)"
	}
	stmt := fmt.Sprintf(`
		WITH %s
		SELECT links.id AS link_id, %s AS count, links.url
		FROM link_clicks lc
		LEFT JOIN links ON links.id = lc.link_id
		WHERE lc.campaign_id = ANY($1::INT[])
			AND lc.created_at >= $2 AND lc.created_at <= $3
			AND lc.campaign_id IN (SELECT id FROM scoped_campaigns)
		GROUP BY links.id, links.url
		ORDER BY count DESC, links.url ASC LIMIT 50`, workspaceCampaignScopeCTE(scope), countExpr)
	args := []any{pq.Array(campIDs), fromDate, toDate}
	args = append(args, scopeArgs...)
	var out []models.CampaignAnalyticsLink
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching campaign link analytics", err)
	}
	return out, nil
}

// GetWorkspaceCampaignReportSummary returns aggregate campaign statistics
// without relying solely on a previously checked ID list.
func (c *Core) GetWorkspaceCampaignReportSummary(access models.WorkspaceAccess, campID int, fromDate, toDate string, individualTracking bool) (models.CampaignReportSummary, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return models.CampaignReportSummary{}, err
	}
	if campID < 1 {
		return models.CampaignReportSummary{}, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("globals.messages.invalidID"))
	}
	scope, scopeArgs := workspaceReadPredicate(access, "scoped_campaign", 4)
	stmt := fmt.Sprintf(`
		WITH %s,
		sent AS (
			SELECT COUNT(*) AS sent FROM campaign_recipients cr
			JOIN scoped_campaigns sc ON sc.id = cr.campaign_id
			WHERE cr.campaign_id = $1 AND cr.sent_at IS NOT NULL
				AND cr.sent_at >= $2 AND cr.sent_at <= $3
		), views AS (
			SELECT COUNT(*) AS views_total, COUNT(DISTINCT subscriber_id) AS unique_viewers
			FROM campaign_views e JOIN scoped_campaigns sc ON sc.id = e.campaign_id
			WHERE e.campaign_id = $1 AND e.created_at >= $2 AND e.created_at <= $3
		), clicks AS (
			SELECT COUNT(*) AS clicks_total, COUNT(DISTINCT subscriber_id) AS unique_clickers
			FROM link_clicks e JOIN scoped_campaigns sc ON sc.id = e.campaign_id
			WHERE e.campaign_id = $1 AND e.created_at >= $2 AND e.created_at <= $3
		), bnc AS (
			SELECT COUNT(*) AS bounced
			FROM bounces e JOIN scoped_campaigns sc ON sc.id = e.campaign_id
			WHERE e.campaign_id = $1 AND e.created_at >= $2 AND e.created_at <= $3
		)
		SELECT $1 AS campaign_id,
			COALESCE((SELECT sent FROM sent), 0) AS sent,
			COALESCE((SELECT bounced FROM bnc), 0) AS bounced,
			COALESCE((SELECT views_total FROM views), 0) AS views_total,
			COALESCE((SELECT clicks_total FROM clicks), 0) AS clicks_total,
			COALESCE((SELECT unique_viewers FROM views), 0) AS unique_viewers,
			COALESCE((SELECT unique_clickers FROM clicks), 0) AS unique_clickers`, workspaceCampaignScopeCTE(scope))
	args := []any{campID, fromDate, toDate}
	args = append(args, scopeArgs...)
	var row models.CampaignReportSummaryDB
	if err := c.db.Get(&row, stmt, args...); err != nil {
		return models.CampaignReportSummary{}, workspaceQueryError("fetching campaign report summary", err)
	}
	out := models.CampaignReportSummary{
		CampaignID:  row.CampaignID,
		Sent:        row.Sent,
		Bounced:     row.Bounced,
		ViewsTotal:  row.ViewsTotal,
		ClicksTotal: row.ClicksTotal,
	}
	if individualTracking {
		out.UniqueViewers = intPtr(row.UniqueViewers)
		out.UniqueClickers = intPtr(row.UniqueClickers)
		out.OpenRate = ratePtr(row.UniqueViewers, row.Sent)
		out.ClickRate = ratePtr(row.UniqueClickers, row.Sent)
		out.CTOR = ratePtr(row.UniqueClickers, row.UniqueViewers)
	}
	return out, nil
}

// GetWorkspaceCampaignsReportSummary is the aggregate counterpart for a set
// of campaign IDs.
func (c *Core) GetWorkspaceCampaignsReportSummary(access models.WorkspaceAccess, campIDs []int, fromDate, toDate string, individualTracking bool) (models.CampaignReportSummary, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return models.CampaignReportSummary{}, err
	}
	if len(campIDs) == 0 {
		return models.CampaignReportSummary{}, nil
	}
	scope, scopeArgs := workspaceReadPredicate(access, "scoped_campaign", 4)
	stmt := fmt.Sprintf(`
		WITH %s,
		sent AS (
			SELECT COUNT(*) AS sent FROM campaign_recipients cr
			JOIN scoped_campaigns sc ON sc.id = cr.campaign_id
			WHERE cr.campaign_id = ANY($1::INT[]) AND cr.sent_at IS NOT NULL
				AND cr.sent_at >= $2 AND cr.sent_at <= $3
		), views AS (
			SELECT COUNT(*) AS views_total, COUNT(DISTINCT subscriber_id) AS unique_viewers
			FROM campaign_views e JOIN scoped_campaigns sc ON sc.id = e.campaign_id
			WHERE e.campaign_id = ANY($1::INT[]) AND e.created_at >= $2 AND e.created_at <= $3
		), clicks AS (
			SELECT COUNT(*) AS clicks_total, COUNT(DISTINCT subscriber_id) AS unique_clickers
			FROM link_clicks e JOIN scoped_campaigns sc ON sc.id = e.campaign_id
			WHERE e.campaign_id = ANY($1::INT[]) AND e.created_at >= $2 AND e.created_at <= $3
		), bnc AS (
			SELECT COUNT(*) AS bounced
			FROM bounces e JOIN scoped_campaigns sc ON sc.id = e.campaign_id
			WHERE e.campaign_id = ANY($1::INT[]) AND e.created_at >= $2 AND e.created_at <= $3
		)
		SELECT COALESCE((SELECT sent FROM sent), 0) AS sent,
			COALESCE((SELECT bounced FROM bnc), 0) AS bounced,
			COALESCE((SELECT views_total FROM views), 0) AS views_total,
			COALESCE((SELECT clicks_total FROM clicks), 0) AS clicks_total,
			COALESCE((SELECT unique_viewers FROM views), 0) AS unique_viewers,
			COALESCE((SELECT unique_clickers FROM clicks), 0) AS unique_clickers`, workspaceCampaignScopeCTE(scope))
	args := []any{pq.Array(campIDs), fromDate, toDate}
	args = append(args, scopeArgs...)
	var row models.CampaignsReportSummaryDB
	if err := c.db.Get(&row, stmt, args...); err != nil {
		return models.CampaignReportSummary{}, workspaceQueryError("fetching campaigns report summary", err)
	}
	out := models.CampaignReportSummary{
		Sent:        row.Sent,
		Bounced:     row.Bounced,
		ViewsTotal:  row.ViewsTotal,
		ClicksTotal: row.ClicksTotal,
	}
	if individualTracking {
		out.UniqueViewers = intPtr(row.UniqueViewers)
		out.UniqueClickers = intPtr(row.UniqueClickers)
		out.OpenRate = ratePtr(row.UniqueViewers, row.Sent)
		out.ClickRate = ratePtr(row.UniqueClickers, row.Sent)
		out.CTOR = ratePtr(row.UniqueClickers, row.UniqueViewers)
	}
	return out, nil
}

func (c *Core) GetWorkspaceCampaignReportSeries(access models.WorkspaceAccess, campID int, fromDate, toDate string) (models.CampaignReportSeries, error) {
	views, err := c.GetWorkspaceCampaignAnalyticsCounts(access, []int{campID}, CampaignAnalyticsViews, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}
	clicks, err := c.GetWorkspaceCampaignAnalyticsCounts(access, []int{campID}, CampaignAnalyticsClicks, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}
	bounces, err := c.GetWorkspaceCampaignAnalyticsCounts(access, []int{campID}, CampaignAnalyticsBounces, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}
	return models.CampaignReportSeries{Views: views, Clicks: clicks, Bounces: bounces}, nil
}

func (c *Core) GetWorkspaceCampaignsReportSeries(access models.WorkspaceAccess, campIDs []int, fromDate, toDate string) (models.CampaignReportSeries, error) {
	if len(campIDs) == 0 {
		return models.CampaignReportSeries{}, nil
	}
	views, err := c.GetWorkspaceCampaignAnalyticsCounts(access, campIDs, CampaignAnalyticsViews, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}
	clicks, err := c.GetWorkspaceCampaignAnalyticsCounts(access, campIDs, CampaignAnalyticsClicks, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}
	bounces, err := c.GetWorkspaceCampaignAnalyticsCounts(access, campIDs, CampaignAnalyticsBounces, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}
	return models.CampaignReportSeries{
		Views:   aggregateCampaignAnalyticsCounts(views),
		Clicks:  aggregateCampaignAnalyticsCounts(clicks),
		Bounces: aggregateCampaignAnalyticsCounts(bounces),
	}, nil
}

func (c *Core) GetWorkspaceCampaignReportLinks(access models.WorkspaceAccess, campID int, fromDate, toDate string, individualTracking bool) ([]models.CampaignReportLinkRow, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return nil, err
	}
	if campID < 1 {
		return []models.CampaignReportLinkRow{}, nil
	}
	scope, scopeArgs := workspaceReadPredicate(access, "scoped_campaign", 4)
	uniqueExpr := "COUNT(DISTINCT lc.subscriber_id)"
	if !individualTracking {
		uniqueExpr = "0"
	}
	stmt := fmt.Sprintf(`
		WITH %s
		SELECT links.id AS link_id, links.url, COUNT(*) AS total_clicks,
			%s AS unique_clickers
		FROM link_clicks lc
		JOIN scoped_campaigns sc ON sc.id = lc.campaign_id
		LEFT JOIN links ON links.id = lc.link_id
		WHERE lc.campaign_id = $1 AND lc.created_at >= $2 AND lc.created_at <= $3
		GROUP BY links.id, links.url
		ORDER BY total_clicks DESC, links.url ASC LIMIT 50`, workspaceCampaignScopeCTE(scope), uniqueExpr)
	args := []any{campID, fromDate, toDate}
	args = append(args, scopeArgs...)
	var raw []models.CampaignReportLinkRowDB
	if err := c.db.Select(&raw, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching campaign report links", err)
	}
	summary, err := c.GetWorkspaceCampaignReportSummary(access, campID, fromDate, toDate, individualTracking)
	if err != nil {
		return nil, err
	}
	out := make([]models.CampaignReportLinkRow, 0, len(raw))
	for _, row := range raw {
		item := models.CampaignReportLinkRow{LinkID: row.LinkID, URL: row.URL, TotalClicks: row.TotalClicks}
		if individualTracking {
			item.UniqueClickers = intPtr(row.UniqueClickers)
			item.UniqueClickRate = ratePtr(row.UniqueClickers, summary.Sent)
		}
		out = append(out, item)
	}
	return out, nil
}

func (c *Core) GetWorkspaceCampaignsReportLinks(access models.WorkspaceAccess, campIDs []int, fromDate, toDate string, individualTracking bool) ([]models.CampaignsReportLinkRow, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return nil, err
	}
	if len(campIDs) == 0 {
		return []models.CampaignsReportLinkRow{}, nil
	}
	scope, scopeArgs := workspaceReadPredicate(access, "scoped_campaign", 4)
	uniqueExpr := "COUNT(DISTINCT lc.subscriber_id)"
	if !individualTracking {
		uniqueExpr = "0"
	}
	stmt := fmt.Sprintf(`
		WITH %s,
		sent AS (
			SELECT campaign_id, COUNT(*) AS sent FROM campaign_recipients
			WHERE campaign_id = ANY($1::INT[]) AND sent_at IS NOT NULL
				AND sent_at >= $2 AND sent_at <= $3 GROUP BY campaign_id
		)
		SELECT lc.campaign_id, c.name AS campaign_name, c.subject AS campaign_subject,
			links.id AS link_id, links.url, COUNT(*) AS total_clicks,
			%s AS unique_clickers, COALESCE(sent.sent, 0) AS sent
		FROM link_clicks lc
		JOIN scoped_campaigns sc ON sc.id = lc.campaign_id
		JOIN campaigns c ON c.id = lc.campaign_id
		LEFT JOIN links ON links.id = lc.link_id
		LEFT JOIN sent ON sent.campaign_id = lc.campaign_id
		WHERE lc.campaign_id = ANY($1::INT[])
			AND lc.created_at >= $2 AND lc.created_at <= $3
		GROUP BY lc.campaign_id, c.name, c.subject, links.id, links.url, sent.sent
		ORDER BY total_clicks DESC, c.name ASC, links.url ASC LIMIT 200`, workspaceCampaignScopeCTE(scope), uniqueExpr)
	args := []any{pq.Array(campIDs), fromDate, toDate}
	args = append(args, scopeArgs...)
	var raw []models.CampaignsReportLinkRowDB
	if err := c.db.Select(&raw, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching campaigns report links", err)
	}
	out := make([]models.CampaignsReportLinkRow, 0, len(raw))
	for _, row := range raw {
		item := models.CampaignsReportLinkRow{CampaignID: row.CampaignID, CampaignName: row.CampaignName, CampaignSubject: row.CampaignSubject, LinkID: row.LinkID, URL: row.URL, TotalClicks: row.TotalClicks}
		if individualTracking {
			item.UniqueClickers = intPtr(row.UniqueClickers)
			item.UniqueClickRate = ratePtr(row.UniqueClickers, row.Sent)
		}
		out = append(out, item)
	}
	return out, nil
}

// scopeReportRecipientQuery adds a statement-local campaign scope to the
// existing report recipient SQL.  The original query has ten arguments; scope
// placeholders therefore begin at $11 and cannot collide with filters,
// pagination, or the user-provided sort expression.
func scopeReportRecipientQuery(base string, access models.WorkspaceAccess, multi bool) (string, []any) {
	scope, args := workspaceManagedCampaignPredicate(access, "scoped_campaign", 11)
	stmt := strings.Replace(base, "WITH view_stats AS (",
		"WITH "+workspaceCampaignScopeCTE(scope)+", view_stats AS (", 1)
	if multi {
		stmt = strings.Replace(stmt, "WHERE cr.campaign_id = ANY($1)",
			"WHERE cr.campaign_id = ANY($1) AND cr.campaign_id IN (SELECT id FROM scoped_campaigns)", 1)
	} else {
		stmt = strings.Replace(stmt, "WHERE cr.campaign_id = $1",
			"WHERE cr.campaign_id = $1 AND cr.campaign_id IN (SELECT id FROM scoped_campaigns)", 1)
	}
	return stmt, args
}

func (c *Core) QueryWorkspaceCampaignReportRecipients(access models.WorkspaceAccess, campID int, fromDate, toDate string, filters models.CampaignReportRecipientFilters, offset, limit int) ([]models.CampaignReportRecipientRow, int, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return nil, 0, err
	}
	orderExpr := makeCampaignReportRecipientOrder(filters.SortBy, filters.Order)
	stmt, scopeArgs := scopeReportRecipientQuery(strings.ReplaceAll(c.q.QueryCampaignReportRecipients, "%order%", orderExpr), access, false)
	search := strings.TrimSpace(filters.Search)
	if search != "" {
		search = "%" + search + "%"
	}
	args := []any{campID, fromDate, toDate, search, normalizeReportTriState(filters.Opened), normalizeReportTriState(filters.Clicked), normalizeReportTriState(filters.Bounced), filters.LinkID, offset, limit}
	args = append(args, scopeArgs...)
	var out []models.CampaignReportRecipientRow
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching campaign report recipients", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

func (c *Core) QueryWorkspaceCampaignsReportRecipients(access models.WorkspaceAccess, campIDs []int, fromDate, toDate string, filters models.CampaignReportRecipientFilters, offset, limit int) ([]models.CampaignsReportRecipientRow, int, error) {
	if err := validateWorkspaceAnalyticsDates(c, fromDate, toDate); err != nil {
		return nil, 0, err
	}
	if len(campIDs) == 0 {
		return []models.CampaignsReportRecipientRow{}, 0, nil
	}
	orderExpr := makeCampaignsReportRecipientOrder(filters.SortBy, filters.Order)
	stmt, scopeArgs := scopeReportRecipientQuery(strings.ReplaceAll(c.q.QueryCampaignsReportRecipients, "%order%", orderExpr), access, true)
	search := strings.TrimSpace(filters.Search)
	if search != "" {
		search = "%" + search + "%"
	}
	args := []any{pq.Array(campIDs), fromDate, toDate, search, normalizeReportTriState(filters.Opened), normalizeReportTriState(filters.Clicked), normalizeReportTriState(filters.Bounced), filters.LinkID, offset, limit}
	args = append(args, scopeArgs...)
	var out []models.CampaignsReportRecipientRow
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching campaigns report recipients", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}
