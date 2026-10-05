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
// customer related requests.
type subQueryReq struct {
	Search                string          `json:"search"`
	Query                 json.RawMessage `json:"query"`
	CustomerListIDs       []int           `json:"customer_list_ids"`
	TargetCustomerListIDs []int           `json:"target_customer_list_ids"`
	CustomerIDs           []int           `json:"ids"`
	Action                string          `json:"action"`
	Status                string          `json:"status"`
	SubscriptionStatus    string          `json:"subscription_status"`
	All                   bool            `json:"all"`
}

// subOptin contains the data that's passed to the double opt-in e-mail template.
type subOptin struct {
	models.Customer

	OptinURL      string
	UnsubURL      string
	CustomerLists []models.CustomerList
}

var (
	dummyCustomer = models.Customer{
		Email:   "demo@listmonk.app",
		Name:    "Demo Customer",
		UUID:    dummyUUID,
		Attribs: models.JSON{"city": "Bengaluru"},
	}
)

// GetCustomer handles the retrieval of a single customer by ID.
// An optional customer_list_id query param identifies the customer_list context the customer
// is being viewed from. E-mail masking applies only when that customer_list has
// masking enabled; without a customer_list context the pre-existing redaction applies.
func (a *App) GetCustomer(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireReadableWorkspaceCustomer(c, access, id); err != nil {
		return err
	}

	// Fetch the customer from the active workspace.  The legacy lookup also
	// loads every customer_list relation for the ID and could therefore leak a
	// cross-workspace association to an organization manager.
	out, err := a.core.GetWorkspaceCustomer(access, id)
	if err != nil {
		return err
	}
	masked, err := a.requestMaskedLists(c, access, "customer_list_id")
	if err != nil {
		return err
	}
	// Oversight and ownership do not replace the sensitive-data permission.
	a.redactWorkspaceCustomerSensitiveFields(access, masked, &out, auth.GetUser(c))

	return c.JSON(http.StatusOK, okResp{out})
}

