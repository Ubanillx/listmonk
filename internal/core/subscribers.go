package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

var (
	allowedSubQueryTables = map[string]struct{}{
		"customers":                 {},
		"customer_list_memberships": {},
		"customer_lists":            {},
		"users":                     {},
		"campaigns":                 {},
		"campaign_customer_lists":   {},
		"campaign_views":            {},
		"links":                     {},
		"link_clicks":               {},
		"bounces":                   {},
	}
)

// GetCustomer fetches a customer by one of the given params.
func (c *Core) GetCustomer(id int, uuid, email string) (models.Customer, error) {
	var uu any
	if uuid != "" {
		uu = uuid
	}

	var out models.Customers
	if err := c.q.GetCustomer.Select(&out, id, uu, email); err != nil {
		c.log.Printf("error fetching customer: %v", err)
		return models.Customer{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching",
				"name", "{globals.terms.customer}", "error", pqErrMsg(err)))
	}
	if len(out) == 0 {
		return models.Customer{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name",
				fmt.Sprintf("{globals.terms.customer} (%d: %s%s)", id, uuid, email)))
	}
	if err := out.LoadLists(c.q.GetCustomerListMembershipsLazy); err != nil {
		c.log.Printf("error loading customer customer_lists: %v", err)
		return models.Customer{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching",
				"name", "{globals.terms.customer_lists}", "error", pqErrMsg(err)))
	}

	return out[0], nil
}

// HasCustomerListMemberships checks if the given customers have at least one of the given customer_lists.
func (c *Core) HasCustomerListMemberships(subIDs []int, customerListIDs []int) (map[int]bool, error) {
	res := []struct {
		SubID int  `db:"customer_id"`
		Has   bool `db:"has"`
	}{}

	if err := c.q.HasCustomerListMemberships.Select(&res, pq.Array(subIDs), pq.Array(customerListIDs)); err != nil {
		c.log.Printf("error fetching customer: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer}", "error", pqErrMsg(err)))
	}

	out := make(map[int]bool, len(res))
	for _, r := range res {
		out[r.SubID] = r.Has
	}

	return out, nil
}

