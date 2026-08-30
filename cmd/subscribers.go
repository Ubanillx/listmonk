package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

const (
	dummyUUID = "00000000-0000-0000-0000-000000000000"
)

// subQueryReq is a "catch all" struct for reading various
// subscriber related requests.
type subQueryReq struct {
	Search             string `json:"search"`
	Query              string `json:"query"`
	ListIDs            []int  `json:"list_ids"`
	TargetListIDs      []int  `json:"target_list_ids"`
	SubscriberIDs      []int  `json:"ids"`
	Action             string `json:"action"`
	Status             string `json:"status"`
	SubscriptionStatus string `json:"subscription_status"`
	All                bool   `json:"all"`
}

// subOptin contains the data that's passed to the double opt-in e-mail template.
type subOptin struct {
	models.Subscriber

	OptinURL string
	UnsubURL string
	Lists    []models.List
}

var (
	dummySubscriber = models.Subscriber{
		Email:   "demo@listmonk.app",
		Name:    "Demo Subscriber",
		UUID:    dummyUUID,
		Attribs: models.JSON{"city": "Bengaluru"},
	}
)

// GetSubscriber handles the retrieval of a single subscriber by ID.
func (a *App) GetSubscriber(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireReadableWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}

	// Fetch the subscriber from the active workspace.  The legacy lookup also
	// loads every list relation for the ID and could therefore leak a
	// cross-workspace association to an organization manager.
	out, err := a.core.GetWorkspaceSubscriber(access, id)
	if err != nil {
		return err
	}
	// Organization managers are explicitly allowed to inspect the complete
	// subscriber and list relationship for members of the active organization.
	// This is read-only at the HTTP/Core mutation boundary (edit/delete/import/
	// export/bulk operations still require ownership), but the detail and list
	// views must include the recipient identity and attributes so managers can
	// administer the organization's audiences and understand ownership.
	a.redactWorkspaceSubscriberSensitiveFields(access, &out)

	return c.JSON(http.StatusOK, okResp{out})
}

