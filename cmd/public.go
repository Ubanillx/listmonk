package main

import (
	"bytes"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/captcha"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/internal/utils"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

const (
	tplMessage = "message"
)

// tplRenderer wraps a template.tplRenderer for echo.
type tplRenderer struct {
	templates           *template.Template
	SiteName            string
	RootURL             string
	LogoURL             string
	FaviconURL          string
	AssetVersion        string
	EnablePublicSubPage bool
	EnablePublicArchive bool
	IndividualTracking  bool
}

// tplData is the data container that is injected
// into public templates for accessing data.
type tplData struct {
	SiteName            string
	RootURL             string
	LogoURL             string
	FaviconURL          string
	AssetVersion        string
	EnablePublicSubPage bool
	EnablePublicArchive bool
	IndividualTracking  bool
	Data                any
	L                   *i18n.I18n
}

type publicTpl struct {
	Title       string
	Description string
}

type unsubTpl struct {
	publicTpl
	Customer         models.Customer
	Subscriptions    []models.Subscription
	SubUUID          string
	AllowBlocklist   bool
	AllowExport      bool
	AllowWipe        bool
	AllowPreferences bool
	ShowManage       bool
}

type optinReq struct {
	SubUUID           string
	CustomerListUUIDs []string              `query:"l" form:"l"`
	CustomerLists     []models.CustomerList `query:"-" form:"-"`
}

type optinTpl struct {
	publicTpl
	optinReq
}

type msgTpl struct {
	publicTpl
	MessageTitle string
	Message      string
}

type subFormTpl struct {
	publicTpl
	CustomerLists []models.CustomerList
	Captcha       struct {
		Enabled    bool
		Provider   string
		Key        string
		Complexity int
	}
}

var (
	pixelPNG = drawTransparentImage(3, 14)
)

// Render executes and renders a template for echo.
func (t *tplRenderer) Render(w io.Writer, name string, data any, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, tplData{
		SiteName:            t.SiteName,
		RootURL:             t.RootURL,
		LogoURL:             t.LogoURL,
		FaviconURL:          t.FaviconURL,
		AssetVersion:        t.AssetVersion,
		EnablePublicSubPage: t.EnablePublicSubPage,
		EnablePublicArchive: t.EnablePublicArchive,
		IndividualTracking:  t.IndividualTracking,
		Data:                data,
		L:                   c.Get("app").(*App).i18n,
	})
}

// GetPublicLists returns the customer_list of public customer_lists with minimal fields
// required to submit a subscription.
func (a *App) GetPublicLists(c echo.Context) error {
	// Get all active public customer_lists that still have an owning workspace.
	customer_lists, err := a.core.GetPublicSubscriptionLists(nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.errorFetchingLists"))
	}

	type customer_list struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}

	out := make([]customer_list, 0, len(customer_lists))
	for _, l := range customer_lists {
		out = append(out, customer_list{
			UUID: l.UUID,
			Name: l.Name,
		})
	}

	return c.JSON(http.StatusOK, out)
}