// GetCustomersByEmail fetches a customer by one of the given params.
func (c *Core) GetCustomersByEmail(emails []string) (models.Customers, error) {
	var out models.Customers

	if err := c.q.GetCustomersByEmails.Select(&out, pq.Array(emails)); err != nil {
		c.log.Printf("error fetching customer: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer}", "error", pqErrMsg(err)))
	}
	if len(out) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("campaigns.noKnownSubsToTest"))
	}

	if err := out.LoadLists(c.q.GetCustomerListMembershipsLazy); err != nil {
		c.log.Printf("error loading customer customer_lists: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer_lists}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// QueryCustomers queries and returns paginated subscrribers based on the given params including the total count.
func (c *Core) QueryCustomers(searchStr, queryExp string, customerListIDs []int, subStatus string, order, orderBy string, offset, limit int) (models.Customers, int, error) {
	// Sort params.
	if !strSliceContains(orderBy, subQuerySortFields) {
		orderBy = "customers.id"
	}
	if order != SortAsc && order != SortDesc {
		order = SortDesc
	}

	// Required for pq.Array()
	if customerListIDs == nil {
		customerListIDs = []int{}
	}

	// There's an arbitrary query condition.
	cond := "TRUE"
	if queryExp != "" {
		cond = queryExp
	}

	// stmt is the raw SQL query.
	stmt := strings.ReplaceAll(c.q.QueryCustomers, "%query%", cond)
	stmt = strings.ReplaceAll(stmt, "%order%", orderBy+" "+order)

	// Validate the tables used in the query.
	if err := validateQueryTables(c.db, stmt, allowedSubQueryTables); err != nil {
		c.log.Printf("error validating query tables: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("customers.errorPreparingQuery", "error", err.Error()))
	}

	// Create a readonly transaction that just does COUNT() to obtain the count of results
	// and to ensure that the arbitrary query is indeed readonly.
	total, err := c.getCustomerCount(searchStr, cond, subStatus, customerListIDs)
	if err != nil {
		c.log.Printf("error getting customer count: %v", err)
		return nil, 0, err
	}

	// No results.
	if total == 0 {
		return models.Customers{}, 0, nil
	}

	tx, err := c.db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		c.log.Printf("error preparing customer query: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusBadRequest, c.i18n.Ts("customers.errorPreparingQuery", "error", pqErrMsg(err)))
	}
	defer tx.Rollback()

	var out models.Customers
	if err := tx.Select(&out, stmt, pq.Array(customerListIDs), subStatus, searchStr, offset, limit); err != nil {
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	// Lazy load customer_lists for each customer.
	if err := out.LoadLists(c.q.GetCustomerListMembershipsLazy); err != nil {
		c.log.Printf("error fetching customer customer_lists: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return out, total, nil
}

// GetCustomerListMemberships returns a customer's customer_lists based on the given conditions.
func (c *Core) GetCustomerListMemberships(subID int, uuid string, customerListIDs []int, listUUIDs []string, subStatus string, listType string) ([]models.CustomerList, error) {
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	if listUUIDs == nil {
		listUUIDs = []string{}
	}

	var uu any
	if uuid != "" {
		uu = uuid
	}

	// Fetch double opt-in customer_lists from the given customer_list IDs.
	// Get the customer_list of subscription customer_lists where the customer hasn't confirmed.
	out := []models.CustomerList{}
	if err := c.q.GetCustomerListMemberships.Select(&out, subID, uu, pq.Array(customerListIDs), pq.Array(listUUIDs), subStatus, listType); err != nil {
		c.log.Printf("error fetching customer_lists for opt-in: %s", pqErrMsg(err))
		return nil, err
	}

	return out, nil
}

// GetCustomerProfileForExport returns the customer's profile data as a JSON exportable.
// Get the customer's data. A single query that gets the profile, customer_list subscriptions, campaign views,
// and link clicks. Names of private customer_lists are replaced with "Private customer_list".
func (c *Core) GetCustomerProfileForExport(id int, uuid string) (models.CustomerExportProfile, error) {
	var uu any
	if uuid != "" {
		uu = uuid
	}

	var out models.CustomerExportProfile
	if err := c.q.ExportCustomerData.Get(&out, id, uu); err != nil {
		c.log.Printf("error fetching customer export data: %v", err)

		return models.CustomerExportProfile{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", err.Error()))
	}

	return out, nil
}

// GetCustomerActivity returns the customer's campaign views and link clicks for the Activity tab.
func (c *Core) GetCustomerActivity(id int) (models.CustomerActivity, error) {
	var out models.CustomerActivity
	if err := c.q.GetCustomerActivity.Get(&out, id); err != nil {
		c.log.Printf("error fetching customer activity: %v", err)

		return models.CustomerActivity{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "activity", "error", err.Error()))
	}

	return out, nil
}

// ExportCustomers returns an iterator function that provides customer_lists of customers based
// on the given criteria in an exportable form. The iterator function returned can be called
// repeatedly until there are nil customers. It's an iterator because exports can be extremely
// large and may have to be fetched in batches from the DB and streamed somewhere.
func (c *Core) ExportCustomers(searchStr, query string, subIDs, customerListIDs []int, subStatus string, batchSize int) (func() ([]models.CustomerExport, error), error) {
	if subIDs == nil {
		subIDs = []int{}
	}
	if customerListIDs == nil {
		customerListIDs = []int{}
	}

	// There's an arbitrary query condition.
	cond := "TRUE"
	if query != "" {
		cond = query
	}

	stmt := strings.ReplaceAll(c.q.QueryCustomersForExport, "%query%", cond)

	// Create a readonly transaction that just does COUNT() to obtain the count of results
	// and to ensure that the arbitrary query is indeed readonly.
	if _, err := c.getCustomerCount(searchStr, cond, subStatus, customerListIDs); err != nil {
		c.log.Printf("error getting customer count: %v", err)
		return nil, err
	}

	// Prepare the actual query statement.
	tx, err := c.db.Preparex(stmt)
	if err != nil {
		c.log.Printf("error preparing customer query: %v", err)
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("customers.errorPreparingQuery", "error", pqErrMsg(err)))
	}

	id := 0
	return func() ([]models.CustomerExport, error) {
		var out []models.CustomerExport
		if err := tx.Select(&out, pq.Array(customerListIDs), id, pq.Array(subIDs), subStatus, searchStr, batchSize); err != nil {
			c.log.Printf("error exporting customers by query: %v", err)
			return nil, echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
		}
		if len(out) == 0 {
			return nil, nil
		}

		id = out[len(out)-1].ID
		return out, nil
	}, nil
}

// InsertCustomer inserts a customer and returns the ID. The first bool indicates if
// it was a new customer, and the second bool indicates if the customer was sent an optin confirmation.
// bool = optinSent?
func (c *Core) InsertCustomer(sub models.Customer, customerListIDs []int, listUUIDs []string, preconfirm, assertOptin bool) (models.Customer, bool, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return models.Customer{}, false, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	sub.UUID = uu.String()

	subStatus := models.SubscriptionStatusUnconfirmed
	if preconfirm {
		subStatus = models.SubscriptionStatusConfirmed
	}
	if sub.Status == "" {
		sub.Status = auth.UserStatusEnabled
	}

	// For pq.Array()
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	if listUUIDs == nil {
		listUUIDs = []string{}
	}

	if err = c.q.InsertCustomer.Get(&sub.ID,
		sub.UUID,
		sub.Email,
		strings.TrimSpace(sub.Name),
		sub.Status,
		sub.Attribs,
		pq.Array(customerListIDs),
		pq.Array(listUUIDs),
		subStatus); err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Constraint == "customers_email_key" {
			return models.Customer{}, false, echo.NewHTTPError(http.StatusConflict, c.i18n.T("customers.emailExists"))
		} else {
			// return sub.Customer, errCustomerExists
			c.log.Printf("error inserting customer: %v", err)
			return models.Customer{}, false, echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.customer}", "error", pqErrMsg(err)))
		}
	}

	// Fetch the customer's full data. If the customer already existed and wasn't
	// created, the id will be empty. Fetch the details by e-mail then.
	out, err := c.GetCustomer(sub.ID, "", sub.Email)
	if err != nil {
		return models.Customer{}, false, err
	}

	hasOptin := false
	if !preconfirm && c.consts.SendOptinConfirmation {
		// Send a confirmation e-mail (if there are any double opt-in customer_lists).
		num, err := c.h.SendOptinConfirmation(out, customerListIDs)
		if assertOptin && err != nil {
			return out, hasOptin, err
		}

		hasOptin = num > 0
	}

	return out, hasOptin, nil
}

// UpdateCustomer updates a customer's properties.
func (c *Core) UpdateCustomer(id int, sub models.Customer) (models.Customer, error) {
	// Format raw JSON attributes.
	attribs := []byte("{}")
	if len(sub.Attribs) > 0 {
		if b, err := json.Marshal(sub.Attribs); err != nil {
			return models.Customer{}, echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating",
					"name", "{globals.terms.customer}", "error", err.Error()))
		} else {
			attribs = b
		}
	}

	_, err := c.q.UpdateCustomer.Exec(id,
		sub.Email,
		strings.TrimSpace(sub.Name),
		sub.Status,
		json.RawMessage(attribs),
		sub.CustomerCode,
	)
	if err != nil {
		c.log.Printf("error updating customer: %v", err)
		return models.Customer{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customer}", "error", pqErrMsg(err)))
	}

	out, err := c.GetCustomer(sub.ID, "", sub.Email)
	if err != nil {
		return models.Customer{}, err
	}

	return out, nil
}

// UpdateCustomerWithLists updates a customer's properties.
// If deleteLists is set to true, all existing subscriptions are deleted and only
// the ones provided are added or retained.
func (c *Core) UpdateCustomerWithLists(id int, sub models.Customer, customerListIDs []int, listUUIDs []string, preconfirm, deleteLists, assertOptin bool, permittedCustomerListIDs []int) (models.Customer, bool, error) {
	subStatus := models.SubscriptionStatusUnconfirmed
	if preconfirm {
		subStatus = models.SubscriptionStatusConfirmed
	}

	// Format raw JSON attributes.
	attribs := []byte("{}")
	if len(sub.Attribs) > 0 {
		if b, err := json.Marshal(sub.Attribs); err != nil {
			return models.Customer{}, false, echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating",
					"name", "{globals.terms.customer}", "error", err.Error()))
		} else {
			attribs = b
		}
	}

	_, err := c.q.UpdateCustomerWithLists.Exec(id,
		sub.Email,
		strings.TrimSpace(sub.Name),
		sub.Status,
		json.RawMessage(attribs),
		pq.Array(customerListIDs),
		pq.Array(listUUIDs),
		subStatus,
		deleteLists,
		pq.Array(permittedCustomerListIDs),
		sub.CustomerCode)
	if err != nil {
		c.log.Printf("error updating customer: %v", err)
		return models.Customer{}, false, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customer}", "error", pqErrMsg(err)))
	}

	out, err := c.GetCustomer(sub.ID, "", sub.Email)
	if err != nil {
		return models.Customer{}, false, err
	}

	hasOptin := false
	if !preconfirm && c.consts.SendOptinConfirmation {
		// Send a confirmation e-mail (if there are any double opt-in customer_lists).
		num, err := c.h.SendOptinConfirmation(out, customerListIDs)
		if assertOptin && err != nil {
			return out, hasOptin, err
		}
		hasOptin = num > 0
	}

	return out, hasOptin, nil
}

