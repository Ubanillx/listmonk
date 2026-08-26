package main

import (
	"errors"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	// tplTag is the template tag that should be present in a template
	// as the placeholder for campaign bodies.
	tplTag = `{{ template "content" . }}`

	dummyTpl = `
		<p>Hi there</p>
		<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Duis et elit ac elit sollicitudin condimentum non a magna. Sed tempor mauris in facilisis vehicula. Aenean nisl urna, accumsan ac tincidunt vitae, interdum cursus massa. Interdum et malesuada fames ac ante ipsum primis in faucibus. Aliquam varius turpis et turpis lacinia placerat. Aenean id ligula a orci lacinia blandit at eu felis. Phasellus vel lobortis lacus. Suspendisse leo elit, luctus sed erat ut, venenatis fermentum ipsum. Donec bibendum neque quis.</p>

		<h3>Sub heading</h3>
		<p>Nam luctus dui non placerat mattis. Morbi non accumsan orci, vel interdum urna. Duis faucibus id nunc ut euismod. Curabitur et eros id erat feugiat fringilla in eget neque. Aliquam accumsan cursus eros sed faucibus.</p>

		<p>Here is a link to <a href="https://listmonk.app" target="_blank">listmonk</a>.</p>`
)

var (
	regexpTplTag = regexp.MustCompile(`{{(\s+)?template\s+?"content"(\s+)?\.(\s+)?}}`)
)

type templateCloneReq struct {
	Name                 string `json:"name"`
	Subject              string `json:"subject"`
	TargetOrganizationID *int   `json:"target_organization_id"`
}

// templateReq keeps the API's write representation of media (a list of IDs)
// separate from Template.Media, which is the read representation sent to the UI.
type templateReq struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	Subject    string      `json:"subject"`
	Type       string      `json:"type"`
	Body       string      `json:"body"`
	BodySource null.String `json:"body_source"`
	MediaIDs   []int       `json:"media"`
	Visibility string      `json:"visibility"`
}

func (r templateReq) template() models.Template {
	return models.Template{
		Base:       models.Base{ID: r.ID},
		Name:       r.Name,
		Subject:    r.Subject,
		Type:       r.Type,
		Body:       r.Body,
		BodySource: r.BodySource,
	}
}

func (r templateReq) mediaIDs() pq.Int64Array {
	ids := make(pq.Int64Array, 0, len(r.MediaIDs))
	for _, id := range r.MediaIDs {
		if id > 0 {
			ids = append(ids, int64(id))
		}
	}
	return ids
}

