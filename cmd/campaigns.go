package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gopkg.in/volatiletech/null.v6"
)

// campReq is a wrapper over the Campaign model for receiving
// campaign creation and update data from APIs.
type campReq struct {
	models.Campaign

	// Visibility is intentionally kept outside the embedded Campaign so a
	// partial update can distinguish an omitted value from the saved scope.
	Visibility string `json:"visibility"`

	// This overrides Campaign.CustomerLists to receive and
	// write a customer_list of int IDs during creation and updation.
	// Campaign.CustomerLists is JSONText for sending customer_lists children
	// to the outside world.
	CustomerListIDs []int `json:"customer_list_ids"`

	MediaIDs []int `json:"media"`

	// This is only relevant to campaign test requests.
	CustomerEmails pq.StringArray `json:"customers"`
}

type campaignPoolAudience struct {
	PoolID    int
	SegmentID *int64
}

type campaignCloneReq struct {
	// Nil means the active workspace. Zero explicitly selects personal space.
	TargetOrganizationID *int   `json:"target_organization_id"`
	Name                 string `json:"name"`
}

// campContentReq wraps params coming from API requests for converting
// campaign content formats.
type campContentReq struct {
	models.Campaign
	From string `json:"from"`
	To   string `json:"to"`
}

var (
	reFromAddress = regexp.MustCompile(`((.+?)\s)?<(.+?)@(.+?)>`)
	reSlug        = regexp.MustCompile(`[^\p{L}\p{M}\p{N}]`)
)

// GetCampaigns handles retrieval of campaigns.
func (a *App) GetCampaigns(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var (
		pg = a.pg.NewFromURL(c.Request().URL.Query())

		status    = c.QueryParams()["status"]
		tags      = c.QueryParams()["tag"]
		query     = strings.TrimSpace(c.FormValue("query"))
		orderBy   = c.FormValue("order_by")
		order     = c.FormValue("order")
		noBody, _ = strconv.ParseBool(c.QueryParam("no_body"))
	)

	// Query and retrieve campaigns from the active workspace only, then apply
	// any legacy campaign/customer_list grants before pagination is exposed.
	res, total, err := a.queryReadableWorkspaceCampaigns(c, access, query, status, tags, orderBy, order, pg.Offset, pg.Limit)
	if err != nil {
		return err
	}

	// Remove the body from the response if requested.
	for i := range res {
		if noBody {
			res[i].Body = ""
			res[i].BodySource.Valid = false
		}
		a.redactCampaignSensitiveFields(access, &res[i])
	}

	// Paginate the response.
	if len(res) == 0 {
		return c.JSON(http.StatusOK, okResp{models.PageResults{Results: []models.Campaign{}}})
	}

	out := models.PageResults{
		Query:   query,
		Results: res,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetCampaign handles retrieval of campaigns.
func (a *App) GetCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireReadableWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	// Get the campaign from the DB.
	out, err := a.core.GetWorkspaceCampaign(access, id)
	if err != nil {
		return err
	}

	// Blank out the body if requested.
	noBody, _ := strconv.ParseBool(c.QueryParam("no_body"))
	if noBody {
		out.Body = ""
	}
	a.redactCampaignSensitiveFields(access, &out)

	return c.JSON(http.StatusOK, okResp{out})
}

// redactCampaignSensitiveFields keeps public and manager read-only views
// useful for inspection and cloning without exposing a member's sender
// identity, arbitrary delivery headers, or audience customer_list names.
func (a *App) redactCampaignSensitiveFields(access models.WorkspaceAccess, campaign *models.Campaign) {
	if a.core.CanSeeSensitiveResource(access, campaign.ResourceScope) {
		return
	}
	campaign.FromEmail = ""
	campaign.Headers = nil
	// Reply mailboxes are company-internal routing addresses, not customer
	// contact data. Keep them visible so operators can verify the effective
	// pool/segment return path; customer addresses remain protected by the pool
	// contact DTO and snapshot boundaries.
	campaign.CustomerLists = []byte("[]")
}

// PreviewCampaign renders the HTML preview of a campaign body.
func (a *App) PreviewCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireReadableWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	var (
		isPost      = c.Request().Method == http.MethodPost
		contentType = c.FormValue("content_type")
		tplID, _    = strconv.Atoi(c.FormValue("template_id"))
	)
	// For visual content, template ID for previewing is irrelevant.
	if contentType == models.CampaignContentTypeVisual || tplID < 1 {
		tplID = 0
	} else if _, err := a.requireReadableWorkspaceResource(c, access, resourceTemplates, tplID, auth.PermTemplatesGet); err != nil {
		return err
	}

	// Get the campaign from the DB for previewing with the `template_body` field.
	camp, err := a.core.GetWorkspaceCampaignForPreview(access, id, tplID)
	if err != nil {
		return err
	}
	if err := a.core.ValidatePoolCampaignAudience(id); err != nil {
		return err
	}

	// There's a body in the request to preview instead of the body in the DB.
	if isPost {
		camp.ContentType = contentType
		camp.Body = c.FormValue("body")
		if v := c.FormValue("auto_track_links"); v != "" {
			camp.AutoTrackLinks, _ = strconv.ParseBool(v)
		}

		// For visual campaigns, template body from the DB shouldn't be used.
		if contentType == models.CampaignContentTypeVisual {
			camp.TemplateBody = ""
		}
	}

	// Use a dummy campaign ID to prevent views and clicks from {{ TrackView }}
	// and {{ TrackLink }} being registered on preview.
	camp.UUID = dummyCustomer.UUID
	if err := camp.CompileTemplate(a.manager.TemplateFuncs(&camp)); err != nil {
		a.log.Printf("error compiling template: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("templates.errorCompiling", "error", err.Error()))
	}

	// Render the message body.
	msg, err := a.manager.NewCampaignMessage(&camp, dummyCustomer)
	if err != nil {
		a.log.Printf("error rendering message: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("templates.errorRendering", "error", err.Error()))
	}

	// Plaintext headers for plain body.
	if camp.ContentType == models.CampaignContentTypePlain {
		return c.String(http.StatusOK, string(msg.Body()))
	}

	return c.HTML(http.StatusOK, string(msg.Body()))
}

// PreviewCampaignArchive renders the public campaign archives page.
func (a *App) PreviewCampaignArchive(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireReadableWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	// Fetch the campaign body from the DB.
	tplID, _ := strconv.Atoi(c.FormValue("template_id"))
	if tplID > 0 {
		if _, err := a.requireReadableWorkspaceResource(c, access, resourceTemplates, tplID, auth.PermTemplatesGet); err != nil {
			return err
		}
	}
	camp, err := a.core.GetWorkspaceCampaignForPreview(access, id, tplID)
	if err != nil {
		return err
	}

	camp.ArchiveMeta = json.RawMessage([]byte(c.FormValue("archive_meta")))

	// "Compile" the campaign template with appropriate data.
	res, err := a.compileArchiveCampaigns([]models.Campaign{camp})
	if err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingCampaign")))
	}

	// Render the campaign body.
	out := res[0].Campaign
	msg, err := a.manager.NewCampaignMessage(out, res[0].Customer)
	if err != nil {
		a.log.Printf("error rendering campaign: %v", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingCampaign")))
	}

	return c.HTML(http.StatusOK, string(msg.Body()))
}

// CampaignContent handles campaign content (body) format conversions.
func (a *App) CampaignContent(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if _, err := a.requireManagedWorkspaceCampaign(c, access, getID(c)); err != nil {
		return err
	}
	var camp campContentReq
	if err := c.Bind(&camp); err != nil {
		return err
	}

	// Convert formats, eg: markdown to HTML.
	out, err := camp.ConvertContent(camp.From, camp.To)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// CreateCampaign handles campaign creation.
// Newly created campaigns are always drafts.
func (a *App) CreateCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsManageAll, auth.PermCampaignsManage); err != nil {
		return err
	}
	var o campReq
	if err := c.Bind(&o); err != nil {
		return err
	}

	regularListIDs, poolAudiences, err := a.splitCampaignAudienceIDs(access, o.CustomerListIDs)
	if err != nil {
		return err
	}
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, regularListIDs, true); err != nil {
		return err
	}
	if err := a.requireUsableCampaignResources(c, access, o); err != nil {
		return err
	}
	if err := a.validateCampaignReplyMailbox(access, &o.Campaign); err != nil {
		return err
	}
	visibility, err := normalizeResourceVisibility(access, resourceCampaigns, o.Visibility)
	if err != nil {
		return err
	}

	// If the campaign's 'opt-in', prepare a default message.
	switch o.Type {
	case models.CampaignTypeOptin:
		op, err := a.makeOptinCampaignMessage(access, o)
		if err != nil {
			return err
		}
		o = op
	case "":
		o.Type = models.CampaignTypeRegular
	}

	if o.Messenger == "" {
		o.Messenger = "email"
	}

	// Validate.
	if c, err := a.validateCampaignFields(o); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	} else {
		o = c
	}

	if o.ArchiveTemplateID.Valid && o.ArchiveTemplateID.Int != 0 {
		o.ArchiveTemplateID = o.TemplateID
	}

	out, err := a.core.CreateCampaignInWorkspace(access, o.Campaign, regularListIDs, o.MediaIDs, core.ApplyWorkspaceScope(access, visibility))
	if err != nil {
		return err
	}
	if err := a.persistCampaignReplyMailbox(out.ID, access.UserID, o.ReplyMailboxID); err != nil {
		return err
	}
	out.ReplyMailboxID = o.ReplyMailboxID
	for _, audience := range poolAudiences {
		if err := a.core.AttachPoolToCampaign(out.ID, audience.PoolID, audience.SegmentID, int64(access.OrganizationID)); err != nil {
			return err
		}
	}
	if len(poolAudiences) > 0 {
		out, err = a.core.GetWorkspaceCampaign(access, out.ID)
		if err != nil {
			return err
		}
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// CloneCampaign performs a server-side snapshot copy. The frontend only
// selects a target; it never supplies customer_lists, sender settings, or media rows.
func (a *App) CloneCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	sourceID := getID(c)
	scope, err := a.requireReadableWorkspaceCampaign(c, access, sourceID)
	if err != nil {
		return err
	}
	if !canCopyWorkspaceCampaign(access, scope) {
		return echo.NewHTTPError(http.StatusForbidden, "campaign is not copyable in the active workspace")
	}

	var req campaignCloneReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	target := access
	if req.TargetOrganizationID != nil {
		target, err = a.workspaceAccessForOrganization(c, *req.TargetOrganizationID)
		if err != nil {
			return err
		}
	}
	if err := requireWritableWorkspace(target); err != nil {
		return err
	}
	if !canCopyWorkspaceCampaign(access, scope) && !workspaceCopyException(access, scope) {
		if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsManageAll, auth.PermCampaignsManage); err != nil {
			return err
		}
	}
	if req.Name != "" && !strHasLen(strings.TrimSpace(req.Name), 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.fieldInvalidName"))
	}
	out, err := a.core.CloneCampaignForWorkspaceWithSource(sourceID, access, target, req.Name)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, okResp{out})
}