// UpdateCustomerWithListsInWorkspace is the owner-scoped mutation path used
// by the HTTP API. It locks both the customer and every target customer_list after
// the organization membership has been revalidated, preventing a customer_list move or
// member removal from creating a cross-workspace subscription relation.
func (c *Core) UpdateCustomerWithListsInWorkspace(access models.WorkspaceAccess, id int, sub models.Customer, customerListIDs []int, preconfirm, deleteLists, assertOptin bool, permittedCustomerListIDs []int) (models.Customer, bool, error) {
	subStatus := models.SubscriptionStatusUnconfirmed
	if preconfirm {
		subStatus = models.SubscriptionStatusConfirmed
	}

	attribs := []byte("{}")
	if len(sub.Attribs) > 0 {
		b, err := json.Marshal(sub.Attribs)
		if err != nil {
			return models.Customer{}, false, echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customer}", "error", err.Error()))
		}
		attribs = b
	}

	err := c.withWorkspaceResourceMutation(access, resourceCustomers, []int{id}, func(tx *sqlx.Tx) error {
		if err := c.lockWorkspaceMutationResources(tx, access, resourceLists, customerListIDs); err != nil {
			return err
		}
		if _, err := tx.Stmtx(c.q.UpdateCustomerWithLists).Exec(id,
			sub.Email,
			strings.TrimSpace(sub.Name),
			sub.Status,
			json.RawMessage(attribs),
			pq.Array(customerListIDs),
			pq.Array([]string{}),
			subStatus,
			deleteLists,
			pq.Array(permittedCustomerListIDs),
			sub.CustomerCode); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customer}", "error", pqErrMsg(err)))
		}
		return nil
	})
	if err != nil {
		return models.Customer{}, false, err
	}

	out, err := c.GetWorkspaceCustomer(access, id)
	if err != nil {
		return models.Customer{}, false, err
	}
	hasOptin := false
	if !preconfirm && c.consts.SendOptinConfirmation {
		num, err := c.h.SendOptinConfirmation(out, customerListIDs)
		if assertOptin && err != nil {
			return out, hasOptin, err
		}
		hasOptin = num > 0
	}
	return out, hasOptin, nil
}