// ViewCampaignMessage renders the HTML view of a campaign message.
// This is the view the {{ MessageURL }} template tag links to in e-mail campaigns.
func (a *App) ViewCampaignMessage(c echo.Context) error {
	// Resolve the bearer URL as a single campaign-recipient relation. Do not
	// independently load two valid UUIDs, which would permit cross-workspace
	// message rendering.
	camp, sub, err := a.core.GetPublicCampaignMessage(c.Param("campUUID"), c.Param("subUUID"))
	if err != nil {
		if er, ok := err.(*echo.HTTPError); ok {
			if er.Code == http.StatusBadRequest || er.Code == http.StatusNotFound {
				return c.Render(http.StatusNotFound, tplMessage,
					makeMsgTpl(a.i18n.T("public.notFoundTitle"), "", a.i18n.T("public.campaignNotFound")))
			}
		}

		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingCampaign")))
	}

	// Compile the template.
	if err := camp.CompileTemplate(a.manager.TemplateFuncs(&camp)); err != nil {
		a.log.Printf("error compiling template: %v", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingCampaign")))
	}

	// Render the message body.
	msg, err := a.manager.NewCampaignMessage(&camp, sub)
	if err != nil {
		a.log.Printf("error rendering message: %v", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingCampaign")))
	}

	// The message body is user-authored HTML; isolate it from the application
	// origin so a campaign cannot run scripts against admin sessions.
	c.Response().Header().Set("Content-Security-Policy", cspSandbox)
	return c.HTML(http.StatusOK, string(msg.Body()))
}

// SubscriptionPage renders the subscription management page and handles unsubscriptions.
// This is the view that {{ UnsubscribeURL }} in campaigns link to.
func (a *App) SubscriptionPage(c echo.Context) error {
	var (
		campUUID      = c.Param("campUUID")
		subUUID       = c.Param("subUUID")
		showManage, _ = strconv.ParseBool(c.FormValue("manage"))
	)

	if campUUID != dummyUUID {
		if ok, err := a.core.IsPublicCampaignRecipient(campUUID, subUUID); err != nil {
			return c.Render(http.StatusInternalServerError, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
		} else if !ok {
			return c.Render(http.StatusNotFound, tplMessage,
				makeMsgTpl(a.i18n.T("public.notFoundTitle"), "", a.i18n.T("public.campaignNotFound")))
		}
	}

	// Get the customer from the legacy table or, for public-pool campaigns,
	// from the pool contact table. Pool contacts intentionally do not become
	// rows in customers; their unsubscribe token is still bound to the campaign
	// snapshot and organization segment.
	s, err := a.core.GetCustomer(0, subUUID, "")
	poolContact := false
	if err != nil {
		// Pool contacts are only valid bearer tokens when paired with a real
		// campaign recipient snapshot. Never allow the legacy dummy campaign
		// UUID to address a pool contact directly, since that would expose the
		// imported address outside the campaign/source-organization boundary.
		if campUUID == dummyUUID {
			return c.Render(http.StatusNotFound, tplMessage,
				makeMsgTpl(a.i18n.T("public.notFoundTitle"), "", a.i18n.T("public.campaignNotFound")))
		}
		p, poolErr := a.core.GetPoolContactByUUID(subUUID)
		if poolErr != nil {
			return c.Render(http.StatusInternalServerError, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
		}
		s = models.Customer{Email: p.Email, Name: p.Name, CustomerCode: p.CustomerCode, Status: models.CustomerStatusEnabled, UUID: p.UUID}
		poolContact = true
	}

	// Prepare the public template.
	out := unsubTpl{
		Customer:         s,
		SubUUID:          subUUID,
		publicTpl:        publicTpl{Title: a.i18n.T("public.unsubscribeTitle")},
		AllowBlocklist:   a.cfg.Privacy.AllowBlocklist,
		AllowExport:      a.cfg.Privacy.AllowExport,
		AllowWipe:        a.cfg.Privacy.AllowWipe,
		AllowPreferences: a.cfg.Privacy.AllowPreferences,
	}

	// If the customer is blocklisted, throw an error.
	if s.Status == models.CustomerStatusBlockListed {
		return c.Render(http.StatusOK, tplMessage, makeMsgTpl(a.i18n.T("public.noSubTitle"), "", a.i18n.Ts("public.blocklisted")))
	}

	// Only show preference management if it's enabled in settings.
	if a.cfg.Privacy.AllowPreferences && !poolContact {
		out.ShowManage = showManage

		// Get the customer's customer_lists from the DB to render in the template.
		subs, err := a.core.GetSubscriptions(0, subUUID, false)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.errorFetchingLists"))
		}

		out.Subscriptions = make([]models.Subscription, 0, len(subs))
		for _, s := range subs {
			// Private customer_lists shouldn't be rendered in the template.
			if s.Type == models.CustomerListTypePrivate {
				continue
			}

			out.Subscriptions = append(out.Subscriptions, s)
		}
	}

	return c.Render(http.StatusOK, "subscription", out)
}

// SubscriptionPrefs renders the subscription management page and
// s unsubscriptions. This is the view that {{ UnsubscribeURL }} in
// campaigns link to.
func (a *App) SubscriptionPrefs(c echo.Context) error {
	// Read the form.
	var req struct {
		Name              string   `form:"name" json:"name"`
		CustomerListUUIDs []string `form:"l" json:"list_uuids"`
		Blocklist         bool     `form:"blocklist" json:"blocklist"`
		Manage            bool     `form:"manage" json:"manage"`
	}
	if err := c.Bind(&req); err != nil {
		return c.Render(http.StatusBadRequest, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("globals.messages.invalidData")))
	}

	// Simple unsubscribe.
	var (
		campUUID  = c.Param("campUUID")
		subUUID   = c.Param("subUUID")
		blocklist = a.cfg.Privacy.AllowBlocklist && req.Blocklist
	)
	setAuditCustomerContext(c, 0, subUUID, map[string]any{
		"channel":     "public_self_service",
		"campaign_id": campUUID,
	})
	setAuditAction(c, "subscription.preferences_updated")
	if customer, lookupErr := a.core.GetCustomer(0, subUUID, ""); lookupErr == nil {
		organizationID := 0
		if customer.OrganizationID.Valid {
			organizationID = customer.OrganizationID.Int
		}
		setAuditCustomerContext(c, organizationID, customer.UUID, nil)
	} else if recipient, lookupErr := a.core.GetPublicPoolCampaignRecipient(campUUID, subUUID); lookupErr == nil && recipient.OrganizationID.Valid {
		setAuditCustomerContext(c, int(recipient.OrganizationID.Int), subUUID, nil)
	}
	if campUUID != dummyUUID {
		if ok, err := a.core.IsPublicCampaignRecipient(campUUID, subUUID); err != nil {
			return c.Render(http.StatusInternalServerError, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.errorProcessingRequest")))
		} else if !ok {
			return c.Render(http.StatusNotFound, tplMessage,
				makeMsgTpl(a.i18n.T("public.notFoundTitle"), "", a.i18n.T("public.campaignNotFound")))
		}
	}
	if !req.Manage || blocklist {
		setAuditAction(c, "subscription.unsubscribed")
		setAuditMetadata(c, map[string]any{"blocklist": blocklist})
		if campUUID == dummyUUID {
			// Opt-in e-mails use the long-standing dummy campaign UUID because
			// they are not sent by a campaign. Keep their self-service page
			// available while applying campaign-recipient validation everywhere
			// a real campaign UUID is supplied.
			if blocklist {
				sub, err := a.core.GetCustomer(0, subUUID, "")
				if err != nil {
					return c.Render(http.StatusNotFound, tplMessage,
						makeMsgTpl(a.i18n.T("public.notFoundTitle"), "", a.i18n.T("public.errorFetchingEmail")))
				}
				if err := a.core.BlocklistCustomers([]int{sub.ID}); err != nil {
					return c.Render(http.StatusInternalServerError, tplMessage,
						makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.errorProcessingRequest")))
				}
			}
			return c.Render(http.StatusOK, tplMessage,
				makeMsgTpl(a.i18n.T("public.unsubbedTitle"), "", a.i18n.T("public.unsubbedInfo")))
		}
		var unsubErr error
		if _, poolErr := a.core.GetPublicPoolCampaignRecipient(campUUID, subUUID); poolErr == nil {
			unsubErr = a.core.UnsubscribePoolByCampaign(campUUID, subUUID, "customer_unsubscribe")
		} else {
			unsubErr = a.core.UnsubscribeByCampaign(subUUID, campUUID, blocklist)
		}
		if unsubErr != nil {
			return c.Render(http.StatusInternalServerError, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.errorProcessingRequest")))
		}

		return c.Render(http.StatusOK, tplMessage,
			makeMsgTpl(a.i18n.T("public.unsubbedTitle"), "", a.i18n.T("public.unsubbedInfo")))
	}

	// Is preference management enabled?
	if !a.cfg.Privacy.AllowPreferences {
		return c.Render(http.StatusBadRequest, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.invalidFeature")))
	}

	// Manage preferences.
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 256 {
		return c.Render(http.StatusBadRequest, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("customers.invalidName")))
	}

	// Get the customer from the DB.
	sub, err := a.core.GetCustomer(0, subUUID, "")
	if err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("globals.messages.pFound",
				"name", a.i18n.T("globals.terms.customer"))))
	}
	sub.Name = req.Name

	// Update the customer properties in the DB.
	if _, err := a.core.UpdateCustomer(sub.ID, sub); err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.errorProcessingRequest")))
	}

	// Get the customer's customer_lists and whatever is not sent in the request (unchecked),
	// unsubscribe them.
	reqUUIDs := make(map[string]struct{})
	for _, u := range req.CustomerListUUIDs {
		reqUUIDs[u] = struct{}{}
	}

	// Get subscription from teh DB.
	subs, err := a.core.GetSubscriptions(0, subUUID, false)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.errorFetchingLists"))
	}

	// Filter the customer_lists in the request against the subscriptions in the DB.
	unsubUUIDs := make([]string, 0, len(req.CustomerListUUIDs))
	for _, s := range subs {
		if s.Type == models.CustomerListTypePrivate {
			continue
		}
		if _, ok := reqUUIDs[s.UUID]; !ok {
			unsubUUIDs = append(unsubUUIDs, s.UUID)
		}
	}

	// Unsubscribe from customer_lists.
	if err := a.core.UnsubscribeLists([]int{sub.ID}, nil, unsubUUIDs); err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.errorProcessingRequest")))

	}
	setAuditMetadata(c, map[string]any{
		"selected_list_count":     len(req.CustomerListUUIDs),
		"unsubscribed_list_count": len(unsubUUIDs),
	})

	return c.Render(http.StatusOK, tplMessage,
		makeMsgTpl(a.i18n.T("globals.messages.done"), "", a.i18n.T("public.prefsSaved")))
}