// GetTemplate handles the retrieval of a template
func (a *App) GetTemplate(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// If no_body is true, blank out the body of the template from the response.
	noBody, _ := strconv.ParseBool(c.QueryParam("no_body"))

	// Get the template from the DB.
	id := getID(c)
	if _, err := a.core.RequireReadResource(access, "templates", id); err != nil {
		return err
	}
	out, err := a.core.GetTemplate(id, noBody)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetTemplates handles retrieval of templates.
func (a *App) GetTemplates(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// If no_body is true, blank out the body of the template from the response.
	noBody, _ := strconv.ParseBool(c.QueryParam("no_body"))

	// Fetch templates from the DB.
	out, err := a.core.GetWorkspaceTemplates(access, "", noBody)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// PreviewTemplate renders the HTML preview of a template in the DB.
func (a *App) PreviewTemplate(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Fetch one template from the DB.
	id := getID(c)
	if _, err := a.core.RequireReadResource(access, "templates", id); err != nil {
		return err
	}
	tpl, err := a.core.GetTemplate(id, false)
	if err != nil {
		return err
	}

	// Render the template.
	out, err := a.previewTemplate(tpl)
	if err != nil {
		return err
	}

	return c.HTML(http.StatusOK, string(out))
}

// PreviewTemplateBody renders the HTML preview of a template given its type and body.
func (a *App) PreviewTemplateBody(c echo.Context) error {
	tpl := models.Template{
		Type: c.FormValue("template_type"),
		Body: c.FormValue("body"),
	}

	// Body is posted with the request.
	if tpl.Type == "" {
		tpl.Type = models.TemplateTypeCampaign
	}

	if tpl.Type == models.TemplateTypeCampaign && !regexpTplTag.MatchString(tpl.Body) {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("templates.placeholderHelp", "placeholder", tplTag))
	}

	// Render the template.
	out, err := a.previewTemplate(tpl)
	if err != nil {
		return err
	}

	return c.HTML(http.StatusOK, string(out))
}

// CreateTemplate handles template creation.
func (a *App) CreateTemplate(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	var req templateReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	o, err := a.prepareTemplate(req.template())
	if err != nil {
		return err
	}
	if err := a.requireReadableMedia(access, req.MediaIDs); err != nil {
		return err
	}
	visibility, err := normalizeResourceVisibility(access, resourceTemplates, req.Visibility)
	if err != nil {
		return err
	}

	// Create the template the in the DB.
	out, err := a.core.CreateTemplate(o.Name, o.Type, o.Subject, []byte(o.Body), o.BodySource, req.mediaIDs(), core.ApplyWorkspaceScope(access, visibility))
	if err != nil {
		return err
	}

	// If it's a transactional template, cache it in the manager
	// to be used for arbitrary incoming tx message pushes.
	if o.Type == models.TemplateTypeTx {
		out.Tpl = o.Tpl
		out.SubjectTpl = o.SubjectTpl
		a.manager.CacheTpl(out.ID, &out)
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// UpdateTemplate handles template modification.
func (a *App) UpdateTemplate(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var req templateReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	o, err := a.prepareTemplate(req.template())
	if err != nil {
		return err
	}

	// Update the template in the DB.
	id := getID(c)
	if _, err := a.core.RequireManageResource(access, "templates", id); err != nil {
		return err
	}
	if err := a.requireReadableMedia(access, req.MediaIDs); err != nil {
		return err
	}
	out, err := a.core.UpdateTemplate(id, o.Name, o.Subject, []byte(o.Body), o.BodySource, req.mediaIDs())
	if err != nil {
		return err
	}
	if req.Visibility != "" {
		visibility, err := normalizeResourceVisibility(access, resourceTemplates, req.Visibility)
		if err != nil {
			return err
		}
		if err := a.core.SetResourceVisibility("templates", id, visibility); err != nil {
			return err
		}
		out.Visibility = visibility
	}

	// If it's a transactional template, cache it.
	if out.Type == models.TemplateTypeTx {
		out.Tpl = o.Tpl
		out.SubjectTpl = o.SubjectTpl
		a.manager.CacheTpl(out.ID, &out)
	}

	return c.JSON(http.StatusOK, okResp{out})

}

// CloneTemplate copies an existing template into a new template with a new name.
func (a *App) CloneTemplate(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.core.RequireReadResource(access, "templates", id); err != nil {
		return err
	}

	src, err := a.core.GetTemplate(id, false)
	if err != nil {
		return err
	}

	var req templateCloneReq
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

	clone, err := a.prepareTemplate(src.Clone(req.Name, req.Subject))
	if err != nil {
		return err
	}

	out, err := a.core.CloneTemplateForWorkspace(id, target, clone.Name, clone.Subject)
	if err != nil {
		return err
	}

	if clone.Type == models.TemplateTypeTx {
		out.Tpl = clone.Tpl
		out.SubjectTpl = clone.SubjectTpl
		a.manager.CacheTpl(out.ID, &out)
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// TemplateSetDefault handles template modification.
func (a *App) TemplateSetDefault(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Update the template in the DB.
	id := getID(c)
	if _, err := a.core.RequireManageResource(access, "templates", id); err != nil {
		return err
	}
	if err := a.core.SetWorkspaceDefaultTemplate(id, access); err != nil {
		return err
	}

	return a.GetTemplates(c)
}

// DeleteTemplate handles template deletion.
func (a *App) DeleteTemplate(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Delete the template from the DB.
	id := getID(c)
	if _, err := a.core.RequireManageResource(access, "templates", id); err != nil {
		return err
	}
	if err := a.core.DeleteTemplate(id); err != nil {
		return err
	}

	// Delete cached in-memory template.
	a.manager.DeleteTpl(id)

	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) requireReadableMedia(access models.WorkspaceAccess, mediaIDs []int) error {
	for _, id := range mediaIDs {
		if id < 1 {
			continue
		}
		if _, err := a.core.RequireReadResource(access, "media", id); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) prepareTemplate(o models.Template) (models.Template, error) {
	if err := a.validateTemplate(o); err != nil {
		return o, err
	}

	// Subject is only relevant for fixed tx templates. For campaigns,
	// the subject changes per campaign and is on models.Campaign.
	var funcs template.FuncMap
	if o.Type == models.TemplateTypeCampaign || o.Type == models.TemplateTypeCampaignVisual {
		o.Subject = ""
		funcs = a.manager.TemplateFuncs(nil)
	} else {
		funcs = a.manager.GenericTemplateFuncs()
	}

	// Compile the template and validate.
	if err := o.Compile(funcs); err != nil {
		return o, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return o, nil
}

// compileTemplate validates template fields.
func (a *App) validateTemplate(o models.Template) error {
	if !strHasLen(o.Name, 1, stdInputMaxLen) {
		return errors.New(a.i18n.T("campaigns.fieldInvalidName"))
	}

	if o.Type == models.TemplateTypeCampaign && !regexpTplTag.MatchString(o.Body) {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("templates.placeholderHelp", "placeholder", tplTag))
	}

	if o.Type == models.TemplateTypeTx && strings.TrimSpace(o.Subject) == "" {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.missingFields", "name", "subject"))
	}

	return nil
}

// previewTemplate renders the HTML preview of a template.
func (a *App) previewTemplate(tpl models.Template) ([]byte, error) {
	var out []byte
	if tpl.Type == models.TemplateTypeCampaign || tpl.Type == models.TemplateTypeCampaignVisual {
		camp := models.Campaign{
			UUID:         dummyUUID,
			Name:         a.i18n.T("templates.dummyName"),
			Subject:      a.i18n.T("templates.dummySubject"),
			FromEmail:    "dummy-campaign@listmonk.app",
			TemplateBody: tpl.Body,
			Body:         dummyTpl,
		}

		if err := camp.CompileTemplate(a.manager.TemplateFuncs(&camp)); err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("templates.errorCompiling", "error", err.Error()))
		}

		// Render the message body.
		msg, err := a.manager.NewCampaignMessage(&camp, dummySubscriber)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("templates.errorRendering", "error", err.Error()))
		}
		out = msg.Body()
	} else {
		// Compile transactional template.
		if err := tpl.Compile(a.manager.GenericTemplateFuncs()); err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		m := models.TxMessage{
			Subject: tpl.Subject,
		}

		// Render the message.
		if err := m.Render(dummySubscriber, &tpl, a.manager.GenericTemplateFuncs()); err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		out = m.Body
	}

	return out, nil
}