// GetSubscriberActivity handles the retrieval of a subscriber's campaign views and link clicks.
func (a *App) GetSubscriberActivity(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireReadableWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}

	// Fetch activity through the workspace-scoped query.  Organization managers
	// may inspect member statistics, but unrelated campaign rows must not be
	// returned for a forged subscriber ID or relation.
	out, err := a.core.GetWorkspaceSubscriberActivity(access, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// QuerySubscribers handles querying subscribers based on an arbitrary SQL expression.
func (a *App) QuerySubscribers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if !access.IsOrganizationManager() && !user.IsPlatformAdmin() {
		if err := requireLegacyPermission(user, auth.PermSubscribersGetAll, auth.PermSubscribersGet); err != nil {
			return err
		}
	}
	listIDs, err := a.workspaceListIDsForRequest(c, access, "list_id", c.QueryParams(), false)
	if err != nil {
		return err
	}

	query := formatSQLExp(c.FormValue("query"))
	if query != "" {
		if err := a.requireSubscriberSQLQuery(c); err != nil {
			return err
		}
	}

	var (
		searchStr = strings.TrimSpace(c.FormValue("search"))
		subStatus = c.FormValue("subscription_status")
		order     = c.FormValue("order")
		orderBy   = c.FormValue("order_by")
		pg        = a.pg.NewFromURL(c.Request().URL.Query())
	)

	// Advanced expressions retain their dedicated permission but always run
	// inside the selected workspace and owner boundary.
	var (
		res   models.Subscribers
		total int
	)
	if query != "" {
		res, total, err = a.core.QueryWorkspaceSubscribersWithSQL(access, searchStr, query, listIDs, subStatus, order, orderBy, pg.Offset, pg.Limit)
	} else {
		res, total, err = a.core.QueryWorkspaceSubscribers(access, searchStr, listIDs, subStatus, order, orderBy, pg.Offset, pg.Limit)
	}
	if err != nil {
		return err
	}
	for i := range res {
		a.redactWorkspaceSubscriberSensitiveFields(access, &res[i])
	}

	out := models.PageResults{
		Query:   query,
		Search:  searchStr,
		Results: res,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// redactWorkspaceSubscriberSensitiveFields is retained for callers that may
// fetch a subscriber through a non-workspace path. In the active organization,
// managers have the documented read-only right to see member subscriber
// details, including e-mail, attributes, and list memberships. They still
// cannot mutate or export those rows: the corresponding handlers use the
// stricter managed/export predicates before reaching this response layer.
func (a *App) redactWorkspaceSubscriberSensitiveFields(access models.WorkspaceAccess, sub *models.Subscriber) {
	if sub == nil || a.core.CanSeeSensitiveResource(access, sub.ResourceScope) ||
		(access.IsOrganizationManager() && access.IsOrganization() &&
			sub.OrganizationID.Valid && int(sub.OrganizationID.Int) == access.OrganizationID &&
			!sub.OrganizationArchived) {
		return
	}
	sub.Email = ""
	sub.UUID = ""
	sub.Attribs = models.JSON{}
	sub.Lists = []byte("[]")
}

// ExportSubscribers handles querying subscribers based on an arbitrary SQL expression.
func (a *App) ExportSubscribers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermSubscribersGetAll, auth.PermSubscribersGet); err != nil {
		return err
	}
	listIDs, err := a.workspaceExportListIDsForRequest(c, access, "list_id", c.QueryParams())
	if err != nil {
		return err
	}

	// Export only specific subscriber IDs?
	subIDs, err := getQueryInts("id", c.QueryParams())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
	}

	// Filter by subscription status
	subStatus := c.QueryParam("subscription_status")

	var (
		searchStr = strings.TrimSpace(c.FormValue("search"))
		query     = formatSQLExp(c.FormValue("query"))
	)
	if query != "" {
		if err := a.requireSubscriberSQLQuery(c); err != nil {
			return err
		}
	}

	var exp func() ([]models.SubscriberExport, error)
	if len(subIDs) > 0 {
		for _, id := range subIDs {
			if _, err := a.requireExportableWorkspaceSubscriber(c, access, id); err != nil {
				return err
			}
		}
	} else {
		subIDs, err = a.core.ListManagedWorkspaceResources(access, resourceSubscribers)
		if err != nil {
			return err
		}
	}
	// An empty explicit set must not mean "all" in the lower-level query.
	if len(subIDs) == 0 {
		subIDs = []int{-1}
	}
	if query != "" {
		exp, err = a.core.ExportWorkspaceSubscribersWithSQL(access, searchStr, query, listIDs, subIDs, subStatus, a.cfg.DBBatchSize)
	} else {
		exp, err = a.core.ExportWorkspaceSubscribers(access, searchStr, listIDs, subIDs, subStatus, a.cfg.DBBatchSize)
	}
	if err != nil {
		return err
	}

	var (
		hdr = c.Response().Header()
		wr  = csv.NewWriter(c.Response())
	)

	hdr.Set(echo.HeaderContentType, echo.MIMEOctetStream)
	hdr.Set("Content-type", "text/csv")
	hdr.Set(echo.HeaderContentDisposition, "attachment; filename="+"subscribers.csv")
	hdr.Set("Content-Transfer-Encoding", "binary")
	hdr.Set("Cache-Control", "no-cache")
	wr.Write([]string{"uuid", "email", "name", "attributes", "status", "created_at", "updated_at"})

loop:
	// Iterate in batches until there are no more subscribers to export.
	for {
		out, err := exp()
		if err != nil {
			return err
		}
		if len(out) == 0 {
			break
		}

		for _, r := range out {
			if err = wr.Write([]string{r.UUID, r.Email, r.Name, r.Attribs, r.Status,
				r.CreatedAt.Time.String(), r.UpdatedAt.Time.String()}); err != nil {
				a.log.Printf("error streaming CSV export: %v", err)
				break loop
			}
		}

		// Flush CSV to stream after each batch.
		wr.Flush()
	}

	return nil
}