// OptinPage renders the double opt-in confirmation page that customers
// see when they click on the "Confirm subscription" button in double-optin
// notifications.
func (a *App) OptinPage(c echo.Context) error {
	var (
		subUUID    = c.Param("subUUID")
		confirm, _ = strconv.ParseBool(c.FormValue("confirm"))
		req        optinReq
	)
	if err := c.Bind(&req); err != nil {
		return err
	}

	// Validate customer_list UUIDs if there are incoming UUIDs in the request.
	if len(req.CustomerListUUIDs) > 0 {
		for _, l := range req.CustomerListUUIDs {
			if !reUUID.MatchString(l) {
				return c.Render(http.StatusBadRequest, tplMessage,
					makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("globals.messages.invalidUUID")))
			}
		}
	}

	// Get the customer_list of subscription customer_lists where the customer hasn't confirmed.
	customer_lists, err := a.core.GetCustomerListMemberships(0, subUUID, nil, req.CustomerListUUIDs, models.SubscriptionStatusUnconfirmed, "")
	if err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingLists")))
	}

	// There are no customer_lists to confirm.
	if len(customer_lists) == 0 {
		return c.Render(http.StatusOK, tplMessage,
			makeMsgTpl(a.i18n.T("public.noSubTitle"), "", a.i18n.Ts("public.noSubInfo")))
	}

	// Confirm.
	if confirm {
		setAuditAction(c, "subscription.optin_confirmed")
		setAuditMetadata(c, map[string]any{"list_count": len(customer_lists)})
		meta := models.JSON{}
		if a.cfg.Privacy.RecordOptinIP {
			if h := c.Request().Header.Get("X-Forwarded-For"); h != "" {
				meta["optin_ip"] = h
			} else if h := c.Request().RemoteAddr; h != "" {
				meta["optin_ip"] = strings.Split(h, ":")[0]
			}
		}

		// Confirm subscriptions in the DB.
		if err := a.core.ConfirmOptionSubscription(subUUID, req.CustomerListUUIDs, meta); err != nil {
			a.log.Printf("error unsubscribing: %v", err)
			return c.Render(http.StatusInternalServerError, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
		}

		return c.Render(http.StatusOK, tplMessage,
			makeMsgTpl(a.i18n.T("public.subConfirmedTitle"), "", a.i18n.Ts("public.subConfirmed")))
	}

	var out optinTpl
	out.CustomerLists = customer_lists
	out.SubUUID = subUUID
	out.Title = a.i18n.T("public.confirmOptinSubTitle")

	return c.Render(http.StatusOK, "optin", out)
}