// UpdateCampaign handles campaign modification.
// Campaigns that are done cannot be modified.
func (a *App) UpdateCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireManagedWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	// Retrieve the campaign from the DB.
	cm, err := a.core.GetWorkspaceCampaign(access, id)
	if err != nil {
		return err
	}

	if !canEditCampaign(cm.Status) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.cantUpdate"))
	}

	// Clear attribs to avoid merging old and new values as json.Unmarshal in JSON.scan() merges maps,
	// merging values already in the DB and incoming values. If this is nil, then DB values remain
	// unchanged.
	cm.Attribs = nil

	// Read the incoming params into the existing campaign fields from the DB.
	// This allows updating of values that have been sent whereas fields
	// that are not in the request retain the old values.
	o := campReq{Campaign: cm}
	if err := c.Bind(&o); err != nil {
		return err
	}

	regularListIDs, poolAudiences, err := a.splitCampaignAudienceIDs(access, o.CustomerListIDs)
	if err != nil {
		return err
	}
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, regularListIDs, true); err != nil {
		return err
	}
	if err := a.requireUsableCampaignResources(c, access, o); err != nil {
		return err
	}
	if err := a.validateCampaignReplyMailbox(access, &o.Campaign); err != nil {
		return err
	}
	visibility := ""
	if o.Visibility != "" {
		visibility, err = normalizeResourceVisibility(access, resourceCampaigns, o.Visibility)
		if err != nil {
			return err
		}
	}

	if c, err := a.validateCampaignFields(o); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	} else {
		o = c
	}

	hasRecipients, err := a.core.HasCampaignRecipientsInWorkspace(access, id)
	if err != nil {
		return err
	}
	// Drafts may have a provisional pool snapshot created by the audience
	// selector. It is rebuilt below on every update; only a campaign that has
	// left draft is locked to its original audience after recipients exist.
	if cm.Status == models.CampaignStatusDraft {
		hasRecipients = false
	}
	if hasRecipients {
		curCustomerListIDs, err := a.core.GetCampaignCustomerListIDsInWorkspace(access, id)
		if err != nil {
			return err
		}
		if !sameIntSlice(curCustomerListIDs, o.CustomerListIDs) {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.cantUpdateListsAfterStart"))
		}
	}

	out, err := a.core.UpdateCampaignInWorkspace(access, id, o.Campaign, regularListIDs, o.MediaIDs, visibility)
	if err != nil {
		return err
	}
	if err := a.persistCampaignReplyMailbox(id, access.UserID, o.ReplyMailboxID); err != nil {
		return err
	}
	out.ReplyMailboxID = o.ReplyMailboxID
	if visibility != "" {
		out.Visibility = visibility
	}
	// Pool audience rows are maintained separately from legacy customer-list
	// rows. Remove pools omitted by the current draft, then attach the selected
	// first-level/secondary pools. Attach is idempotent and resolves a first-level
	// pool to the target organization's secondary segment at send time.
	poolIDs := make([]int, 0, len(poolAudiences))
	for _, audience := range poolAudiences {
		poolIDs = append(poolIDs, audience.PoolID)
	}
	if !hasRecipients {
		// A draft has no immutable send snapshot yet. Rebuild all pool
		// associations so removing a secondary list (or replacing it with a
		// different one under the same first-level pool) cannot leave stale
		// audience metadata or recipients behind.
		if _, err := a.db.Exec(`DELETE FROM campaign_pool_recipients WHERE campaign_id=$1`, id); err != nil {
			return err
		}
		if _, err := a.db.Exec(`DELETE FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL`, id); err != nil {
			return err
		}
	} else if len(poolIDs) == 0 {
		if _, err := a.db.Exec(`DELETE FROM campaign_pool_recipients WHERE campaign_id=$1`, id); err != nil {
			return err
		}
		if _, err := a.db.Exec(`DELETE FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL`, id); err != nil {
			return err
		}
	} else if _, err := a.db.Exec(`DELETE FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_id <> ALL($2::INT[])`, id, pq.Array(poolIDs)); err != nil {
		return err
	}
	if len(poolIDs) > 0 {
		if _, err := a.db.Exec(`DELETE FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL AND pool_id <> ALL($2::INT[])`, id, pq.Array(poolIDs)); err != nil {
			return err
		}
	}
	for _, audience := range poolAudiences {
		if err := a.core.AttachPoolToCampaign(id, audience.PoolID, audience.SegmentID, int64(access.OrganizationID)); err != nil {
			return err
		}
	}
	if len(poolAudiences) > 0 {
		out, err = a.core.GetWorkspaceCampaign(access, id)
		if err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// UpdateCampaignStatus handles campaign status modification.
func (a *App) UpdateCampaignStatus(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireManagedWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	req := struct {
		Status string `json:"status"`
	}{}
	if err := c.Bind(&req); err != nil {
		return err
	}
	current, err := a.core.GetWorkspaceCampaign(access, id)
	if err != nil {
		return err
	}
	if req.Status == models.CampaignStatusScheduled || req.Status == models.CampaignStatusRunning {
		if err := requireAPIKeyScope(c, apiKeyScopeCampaignsSend); err != nil {
			return err
		}
		if err := requireCampaignSendOwnership(auth.GetUser(c), current); err != nil {
			return err
		}
	}
	if (req.Status == models.CampaignStatusScheduled || req.Status == models.CampaignStatusRunning) &&
		email.IsMessengerName(current.Messenger) {
		if !current.OwnerUserID.Valid || current.OwnerUserID.Int < 1 {
			return echo.NewHTTPError(http.StatusConflict, "campaign owner has no personal SMTP configured")
		}
		if err := a.requirePersonalSMTPAvailable(int(current.OwnerUserID.Int)); err != nil {
			return err
		}
	}
	if req.Status == models.CampaignStatusScheduled || req.Status == models.CampaignStatusRunning {
		if err := a.core.ValidatePoolCampaignAudience(id); err != nil {
			return err
		}
	}

	// Update the campaign status in the DB.
	out, err := a.core.UpdateCampaignStatusInWorkspace(access, id, req.Status)
	if err != nil {
		return err
	}

	// If the campaign is being stopped, send the signal to the manager to stop it in flight.
	if req.Status == models.CampaignStatusPaused || req.Status == models.CampaignStatusCancelled {
		a.manager.StopCampaign(id, req.Status)
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// UpdateCampaignArchive handles campaign status modification.
func (a *App) UpdateCampaignArchive(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)

	if _, err := a.requireManagedWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	req := struct {
		Archive     bool        `json:"archive"`
		TemplateID  int         `json:"archive_template_id"`
		Meta        models.JSON `json:"archive_meta"`
		ArchiveSlug string      `json:"archive_slug"`
	}{}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.TemplateID > 0 {
		// An archive template is rendered into a public page. Read-only
		// organization-manager access must not allow attaching a member's
		// private template to the caller's campaign.
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceTemplates, req.TemplateID, auth.PermTemplatesGet); err != nil {
			return err
		}
	}

	if req.ArchiveSlug != "" {
		// Format the slug to be alpha-numeric-dash.
		s := strings.ToLower(req.ArchiveSlug)
		s = strings.TrimSpace(reSlug.ReplaceAllString(s, " "))
		s = regexpSpaces.ReplaceAllString(s, "-")
		req.ArchiveSlug = s
	}

	if err := a.core.UpdateCampaignArchiveInWorkspace(access, id, req.Archive, req.TemplateID, req.Meta, req.ArchiveSlug); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{req})
}