// CreateSubscriber handles the creation of a new subscriber.
func (a *App) CreateSubscriber(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermSubscribersManage); err != nil {
		return err
	}

	// Get and validate fields.
	var req subimporter.SubReq
	if err := c.Bind(&req); err != nil {
		return err
	}

	// Validate fields.
	req, err = a.importer.ValidateFields(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := a.requireWorkspaceListIDsForRequest(c, access, req.Lists, true); err != nil {
		return err
	}

	// Insert the subscriber into the active user/workspace boundary.
	sub, _, err := a.core.InsertWorkspaceSubscriber(access, req.Subscriber, req.Lists, req.PreconfirmSubs, false)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{sub})
}

// UpdateSubscriber handles modification of a subscriber.
func (a *App) UpdateSubscriber(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}

	// Get and validate fields.
	req := struct {
		models.Subscriber
		Lists          []int `json:"lists"`
		PreconfirmSubs bool  `json:"preconfirm_subscriptions"`
	}{}
	if err := c.Bind(&req); err != nil {
		return err
	}

	// Sanitize and validate the email field.
	if em, err := a.importer.SanitizeEmail(req.Email); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	} else {
		req.Email = em
	}

	if req.Name != "" && !strHasLen(req.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}

	if err := a.requireWorkspaceListIDsForRequest(c, access, req.Lists, true); err != nil {
		return err
	}

	permittedLists, err := a.managedWorkspaceLegacyListIDs(c, access)
	if err != nil {
		return err
	}
	req.Subscriber.ID = id

	out, _, err := a.core.UpdateSubscriberWithListsInWorkspace(access, id, req.Subscriber, req.Lists, req.PreconfirmSubs, true, false, permittedLists)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// SubscriberSendOptin sends an optin confirmation e-mail to a subscriber.
func (a *App) SubscriberSendOptin(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Fetch the subscriber.
	id := getID(c)
	if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}
	out, err := a.core.GetWorkspaceSubscriber(access, id)
	if err != nil {
		return err
	}

	// Trigger the opt-in confirmation e-mail hook.
	if _, err := a.fnOptinNotify(out, nil); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("subscribers.errorSendingOptin"))
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// BlocklistSubscriber handles the blocklisting of a given subscriber.
func (a *App) BlocklistSubscriber(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}
	if err := a.core.BlocklistSubscribersInWorkspace(access, []int{id}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// BlocklistSubscribers handles the blocklisting of one or more subscribers.
func (a *App) BlocklistSubscribers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}
	if len(req.SubscriberIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", "ids"))
	}
	for _, id := range req.SubscriberIDs {
		if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
			return err
		}
	}

	// Update the subscribers in the DB.
	if err := a.core.BlocklistSubscribersInWorkspace(access, req.SubscriberIDs); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ManageSubscriberLists handles bulk addition or removal of subscribers
// from or to one or more target lists.
// It takes either an ID in the URI, or a list of IDs in the request body.
func (a *App) ManageSubscriberLists(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}

	// Is it an /:id call?
	var (
		pID    = c.Param("id")
		subIDs []int
	)
	if pID != "" {
		id, _ := strconv.Atoi(pID)
		if id < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
		subIDs = append(subIDs, id)
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}
	if len(req.SubscriberIDs) == 0 && len(subIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.errorNoIDs"))
	}
	if len(subIDs) == 0 {
		subIDs = req.SubscriberIDs
	}
	if len(req.TargetListIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.errorNoListsGiven"))
	}

	for _, id := range subIDs {
		if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
			return err
		}
	}
	if err := a.requireWorkspaceListIDsForRequest(c, access, req.TargetListIDs, true); err != nil {
		return err
	}

	// Run the action in the DB.
	switch req.Action {
	case "add":
		err = a.core.AddSubscriptionsInWorkspace(access, subIDs, req.TargetListIDs, req.Status)
	case "remove":
		err = a.core.DeleteSubscriptionsInWorkspace(access, subIDs, req.TargetListIDs)
	case "unsubscribe":
		err = a.core.UnsubscribeListsInWorkspace(access, subIDs, req.TargetListIDs)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidAction"))
	}

	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteSubscriber handles deletion of a single subscriber.