// SubscriptionFormPage handles subscription requests coming from public
// HTML subscription forms.
func (a *App) SubscriptionFormPage(c echo.Context) error {
	if !a.cfg.EnablePublicSubPage {
		return c.Render(http.StatusNotFound, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.invalidFeature")))
	}

	// Get all active public customer_lists from the DB.
	customer_lists, err := a.core.GetPublicSubscriptionLists(nil)
	if err != nil {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorFetchingLists")))
	}

	// There are no public customer_lists available for subscription.
	if len(customer_lists) == 0 {
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.noListsAvailable")))
	}

	out := subFormTpl{}
	out.Title = a.i18n.T("public.sub")
	out.CustomerLists = customer_lists

	// Captcha configuration for template rendering.
	if a.cfg.Security.Captcha.Altcha.Enabled {
		out.Captcha.Enabled = true
		out.Captcha.Provider = "altcha"
		out.Captcha.Complexity = a.cfg.Security.Captcha.Altcha.Complexity
	} else if a.cfg.Security.Captcha.HCaptcha.Enabled {
		out.Captcha.Enabled = true
		out.Captcha.Provider = "hcaptcha"
		out.Captcha.Key = a.cfg.Security.Captcha.HCaptcha.Key
	}

	return c.Render(http.StatusOK, "subscription-form", out)
}

// SubscriptionForm handles subscription requests coming from public
// HTML subscription forms.
func (a *App) SubscriptionForm(c echo.Context) error {
	if !a.cfg.EnablePublicSubPage {
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.T("public.invalidFeature"))

	}

	// Reject bots and CAPTCHA failures before processing the form.
	if err := a.verifyPublicSubscriptionGuard(c); err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok {
			return c.Render(httpErr.Code, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", fmt.Sprintf("%s", httpErr.Message)))
		}

		return err
	}

	hasOptin, err := a.processSubForm(c)
	if err != nil {
		e, ok := err.(*echo.HTTPError)
		if !ok {
			return err
		}

		return c.Render(e.Code, tplMessage, makeMsgTpl(a.i18n.T("public.errorTitle"), "", fmt.Sprintf("%s", e.Message)))
	}

	// If there were double optin customer_lists, show the opt-in pending message instead of
	// the subscription confirmation message.
	msg := "public.subConfirmed"
	if hasOptin {
		msg = "public.subOptinPending"
	}

	return c.Render(http.StatusOK, tplMessage, makeMsgTpl(a.i18n.T("public.subTitle"), "", a.i18n.Ts(msg)))
}