// DeleteCampaign handles campaign deletion.
// Only scheduled campaigns that have not started yet can be deleted.
func (a *App) DeleteCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireManagedWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	// Delete the campaign from the DB.
	if err := a.core.DeleteCampaignInWorkspace(access, id); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteCampaigns deletes multiple campaigns by IDs or by query.
func (a *App) DeleteCampaigns(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}

	var (
		ids   []int
		query string
		all   bool
	)

	// Check for IDs in query params.
	if len(c.Request().URL.Query()["id"]) > 0 {
		var err error
		ids, err = parseStringIDs(c.Request().URL.Query()["id"])
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
		}
	} else {
		// Check for query param.
		query = strings.TrimSpace(c.FormValue("query"))
		all = c.FormValue("all") == "true"
	}

	// Validate that either IDs or query is provided.
	if len(ids) == 0 && (query == "" && !all) {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", "id or query required"))
	}

	if len(ids) > 0 {
		for _, id := range ids {
			if _, err := a.requireManagedWorkspaceCampaign(c, access, id); err != nil {
				return err
			}
		}
	} else {
		if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsManageAll, auth.PermCampaignsManage); err != nil {
			return err
		}
		managed, err := a.core.CustomerListManagedWorkspaceResources(access, resourceCampaigns)
		if err != nil {
			return err
		}
		// The UI's "select all" action still means every record matching the
		// current filter. Intersect it with managed resources so an organization
		// manager cannot delete a member's campaign and a forged all=true request
		// cannot ignore the query.
		visible, _, err := a.queryReadableWorkspaceCampaigns(c, access, query, nil, nil, "created_at", "DESC", 0, 0)
		if err != nil {
			return err
		}
		allowed := make(map[int]struct{}, len(managed))
		for _, id := range managed {
			allowed[id] = struct{}{}
		}
		for _, campaign := range visible {
			if _, ok := allowed[campaign.ID]; ok {
				ids = append(ids, campaign.ID)
			}
		}
	}

	// The legacy deletion query treats an empty ID array as an unrestricted
	// search. Do not call it when no caller-owned workspace resources matched.
	if len(ids) > 0 {
		if err := a.core.DeleteCampaignsInWorkspace(access, ids); err != nil {
			return err
		}
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// GetRunningCampaignStats returns stats of a given set of campaign IDs.
func (a *App) GetRunningCampaignStats(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the running campaign stats from the DB.
	out, err := a.core.GetRunningCampaignStats()
	if err != nil {
		return err
	}
	// A running campaign can be readable without being reportable (for
	// example, a public campaign viewed by an ordinary member).  Filter each
	// row through the same analytics authorization used by the report
	// endpoints instead of relying on the broad campaign read query.  This is
	// intentionally done after loading the short-lived stats snapshot: a
	// campaign may finish or be moved while this request is in flight.
	filtered := out[:0]
	for _, stat := range out {
		if err := a.requireCampaignAnalytics(c, access, stat.ID); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code == http.StatusForbidden {
				continue
			}
			return err
		}
		filtered = append(filtered, stat)
	}
	out = filtered

	if len(out) == 0 {
		return c.JSON(http.StatusOK, okResp{[]struct{}{}})
	}

	// Compute rate.
	for i, c := range out {
		if c.Started.Valid && c.UpdatedAt.Valid {
			diff := max(int(c.UpdatedAt.Time.Sub(c.Started.Time).Minutes()), 1)

			rate := c.Sent / diff
			if rate > c.Sent || rate > c.ToSend {
				rate = c.Sent
			}

			// Rate since the starting of the campaign.
			out[i].NetRate = rate

			// Realtime running rate over the last minute.
			out[i].Rate = a.manager.GetCampaignStats(c.ID).SendRate
		}
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// TestCampaign handles the sending of a campaign message to
// arbitrary customers for testing.
func (a *App) TestCampaign(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the campaign ID.
	id := getID(c)

	if _, err := a.requireManagedWorkspaceCampaign(c, access, id); err != nil {
		return err
	}

	// Get and validate fields.
	var req campReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	// The test form historically omitted messenger when it used the campaign's
	// saved value. Resolve that value before validation and SMTP checks; an
	// empty request must not accidentally bypass the account-SMTP guard (or be
	// rejected as an unknown messenger).
	if strings.TrimSpace(req.Messenger) == "" {
		saved, err := a.core.GetWorkspaceCampaign(access, id)
		if err != nil {
			return err
		}
		req.Messenger = saved.Messenger
	}

	// Validate.
	if c, err := a.validateCampaignFields(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	} else {
		req = c
	}
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, req.CustomerListIDs, true); err != nil {
		return err
	}
	if err := a.requireUsableCampaignResources(c, access, req); err != nil {
		return err
	}
	if len(req.CustomerEmails) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.noSubsToTest"))
	}

	// Sanitize customer e-mails.
	for i := range req.CustomerEmails {
		req.CustomerEmails[i] = strings.ToLower(strings.TrimSpace(req.CustomerEmails[i]))
	}

	// Get the customers from the DB by their e-mails.
	subs, err := a.core.GetManagedWorkspaceCustomersByEmails(access, req.CustomerEmails)
	if err != nil {
		return err
	}

	// Get the campaign from the DB for previewing. The request is normally JSON,
	// so use the bound value rather than FormValue (which only reliably covers
	// form-encoded requests).
	tplID := 0
	if req.TemplateID.Valid {
		tplID = int(req.TemplateID.Int)
	}
	camp, err := a.core.GetWorkspaceCampaignForPreview(access, id, tplID)
	if err != nil {
		return err
	}
	// Platform administrators have global management visibility, but must not
	// initiate delivery on behalf of another account. The campaign owner must
	// explicitly perform both scheduling/running and test sends so the message
	// can only resolve that owner's personal SMTP pool.
	if err := requireCampaignSendOwnership(auth.GetUser(c), camp); err != nil {
		return err
	}
	if email.IsMessengerName(req.Messenger) {
		if !camp.OwnerUserID.Valid || camp.OwnerUserID.Int < 1 {
			return echo.NewHTTPError(http.StatusConflict, "campaign owner has no personal SMTP configured")
		}
		if err := a.requirePersonalSMTPAvailable(int(camp.OwnerUserID.Int)); err != nil {
			return err
		}
	}
	// Override certain values from the DB with incoming values.
	camp.Name = req.Name
	camp.Subject = req.Subject
	camp.FromEmail = req.FromEmail
	camp.Body = req.Body
	camp.AltBody = req.AltBody
	camp.Messenger = req.Messenger
	camp.ContentType = req.ContentType
	camp.Headers = req.Headers
	camp.TemplateID = req.TemplateID
	// For a test send the submitted media customer_list is authoritative. The preview
	// query also includes the campaign's saved associations, which would make a
	// media item that the user just removed reappear in the test message. Start
	// from the request and let preloadTestCampaignMedia append the selected
	// template's own associations below.
	camp.MediaIDs = camp.MediaIDs[:0]
	seenMedia := make(map[int64]struct{}, len(req.MediaIDs))
	for _, id := range req.MediaIDs {
		if id < 1 {
			continue
		}
		mid := int64(id)
		if _, exists := seenMedia[mid]; exists {
			continue
		}
		camp.MediaIDs = append(camp.MediaIDs, mid)
		seenMedia[mid] = struct{}{}
	}

	// The preview query returns the campaign/template media IDs, but those IDs
	// are only references. Test sends must re-check each association and load
	// the binary through the active workspace boundary after applying request
	// overrides, so newly selected media and an explicitly selected template are
	// included in the materialized snapshot.
	if err := a.preloadTestCampaignMedia(c, access, &camp, tplID); err != nil {
		return err
	}

	// Send the test messages.
	for _, s := range subs {
		sub := s

		if err := a.sendTestMessage(sub, &camp); err != nil {
			a.log.Printf("error sending test message: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError,
				a.i18n.Ts("campaigns.errorSendTest", "error", err.Error()))
		}
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// requireCampaignSendOwnership prevents a platform administrator's global
// read/manage access from becoming an ability to send through another user's
// personal SMTP credentials. It is intentionally applied to every messenger,
// not only e-mail, because a campaign send is an account-owned operation.
func requireCampaignSendOwnership(user auth.User, camp models.Campaign) error {
	if !camp.OwnerUserID.Valid || camp.OwnerUserID.Int != user.ID {
		return echo.NewHTTPError(http.StatusForbidden, "only the campaign owner can send this campaign")
	}
	return nil
}

// GetCampaignViewAnalytics retrieves view counts for a campaign.
func (a *App) GetCampaignViewAnalytics(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	ids, err := parseStringIDs(c.Request().URL.Query()["id"])
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}

	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.missingFields", "name", "`id`"))
	}
	for _, id := range ids {
		if err := a.requireCampaignAnalytics(c, access, id); err != nil {
			return err
		}
	}

	var (
		typ  = c.Param("type")
		from = c.QueryParams().Get("from")
		to   = c.QueryParams().Get("to")
	)
	if !strHasLen(from, 10, 30) || !strHasLen(to, 10, 30) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("analytics.invalidDates"))
	}

	// Campaign link stats.
	if typ == "links" {
		out, err := a.core.GetWorkspaceCampaignAnalyticsLinks(access, ids, from, to, a.cfg.Privacy.IndividualTracking)
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, okResp{out})
	}

	// Get the analytics numbers from the DB for the campaigns.
	out, err := a.core.GetWorkspaceCampaignAnalyticsCounts(access, ids, typ, from, to)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignReportSummary(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if err := a.requireCampaignAnalytics(c, access, id); err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	out, err := a.core.GetWorkspaceCampaignReportSummary(access, id, from, to, a.cfg.Privacy.IndividualTracking)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignsReportSummary(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	ids, err := a.getAccessibleCampaignReportIDs(c)
	if err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	out, err := a.core.GetWorkspaceCampaignsReportSummary(access, ids, from, to, a.cfg.Privacy.IndividualTracking)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignReportSeries(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if err := a.requireCampaignAnalytics(c, access, id); err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	out, err := a.core.GetWorkspaceCampaignReportSeries(access, id, from, to)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignsReportSeries(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	ids, err := a.getAccessibleCampaignReportIDs(c)
	if err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	out, err := a.core.GetWorkspaceCampaignsReportSeries(access, ids, from, to)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignReportLinks(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if err := a.requireCampaignAnalytics(c, access, id); err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	out, err := a.core.GetWorkspaceCampaignReportLinks(access, id, from, to, a.cfg.Privacy.IndividualTracking)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignsReportLinks(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	ids, err := a.getAccessibleCampaignReportIDs(c)
	if err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	out, err := a.core.GetWorkspaceCampaignsReportLinks(access, ids, from, to, a.cfg.Privacy.IndividualTracking)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignReportRecipients(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)

	// This report includes recipient identities, so organization managers may
	// not retrieve another member's data even though they may view aggregates.
	if _, err := a.requireSensitiveWorkspaceCampaign(c, access, id); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersGetAll, auth.PermCustomersGet); err != nil {
		return err
	}
	if !a.cfg.Privacy.IndividualTracking {
		return echo.NewHTTPError(http.StatusForbidden, a.i18n.T("analytics.nonIndividualTracking"))
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	linkID := 0
	if v := c.QueryParam("link_id"); v != "" {
		linkID, err = strconv.Atoi(v)
		if err != nil || linkID < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
	}

	pg := a.pg.NewFromURL(c.Request().URL.Query())
	out, total, err := a.core.QueryWorkspaceCampaignReportRecipients(access, id, from, to, models.CampaignReportRecipientFilters{
		Search:  c.QueryParam("search"),
		Opened:  c.QueryParam("opened"),
		Clicked: c.QueryParam("clicked"),
		Bounced: c.QueryParam("bounced"),
		LinkID:  linkID,
		SortBy:  c.QueryParam("sort_by"),
		Order:   c.QueryParam("order"),
	}, pg.Offset, pg.Limit)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{models.PageResults{
		Results: out,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}})
}

func (a *App) GetCampaignsReportRecipients(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersGetAll, auth.PermCustomersGet); err != nil {
		return err
	}
	if !a.cfg.Privacy.IndividualTracking {
		return echo.NewHTTPError(http.StatusForbidden, a.i18n.T("analytics.nonIndividualTracking"))
	}

	ids, err := a.getManagedCampaignReportIDs(c, access)
	if err != nil {
		return err
	}

	from, to, err := a.getCampaignReportDateRange(c)
	if err != nil {
		return err
	}

	linkID := 0
	if v := c.QueryParam("link_id"); v != "" {
		linkID, err = strconv.Atoi(v)
		if err != nil || linkID < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
	}

	pg := a.pg.NewFromURL(c.Request().URL.Query())
	out, total, err := a.core.QueryWorkspaceCampaignsReportRecipients(access, ids, from, to, models.CampaignReportRecipientFilters{
		Search:  c.QueryParam("search"),
		Opened:  c.QueryParam("opened"),
		Clicked: c.QueryParam("clicked"),
		Bounced: c.QueryParam("bounced"),
		LinkID:  linkID,
		SortBy:  c.QueryParam("sort_by"),
		Order:   c.QueryParam("order"),
	}, pg.Offset, pg.Limit)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{models.PageResults{
		Results: out,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}})
}

func (a *App) getCampaignReportDateRange(c echo.Context) (string, string, error) {
	from := c.QueryParam("from")
	to := c.QueryParam("to")
	if !strHasLen(from, 10, 30) || !strHasLen(to, 10, 30) {
		return "", "", echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("analytics.invalidDates"))
	}
	return from, to, nil
}

func (a *App) getAccessibleCampaignReportIDs(c echo.Context) ([]int, error) {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return nil, err
	}
	ids, err := getQueryInts("id", c.QueryParams())
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}

	if len(ids) > 0 {
		seen := make(map[int]struct{}, len(ids))
		out := make([]int, 0, len(ids))
		for _, id := range ids {
			if id < 1 {
				return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
			}
			if err := a.requireCampaignAnalytics(c, access, id); err != nil {
				return nil, err
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		return out, nil
	}

	camps, _, err := a.queryReadableWorkspaceCampaigns(c, access, "", nil, nil, "created_at", "DESC", 0, 0)
	if err != nil {
		return nil, err
	}

	// The customer_list endpoint intentionally contains campaigns that are merely
	// readable (for example organization/global publications).  Analytics are
	// narrower: ordinary users may report only on campaigns they own, while an
	// organization manager may report on campaigns in the active organization.
	// Re-run the same authorization for every row before constructing the ID
	// set; otherwise a no-id report request would bypass the owner boundary.
	out := make([]int, 0, len(camps))
	for _, camp := range camps {
		if err := a.requireCampaignAnalytics(c, access, camp.ID); err != nil {
			if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code == http.StatusForbidden {
				continue
			}
			return nil, err
		}
		out = append(out, camp.ID)
	}

	return out, nil
}

func (a *App) getManagedCampaignReportIDs(c echo.Context, access models.WorkspaceAccess) ([]int, error) {
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsGetAnalytics); err != nil {
		return nil, err
	}
	ids, err := getQueryInts("id", c.QueryParams())
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}
	if len(ids) == 0 {
		managed, err := a.core.CustomerListManagedWorkspaceResources(access, resourceCampaigns)
		if err != nil {
			return nil, err
		}
		out := make([]int, 0, len(managed))
		for _, id := range managed {
			if _, err := a.requireSensitiveWorkspaceCampaign(c, access, id); err != nil {
				if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code == http.StatusForbidden {
					continue
				}
				return nil, err
			}
			out = append(out, id)
		}
		return out, nil
	}
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id < 1 {
			return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
		if _, err := a.requireSensitiveWorkspaceCampaign(c, access, id); err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// sendTestMessage takes a campaign and a customer and sends out a sample campaign message.
func (a *App) sendTestMessage(sub models.Customer, camp *models.Campaign) error {
	if err := camp.CompileTemplate(a.manager.TemplateFuncs(camp)); err != nil {
		a.log.Printf("error compiling template: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("templates.errorCompiling", "error", err.Error()))
	}

	// Create a sample campaign message.
	msg, err := a.manager.NewCampaignMessage(camp, sub)
	if err != nil {
		a.log.Printf("error rendering message: %v", err)
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("templates.errorRendering", "error", err.Error()))
	}

	return a.manager.PushCampaignMessage(msg)
}

// preloadTestCampaignMedia validates and materializes every media reference
// used by a test campaign.  Campaign-owned media must be directly usable in
// the active workspace.  A private image owned by the author of a shared
// template is the one deliberate exception: CanUseTemplateMedia grants that
// image only when the association is real and the template itself is usable.
// Once Attachments is populated, the manager will not perform an unscoped
// media lookup while queuing the test message.
func (a *App) preloadTestCampaignMedia(c echo.Context, access models.WorkspaceAccess, camp *models.Campaign, requestedTemplateID int) error {
	if camp == nil {
		return nil
	}

	templateID := requestedTemplateID
	if templateID < 1 && camp.TemplateID.Valid && camp.TemplateID.Int > 0 {
		templateID = int(camp.TemplateID.Int)
	}

	// A test request contains only explicitly selected campaign attachments.
	// Template attachments are not normally included in that form field (the
	// editor displays them separately), so discover the complete template
	// association below and append it to the effective media set.
	ids := make([]int, 0, len(camp.MediaIDs))
	seen := make(map[int]struct{}, len(camp.MediaIDs))
	for _, rawID := range camp.MediaIDs {
		id := int(rawID)
		if id < 1 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	// Keep the source of each reference separate.  A template-only private
	// image may use the shared-template exception, while a campaign association
	// must always pass the direct resource check.
	type mediaReference struct {
		MediaID int    `db:"media_id"`
		Source  string `db:"source"`
	}
	var refs []mediaReference
	query := `
		SELECT cm.media_id, 'campaign' AS source
		FROM campaign_media cm
		WHERE cm.campaign_id = $1 AND cm.media_id IS NOT NULL
		UNION ALL
		SELECT tm.media_id, 'template' AS source
		FROM template_media tm
		WHERE $2 > 0 AND tm.template_id = $2 AND tm.media_id IS NOT NULL
		ORDER BY media_id, source`
	if err := a.db.Select(&refs, query, camp.ID, templateID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.media}", "error", err.Error()))
	}
	campaignRefs := make(map[int]bool, len(refs))
	templateRefs := make(map[int]bool, len(refs))
	for _, ref := range refs {
		id := ref.MediaID
		if id < 1 {
			continue
		}
		switch ref.Source {
		case "campaign":
			campaignRefs[id] = true
		case "template":
			templateRefs[id] = true
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		camp.MediaIDs = nil
		camp.Attachments = nil
		return nil
	}

	// The selected template itself must still be usable. This is normally
	// checked by requireUsableCampaignResources, but the saved campaign
	// template path can be populated when the request omits template_id.
	if templateID > 0 {
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceTemplates, templateID, auth.PermTemplatesGet); err != nil {
			return err
		}
	}

	// First authorize direct workspace media. A media ID that is already linked
	// to the campaign is never allowed to use the template-private exception:
	// otherwise a forged campaign_media row could turn a private binary into a
	// sendable attachment merely by also linking it from a shared template.
	directUsable := make(map[int]bool, len(ids))
	directErrors := make(map[int]error, len(ids))
	templateExceptionIDs := make([]int64, 0, len(templateRefs))
	for _, id := range ids {
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceMedia, id, auth.PermMediaGet); err != nil {
			directErrors[id] = err
			if templateRefs[id] && !campaignRefs[id] && templateID > 0 {
				templateExceptionIDs = append(templateExceptionIDs, int64(id))
				continue
			}
			return err
		}
		directUsable[id] = true
	}

	// Resolve template-private binaries through the association-aware store.
	// GetWorkspaceMediaByID intentionally enforces ordinary workspace visibility;
	// it is therefore the wrong loader after CanUseTemplateMedia has granted the
	// narrow published-template exception.
	templateAttachments := make(map[int]models.Attachment, len(templateExceptionIDs))
	if len(templateExceptionIDs) > 0 {
		loaded, err := newManagerStore(a.queries, a.core, a.media, a.db).
			GetTemplateAttachments(access, templateID, templateExceptionIDs)
		if err != nil {
			return err
		}
		for _, attachment := range loaded {
			templateAttachments[attachment.MediaID] = attachment
		}
	}

	attachments := make([]models.Attachment, 0, len(ids))
	for _, id := range ids {
		if !directUsable[id] {
			if attachment, ok := templateAttachments[id]; ok {
				attachments = append(attachments, attachment)
				continue
			}
			if err := directErrors[id]; err != nil {
				return err
			}
			return echo.NewHTTPError(http.StatusForbidden, "media is not usable in the active workspace")
		}

		m, err := a.core.GetWorkspaceMediaByID(access, id)
		if err != nil {
			return err
		}
		m.URL = a.media.GetURL(m.Filename)
		content, err := a.media.GetBlob(m.URL)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				a.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.media}", "error", err.Error()))
		}
		attachments = append(attachments, models.Attachment{
			Name:      m.Filename,
			Content:   content,
			Header:    manager.MakeAttachmentHeader(m.Filename, "base64", m.ContentType),
			MediaID:   m.ID,
			SourceURL: workspaceMediaIDFileURL(m.ID, m.Filename),
		})
	}
	// Keep the effective IDs on the campaign so the renderer's CID replacement
	// can match both explicitly selected and template-provided images.
	camp.MediaIDs = make(pq.Int64Array, 0, len(ids))
	for _, id := range ids {
		camp.MediaIDs = append(camp.MediaIDs, int64(id))
	}
	camp.Attachments = attachments
	return nil
}