// BlocklistCustomers blocklists the given customer_list of customers.
func (c *Core) BlocklistCustomers(subIDs []int) error {
	if _, err := c.q.BlocklistCustomers.Exec(pq.Array(subIDs)); err != nil {
		c.log.Printf("error blocklisting customers: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("customers.errorBlocklisting", "error", err.Error()))
	}

	return nil
}

// BlocklistCustomersInWorkspace is the transactional workspace variant for
// single and bulk actions. It deliberately does not reuse the unscoped legacy
// method because the resource lock must remain held through both the customer
// and subscription updates.
func (c *Core) BlocklistCustomersInWorkspace(access models.WorkspaceAccess, subIDs []int) error {
	subIDs = uniqueMutationIDs(subIDs)
	if len(subIDs) == 0 {
		return nil
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, subIDs, func(tx *sqlx.Tx) error {
		if _, err := tx.Stmtx(c.q.BlocklistCustomers).Exec(pq.Array(subIDs)); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("customers.errorBlocklisting", "error", pqErrMsg(err)))
		}
		return nil
	})
}

// BlocklistCustomersByQuery blocklists the given customer_list of customers.
func (c *Core) BlocklistCustomersByQuery(searchStr, queryExp string, customerListIDs []int, subStatus string) error {
	if err := c.q.ExecSubQueryTpl(searchStr, sanitizeSQLExp(queryExp), c.q.BlocklistCustomersByQuery, customerListIDs, c.db, subStatus); err != nil {
		c.log.Printf("error blocklisting customers: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("customers.errorBlocklisting", "error", pqErrMsg(err)))
	}

	return nil
}

// DeleteCustomers deletes the given customer_list of customers.
func (c *Core) DeleteCustomers(subIDs []int, subUUIDs []string) error {
	if subIDs == nil {
		subIDs = []int{}
	}
	if subUUIDs == nil {
		subUUIDs = []string{}
	}

	if _, err := c.q.DeleteCustomers.Exec(pq.Array(subIDs), pq.Array(subUUIDs)); err != nil {
		c.log.Printf("error deleting customers: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return nil
}

func (c *Core) DeleteCustomersInWorkspace(access models.WorkspaceAccess, subIDs []int) error {
	subIDs = uniqueMutationIDs(subIDs)
	if len(subIDs) == 0 {
		return nil
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, subIDs, func(tx *sqlx.Tx) error {
		if _, err := tx.Stmtx(c.q.DeleteCustomers).Exec(pq.Array(subIDs), pq.Array([]string{})); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
		}
		return nil
	})
}

// DeleteCustomersByQuery deletes customers by a given arbitrary query expression.
func (c *Core) DeleteCustomersByQuery(searchStr, queryExp string, customerListIDs []int, subStatus string) error {
	err := c.q.ExecSubQueryTpl(searchStr, sanitizeSQLExp(queryExp), c.q.DeleteCustomersByQuery, customerListIDs, c.db, subStatus)
	if err != nil {
		c.log.Printf("error deleting customers: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return err
}

// UnsubscribeByCampaign unsubscribes a given customer from customer_lists in a given campaign.
func (c *Core) UnsubscribeByCampaign(subUUID, campUUID string, blocklist bool) error {
	if _, err := c.q.UnsubscribeByCampaign.Exec(campUUID, subUUID, blocklist); err != nil {
		c.log.Printf("error unsubscribing: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return nil
}

// ConfirmOptionSubscription confirms a customer's optin subscription.
func (c *Core) ConfirmOptionSubscription(subUUID string, listUUIDs []string, meta models.JSON) error {
	if meta == nil {
		meta = models.JSON{}
	}

	if _, err := c.q.ConfirmSubscriptionOptin.Exec(subUUID, pq.Array(listUUIDs), meta); err != nil {
		c.log.Printf("error confirming subscription: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return nil
}

// DeleteCustomerBounces deletes the given customer_list of customers.
func (c *Core) DeleteCustomerBounces(id int, uuid string) error {
	var uu any
	if uuid != "" {
		uu = uuid
	}

	if _, err := c.q.DeleteBouncesByCustomer.Exec(id, uu); err != nil {
		c.log.Printf("error deleting bounces: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.bounces}", "error", pqErrMsg(err)))
	}

	return nil
}

func (c *Core) DeleteCustomerBouncesInWorkspace(access models.WorkspaceAccess, id int) error {
	return c.withWorkspaceResourceMutation(access, resourceCustomers, []int{id}, func(tx *sqlx.Tx) error {
		if _, err := tx.Stmtx(c.q.DeleteBouncesByCustomer).Exec(id, nil); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.bounces}", "error", pqErrMsg(err)))
		}
		return nil
	})
}

// DeleteOrphanCustomers deletes orphan customer records (customers without customer_lists).
func (c *Core) DeleteOrphanCustomers() (int, error) {
	res, err := c.q.DeleteOrphanCustomers.Exec()
	if err != nil {
		c.log.Printf("error deleting orphan customers: %v", err)
		return 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteBlocklistedCustomers deletes blocklisted customers.
func (c *Core) DeleteBlocklistedCustomers() (int, error) {
	res, err := c.q.DeleteBlocklistedCustomers.Exec()
	if err != nil {
		c.log.Printf("error deleting blocklisted customers: %v", err)
		return 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	n, _ := res.RowsAffected()
	return int(n), nil
}

func (c *Core) getCustomerCount(searchStr, queryExp, subStatus string, customerListIDs []int) (int, error) {
	// If there's no condition, it's a "get all" call which can probably be optionally pulled from cache.
	if queryExp == "" {
		_ = c.refreshCache(matListSubStats, false)

		total := 0
		if err := c.q.QueryCustomersCountAll.Get(&total, pq.Array(customerListIDs), subStatus); err != nil {
			return 0, echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
		}

		return total, nil
	}

	// Create a readonly transaction that just does COUNT() to obtain the count of results
	// and to ensure that the arbitrary query is indeed readonly.
	stmt := strings.ReplaceAll(c.q.QueryCustomersCount, "%query%", queryExp)
	tx, err := c.db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		c.log.Printf("error preparing customer query: %v", err)
		return 0, echo.NewHTTPError(http.StatusBadRequest, c.i18n.Ts("customers.errorPreparingQuery", "error", pqErrMsg(err)))
	}
	defer tx.Rollback()

	// Execute the readonly query and get the count of results.
	total := 0
	if err := tx.Get(&total, stmt, pq.Array(customerListIDs), subStatus, searchStr); err != nil {
		return 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return total, nil
}

// validateQueryTables checks if the query accesses only allowed tables.
func validateQueryTables(db *sqlx.DB, query string, allowedTables map[string]struct{}) error {
	return validateQueryTablesWithArgs(db, query, allowedTables,
		nil, models.CustomerStatusEnabled, "", 0, 10)
}

// validateQueryTablesWithArgs is the parameter-aware counterpart used by
// workspace-scoped raw customer queries. EXPLAIN lets PostgreSQL parse the
// final statement before it is executed and exposes every relation in its
// plan, including subqueries and CTEs.
func validateQueryTablesWithArgs(db *sqlx.DB, query string, allowedTables map[string]struct{}, args ...any) error {
	// Get the EXPLAIN (FORMAT JSON) output.
	tx, err := db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var plan string
	if err = tx.QueryRow("EXPLAIN (FORMAT JSON) "+query, args...).Scan(&plan); err != nil {
		return err
	}

	// Extract all relation names from the JSON plan.
	tables, err := getTablesFromQueryPlan(plan)
	if err != nil {
		return fmt.Errorf("error getting tables from query: %v", err)
	}

	// Validate against allowed tables.
	for _, table := range tables {
		if _, ok := allowedTables[table]; !ok {
			return fmt.Errorf("table '%s' is not allowed", table)
		}
	}

	return nil
}

// getTablesFromQueryPlan parses the EXPLAIN JSON to find all "Relation Name" entries.
func getTablesFromQueryPlan(explainJSON string) ([]string, error) {
	var plans []map[string]any
	if err := json.Unmarshal([]byte(explainJSON), &plans); err != nil {
		return nil, err
	}

	// Collect table names in `tables` recursively.
	tables := make(map[string]struct{})
	for _, plan := range plans {
		traverseQueryPlan(plan, tables)
	}

	result := make([]string, 0, len(tables))
	for table := range tables {
		result = append(result, table)
	}
	return result, nil
}

func traverseQueryPlan(node map[string]any, tables map[string]struct{}) {
	if relName, ok := node["Relation Name"].(string); ok {
		tables[relName] = struct{}{}
	}

	// Recursively check nested plans (e.g., subqueries, CTEs).
	for _, v := range node {
		switch v := v.(type) {
		case map[string]any:
			traverseQueryPlan(v, tables)
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					traverseQueryPlan(m, tables)
				}
			}
		}
	}
}