// PublicSubscription handles subscription requests coming from public
// API calls.
func (a *App) PublicSubscription(c echo.Context) error {
	if !a.cfg.EnablePublicSubPage {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.invalidFeature"))
	}

	// The JSON API goes through the same honeypot and CAPTCHA gate as the HTML
	// form. Without this a script could bypass a CAPTCHA the administrator
	// explicitly enabled just by posting to the API instead of the form.
	if err := a.verifyPublicSubscriptionGuard(c); err != nil {
		return err
	}

	hasOptin, err := a.processSubForm(c)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{struct {
		HasOptin bool `json:"has_optin"`
	}{hasOptin}})
}

// verifyPublicSubscriptionGuard enforces the honeypot field and the configured
// CAPTCHA for any public subscription entry point. It returns an *echo.HTTPError
// whose message is safe to render on the HTML form.
func (a *App) verifyPublicSubscriptionGuard(c echo.Context) error {
	// If there's a nonce value, a bot could've filled the form.
	if c.FormValue("nonce") != "" {
		return echo.NewHTTPError(http.StatusBadGateway, a.i18n.T("public.invalidFeature"))
	}

	// No CAPTCHA configured: nothing further to verify.
	if !a.captcha.IsEnabled() {
		return nil
	}

	// Get the appropriate captcha response field based on provider.
	var val string
	switch a.captcha.GetProvider() {
	case captcha.ProviderHCaptcha:
		val = c.FormValue("h-captcha-response")
	case captcha.ProviderAltcha:
		val = c.FormValue("altcha")
	default:
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.invalidCaptcha"))
	}

	if val == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.invalidCaptcha"))
	}

	err, ok := a.captcha.Verify(val)
	if err != nil {
		a.log.Printf("captcha request failed: %v", err)
	}
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.invalidCaptcha"))
	}

	return nil
}