func (a *App) requireUsableCampaignResources(c echo.Context, access models.WorkspaceAccess, req campReq) error {
	if req.TemplateID.Valid && req.TemplateID.Int > 0 {
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceTemplates, int(req.TemplateID.Int), auth.PermTemplatesGet); err != nil {
			return err
		}
	}
	if req.ArchiveTemplateID.Valid && req.ArchiveTemplateID.Int > 0 {
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceTemplates, int(req.ArchiveTemplateID.Int), auth.PermTemplatesGet); err != nil {
			return err
		}
	}
	for _, id := range req.MediaIDs {
		if id < 1 {
			continue
		}
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceMedia, id, auth.PermMediaGet); err == nil {
			continue
		} else {
			// A shared/global template may deliberately carry a private image
			// owned by its author. That image is usable only when this exact
			// media ID is linked from the exact template ID; never widen this
			// exception based solely on a client-provided media ID.
			if req.TemplateID.Valid && req.TemplateID.Int > 0 {
				allowed, templateErr := a.core.CanUseTemplateMedia(access, int(req.TemplateID.Int), id)
				if templateErr != nil {
					return templateErr
				}
				if allowed {
					continue
				}
			}
			return err
		}
	}
	return nil
}

// splitCampaignAudienceIDs separates legacy customer lists from first-class
// public pools. Pool IDs are authorized through pool delivery grants/segments,
// not through customer-list read/manage permissions; this is what permits an
// organization to select a pool while keeping contact details hidden.
func (a *App) splitCampaignAudienceIDs(access models.WorkspaceAccess, ids []int) ([]int, []campaignPoolAudience, error) {
	regular := make([]int, 0, len(ids))
	pools := make([]campaignPoolAudience, 0)
	if len(ids) == 0 {
		return regular, pools, nil
	}
	var rows []struct {
		ID     int      `db:"id"`
		Type   string   `db:"type"`
		PoolID null.Int `db:"pool_parent_id"`
	}
	if err := a.db.Select(&rows, `SELECT id,type,pool_parent_id FROM customer_lists WHERE id = ANY($1::INT[])`, pq.Array(ids)); err != nil {
		return nil, nil, err
	}
	seen := make(map[int]bool, len(rows))
	requested := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}
	for _, row := range rows {
		seen[row.ID] = true
		switch row.Type {
		case models.CustomerListTypePool:
			if access.OrganizationID <= 0 {
				return nil, nil, echo.NewHTTPError(http.StatusForbidden, "public pool requires an organization workspace")
			}
			if !access.PlatformAdmin {
				if !access.IsOrganization() {
					return nil, nil, echo.NewHTTPError(http.StatusForbidden, "public pool requires an organization workspace")
				}
				ok, err := a.core.HasPoolOrganizationPermission(row.ID, int64(access.OrganizationID))
				if err != nil {
					return nil, nil, err
				}
				if !ok {
					if err := a.db.Get(&ok, `SELECT EXISTS(SELECT 1 FROM pool_segments WHERE pool_id=$1 AND organization_id=$2)`, row.ID, access.OrganizationID); err != nil {
						return nil, nil, err
					}
				}
				if !ok {
					return nil, nil, echo.NewHTTPError(http.StatusForbidden, "pool delivery access has not been granted")
				}
			}
			p := campaignPoolAudience{PoolID: row.ID}
			pools = append(pools, p)
		case models.CustomerListTypePoolSegment:
			if !row.PoolID.Valid {
				return nil, nil, echo.NewHTTPError(http.StatusBadRequest, "public-pool segment is not bound to a first-level pool")
			}
			var orgID int64
			if err := a.db.Get(&orgID, `SELECT organization_id FROM pool_segments WHERE list_id=$1 AND pool_id=$2`, row.ID, row.PoolID.Int); err != nil {
				return nil, nil, err
			}
			if !access.PlatformAdmin && (!access.IsOrganization() || orgID != int64(access.OrganizationID)) {
				return nil, nil, echo.NewHTTPError(http.StatusForbidden, "pool segment is outside the active organization")
			}
			segmentID := int64(row.ID)
			if err := a.db.Get(&segmentID, `SELECT id FROM pool_segments WHERE list_id=$1 AND pool_id=$2`, row.ID, row.PoolID.Int); err != nil {
				return nil, nil, err
			}
			pools = append(pools, campaignPoolAudience{PoolID: int(row.PoolID.Int), SegmentID: &segmentID})
		default:
			regular = append(regular, row.ID)
		}
	}
	if len(seen) != len(requested) {
		return nil, nil, echo.NewHTTPError(http.StatusBadRequest, "one or more customer lists were not found")
	}
	return regular, pools, nil
}

