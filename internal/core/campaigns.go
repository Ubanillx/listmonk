package core

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

const (
	CampaignAnalyticsViews   = "views"
	CampaignAnalyticsClicks  = "clicks"
	CampaignAnalyticsBounces = "bounces"

	campaignTplDefault = "default"
	campaignTplArchive = "archive"
)

var campaignReportRecipientSortFields = map[string]string{
	"last_engaged_at": "last_engaged_at",
	"email":           "email",
	"view_count":      "view_count",
	"click_count":     "click_count",
	"sent_at":         "sent_at",
}

var campaignsReportRecipientSortFields = map[string]string{
	"campaign_subject": "campaign_subject",
	"email":            "email",
	"view_count":       "view_count",
	"click_count":      "click_count",
	"bounce_count":     "bounce_count",
	"last_engaged_at":  "last_engaged_at",
	"sent_at":          "sent_at",
}

// QueryCampaigns retrieves paginated campaigns optionally filtering them by the given arbitrary
// query expression. It also returns the total number of records in the DB.
func (c *Core) QueryCampaigns(searchStr string, statuses, tags []string, orderBy, order string, getAll bool, permittedLists []int, offset, limit int) (models.Campaigns, int, error) {
	queryStr, stmt := makeSearchQuery(searchStr, orderBy, order, c.q.QueryCampaigns, campQuerySortFields)

	if statuses == nil {
		statuses = []string{}
	}

	if tags == nil {
		tags = []string{}
	}

	// Unsafe to ignore scanning fields not present in models.Campaigns.
	var out models.Campaigns
	if err := c.db.Select(&out, stmt, 0, pq.StringArray(statuses), pq.StringArray(tags), queryStr, getAll, pq.Array(permittedLists), offset, limit); err != nil {
		c.log.Printf("error fetching campaigns: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	for i := range out {
		// Replace null tags.
		if out[i].Tags == nil {
			out[i].Tags = []string{}
		}
	}

	// Lazy load stats.
	if err := out.LoadStats(c.q.GetCampaignStats); err != nil {
		c.log.Printf("error fetching campaign stats: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaigns}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}

	return out, total, nil
}

// GetCampaign retrieves a campaign.
func (c *Core) GetCampaign(id int, uuid, archiveSlug string) (models.Campaign, error) {
	return c.getCampaign(id, uuid, archiveSlug, campaignTplDefault)
}

// GetArchivedCampaign retrieves a campaign with the archive template body.
func (c *Core) GetArchivedCampaign(id int, uuid, archiveSlug string) (models.Campaign, error) {
	out, err := c.getCampaign(id, uuid, archiveSlug, campaignTplArchive)
	if err != nil {
		return out, err
	}

	if !out.Archive {
		return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}

	return out, nil
}

// getCampaign retrieves a campaign. If typlType=default, then the campaign's
// template body is returned as "template_body". If tplType="archive",
// the archive template is returned.
func (c *Core) getCampaign(id int, uuid, archiveSlug string, tplType string) (models.Campaign, error) {
	// Unsafe to ignore scanning fields not present in models.Campaigns.
	var uu any
	if uuid != "" {
		uu = uuid
	}

	var out models.Campaigns
	if err := c.q.GetCampaign.Select(&out, id, uu, archiveSlug, tplType); err != nil {
		// if err := c.db.Select(&out, stmt, 0, pq.Array([]string{}), queryStr, 0, 1); err != nil {
		c.log.Printf("error fetching campaign: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	if len(out) == 0 {
		return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}

	for i := 0; i < len(out); i++ {
		// Replace null tags.
		if out[i].Tags == nil {
			out[i].Tags = []string{}
		}
	}

	// Lazy load stats.
	if err := out.LoadStats(c.q.GetCampaignStats); err != nil {
		c.log.Printf("error fetching campaign stats: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return out[0], nil
}

// GetCampaignForPreview retrieves a campaign with a template body. If the optional tplID is > 0
// that particular template is used, otherwise, the template saved on the campaign is.
func (c *Core) GetCampaignForPreview(id, tplID int) (models.Campaign, error) {
	var out models.Campaign
	if err := c.q.GetCampaignForPreview.Get(&out, id, tplID); err != nil {
		if err == sql.ErrNoRows {
			return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
				c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
		}

		c.log.Printf("error fetching campaign: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetArchivedCampaigns retrieves campaigns with a template body.
func (c *Core) GetArchivedCampaigns(offset, limit int) (models.Campaigns, int, error) {
	var out models.Campaigns
	if err := c.q.GetArchivedCampaigns.Select(&out, offset, limit, campaignTplArchive); err != nil {
		c.log.Printf("error fetching public campaigns: %v", err)
		return models.Campaigns{}, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}

	return out, total, nil
}

// CreateCampaign creates a new campaign.
func (c *Core) CreateCampaign(o models.Campaign, listIDs []int, mediaIDs []int) (models.Campaign, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}

	// Insert and read ID.
	var newID int
	if err := c.q.CreateCampaign.Get(&newID,
		uu,
		o.Type,
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
		o.AutoTrackLinks,
	); err != nil {
		if err == sql.ErrNoRows {
			return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("campaigns.noSubs"))
		}

		c.log.Printf("error creating campaign: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	out, err := c.GetCampaign(newID, "", "")
	if err != nil {
		return models.Campaign{}, err
	}

	return out, nil
}

// UpdateCampaign updates a campaign.
func (c *Core) UpdateCampaign(id int, o models.Campaign, listIDs []int, mediaIDs []int) (models.Campaign, error) {
	_, err := c.q.UpdateCampaign.Exec(id,
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
		c.log.Printf("error updating campaign: %v", err)
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	out, err := c.GetCampaign(id, "", "")
	if err != nil {
		return models.Campaign{}, err
	}

	return out, nil
}

// UpdateCampaignStatus updates a campaign's status, eg: draft to running.
func (c *Core) UpdateCampaignStatus(id int, status string) (models.Campaign, error) {
	cm, err := c.GetCampaign(id, "", "")
	if err != nil {
		return models.Campaign{}, err
	}

	errMsg := ""
	switch status {
	case models.CampaignStatusDraft:
		if cm.Status != models.CampaignStatusScheduled {
			errMsg = c.i18n.T("campaigns.onlyScheduledAsDraft")
		}
	case models.CampaignStatusScheduled:
		if cm.Status != models.CampaignStatusDraft && cm.Status != models.CampaignStatusPaused && cm.Status != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyDraftAsScheduled")
		}
		if !cm.SendAt.Valid {
			errMsg = c.i18n.T("campaigns.needsSendAt")
		}

	case models.CampaignStatusRunning:
		if cm.Status != models.CampaignStatusPaused && cm.Status != models.CampaignStatusDraft && cm.Status != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyPausedDraft")
		}
	case models.CampaignStatusPaused:
		if cm.Status != models.CampaignStatusRunning && cm.Status != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyActivePause")
		}
	case models.CampaignStatusCancelled:
		if cm.Status != models.CampaignStatusRunning && cm.Status != models.CampaignStatusPaused && cm.Status != models.CampaignStatusDeferred {
			errMsg = c.i18n.T("campaigns.onlyActiveCancel")
		}
	}

	if len(errMsg) > 0 {
		return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest, errMsg)
	}

	res, err := c.q.UpdateCampaignStatus.Exec(cm.ID, status)
	if err != nil {
		c.log.Printf("error updating campaign status: %v", err)

		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return models.Campaign{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	cm.Status = status
	return cm, nil
}

func (c *Core) HasCampaignRecipients(id int) (bool, error) {
	var has bool
	if err := c.q.HasCampaignRecipients.Get(&has, id); err != nil {
		c.log.Printf("error fetching campaign recipients: %v", err)
		return false, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return has, nil
}

func (c *Core) GetCampaignListIDs(id int) ([]int, error) {
	out := []int{}
	if err := c.q.GetCampaignListIDs.Select(&out, id); err != nil {
		c.log.Printf("error fetching campaign lists: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return out, nil
}

func (c *Core) UpdateCampaignRecipientStatuses(id int, toStatus string, fromStatuses []string) error {
	if _, err := c.q.UpdateCampaignRecipientStatuses.Exec(id, toStatus, pq.Array(fromStatuses)); err != nil {
		c.log.Printf("error updating campaign recipients: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return nil
}

// UpdateCampaignArchive updates a campaign's archive properties.
func (c *Core) UpdateCampaignArchive(id int, enabled bool, tplID int, meta models.JSON, archiveSlug string) error {
	if _, err := c.q.UpdateCampaignArchive.Exec(id, enabled, archiveSlug, tplID, meta); err != nil {
		c.log.Printf("error updating campaign: %v", err)

		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return nil
}

// DeleteCampaign deletes a campaign.
func (c *Core) DeleteCampaign(id int) error {
	res, err := c.q.DeleteCampaign.Exec(id)
	if err != nil {
		c.log.Printf("error deleting campaign: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))

	}

	if n, _ := res.RowsAffected(); n == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}

	return nil
}

// DeleteCampaigns deletes multiple campaigns by IDs or by query.
func (c *Core) DeleteCampaigns(ids []int, query string, hasAllPerm bool, permittedLists []int) error {
	var queryStr string

	if len(ids) > 0 {
		queryStr = ""
	} else {
		queryStr = makeSearchString(query)
	}

	if _, err := c.q.DeleteCampaigns.Exec(pq.Array(ids), queryStr, hasAllPerm, pq.Array(permittedLists)); err != nil {
		c.log.Printf("error deleting campaigns: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.campaigns}", "error", pqErrMsg(err)))
	}

	return nil
}

// CampaignHasLists checks if a campaign has any of the given list IDs.
func (c *Core) CampaignHasLists(id int, listIDs []int) (bool, error) {
	has := false
	if err := c.q.CampaignHasLists.Get(&has, id, pq.Array(listIDs)); err != nil {
		c.log.Printf("error checking campaign lists: %v", err)
		return false, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}

	return has, nil
}

// GetRunningCampaignStats returns the progress stats of running campaigns.
func (c *Core) GetRunningCampaignStats() ([]models.CampaignStats, error) {
	out := []models.CampaignStats{}
	if err := c.q.GetCampaignStatus.Select(&out, models.CampaignStatusRunning); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		c.log.Printf("error fetching campaign stats: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	} else if len(out) == 0 {
		return nil, nil
	}

	return out, nil
}

func (c *Core) GetCampaignAnalyticsCounts(campIDs []int, typ, fromDate, toDate string) ([]models.CampaignAnalyticsCount, error) {
	// Pick campaign view counts or click counts.
	var stmt *sqlx.Stmt
	switch typ {
	case "views":
		stmt = c.q.GetCampaignViewCounts
	case "clicks":
		stmt = c.q.GetCampaignClickCounts
	case "bounces":
		stmt = c.q.GetCampaignBounceCounts
	default:
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("globals.messages.invalidData"))
	}

	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	out := []models.CampaignAnalyticsCount{}
	if err := stmt.Select(&out, pq.Array(campIDs), fromDate, toDate); err != nil {
		c.log.Printf("error fetching campaign %s: %v", typ, err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetCampaignAnalyticsLinks returns link click analytics for the given campaign IDs.
func (c *Core) GetCampaignAnalyticsLinks(campIDs []int, typ, fromDate, toDate string) ([]models.CampaignAnalyticsLink, error) {
	out := []models.CampaignAnalyticsLink{}
	if err := c.q.GetCampaignLinkCounts.Select(&out, pq.Array(campIDs), fromDate, toDate); err != nil {
		c.log.Printf("error fetching campaign %s: %v", typ, err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
	}

	return out, nil
}

func (c *Core) GetCampaignReportSummary(campID int, fromDate, toDate string, individualTracking bool) (models.CampaignReportSummary, error) {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return models.CampaignReportSummary{}, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	var row models.CampaignReportSummaryDB
	if err := c.q.GetCampaignReportSummary.Get(&row, campID, fromDate, toDate); err != nil {
		c.log.Printf("error fetching campaign report summary: %v", err)
		return models.CampaignReportSummary{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
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

func (c *Core) GetCampaignsReportSummary(campIDs []int, fromDate, toDate string, individualTracking bool) (models.CampaignReportSummary, error) {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return models.CampaignReportSummary{}, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	if len(campIDs) == 0 {
		return models.CampaignReportSummary{}, nil
	}

	var row models.CampaignsReportSummaryDB
	if err := c.q.GetCampaignsReportSummary.Get(&row, pq.Array(campIDs), fromDate, toDate); err != nil {
		c.log.Printf("error fetching campaigns report summary: %v", err)
		return models.CampaignReportSummary{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
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

func (c *Core) GetCampaignReportSeries(campID int, fromDate, toDate string) (models.CampaignReportSeries, error) {
	views, err := c.GetCampaignAnalyticsCounts([]int{campID}, CampaignAnalyticsViews, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}

	clicks, err := c.GetCampaignAnalyticsCounts([]int{campID}, CampaignAnalyticsClicks, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}

	bounces, err := c.GetCampaignAnalyticsCounts([]int{campID}, CampaignAnalyticsBounces, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}

	return models.CampaignReportSeries{
		Views:   views,
		Clicks:  clicks,
		Bounces: bounces,
	}, nil
}

func (c *Core) GetCampaignsReportSeries(campIDs []int, fromDate, toDate string) (models.CampaignReportSeries, error) {
	if len(campIDs) == 0 {
		return models.CampaignReportSeries{}, nil
	}

	views, err := c.GetCampaignAnalyticsCounts(campIDs, CampaignAnalyticsViews, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}

	clicks, err := c.GetCampaignAnalyticsCounts(campIDs, CampaignAnalyticsClicks, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}

	bounces, err := c.GetCampaignAnalyticsCounts(campIDs, CampaignAnalyticsBounces, fromDate, toDate)
	if err != nil {
		return models.CampaignReportSeries{}, err
	}

	return models.CampaignReportSeries{
		Views:   aggregateCampaignAnalyticsCounts(views),
		Clicks:  aggregateCampaignAnalyticsCounts(clicks),
		Bounces: aggregateCampaignAnalyticsCounts(bounces),
	}, nil
}

func (c *Core) GetCampaignReportLinks(campID int, fromDate, toDate string, individualTracking bool) ([]models.CampaignReportLinkRow, error) {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	var raw []models.CampaignReportLinkRowDB
	if err := c.q.GetCampaignReportLinks.Select(&raw, campID, fromDate, toDate); err != nil {
		c.log.Printf("error fetching campaign report links: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
	}

	summary, err := c.GetCampaignReportSummary(campID, fromDate, toDate, individualTracking)
	if err != nil {
		return nil, err
	}

	out := make([]models.CampaignReportLinkRow, 0, len(raw))
	for _, row := range raw {
		item := models.CampaignReportLinkRow{
			LinkID:      row.LinkID,
			URL:         row.URL,
			TotalClicks: row.TotalClicks,
		}

		if individualTracking {
			item.UniqueClickers = intPtr(row.UniqueClickers)
			item.UniqueClickRate = ratePtr(row.UniqueClickers, summary.Sent)
		}

		out = append(out, item)
	}

	return out, nil
}

func (c *Core) GetCampaignsReportLinks(campIDs []int, fromDate, toDate string, individualTracking bool) ([]models.CampaignsReportLinkRow, error) {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	if len(campIDs) == 0 {
		return []models.CampaignsReportLinkRow{}, nil
	}

	var raw []models.CampaignsReportLinkRowDB
	if err := c.q.GetCampaignsReportLinks.Select(&raw, pq.Array(campIDs), fromDate, toDate); err != nil {
		c.log.Printf("error fetching campaigns report links: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
	}

	out := make([]models.CampaignsReportLinkRow, 0, len(raw))
	for _, row := range raw {
		item := models.CampaignsReportLinkRow{
			CampaignID:      row.CampaignID,
			CampaignName:    row.CampaignName,
			CampaignSubject: row.CampaignSubject,
			LinkID:          row.LinkID,
			URL:             row.URL,
			TotalClicks:     row.TotalClicks,
		}

		if individualTracking {
			item.UniqueClickers = intPtr(row.UniqueClickers)
			item.UniqueClickRate = ratePtr(row.UniqueClickers, row.Sent)
		}

		out = append(out, item)
	}

	return out, nil
}

func (c *Core) QueryCampaignReportRecipients(campID int, fromDate, toDate string, filters models.CampaignReportRecipientFilters, offset, limit int) ([]models.CampaignReportRecipientRow, int, error) {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return nil, 0, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	orderExpr := makeCampaignReportRecipientOrder(filters.SortBy, filters.Order)
	stmt := strings.ReplaceAll(c.q.QueryCampaignReportRecipients, "%order%", orderExpr)

	search := strings.TrimSpace(filters.Search)
	if search != "" {
		search = "%" + search + "%"
	}

	opened := normalizeReportTriState(filters.Opened)
	clicked := normalizeReportTriState(filters.Clicked)
	bounced := normalizeReportTriState(filters.Bounced)

	var out []models.CampaignReportRecipientRow
	if err := c.db.Select(&out, stmt, campID, fromDate, toDate, search, opened, clicked, bounced, filters.LinkID, offset, limit); err != nil {
		c.log.Printf("error fetching campaign report recipients: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}

	return out, total, nil
}

func (c *Core) QueryCampaignsReportRecipients(campIDs []int, fromDate, toDate string, filters models.CampaignReportRecipientFilters, offset, limit int) ([]models.CampaignsReportRecipientRow, int, error) {
	if !strHasLen(fromDate, 10, 30) || !strHasLen(toDate, 10, 30) {
		return nil, 0, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("analytics.invalidDates"))
	}

	if len(campIDs) == 0 {
		return []models.CampaignsReportRecipientRow{}, 0, nil
	}

	orderExpr := makeCampaignsReportRecipientOrder(filters.SortBy, filters.Order)
	stmt := strings.ReplaceAll(c.q.QueryCampaignsReportRecipients, "%order%", orderExpr)

	search := strings.TrimSpace(filters.Search)
	if search != "" {
		search = "%" + search + "%"
	}

	opened := normalizeReportTriState(filters.Opened)
	clicked := normalizeReportTriState(filters.Clicked)
	bounced := normalizeReportTriState(filters.Bounced)

	var out []models.CampaignsReportRecipientRow
	if err := c.db.Select(&out, stmt, pq.Array(campIDs), fromDate, toDate, search, opened, clicked, bounced, filters.LinkID, offset, limit); err != nil {
		c.log.Printf("error fetching campaigns report recipients: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.analytics}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}

	return out, total, nil
}

func normalizeReportTriState(v string) string {
	switch v {
	case "yes", "no":
		return v
	default:
		return "all"
	}
}

func makeCampaignReportRecipientOrder(sortBy, order string) string {
	field, ok := campaignReportRecipientSortFields[sortBy]
	if !ok {
		field = campaignReportRecipientSortFields["last_engaged_at"]
	}

	if order != SortAsc && order != SortDesc {
		order = SortDesc
	}

	switch field {
	case "last_engaged_at", "sent_at":
		return field + " " + order + " NULLS LAST"
	default:
		return field + " " + order
	}
}

func makeCampaignsReportRecipientOrder(sortBy, order string) string {
	field, ok := campaignsReportRecipientSortFields[sortBy]
	if !ok {
		field = campaignsReportRecipientSortFields["last_engaged_at"]
	}

	if order != SortAsc && order != SortDesc {
		order = SortDesc
	}

	switch field {
	case "last_engaged_at", "sent_at":
		return field + " " + order + " NULLS LAST"
	default:
		return field + " " + order
	}
}

func aggregateCampaignAnalyticsCounts(in []models.CampaignAnalyticsCount) []models.CampaignAnalyticsCount {
	if len(in) == 0 {
		return []models.CampaignAnalyticsCount{}
	}

	bucket := make(map[time.Time]int)
	for _, row := range in {
		bucket[row.Timestamp] += row.Count
	}

	keys := make([]time.Time, 0, len(bucket))
	for stamp := range bucket {
		keys = append(keys, stamp)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Before(keys[j])
	})

	out := make([]models.CampaignAnalyticsCount, 0, len(keys))
	for _, stamp := range keys {
		out = append(out, models.CampaignAnalyticsCount{
			Count:     bucket[stamp],
			Timestamp: stamp,
		})
	}

	return out
}

func intPtr(v int) *int {
	out := v
	return &out
}

func ratePtr(num, den int) *float64 {
	if den <= 0 {
		return nil
	}

	out := (float64(num) / float64(den)) * 100
	return &out
}

// RegisterCampaignView registers a subscriber's view on a campaign.
func (c *Core) RegisterCampaignView(campUUID, subUUID string) error {
	if _, err := c.q.RegisterCampaignView.Exec(campUUID, subUUID); err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Column == "campaign_id" {
			return nil
		}

		c.log.Printf("error registering campaign view: %s", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.campaign}", "error", pqErrMsg(err)))
	}
	return nil
}

// GetLinkURL returns the original URL for a link UUID without recording a click.
func (c *Core) GetLinkURL(linkUUID string) (string, error) {
	var url string
	if err := c.q.GetLinkURL.Get(&url, linkUUID); err != nil {
		c.log.Printf("error getting link URL: %s", err)
		return "", echo.NewHTTPError(http.StatusInternalServerError, c.i18n.Ts("public.errorProcessingRequest"))
	}
	return url, nil
}

// RegisterCampaignLinkClick registers a subscriber's link click on a campaign.
func (c *Core) RegisterCampaignLinkClick(linkUUID, campUUID, subUUID string) (string, error) {
	var url string
	if err := c.q.RegisterLinkClick.Get(&url, linkUUID, campUUID, subUUID); err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Column == "link_id" {
			return "", echo.NewHTTPError(http.StatusBadRequest, c.i18n.Ts("public.invalidLink"))
		}

		c.log.Printf("error registering link click: %s", err)
		return "", echo.NewHTTPError(http.StatusInternalServerError, c.i18n.Ts("public.errorProcessingRequest"))
	}

	return url, nil
}

// DeleteCampaignViews deletes campaign views older than a given date.
func (c *Core) DeleteCampaignViews(before time.Time) error {
	if _, err := c.q.DeleteCampaignViews.Exec(before); err != nil {
		c.log.Printf("error deleting campaign views: %s", err)
		return echo.NewHTTPError(http.StatusInternalServerError, c.i18n.Ts("public.errorProcessingRequest"))
	}

	return nil
}

// DeleteCampaignLinkClicks deletes campaign views older than a given date.
func (c *Core) DeleteCampaignLinkClicks(before time.Time) error {
	if _, err := c.q.DeleteCampaignLinkClicks.Exec(before); err != nil {
		c.log.Printf("error deleting campaign link clicks: %s", err)
		return echo.NewHTTPError(http.StatusInternalServerError, c.i18n.Ts("public.errorProcessingRequest"))
	}

	return nil
}