func (a *App) DeleteSubscriber(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}
	if err := a.core.DeleteSubscribersInWorkspace(access, []int{id}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteSubscribers handles bulk deletion of one or more subscribers.
func (a *App) DeleteSubscribers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Multiple IDs.
	ids, err := parseStringIDs(c.Request().URL.Query()["id"])
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", "ids"))
	}
	for _, id := range ids {
		if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
			return err
		}
	}

	// Delete the subscribers from the DB.
	if err := a.core.DeleteSubscribersInWorkspace(access, ids); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteSubscribersByQuery bulk deletes based on an
// arbitrary SQL expression.
func (a *App) DeleteSubscribersByQuery(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermSubscribersManage); err != nil {
		return err
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return err
	}

	req.Search = strings.TrimSpace(req.Search)
	req.Query = formatSQLExp(req.Query)
	if req.All {
		// If the "all" flag is set, ignore any subquery that may be present.
		req.Search = ""
		req.Query = ""
	} else if req.Search == "" && req.Query == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "query"))
	}

	listIDs, err := a.workspaceManagedListIDsForRequest(c, access, req.ListIDs)
	if err != nil {
		return err
	}
	req.ListIDs = listIDs
	if req.Query != "" {
		if err := a.requireSubscriberSQLQuery(c); err != nil {
			return err
		}
		ids, err := a.managedWorkspaceSubscriberIDsWithSQL(access, req.Search, req.Query, req.ListIDs, req.SubscriptionStatus)
		if err != nil {
			return err
		}
		if err := a.core.DeleteSubscribersInWorkspace(access, ids); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, okResp{true})
	}

	ids, err := a.managedWorkspaceSubscriberIDs(access, req.Search, req.ListIDs, req.SubscriptionStatus)
	if err != nil {
		return err
	}
	if err := a.core.DeleteSubscribersInWorkspace(access, ids); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// BlocklistSubscribersByQuery bulk blocklists subscribers
// based on an arbitrary SQL expression.
func (a *App) BlocklistSubscribersByQuery(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermSubscribersManage); err != nil {
		return err
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return err
	}

	req.Search = strings.TrimSpace(req.Search)
	req.Query = formatSQLExp(req.Query)
	if req.All {
		// If the "all" flag is set, ignore any subquery that may be present.
		req.Search = ""
		req.Query = ""
	} else if req.Search == "" && req.Query == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "query"))
	}
	listIDs, err := a.workspaceManagedListIDsForRequest(c, access, req.ListIDs)
	if err != nil {
		return err
	}
	req.ListIDs = listIDs
	if req.Query != "" {
		if err := a.requireSubscriberSQLQuery(c); err != nil {
			return err
		}
		ids, err := a.managedWorkspaceSubscriberIDsWithSQL(access, req.Search, req.Query, req.ListIDs, req.SubscriptionStatus)
		if err != nil {
			return err
		}
		if err := a.core.BlocklistSubscribersInWorkspace(access, ids); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, okResp{true})
	}

	ids, err := a.managedWorkspaceSubscriberIDs(access, req.Search, req.ListIDs, req.SubscriptionStatus)
	if err != nil {
		return err
	}
	if err := a.core.BlocklistSubscribersInWorkspace(access, ids); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ManageSubscriberListsByQuery bulk adds/removes/unsubscribes subscribers