// validateCampaignFields validates incoming campaign field values.
func (a *App) validateCampaignFields(c campReq) (campReq, error) {
	if c.FromEmail == "" {
		c.FromEmail = a.cfg.FromEmail
	} else if !reFromAddress.Match([]byte(c.FromEmail)) {
		if _, err := a.importer.SanitizeEmail(c.FromEmail); err != nil {
			return c, errors.New(a.i18n.T("campaigns.fieldInvalidFromEmail"))
		}
	}

	if !strHasLen(c.Name, 1, stdInputMaxLen) {
		return c, errors.New(a.i18n.T("campaigns.fieldInvalidName"))
	}

	// Larger char limit for subject as it can contain {{ go templating }} logic.
	if !strHasLen(c.Subject, 1, 5000) {
		return c, errors.New(a.i18n.T("campaigns.fieldInvalidSubject"))
	}

	// If no content-type is specified, default to richtext.
	if c.ContentType != models.CampaignContentTypeRichtext &&
		c.ContentType != models.CampaignContentTypeHTML &&
		c.ContentType != models.CampaignContentTypePlain &&
		c.ContentType != models.CampaignContentTypeVisual &&
		c.ContentType != models.CampaignContentTypeMarkdown {
		c.ContentType = models.CampaignContentTypeRichtext
	}

	if c.ContentType != models.CampaignContentTypeVisual {
		c.BodySource.Valid = false
	}

	// If there's a "send_at" date, it should be in the future.
	if c.SendAt.Valid {
		if c.SendAt.Time.UTC().Before(time.Now().UTC()) {
			return c, errors.New(a.i18n.T("campaigns.fieldInvalidSendAt"))
		}
	}

	if len(c.CustomerListIDs) == 0 {
		return c, errors.New(a.i18n.T("campaigns.fieldInvalidCustomerListIDs"))
	}

	if email.IsMessengerName(c.Messenger) {
		// Campaigns never select a specific SMTP server. The account-owned
		// round-robin pool is resolved by the campaign owner at send time.
		c.Messenger = emailMsgr
	} else if !a.manager.HasMessenger(c.Messenger) {
		return c, errors.New(a.i18n.Ts("campaigns.fieldInvalidMessenger", "name", c.Messenger))
	}

	if c.Type == "" {
		c.Type = models.CampaignTypeRegular
	}

	if c.Type == models.CampaignTypeRegular && email.IsMessengerName(c.Messenger) {
		if c.DailySendLimit < 1 {
			// Backward compatibility for legacy campaigns/clients created before
			// daily SMTP limits became mandatory for regular email campaigns.
			c.DailySendLimit = 300
			a.log.Printf("campaign %d using legacy daily_send_limit fallback=300 (messenger=%s)", c.ID, c.Messenger)
		}
		if c.DailyResumeTime == "" {
			c.DailyResumeTime = "09:00"
		}
		if _, err := time.ParseInLocation(dailyResumeLayout, c.DailyResumeTime, time.Local); err != nil {
			return c, errors.New(a.i18n.T("campaigns.fieldInvalidDailyResumeTime"))
		}
	} else {
		c.DailySendLimit = 0
		c.DailyResumeTime = "09:00"
	}

	camp := models.Campaign{
		Body:           c.Body,
		TemplateBody:   tplTag,
		ContentType:    c.ContentType,
		AutoTrackLinks: c.AutoTrackLinks,
	}
	if err := c.CompileTemplate(a.manager.TemplateFuncs(&camp)); err != nil {
		return c, errors.New(a.i18n.Ts("campaigns.fieldInvalidBody", "error", err.Error()))
	}

	if len(c.Headers) == 0 {
		c.Headers = make([]map[string]string, 0)
	}

	// Validate and initialize attribs.
	if c.Attribs != nil {
		if _, err := json.Marshal(c.Attribs); err != nil {
			return c, errors.New(a.i18n.T("customers.invalidJSON"))
		}
	}

	if len(c.ArchiveMeta) == 0 {
		c.ArchiveMeta = json.RawMessage("{}")
	}

	if c.ArchiveSlug.String != "" {
		// Format the slug to be alpha-numeric-dash.
		s := strings.ToLower(c.ArchiveSlug.String)
		s = strings.TrimSpace(reSlug.ReplaceAllString(s, " "))
		s = regexpSpaces.ReplaceAllString(s, "-")

		c.ArchiveSlug = null.NewString(s, true)
	} else {
		// If there's no slug set, set it to NULL in the DB.
		c.ArchiveSlug.Valid = false
	}

	return c, nil
}