// LinkRedirect redirects a link UUID to its original underlying link
// after recording the link click for a particular customer in the particular
// campaign. These links are generated by {{ TrackLink }} tags or automatic
// tracking in campaigns.
func (a *App) LinkRedirect(c echo.Context) error {
	var (
		linkUUID = c.Param("linkUUID")
		campUUID = c.Param("campUUID")
	)

	// If tracking is globally disabled, resolve the URL without recording a click.
	if a.cfg.Privacy.DisableTracking {
		url, err := a.core.GetLinkURL(linkUUID)
		if err != nil {
			e := err.(*echo.HTTPError)
			return c.Render(e.Code, tplMessage, makeMsgTpl(a.i18n.T("public.errorTitle"), "", e.Error()))
		}
		return c.Redirect(http.StatusTemporaryRedirect, url)
	}

	// If individual tracking is disabled, do not record the customer ID.
	subUUID := c.Param("subUUID")
	if !a.cfg.Privacy.IndividualTracking {
		subUUID = ""
	}

	url, err := a.core.RegisterCampaignLinkClick(linkUUID, campUUID, subUUID)
	if err != nil {
		e := err.(*echo.HTTPError)
		return c.Render(e.Code, tplMessage, makeMsgTpl(a.i18n.T("public.errorTitle"), "", e.Error()))
	}

	return c.Redirect(http.StatusTemporaryRedirect, url)
}

// RegisterCampaignView registers a campaign view which comes in
// the form of an pixel image request. Regardless of errors, this handler
// should always render the pixel image bytes. The pixel URL is generated by
// the {{ TrackView }} template tag in campaigns.
func (a *App) RegisterCampaignView(c echo.Context) error {
	// If tracking is globally disabled, return the pixel without recording.
	if a.cfg.Privacy.DisableTracking {
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.Blob(http.StatusOK, "image/png", pixelPNG)
	}

	// If individual tracking is disabled, do not record the customer ID.
	subUUID := c.Param("subUUID")
	if !a.cfg.Privacy.IndividualTracking {
		subUUID = ""
	}

	// Exclude dummy hits from template previews.
	campUUID := c.Param("campUUID")
	if campUUID != dummyUUID && subUUID != dummyUUID {
		if err := a.core.RegisterCampaignView(campUUID, subUUID); err != nil {
			a.log.Printf("error registering campaign view: %s", err)
		}
	}

	c.Response().Header().Set("Cache-Control", "no-cache")
	return c.Blob(http.StatusOK, "image/png", pixelPNG)
}