// from one or more lists based on an arbitrary SQL expression.
func (a *App) ManageSubscriberListsByQuery(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermSubscribersManage); err != nil {
		return err
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.TargetListIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.T("subscribers.errorNoListsGiven"))
	}

	req.Search = strings.TrimSpace(req.Search)
	req.Query = formatSQLExp(req.Query)

	listIDs, err := a.workspaceManagedListIDsForRequest(c, access, req.ListIDs)
	if err != nil {
		return err
	}
	req.ListIDs = listIDs
	if err := a.requireWorkspaceListIDsForRequest(c, access, req.TargetListIDs, true); err != nil {
		return err
	}
	if req.Query != "" {
		if err := a.requireSubscriberSQLQuery(c); err != nil {
			return err
		}
		subIDs, err := a.managedWorkspaceSubscriberIDsWithSQL(access, req.Search, req.Query, req.ListIDs, req.SubscriptionStatus)
		if err != nil {
			return err
		}
		var runErr error
		switch req.Action {
		case "add":
			runErr = a.core.AddSubscriptionsInWorkspace(access, subIDs, req.TargetListIDs, req.Status)
		case "remove":
			runErr = a.core.DeleteSubscriptionsInWorkspace(access, subIDs, req.TargetListIDs)
		case "unsubscribe":
			runErr = a.core.UnsubscribeListsInWorkspace(access, subIDs, req.TargetListIDs)
		default:
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidAction"))
		}
		if runErr != nil {
			return runErr
		}
		return c.JSON(http.StatusOK, okResp{true})
	}

	subIDs, err := a.managedWorkspaceSubscriberIDs(access, req.Search, req.ListIDs, req.SubscriptionStatus)
	if err != nil {
		return err
	}

	// Run the action in the DB.
	var runErr error
	switch req.Action {
	case "add":
		runErr = a.core.AddSubscriptionsInWorkspace(access, subIDs, req.TargetListIDs, req.Status)
	case "remove":
		runErr = a.core.DeleteSubscriptionsInWorkspace(access, subIDs, req.TargetListIDs)
	case "unsubscribe":
		runErr = a.core.UnsubscribeListsInWorkspace(access, subIDs, req.TargetListIDs)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidAction"))
	}

	if runErr != nil {
		return runErr
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteSubscriberBounces deletes all the bounces on a subscriber.
func (a *App) DeleteSubscriberBounces(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Delete the bounces from the DB.
	id := getID(c)
	if _, err := a.requireManagedWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}
	if err := a.core.DeleteSubscriberBouncesInWorkspace(access, id); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ExportSubscriberData pulls the subscriber's profile,
// list subscriptions, campaign views and clicks and produces
// a JSON report. This is a privacy feature and depends on the
// configuration in a.Constants.Privacy.
func (a *App) ExportSubscriberData(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Get the subscriber's data. A single query that gets the profile,
	// list subscriptions, campaign views, and link clicks. Names of
	// private lists are replaced with "Private list".
	id := getID(c)
	if _, err := a.requireExportableWorkspaceSubscriber(c, access, id); err != nil {
		return err
	}
	_, b, err := a.exportWorkspaceSubscriberData(access, id, a.cfg.Privacy.Exportable)
	if err != nil {
		a.log.Printf("error exporting subscriber data: %s", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.subscribers}", "error", err.Error()))
	}

	// Set headers to force the browser to prompt for download.
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="data.json"`)
	return c.Blob(http.StatusOK, "application/json", b)
}

// exportWorkspaceSubscriberData is the authenticated counterpart to the
// bearer-token privacy export helper below.  It deliberately uses the
// workspace-aware Core query so an organization manager (who may inspect a
// member's record) cannot export that member's personal audience, while an
// owner or platform administrator can still export the rows they manage.
func (a *App) exportWorkspaceSubscriberData(access models.WorkspaceAccess, id int, exportables map[string]bool) (models.SubscriberExportProfile, []byte, error) {
	data, err := a.core.GetWorkspaceSubscriberProfileForExport(access, id)
	if err != nil {
		return data, nil, err
	}

	filterSubscriberExportables(&data, exportables)
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		a.log.Printf("error marshalling subscriber export data: %v", err)
		return data, nil, err
	}
	return data, b, nil
}

// exportSubscriberData collates the data of a subscriber including profile,
// subscriptions, campaign_views, link_clicks (if they're enabled in the config)
// and returns a formatted, indented JSON payload. Either takes a numeric id
// and an empty subUUID or takes 0 and a string subUUID.
func (a *App) exportSubscriberData(id int, subUUID string, exportables map[string]bool) (models.SubscriberExportProfile, []byte, error) {
	data, err := a.core.GetSubscriberProfileForExport(id, subUUID)
	if err != nil {
		return data, nil, err
	}

	filterSubscriberExportables(&data, exportables)

	// Marshal the data into an indented payload.
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		a.log.Printf("error marshalling subscriber export data: %v", err)
		return data, nil, err
	}

	return data, b, nil
}

func filterSubscriberExportables(data *models.SubscriberExportProfile, exportables map[string]bool) {
	if data == nil {
		return
	}
	if _, ok := exportables["profile"]; !ok {
		data.Profile = nil
	}
	if _, ok := exportables["subscriptions"]; !ok {
		data.Subscriptions = nil
	}
	if _, ok := exportables["campaign_views"]; !ok {
		data.CampaignViews = nil
	}
	if _, ok := exportables["link_clicks"]; !ok {
		data.LinkClicks = nil
	}
}

// hasSubPerm checks whether the current user has permission to access the given list
// of subscriber IDs.
func (a *App) hasSubPerm(access models.WorkspaceAccess, u auth.User, subIDs []int) error {
	allPerm, listIDs := legacyReadableListIDs(u)

	// User has blanket get_all|manage_all permission.
	if allPerm {
		return nil
	}

	// Check whether the subscribers have the list IDs permitted to the user.
	res, err := a.core.HasSubscriberListsInWorkspace(access, subIDs, listIDs)
	if err != nil {
		return err
	}

	for id, has := range res {
		if !has {
			return echo.NewHTTPError(http.StatusForbidden, a.i18n.Ts("globals.messages.permissionDenied", "name", fmt.Sprintf("subscriber: %d", id)))
		}
	}

	return nil
}

func (a *App) hasManagedSubPerm(access models.WorkspaceAccess, u auth.User, subIDs []int) error {
	if u.IsPlatformAdmin() || u.HasPerm(auth.PermListManageAll) {
		return nil
	}
	if len(u.ManageListIDs) == 0 {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied: list:manage")
	}
	res, err := a.core.HasSubscriberListsInWorkspace(access, subIDs, u.ManageListIDs)
	if err != nil {
		return err
	}
	for id, has := range res {
		if !has {
			return echo.NewHTTPError(http.StatusForbidden,
				a.i18n.Ts("globals.messages.permissionDenied", "name", fmt.Sprintf("subscriber: %d", id)))
		}
	}
	return nil
}

// filterListQueryByPerm filters the list IDs in the query params and returns the list IDs to which the user has access.
func (a *App) filterListQueryByPerm(param string, qp url.Values, user auth.User) ([]int, error) {
	var listIDs []int

	// If there are incoming list query params, filter them by permission.
	if qp.Has(param) {
		ids, err := getQueryInts(param, qp)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}

		listIDs = user.FilterListsByPerm(auth.PermTypeGet|auth.PermTypeManage, ids)
	}

	// There are no incoming params. If the user doesn't have permission to get all subscribers,
	// filter by the lists they have access to.
	if len(listIDs) == 0 {
		if _, ok := user.PermissionsMap[auth.PermSubscribersGetAll]; !ok {
			if len(user.GetListIDs) > 0 {
				listIDs = user.GetListIDs
			} else {
				// User doesn't have access to any lists.
				listIDs = []int{-1}
			}
		}
	}

	return listIDs, nil
}

// workspaceListIDs resolves optional list query parameters and verifies that
// they belong to the selected workspace. List membership from the legacy role
// system is intentionally not considered here: it must never cross an owner
// or organization boundary.
func (a *App) workspaceListIDs(access models.WorkspaceAccess, param string, qp url.Values, manage bool) ([]int, error) {
	return a.workspaceListIDsForRequest(nil, access, param, qp, manage)
}

func (a *App) workspaceListIDsForRequest(c echo.Context, access models.WorkspaceAccess, param string, qp url.Values, manage bool) ([]int, error) {
	if !qp.Has(param) {
		if c == nil {
			return []int{}, nil
		}
		if manage {
			return a.workspaceManagedListIDsForRequest(c, access, nil)
		}
		user := auth.GetUser(c)
		if access.IsOrganizationManager() || user.IsPlatformAdmin() {
			return []int{}, nil
		}
		hasAll, permitted := legacyReadableSubscriberListIDs(user)
		if hasAll {
			return []int{}, nil
		}
		if len(permitted) == 0 {
			return []int{-1}, nil
		}
		return permitted, nil
	}
	ids, err := getQueryInts(param, qp)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
	}
	if err := a.requireWorkspaceListIDsForRequest(c, access, ids, manage); err != nil {
		return nil, err
	}
	return ids, nil
}

// workspaceExportListIDsForRequest intentionally does not use the
// organization-manager inspection exception. CSV export is sensitive data and
// therefore remains subject to the caller's pre-existing list grants as well
// as the owner boundary enforced by the export query.
func (a *App) workspaceExportListIDsForRequest(c echo.Context, access models.WorkspaceAccess, param string, qp url.Values) ([]int, error) {
	user := auth.GetUser(c)
	if !qp.Has(param) {
		hasAll, permitted := legacyReadableSubscriberListIDs(user)
		if hasAll {
			return []int{}, nil
		}
		if len(permitted) == 0 {
			return []int{-1}, nil
		}
		return permitted, nil
	}
	ids, err := getQueryInts(param, qp)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
	}
	for _, id := range ids {
		if id < 1 {
			return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
		if _, err := a.core.RequireReadResource(access, resourceLists, id); err != nil {
			return nil, err
		}
		if err := requireLegacyListPermission(user, id, false); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (a *App) requireWorkspaceListIDs(access models.WorkspaceAccess, ids []int, manage bool) error {
	return a.requireWorkspaceListIDsForRequest(nil, access, ids, manage)
}

func (a *App) requireWorkspaceListIDsForRequest(c echo.Context, access models.WorkspaceAccess, ids []int, manage bool) error {
	for _, id := range ids {
		if id < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
		if c != nil {
			if manage {
				if _, err := a.requireManagedWorkspaceList(c, access, id); err != nil {
					return err
				}
			} else if _, err := a.requireReadableWorkspaceList(c, access, id); err != nil {
				return err
			}
			continue
		}
		if manage {
			if _, err := a.core.RequireManageResource(access, resourceLists, id); err != nil {
				return err
			}
		} else if _, err := a.core.RequireReadResource(access, resourceLists, id); err != nil {
			return err
		}
	}
	return nil
}

// workspaceManagedListIDsForRequest supplies a restrictive list filter for
// every bulk subscriber mutation. Empty request filters must never mean every
// list when the caller only has per-list management grants.
func (a *App) workspaceManagedListIDsForRequest(c echo.Context, access models.WorkspaceAccess, ids []int) ([]int, error) {
	if len(ids) > 0 {
		if err := a.requireWorkspaceListIDsForRequest(c, access, ids, true); err != nil {
			return nil, err
		}
		return ids, nil
	}
	hasAll, permitted := legacyManageableListIDs(auth.GetUser(c))
	if hasAll {
		return []int{}, nil
	}
	if len(permitted) == 0 {
		return []int{-1}, nil
	}
	return permitted, nil
}

// managedWorkspaceSubscriberIDs applies the writable owner boundary after a
// workspace read query. This matters for organization managers, who may read
// every member's records but must never change them through a bulk endpoint.
func (a *App) managedWorkspaceSubscriberIDs(access models.WorkspaceAccess, search string, listIDs []int, status string) ([]int, error) {
	ids, err := a.core.GetWorkspaceSubscriberIDs(access, search, listIDs, status)
	if err != nil {
		return nil, err
	}
	managed, err := a.core.ListManagedWorkspaceResources(access, resourceSubscribers)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int]struct{}, len(managed))
	for _, id := range managed {
		allowed[id] = struct{}{}
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := allowed[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// managedWorkspaceSubscriberIDsWithSQL resolves a raw-expression result set
// through the fixed workspace predicate first, then intersects it with the
// caller's mutable resources. This keeps a permitted advanced query from
// becoming a cross-owner bulk-write capability.
func (a *App) managedWorkspaceSubscriberIDsWithSQL(access models.WorkspaceAccess, search, query string, listIDs []int, status string) ([]int, error) {
	ids, err := a.core.GetWorkspaceSubscriberIDsWithSQL(access, search, query, listIDs, status)
	if err != nil {
		return nil, err
	}
	managed, err := a.core.ListManagedWorkspaceResources(access, resourceSubscribers)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int]struct{}, len(managed))
	for _, id := range managed {
		allowed[id] = struct{}{}
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := allowed[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func (a *App) requireSubscriberSQLQuery(c echo.Context) error {
	user := auth.GetUser(c)
	if user.HasPerm(auth.PermSubscribersSqlQuery) {
		return nil
	}
	return echo.NewHTTPError(http.StatusForbidden,
		a.i18n.Ts("globals.messages.permissionDenied", "name", auth.PermSubscribersSqlQuery))
}

// formatSQLExp normalizes arbitrary SQL expressions before the workspace
// validator handles them. It intentionally retains statement separators so
// the validator can reject them rather than silently accepting a trailing one.
func formatSQLExp(q string) string {
	return strings.TrimSpace(q)
}

// makeOptinNotifyHook returns an enclosed callback that sends optin confirmation e-mails.
// This is plugged into the 'core' package to send optin confirmations when a new subscriber is
// created via `core.CreateSubscriber()`.
func makeOptinNotifyHook(unsubHeader bool, u *UrlConfig, q *models.Queries, i *i18n.I18n) func(sub models.Subscriber, listIDs []int) (int, error) {
	return func(sub models.Subscriber, listIDs []int) (int, error) {
		// Fetch double opt-in lists from the given list IDs.
		// Get the list of subscription lists where the subscriber hasn't confirmed.
		var lists = []models.List{}
		if err := q.GetSubscriberLists.Select(&lists, sub.ID, nil, pq.Array(listIDs), nil, models.SubscriptionStatusUnconfirmed, models.ListOptinDouble); err != nil {
			lo.Printf("error fetching lists for opt-in: %s", err)
			return 0, err
		}

		// None.
		if len(lists) == 0 {
			return 0, nil
		}

		var (
			out      = subOptin{Subscriber: sub, Lists: lists}
			qListIDs = url.Values{}
		)

		// Construct the opt-in URL with list IDs.
		for _, l := range out.Lists {
			qListIDs.Add("l", l.UUID)
		}
		out.OptinURL = fmt.Sprintf(u.OptinURL, sub.UUID, qListIDs.Encode())
		out.UnsubURL = fmt.Sprintf(u.UnsubURL, dummyUUID, sub.UUID)

		// Unsub headers.
		hdr := textproto.MIMEHeader{}
		hdr.Set(models.EmailHeaderSubscriberUUID, sub.UUID)

		// Attach List-Unsubscribe headers?
		if unsubHeader {
			unsubURL := fmt.Sprintf(u.UnsubURL, dummyUUID, sub.UUID)
			hdr.Set("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
			hdr.Set("List-Unsubscribe", `<`+unsubURL+`>`)
		}

		// Send the e-mail.
		if err := notifs.Notify([]string{sub.Email}, i.T("subscribers.optinSubject"), notifs.TplSubscriberOptin, out, hdr); err != nil {
			lo.Printf("error sending opt-in e-mail for subscriber %d (%s): %s", sub.ID, sub.UUID, err)
			return 0, err
		}

		return len(lists), nil
	}
}