// makeOptinCampaignMessage makes a default opt-in campaign message body.
func (a *App) makeOptinCampaignMessage(access models.WorkspaceAccess, o campReq) (campReq, error) {
	if len(o.CustomerListIDs) == 0 {
		return o, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.fieldInvalidCustomerListIDs"))
	}

	// Fetch double opt-in customer_lists from the given customer_list IDs from the DB.
	customer_lists, err := a.core.GetListsByOptinInWorkspace(access, o.CustomerListIDs, models.CustomerListOptinDouble)
	if err != nil {
		return o, err
	}

	// There are no double opt-in customer_lists.
	if len(customer_lists) == 0 {
		return o, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.noOptinLists"))
	}

	// Construct the opt-in URL with customer_list IDs.
	customerListIDs := url.Values{}
	for _, l := range customer_lists {
		customerListIDs.Add("l", l.UUID)
	}
	// optinURLFunc := template.URL("{{ OptinURL }}?" + customerListIDs.Encode())
	optinURLAttr := template.HTMLAttr(fmt.Sprintf(`href="{{ OptinURL }}%s"`, customerListIDs.Encode()))

	// Prepare sample opt-in message for the campaign.
	var b bytes.Buffer

	if err := notifs.Tpls.ExecuteTemplate(&b, "optin-campaign", struct {
		CustomerLists []models.CustomerList
		OptinURLAttr  template.HTMLAttr
	}{customer_lists, optinURLAttr}); err != nil {
		a.log.Printf("error compiling 'optin-campaign' template: %v", err)
		return o, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("templates.errorCompiling", "error", err.Error()))
	}

	o.Body = b.String()
	return o, nil
}

// canEditCampaign returns true if a campaign is in a status where updating
// its properties is allowed.
func canEditCampaign(status string) bool {
	return status == models.CampaignStatusDraft ||
		status == models.CampaignStatusPaused ||
		status == models.CampaignStatusScheduled ||
		status == models.CampaignStatusDeferred
}

func sameIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}

	left := append([]int(nil), a...)
	right := append([]int(nil), b...)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}
