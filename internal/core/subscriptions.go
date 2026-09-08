package core

import (
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// GetSubscriptions retrieves the subscriptions for a customer.
func (c *Core) GetSubscriptions(subID int, subUUID string, allLists bool) ([]models.Subscription, error) {
	var out []models.Subscription
	err := c.q.GetSubscriptions.Select(&out, subID, subUUID, allLists)
	if err != nil {
		c.log.Printf("error getting subscriptions: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customers}", "error", err.Error()))
	}

	return out, err
}

// AddSubscriptions adds customer_list subscriptions to customers.
func (c *Core) AddSubscriptions(subIDs, customerListIDs []int, status string) error {
	if _, err := c.q.AddCustomersToLists.Exec(pq.Array(subIDs), pq.Array(customerListIDs), status); err != nil {
		c.log.Printf("error adding subscriptions: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", err.Error()))
	}

	return nil
}

// AddSubscriptionsInWorkspace applies a bulk subscription change while both
// sides of every relation are locked inside the active workspace transaction.
func (c *Core) AddSubscriptionsInWorkspace(access models.WorkspaceAccess, subIDs, customerListIDs []int, status string) error {
	subIDs = uniqueMutationIDs(subIDs)
	customerListIDs = uniqueMutationIDs(customerListIDs)
	if len(subIDs) == 0 || len(customerListIDs) == 0 {
		return nil
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, subIDs, func(tx *sqlx.Tx) error {
		if err := c.lockWorkspaceMutationResources(tx, access, resourceLists, customerListIDs); err != nil {
			return err
		}
		if _, err := tx.Stmtx(c.q.AddCustomersToLists).Exec(pq.Array(subIDs), pq.Array(customerListIDs), status); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
		}
		return nil
	})
}

// AddSubscriptionsByQuery adds customer_list subscriptions to customers by a given arbitrary query expression.
// sourceCustomerListIDs is the customer_list of customer_list IDs to filter the customer query with.
func (c *Core) AddSubscriptionsByQuery(searchStr, queryExp string, sourceCustomerListIDs, targetCustomerListIDs []int, status string, subStatus string) error {
	if sourceCustomerListIDs == nil {
		sourceCustomerListIDs = []int{}
	}

	err := c.q.ExecSubQueryTpl(searchStr, queryExp, c.q.AddCustomersToListsByQuery, sourceCustomerListIDs, c.db, subStatus, pq.Array(targetCustomerListIDs), status)
	if err != nil {
		c.log.Printf("error adding subscriptions by query: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return nil
}

// DeleteSubscriptions delete customer_list subscriptions from customers.
func (c *Core) DeleteSubscriptions(subIDs, customerListIDs []int) error {
	if _, err := c.q.DeleteSubscriptions.Exec(pq.Array(subIDs), pq.Array(customerListIDs)); err != nil {
		c.log.Printf("error deleting subscriptions: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", err.Error()))

	}

	return nil
}

func (c *Core) DeleteSubscriptionsInWorkspace(access models.WorkspaceAccess, subIDs, customerListIDs []int) error {
	subIDs = uniqueMutationIDs(subIDs)
	customerListIDs = uniqueMutationIDs(customerListIDs)
	if len(subIDs) == 0 || len(customerListIDs) == 0 {
		return nil
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, subIDs, func(tx *sqlx.Tx) error {
		if err := c.lockWorkspaceMutationResources(tx, access, resourceLists, customerListIDs); err != nil {
			return err
		}
		if _, err := tx.Stmtx(c.q.DeleteSubscriptions).Exec(pq.Array(subIDs), pq.Array(customerListIDs)); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
		}
		return nil
	})
}

// DeleteSubscriptionsByQuery deletes customer_list subscriptions from customers by a given arbitrary query expression.
// sourceCustomerListIDs is the customer_list of customer_list IDs to filter the customer query with.
func (c *Core) DeleteSubscriptionsByQuery(searchStr, queryExp string, sourceCustomerListIDs, targetCustomerListIDs []int, subStatus string) error {
	if sourceCustomerListIDs == nil {
		sourceCustomerListIDs = []int{}
	}

	err := c.q.ExecSubQueryTpl(searchStr, queryExp, c.q.DeleteSubscriptionsByQuery, sourceCustomerListIDs, c.db, subStatus, pq.Array(targetCustomerListIDs))
	if err != nil {
		c.log.Printf("error deleting subscriptions by query: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return nil
}

// UnsubscribeLists sets customer_list subscriptions to 'unsubscribed'.
func (c *Core) UnsubscribeLists(subIDs, customerListIDs []int, listUUIDs []string) error {
	if _, err := c.q.UnsubscribeCustomersFromLists.Exec(pq.Array(subIDs), pq.Array(customerListIDs), pq.StringArray(listUUIDs)); err != nil {
		c.log.Printf("error unsubscribing from customer_lists: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", err.Error()))
	}

	return nil
}

func (c *Core) UnsubscribeListsInWorkspace(access models.WorkspaceAccess, subIDs, customerListIDs []int) error {
	subIDs = uniqueMutationIDs(subIDs)
	customerListIDs = uniqueMutationIDs(customerListIDs)
	if len(subIDs) == 0 || len(customerListIDs) == 0 {
		return nil
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, subIDs, func(tx *sqlx.Tx) error {
		if err := c.lockWorkspaceMutationResources(tx, access, resourceLists, customerListIDs); err != nil {
			return err
		}
		if _, err := tx.Stmtx(c.q.UnsubscribeCustomersFromLists).Exec(pq.Array(subIDs), pq.Array(customerListIDs), pq.Array([]string{})); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
		}
		return nil
	})
}

// UnsubscribeListsByQuery sets customer_list subscriptions to 'unsubscribed' by a given arbitrary query expression.
// sourceCustomerListIDs is the customer_list of customer_list IDs to filter the customer query with.
func (c *Core) UnsubscribeListsByQuery(searchStr, queryExp string, sourceCustomerListIDs, targetCustomerListIDs []int, subStatus string) error {
	if sourceCustomerListIDs == nil {
		sourceCustomerListIDs = []int{}
	}

	err := c.q.ExecSubQueryTpl(searchStr, queryExp, c.q.UnsubscribeCustomersFromListsByQuery, sourceCustomerListIDs, c.db, subStatus, pq.Array(targetCustomerListIDs))
	if err != nil {
		c.log.Printf("error unsubscribing from customer_lists by query: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	return nil
}

// DeleteUnconfirmedSubscriptions sets customer_list subscriptions to 'unsubscribed' by a given arbitrary query expression.
// sourceCustomerListIDs is the customer_list of customer_list IDs to filter the customer query with.
func (c *Core) DeleteUnconfirmedSubscriptions(beforeDate time.Time) (int, error) {
	res, err := c.q.DeleteUnconfirmedSubscriptions.Exec(beforeDate)
	if err != nil {
		c.log.Printf("error deleting unconfirmed customers: %v", err)
		return 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customers}", "error", pqErrMsg(err)))
	}

	n, _ := res.RowsAffected()
	return int(n), nil
}