// SelfExportCustomerData pulls the customer's profile, customer_list subscriptions,
// campaign views and clicks and produces a JSON report that is then e-mailed
// to the customer. This is a privacy feature and the data that's exported
// is dependent on the configuration.
func (a *App) SelfExportCustomerData(c echo.Context) error {
	setAuditAction(c, "customer.data_exported")
	setAuditMetadata(c, map[string]any{"channel": "public_self_service"})
	// Is export allowed?
	if !a.cfg.Privacy.AllowExport {
		return c.Render(http.StatusBadRequest, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.invalidFeature")))
	}

	// Get the customer's data. A single query that gets the profile,
	// customer_list subscriptions, campaign views, and link clicks. Names of
	// private customer_lists are replaced with "Private customer_list".
	subUUID := c.Param("subUUID")
	data, b, err := a.exportCustomerData(0, subUUID, a.cfg.Privacy.Exportable)
	if err != nil {
		a.log.Printf("error exporting customer data: %s", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
	}

	// Prepare the attachment e-mail.
	var msg bytes.Buffer
	if err := notifs.Tpls.ExecuteTemplate(&msg, notifs.TplCustomerData, data); err != nil {
		a.log.Printf("error compiling notification template '%s': %v", notifs.TplCustomerData, err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
	}

	subject, body := utils.GetTplSubject(a.i18n.Ts("email.data.title"), msg.Bytes())

	// E-mail the data as a JSON attachment to the customer.
	const fname = "data.json"
	if err := a.emailMsgr.Push(models.Message{
		From:    a.emailMsgr.DefaultFromEmail(),
		To:      []string{data.Email},
		Subject: subject,
		Body:    body,
		Attachments: []models.Attachment{
			{
				Name:    fname,
				Content: b,
				Header:  manager.MakeAttachmentHeader(fname, "base64", "application/json"),
			},
		},
	}); err != nil {
		a.log.Printf("error e-mailing customer profile: %s", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
	}

	return c.Render(http.StatusOK, tplMessage,
		makeMsgTpl(a.i18n.T("public.dataSentTitle"), "", a.i18n.T("public.dataSent")))
}

// WipeCustomerData allows a customer to delete their data. The
// profile and subscriptions are deleted, while the campaign_views and link
// clicks remain as orphan data unconnected to any customer.
func (a *App) WipeCustomerData(c echo.Context) error {
	setAuditAction(c, "customer.data_erased")
	setAuditMetadata(c, map[string]any{"channel": "public_self_service"})
	// Is wiping allowed?
	if !a.cfg.Privacy.AllowWipe {
		return c.Render(http.StatusBadRequest, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.invalidFeature")))
	}

	subUUID := c.Param("subUUID")
	if err := a.core.DeleteCustomers(nil, []string{subUUID}); err != nil {
		a.log.Printf("error wiping customer data: %s", err)
		return c.Render(http.StatusInternalServerError, tplMessage,
			makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.Ts("public.errorProcessingRequest")))
	}

	return c.Render(http.StatusOK, tplMessage,
		makeMsgTpl(a.i18n.T("public.dataRemovedTitle"), "", a.i18n.T("public.dataRemoved")))
}

// AltchaChallenge generates a challenge for Altcha captcha.
func (a *App) AltchaChallenge(c echo.Context) error {
	// Check if Altcha is enabled.
	if !a.captcha.IsEnabled() || a.captcha.GetProvider() != captcha.ProviderAltcha {
		return echo.NewHTTPError(http.StatusNotFound, "captcha not enabled")
	}

	// Generate challenge.
	out, err := a.captcha.GenerateChallenge()
	if err != nil {
		a.log.Printf("error generating altcha challenge: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Error generating challenge")
	}

	// Return the challenge as JSON.
	c.Response().Header().Set("Content-Type", "application/json")
	return c.String(http.StatusOK, out)
}

// drawTransparentImage draws a transparent PNG of given dimensions
// and returns the PNG bytes.
func drawTransparentImage(h, w int) []byte {
	var (
		img = image.NewRGBA(image.Rect(0, 0, w, h))
		out = &bytes.Buffer{}
	)
	_ = png.Encode(out, img)

	return out.Bytes()
}

// processSubForm processes an incoming form/public API subscription request.
// The bool indicates whether there was subscription to an optin customer_list so that
// an appropriate message can be shown.
func (a *App) processSubForm(c echo.Context) (bool, error) {
	// Get and validate fields.
	var req struct {
		Name          string   `form:"name" json:"name"`
		Email         string   `form:"email" json:"email"`
		FormListUUIDs []string `form:"l" json:"list_uuids"`
	}
	if err := c.Bind(&req); err != nil {
		return false, err
	}

	if len(req.FormListUUIDs) == 0 {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("public.noListsSelected"))
	}

	// Validate fields.
	if len(req.Email) > 1000 {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidEmail"))
	}

	em, err := a.importer.SanitizeEmail(req.Email)
	if err != nil {
		return false, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	req.Email = em

	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) == 0 {
		// If there's no name, use the name bit from the e-mail.
		req.Name = strings.Split(req.Email, "@")[0]
	} else if len(req.Name) > stdInputMaxLen {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidName"))
	}

	seen := make(map[string]struct{}, len(req.FormListUUIDs))
	for _, listUUID := range req.FormListUUIDs {
		if !reUUID.MatchString(listUUID) {
			return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidUUID"))
		}
		seen[listUUID] = struct{}{}
	}
	if len(seen) != len(req.FormListUUIDs) {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidUUID"))
	}

	customer_lists, err := a.core.GetPublicSubscriptionLists(req.FormListUUIDs)
	if err != nil {
		return false, err
	}
	if len(customer_lists) != len(req.FormListUUIDs) {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidUUID"))
	}
	access, err := publicSubscriptionWorkspace(customer_lists)
	if err != nil {
		return false, err
	}
	setAuditCustomerContext(c, access.OrganizationID, "", map[string]any{
		"channel":             "public_subscription",
		"selected_list_count": len(customer_lists),
	})
	customerListIDs := make([]int, 0, len(customer_lists))
	for _, customer_list := range customer_lists {
		customerListIDs = append(customerListIDs, customer_list.ID)
	}

	// Insert or reuse a customer inside the owning user/workspace boundary.
	customer, hasOptin, err := a.core.UpsertPublicWorkspaceCustomer(access, models.Customer{
		Name:   req.Name,
		Email:  req.Email,
		Status: models.CustomerStatusEnabled,
	}, customerListIDs)
	if err != nil {
		if e, ok := err.(*echo.HTTPError); ok {
			return false, echo.NewHTTPError(e.Code, fmt.Sprintf("%s", e.Message))
		}
		return false, echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("public.errorProcessingRequest"))
	}
	setAuditCustomerContext(c, access.OrganizationID, customer.UUID, map[string]any{
		"has_optin": hasOptin,
	})
	return hasOptin, nil
}

// publicSubscriptionWorkspace derives the only valid scope for an
// unauthenticated subscription request. A browser may choose several public
// customer_lists, but they must all be owned by the same user in the same workspace.
func publicSubscriptionWorkspace(customer_lists []models.CustomerList) (models.WorkspaceAccess, error) {
	if len(customer_lists) == 0 || !customer_lists[0].OwnerUserID.Valid {
		return models.WorkspaceAccess{}, echo.NewHTTPError(http.StatusBadRequest, "selected public customer_list has no owner")
	}
	first := customer_lists[0]
	ownerID := int(first.OwnerUserID.Int)
	organizationID := 0
	if first.OrganizationID.Valid {
		organizationID = int(first.OrganizationID.Int)
	}
	for _, customer_list := range customer_lists[1:] {
		listOrganizationID := 0
		if customer_list.OrganizationID.Valid {
			listOrganizationID = int(customer_list.OrganizationID.Int)
		}
		if !customer_list.OwnerUserID.Valid || int(customer_list.OwnerUserID.Int) != ownerID || listOrganizationID != organizationID {
			return models.WorkspaceAccess{}, echo.NewHTTPError(http.StatusBadRequest, "selected public customer_lists belong to different workspaces")
		}
	}
	return models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: organizationID, Personal: organizationID == 0},
		UserID:    ownerID,
	}, nil
}