// GetCustomerActivity handles the retrieval of a customer's campaign views and link clicks.
func (a *App) GetCustomerActivity(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireReadableWorkspaceCustomer(c, access, id); err != nil {
		return err
	}

	// Fetch activity through the workspace-scoped query.  Organization managers
	// may inspect member statistics, but unrelated campaign rows must not be
	// returned for a forged customer ID or relation.
	out, err := a.core.GetWorkspaceCustomerActivity(access, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// QueryCustomers lists customers using the supported search filters.
func (a *App) QueryCustomers(c echo.Context) error {
	if _, supplied := c.QueryParams()["query"]; supplied {
		return echo.NewHTTPError(http.StatusBadRequest, "advanced customer queries are no longer supported")
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCustomersGetAll, auth.PermCustomersGet); err != nil {
		return err
	}
	CustomerListIDs, err := a.workspaceCustomerListIDsForRequest(c, access, "customer_list_id", c.QueryParams(), false)
	if err != nil {
		return err
	}

	var (
		searchStr = strings.TrimSpace(c.FormValue("search"))
		subStatus = c.FormValue("subscription_status")
		order     = c.FormValue("order")
		orderBy   = c.FormValue("order_by")
		pg        = a.pg.NewFromURL(c.Request().URL.Query())
	)

	res, total, err := a.core.QueryWorkspaceCustomers(access, searchStr, CustomerListIDs, subStatus, order, orderBy, pg.Offset, pg.Limit)
	if err != nil {
		return err
	}
	// E-mail masking follows the customer_lists currently being viewed. Only an
	// explicit customer_list_id filter supplies that context; a global view has no
	// "current customer_list" and keeps the pre-existing redaction for non-sensitive
	// viewers.
	masked, err := a.requestMaskedLists(c, access, "customer_list_id")
	if err != nil {
		return err
	}
	for i := range res {
		a.redactWorkspaceCustomerSensitiveFields(access, masked, &res[i], auth.GetUser(c))
	}

	out := models.PageResults{
		Query:   "",
		Search:  searchStr,
		Results: res,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// requestMaskedLists resolves the subset of the explicitly requested customer_lists
// (via the given query param) that have e-mail masking enabled. When the
// request carries no customer_list filter, an empty map is returned so callers keep
// the existing redaction behavior instead of masking.
func (a *App) requestMaskedLists(c echo.Context, access models.WorkspaceAccess, param string) (map[int]bool, error) {
	if c == nil || !c.QueryParams().Has(param) {
		return map[int]bool{}, nil
	}
	ids, err := a.workspaceCustomerListIDsForRequest(c, access, param, c.QueryParams(), false)
	if err != nil {
		return nil, err
	}
	return a.core.MaskedCustomerListIDs(ids)
}

// exportMasked decides whether a CSV export should mask e-mail addresses. It
// applies when the selected lists enable masking and the caller lacks the
// sensitive-data permission. Ownership and list maintenance do not bypass it.
func (a *App) exportMasked(c echo.Context, access models.WorkspaceAccess, CustomerListIDs []int) (bool, error) {
	masked, err := a.core.MaskedCustomerListIDs(CustomerListIDs)
	if err != nil {
		return false, err
	}
	if len(masked) == 0 {
		return false, nil
	}
	user := auth.GetUser(c)
	if user.HasPerm(auth.PermCustomersSensitiveRead) {
		return false, nil
	}
	return true, nil
}

// maskEmail masks a customer's e-mail address for viewers without
// sensitive-data access. The local part keeps its first 3 characters while the
// remainder is replaced with 'x's, preserving the original length. Local parts
// of 3 characters or fewer are fully replaced so no private prefix leaks.
// Eg: liuxin@gmail.com => liuxxx@gmail.com.
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 3 {
		return strings.Repeat("x", len(local)) + domain
	}
	return local[:3] + strings.Repeat("x", len(local)-3) + domain
}

// redactWorkspaceCustomerSensitiveFields runs after the resource boundary.
// Sensitive data requires an explicit grant; membership stays available for
// maintenance. Email is masked in masked-list views and otherwise hidden.
func (a *App) redactWorkspaceCustomerSensitiveFields(access models.WorkspaceAccess, maskedLists map[int]bool, sub *models.Customer, user auth.User) {
	if sub == nil || user.HasPerm(auth.PermCustomersSensitiveRead) {
		return
	}
	if len(maskedLists) > 0 && sub.Email != "" {
		sub.Email = maskEmail(sub.Email)
	} else {
		sub.Email = ""
	}
	sub.UUID = ""
	sub.Attribs = models.JSON{}
}

// ExportCustomers exports customers using the supported search filters.
func (a *App) ExportCustomers(c echo.Context) error {
	if _, supplied := c.QueryParams()["query"]; supplied {
		return echo.NewHTTPError(http.StatusBadRequest, "advanced customer queries are no longer supported")
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersExport); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersGetAll, auth.PermCustomersGet); err != nil {
		return err
	}
	CustomerListIDs, err := a.workspaceExportCustomerListIDsForRequest(c, access, "customer_list_id", c.QueryParams())
	if err != nil {
		return err
	}

	// Export only specific customer IDs?
	subIDs, err := getQueryInts("id", c.QueryParams())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
	}

	// Filter by subscription status
	subStatus := c.QueryParam("subscription_status")

	searchStr := strings.TrimSpace(c.FormValue("search"))

	var exp func() ([]models.CustomerExport, error)
	if len(subIDs) > 0 {
		for _, id := range subIDs {
			if _, err := a.requireExportableWorkspaceCustomer(c, access, id); err != nil {
				return err
			}
		}
	} else {
		subIDs, err = a.core.CustomerListManagedWorkspaceResources(access, resourceCustomers)
		if err != nil {
			return err
		}
	}
	// An empty explicit set must not mean "all" in the lower-level query.
	if len(subIDs) == 0 {
		subIDs = []int{-1}
	}
	exp, err = a.core.ExportWorkspaceCustomers(access, searchStr, CustomerListIDs, subIDs, subStatus, a.cfg.DBBatchSize)
	if err != nil {
		return err
	}

	var (
		hdr = c.Response().Header()
		wr  = csv.NewWriter(c.Response())
	)

	// The sensitive-data grant controls export fields independently of ownership.
	// Selected masked lists use masked email; other exports hide it entirely.
	maskExport, err := a.exportMasked(c, access, CustomerListIDs)
	if err != nil {
		return err
	}

	hdr.Set(echo.HeaderContentType, echo.MIMEOctetStream)
	hdr.Set("Content-type", "text/csv")
	hdr.Set(echo.HeaderContentDisposition, "attachment; filename="+"customers.csv")
	hdr.Set("Content-Transfer-Encoding", "binary")
	hdr.Set("Cache-Control", "no-cache")
	wr.Write([]string{"uuid", "email", "name", "customer_code", "attributes", "status", "created_at", "updated_at"})

loop:
	// Iterate in batches until there are no more customers to export.
	for {
		out, err := exp()
		if err != nil {
			return err
		}
		if len(out) == 0 {
			break
		}

		for _, r := range out {
			email := r.Email
			user := auth.GetUser(c)
			if !user.HasPerm(auth.PermCustomersSensitiveRead) {
				if maskExport {
					email = maskEmail(email)
				} else {
					email = ""
				}
				r.UUID, r.Attribs = "", "{}"
			}
			if err = wr.Write([]string{r.UUID, email, r.Name, r.CustomerCode, r.Attribs, r.Status,
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

// CreateCustomer handles the creation of a new customer.
func (a *App) CreateCustomer(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersManage); err != nil {
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

	// Customer code is a required business identifier on the admin path.
	req.CustomerCode = strings.TrimSpace(req.CustomerCode)
	if req.CustomerCode == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidCustomerCode"))
	}

	if err := a.requireOwnedWorkspaceCustomerListIDsForRequest(c, access, req.CustomerLists, true); err != nil {
		return err
	}

	// Insert the customer into the active user/workspace boundary.
	sub, _, err := a.core.InsertWorkspaceCustomer(access, req.Customer, req.CustomerLists, req.PreconfirmSubs, false)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.Itoa(sub.ID))
	setAuditObjectDetails(c, auditCustomerDetails(sub))
	a.redactWorkspaceCustomerSensitiveFields(access, nil, &sub, auth.GetUser(c))

	return c.JSON(http.StatusOK, okResp{sub})
}

// UpdateCustomer handles modification of a customer.
func (a *App) UpdateCustomer(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireManagedWorkspaceCustomer(c, access, id); err != nil {
		return err
	}
	previous, err := a.core.GetWorkspaceCustomer(access, id)
	if err != nil {
		return err
	}

	// Get and validate fields.
	req := struct {
		models.Customer
		CustomerLists  []int `json:"customer_list_ids"`
		PreconfirmSubs bool  `json:"preconfirm_subscriptions"`
	}{}
	// Omitted sensitive fields retain stored values, allowing edits of a
	// redacted record without overwriting its email or attributes.
	req.Customer = previous
	req.Attribs = nil
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Attribs == nil {
		req.Attribs = previous.Attribs
	}

	// Sanitize and validate the email field.
	if em, err := a.importer.SanitizeEmail(req.Email); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	} else {
		req.Email = em
	}

	if req.Name != "" && !strHasLen(req.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidName"))
	}

	// Customer code is a required business identifier on the admin path.
	req.CustomerCode = strings.TrimSpace(req.CustomerCode)
	if req.CustomerCode == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidCustomerCode"))
	}

	if err := a.requireOwnedWorkspaceCustomerListIDsForRequest(c, access, req.CustomerLists, true); err != nil {
		return err
	}

	permittedLists, err := a.managedWorkspaceLegacyCustomerListIDs(c, access)
	if err != nil {
		return err
	}
	req.Customer.ID = id

	out, _, err := a.core.UpdateCustomerWithListsInWorkspace(access, id, req.Customer, req.CustomerLists, req.PreconfirmSubs, true, false, permittedLists)
	if err != nil {
		return err
	}
	setAuditObjectDetails(c, auditCustomerDetails(out))
	a.redactWorkspaceCustomerSensitiveFields(access, nil, &out, auth.GetUser(c))

	return c.JSON(http.StatusOK, okResp{out})
}

// CustomerSendOptin sends an optin confirmation e-mail to a customer.
func (a *App) CustomerSendOptin(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Fetch the customer.
	id := getID(c)
	if _, err := a.requireManagedWorkspaceCustomer(c, access, id); err != nil {
		return err
	}
	out, err := a.core.GetWorkspaceCustomer(access, id)
	if err != nil {
		return err
	}
	setAuditObjectDetails(c, auditCustomerDetails(out))

	// Trigger the opt-in confirmation e-mail hook.
	if _, err := a.fnOptinNotify(out, nil); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("customers.errorSendingOptin"))
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// BlocklistCustomer handles the blocklisting of a given customer.
func (a *App) BlocklistCustomer(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireManagedWorkspaceCustomerWithPermissions(c, access, id,
		auth.PermCustomersBlocklist); err != nil {
		return err
	}
	if err := a.core.BlocklistCustomersInWorkspace(access, []int{id}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// BlocklistCustomers handles the blocklisting of one or more customers.
func (a *App) BlocklistCustomers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", err.Error()))
	}
	if len(req.CustomerIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorInvalidIDs", "error", "ids"))
	}
	for _, id := range req.CustomerIDs {
		if _, err := a.requireManagedWorkspaceCustomerWithPermissions(c, access, id,
			auth.PermCustomersBlocklist); err != nil {
			return err
		}
	}

	// Update the customers in the DB.
	if err := a.core.BlocklistCustomersInWorkspace(access, req.CustomerIDs); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ManageCustomerListMemberships handles bulk addition or removal of customers
// from or to one or more target customer_lists.
// It takes either an ID in the URI, or a customer_list of IDs in the request body.
func (a *App) ManageCustomerListMemberships(c echo.Context) error {
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
	if len(req.CustomerIDs) == 0 && len(subIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.errorNoIDs"))
	}
	if len(subIDs) == 0 {
		subIDs = req.CustomerIDs
	}
	if len(req.TargetCustomerListIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.errorNoListsGiven"))
	}

	for _, id := range subIDs {
		if _, err := a.requireManagedWorkspaceCustomerWithPermissions(c, access, id,
			auth.PermCustomersMembershipManage); err != nil {
			return err
		}
	}
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, req.TargetCustomerListIDs, true); err != nil {
		return err
	}

	// Run the action in the DB.
	switch req.Action {
	case "add":
		err = a.core.AddSubscriptionsInWorkspace(access, subIDs, req.TargetCustomerListIDs, req.Status)
	case "remove":
		err = a.core.DeleteSubscriptionsInWorkspace(access, subIDs, req.TargetCustomerListIDs)
	case "unsubscribe":
		err = a.core.UnsubscribeListsInWorkspace(access, subIDs, req.TargetCustomerListIDs)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidAction"))
	}

	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteCustomer handles deletion of a single customer.
func (a *App) DeleteCustomer(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if _, err := a.requireManagedWorkspaceCustomerWithPermissions(c, access, id,
		auth.PermCustomersDelete); err != nil {
		return err
	}
	if out, err := a.core.GetWorkspaceCustomer(access, id); err == nil {
		setAuditObjectDetails(c, auditCustomerDetails(out))
	}
	if err := a.core.DeleteCustomersInWorkspace(access, []int{id}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteCustomers handles bulk deletion of one or more customers.
func (a *App) DeleteCustomers(c echo.Context) error {
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
		if _, err := a.requireManagedWorkspaceCustomerWithPermissions(c, access, id,
			auth.PermCustomersDelete); err != nil {
			return err
		}
	}

	// Delete the customers from the DB.
	if err := a.core.DeleteCustomersInWorkspace(access, ids); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteCustomersByFilter deletes the managed customers matching ordinary filters.
func (a *App) DeleteCustomersByFilter(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersDelete); err != nil {
		return err
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Query != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "advanced customer queries are no longer supported")
	}

	req.Search = strings.TrimSpace(req.Search)
	if req.All {
		// The explicit all flag selects every managed customer in the scope.
		req.Search = ""
	} else if req.Search == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "search"))
	}

	CustomerListIDs, err := a.workspaceManagedCustomerListIDsForRequest(c, access, req.CustomerListIDs)
	if err != nil {
		return err
	}
	req.CustomerListIDs = CustomerListIDs
	ids, err := a.managedWorkspaceCustomerIDs(access, req.Search, req.CustomerListIDs, req.SubscriptionStatus)
	if err != nil {
		return err
	}
	if err := a.core.DeleteCustomersInWorkspace(access, ids); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// BlocklistCustomersByFilter blocklists managed customers matching ordinary filters.
func (a *App) BlocklistCustomersByFilter(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersBlocklist); err != nil {
		return err
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Query != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "advanced customer queries are no longer supported")
	}

	req.Search = strings.TrimSpace(req.Search)
	if req.All {
		req.Search = ""
	} else if req.Search == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "search"))
	}
	CustomerListIDs, err := a.workspaceManagedCustomerListIDsForRequest(c, access, req.CustomerListIDs)
	if err != nil {
		return err
	}
	req.CustomerListIDs = CustomerListIDs
	ids, err := a.managedWorkspaceCustomerIDs(access, req.Search, req.CustomerListIDs, req.SubscriptionStatus)
	if err != nil {
		return err
	}
	if err := a.core.BlocklistCustomersInWorkspace(access, ids); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ManageCustomerListMembershipsByFilter changes memberships for filtered customers.
func (a *App) ManageCustomerListMembershipsByFilter(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersMembershipManage); err != nil {
		return err
	}

	var req subQueryReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Query != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "advanced customer queries are no longer supported")
	}
	if len(req.TargetCustomerListIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.T("customers.errorNoListsGiven"))
	}

	req.Search = strings.TrimSpace(req.Search)

	CustomerListIDs, err := a.workspaceManagedCustomerListIDsForRequest(c, access, req.CustomerListIDs)
	if err != nil {
		return err
	}
	req.CustomerListIDs = CustomerListIDs
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, req.TargetCustomerListIDs, true); err != nil {
		return err
	}
	subIDs, err := a.managedWorkspaceCustomerIDs(access, req.Search, req.CustomerListIDs, req.SubscriptionStatus)
	if err != nil {
		return err
	}

	// Run the action in the DB.
	var runErr error
	switch req.Action {
	case "add":
		runErr = a.core.AddSubscriptionsInWorkspace(access, subIDs, req.TargetCustomerListIDs, req.Status)
	case "remove":
		runErr = a.core.DeleteSubscriptionsInWorkspace(access, subIDs, req.TargetCustomerListIDs)
	case "unsubscribe":
		runErr = a.core.UnsubscribeListsInWorkspace(access, subIDs, req.TargetCustomerListIDs)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customers.invalidAction"))
	}

	if runErr != nil {
		return runErr
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteCustomerBounces deletes all the bounces on a customer.
func (a *App) DeleteCustomerBounces(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermBouncesDelete); err != nil {
		return err
	}
	// Delete the bounces from the DB.
	id := getID(c)
	if _, err := a.core.RequireManageResource(access, resourceCustomers, id); err != nil {
		return err
	}
	if err := a.core.DeleteCustomerBouncesInWorkspace(access, id); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ExportCustomerData pulls the customer's profile,
// customer_list subscriptions, campaign views and clicks and produces
// a JSON report. This is a privacy feature and depends on the
// configuration in a.Constants.Privacy.
func (a *App) ExportCustomerData(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersExport); err != nil {
		return err
	}
	// Get the customer's data. A single query that gets the profile,
	// customer_list subscriptions, campaign views, and link clicks. Names of
	// private customer_lists are replaced with "Private customer_list".
	id := getID(c)
	if _, err := a.requireExportableWorkspaceCustomer(c, access, id); err != nil {
		return err
	}
	data, _, err := a.exportWorkspaceCustomerData(access, id, a.cfg.Privacy.Exportable)
	if err != nil {
		a.log.Printf("error exporting customer data: %s", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", err.Error()))
	}
	user := auth.GetUser(c)
	if !user.HasPerm(auth.PermCustomersSensitiveRead) {
		if len(data.Profile) > 0 {
			data.Profile, err = redactCustomerExportProfile(data.Profile)
			if err != nil {
				return err
			}
		}
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	// Set headers to force the browser to prompt for download.
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="data.json"`)
	return c.Blob(http.StatusOK, "application/json", b)
}

// exportWorkspaceCustomerData is the authenticated counterpart to the
// bearer-token privacy export helper below.  It deliberately uses the
// workspace-aware Core query so an organization manager (who may inspect a
// member's record) cannot export that member's personal audience, while an
// owner or platform administrator can still export the rows they manage.
func (a *App) exportWorkspaceCustomerData(access models.WorkspaceAccess, id int, exportables map[string]bool) (models.CustomerExportProfile, []byte, error) {
	data, err := a.core.GetWorkspaceCustomerProfileForExport(access, id)
	if err != nil {
		return data, nil, err
	}

	filterCustomerExportables(&data, exportables)
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		a.log.Printf("error marshalling customer export data: %v", err)
		return data, nil, err
	}
	return data, b, nil
}

// exportCustomerData collates the data of a customer including profile,
// subscriptions, campaign_views, link_clicks (if they're enabled in the config)
// and returns a formatted, indented JSON payload. Either takes a numeric id
// and an empty subUUID or takes 0 and a string subUUID.
func (a *App) exportCustomerData(id int, subUUID string, exportables map[string]bool) (models.CustomerExportProfile, []byte, error) {
	data, err := a.core.GetCustomerProfileForExport(id, subUUID)
	if err != nil {
		return data, nil, err
	}

	filterCustomerExportables(&data, exportables)

	// Marshal the data into an indented payload.
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		a.log.Printf("error marshalling customer export data: %v", err)
		return data, nil, err
	}

	return data, b, nil
}

func filterCustomerExportables(data *models.CustomerExportProfile, exportables map[string]bool) {
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

// hasSubPerm checks whether the current user has permission to access the given customer_list
// of customer IDs.
func (a *App) hasSubPerm(access models.WorkspaceAccess, u auth.User, subIDs []int) error {
	allPerm, CustomerListIDs := legacyReadableCustomerListIDs(u)

	// User has blanket get_all|manage_all permission.
	if allPerm {
		return nil
	}

	// Check whether the customers have the customer_list IDs permitted to the user.
	res, err := a.core.HasCustomerListMembershipsInWorkspace(access, subIDs, CustomerListIDs)
	if err != nil {
		return err
	}

	for id, has := range res {
		if !has {
			return echo.NewHTTPError(http.StatusForbidden, a.i18n.Ts("globals.messages.permissionDenied", "name", fmt.Sprintf("customer: %d", id)))
		}
	}

	return nil
}

func (a *App) hasManagedSubPerm(access models.WorkspaceAccess, u auth.User, subIDs []int) error {
	if u.IsPlatformAdmin() || u.HasPerm(auth.PermListManageAll) {
		return nil
	}
	if len(u.ManageCustomerListIDs) == 0 {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied: customer_list:manage")
	}
	res, err := a.core.HasCustomerListMembershipsInWorkspace(access, subIDs, u.ManageCustomerListIDs)
	if err != nil {
		return err
	}
	for id, has := range res {
		if !has {
			return echo.NewHTTPError(http.StatusForbidden,
				a.i18n.Ts("globals.messages.permissionDenied", "name", fmt.Sprintf("customer: %d", id)))
		}
	}
	return nil
}

// filterListQueryByPerm filters the customer_list IDs in the query params and returns the customer_list IDs to which the user has access.
func (a *App) filterListQueryByPerm(param string, qp url.Values, user auth.User) ([]int, error) {
	var CustomerListIDs []int

	// If there are incoming customer_list query params, filter them by permission.
	if qp.Has(param) {
		ids, err := getQueryInts(param, qp)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}

		CustomerListIDs = user.FilterListsByPerm(auth.PermTypeGet|auth.PermTypeManage, ids)
	}

	// There are no incoming params. If the user doesn't have permission to get all customers,
	// filter by the customer_lists they have access to.
	if len(CustomerListIDs) == 0 {
		if _, ok := user.PermissionsMap[auth.PermCustomersGetAll]; !ok {
			if len(user.GetCustomerListIDs) > 0 {
				CustomerListIDs = user.GetCustomerListIDs
			} else {
				// User doesn't have access to any customer_lists.
				CustomerListIDs = []int{-1}
			}
		}
	}

	return CustomerListIDs, nil
}

// workspaceCustomerListIDs resolves optional customer_list query parameters and verifies that
// they belong to the selected workspace. CustomerList membership from the legacy role
// system is intentionally not considered here: it must never cross an owner
// or organization boundary.
func (a *App) workspaceCustomerListIDs(access models.WorkspaceAccess, param string, qp url.Values, manage bool) ([]int, error) {
	return a.workspaceCustomerListIDsForRequest(nil, access, param, qp, manage)
}

func (a *App) workspaceCustomerListIDsForRequest(c echo.Context, access models.WorkspaceAccess, param string, qp url.Values, manage bool) ([]int, error) {
	if !qp.Has(param) {
		if c == nil {
			return []int{}, nil
		}
		if manage {
			return a.workspaceManagedCustomerListIDsForRequest(c, access, nil)
		}
		user := auth.GetUser(c)
		if access.IsOrganizationManager() || user.IsPlatformAdmin() {
			return []int{}, nil
		}
		hasAll, permitted := legacyReadableCustomerListIDs(user)
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
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, ids, manage); err != nil {
		return nil, err
	}
	return ids, nil
}

// workspaceExportCustomerListIDsForRequest intentionally does not use the
// organization-manager inspection exception. CSV export is sensitive data and
// therefore remains subject to the caller's pre-existing customer_list grants as well
// as the owner boundary enforced by the export query.
func (a *App) workspaceExportCustomerListIDsForRequest(c echo.Context, access models.WorkspaceAccess, param string, qp url.Values) ([]int, error) {
	user := auth.GetUser(c)
	if !qp.Has(param) {
		hasAll, permitted := legacyReadableCustomerListIDs(user)
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

func (a *App) requireWorkspaceCustomerListIDs(access models.WorkspaceAccess, ids []int, manage bool) error {
	return a.requireWorkspaceCustomerListIDsForRequest(nil, access, ids, manage)
}

func (a *App) requireWorkspaceCustomerListIDsForRequest(c echo.Context, access models.WorkspaceAccess, ids []int, manage bool) error {
	return a.requireWorkspaceCustomerListIDsForRequestWithOptions(c, access, ids, manage, false, false)
}

func (a *App) requireOwnedWorkspaceCustomerListIDsForRequest(c echo.Context, access models.WorkspaceAccess, ids []int, manage bool) error {
	return a.requireWorkspaceCustomerListIDsForRequestWithOptions(c, access, ids, manage, false, true)
}

func (a *App) requireWorkspaceCustomerListIDsForRequestAllowPool(c echo.Context, access models.WorkspaceAccess, ids []int, manage bool) error {
	return a.requireWorkspaceCustomerListIDsForRequestWithOptions(c, access, ids, manage, true, false)
}

func (a *App) requireWorkspaceCustomerListIDsForRequestWithOptions(c echo.Context, access models.WorkspaceAccess, ids []int, manage, allowPool, ownerOnly bool) error {
	for _, id := range ids {
		if id < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidID"))
		}
		var (
			scope models.ResourceScope
			err   error
		)
		if c != nil {
			if manage {
				scope, err = a.requireManagedWorkspaceList(c, access, id)
			} else {
				scope, err = a.requireReadableWorkspaceList(c, access, id)
			}
		} else if manage {
			scope, err = a.core.RequireManageResource(access, resourceLists, id)
		} else {
			scope, err = a.core.RequireReadResource(access, resourceLists, id)
		}
		if err != nil {
			return err
		}
		if !allowPool && !customerListInActiveWorkspace(access, scope, ownerOnly) {
			return customerListOutsideActiveWorkspaceError()
		}
	}
	return nil
}

// workspaceManagedCustomerListIDsForRequest supplies a restrictive customer_list filter for
// every bulk customer mutation. Empty request filters must never mean every
// customer_list when the caller only has per-customer_list management grants.
func (a *App) workspaceManagedCustomerListIDsForRequest(c echo.Context, access models.WorkspaceAccess, ids []int) ([]int, error) {
	if len(ids) > 0 {
		if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, ids, true); err != nil {
			return nil, err
		}
		return ids, nil
	}
	hasAll, permitted := legacyManageableCustomerListIDs(auth.GetUser(c))
	if hasAll {
		return []int{}, nil
	}
	if len(permitted) == 0 {
		return []int{-1}, nil
	}
	return permitted, nil
}

// managedWorkspaceCustomerIDs applies the writable owner boundary after a
// workspace read query. This matters for organization managers, who may read
// every member's records but must never change them through a bulk endpoint.
func (a *App) managedWorkspaceCustomerIDs(access models.WorkspaceAccess, search string, CustomerListIDs []int, status string) ([]int, error) {
	ids, err := a.core.GetWorkspaceCustomerIDs(access, search, CustomerListIDs, status)
	if err != nil {
		return nil, err
	}
	managed, err := a.core.CustomerListManagedWorkspaceResources(access, resourceCustomers)
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

// makeOptinNotifyHook returns an enclosed callback that sends optin confirmation e-mails.
// This is plugged into the 'core' package to send optin confirmations when a new customer is
// created via `core.CreateCustomer()`.
func makeOptinNotifyHook(unsubHeader bool, u *UrlConfig, q *models.Queries, i *i18n.I18n) func(sub models.Customer, CustomerListIDs []int) (int, error) {
	return func(sub models.Customer, CustomerListIDs []int) (int, error) {
		// Fetch double opt-in customer_lists from the given customer_list IDs.
		// Get the customer_list of subscription customer_lists where the customer hasn't confirmed.
		var customer_lists = []models.CustomerList{}
		if err := q.GetCustomerListMemberships.Select(&customer_lists, sub.ID, nil, pq.Array(CustomerListIDs), nil, models.SubscriptionStatusUnconfirmed, models.CustomerListOptinDouble); err != nil {
			lo.Printf("error fetching customer_lists for opt-in: %s", err)
			return 0, err
		}

		// None.
		if len(customer_lists) == 0 {
			return 0, nil
		}

		var (
			out              = subOptin{Customer: sub, CustomerLists: customer_lists}
			qCustomerListIDs = url.Values{}
		)

		// Construct the opt-in URL with customer_list IDs.
		for _, l := range out.CustomerLists {
			qCustomerListIDs.Add("l", l.UUID)
		}
		out.OptinURL = fmt.Sprintf(u.OptinURL, sub.UUID, qCustomerListIDs.Encode())
		out.UnsubURL = fmt.Sprintf(u.UnsubURL, dummyUUID, sub.UUID)

		// Unsub headers.
		hdr := textproto.MIMEHeader{}
		hdr.Set(models.EmailHeaderCustomerUUID, sub.UUID)

		// Attach RFC 8058 one-click unsubscribe headers.
		if unsubHeader {
			unsubURL := fmt.Sprintf(u.UnsubURL, dummyUUID, sub.UUID)
			models.SetListUnsubscribeHeaders(hdr, unsubURL)
		}

		// Send the e-mail.
		if err := notifs.Notify([]string{sub.Email}, i.T("customers.optinSubject"), notifs.TplCustomerOptin, out, hdr); err != nil {
			lo.Printf("error sending opt-in e-mail for customer %d (%s): %s", sub.ID, sub.UUID, err)
			return 0, err
		}

		return len(customer_lists), nil
	}
}
