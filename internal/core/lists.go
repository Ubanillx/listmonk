package core

import (
	"net/http"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

type listType struct {
	ID   int    `json:"id"`
	UUID string `json:"uuid"`
	Type string `json:"type"`
}

// GetLists gets all customer_lists optionally filtered by type and status.
func (c *Core) GetLists(typ, status string, getAll bool, permittedIDs []int) ([]models.CustomerList, error) {
	out := []models.CustomerList{}

	if err := c.q.GetLists.Select(&out, typ, status, "id", getAll, pq.Array(permittedIDs)); err != nil {
		c.log.Printf("error fetching customer_lists: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer_lists}", "error", pqErrMsg(err)))
	}

	// Replace null tags.
	for i, l := range out {
		if l.Tags == nil {
			out[i].Tags = []string{}
		}

		// Total counts.
		for _, c := range l.CustomerCounts {
			out[i].CustomerCount += c
		}
	}

	return out, nil
}

// QueryLists gets multiple customer_lists based on multiple query params. Along with the  paginated and sliced
// results, the total number of customer_lists in the DB is returned.
func (c *Core) QueryLists(searchStr, typ, optin, status string, tags []string, orderBy, order string, getAll bool, permittedIDs []int, offset, limit int) ([]models.CustomerList, int, error) {
	_ = c.refreshCache(matListSubStats, false)

	if tags == nil {
		tags = []string{}
	}

	var (
		out            = []models.CustomerList{}
		queryStr, stmt = makeSearchQuery(searchStr, orderBy, order, c.q.QueryLists, listQuerySortFields)
	)
	if err := c.db.Select(&out, stmt, 0, "", queryStr, typ, optin, status, pq.StringArray(tags), getAll, pq.Array(permittedIDs), offset, limit); err != nil {
		c.log.Printf("error fetching customer_lists: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer_lists}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total

		// Replace null tags.
		for i, l := range out {
			if l.Tags == nil {
				out[i].Tags = []string{}
			}
		}
	}

	return out, total, nil
}

// GetList gets a customer_list by its ID or UUID.
func (c *Core) GetList(id int, uuid string) (models.CustomerList, error) {
	var uu any
	if uuid != "" {
		uu = uuid
	}

	var res []models.CustomerList
	queryStr, stmt := makeSearchQuery("", "", "", c.q.QueryLists, nil)
	if err := c.db.Select(&res, stmt, id, uu, queryStr, "", "", "", pq.StringArray{}, true, nil, 0, 1); err != nil {
		c.log.Printf("error fetching customer_lists: %v", err)
		return models.CustomerList{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer_lists}", "error", pqErrMsg(err)))
	}

	if len(res) == 0 {
		return models.CustomerList{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.customer_list}"))
	}

	out := res[0]
	if out.Tags == nil {
		out.Tags = []string{}
	}
	// Total counts.
	for _, c := range out.CustomerCounts {
		out.CustomerCount += c
	}

	return out, nil
}

// GetListsByOptin returns customer_lists by optin type.
func (c *Core) GetListsByOptin(ids []int, optinType string) ([]models.CustomerList, error) {
	out := []models.CustomerList{}
	if err := c.q.GetListsByOptin.Select(&out, optinType, pq.Array(ids), nil); err != nil {
		c.log.Printf("error fetching customer_lists for opt-in: %s", pqErrMsg(err))
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer_list}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetListTypes returns customer_lists by their IDs or UUIDs.
// If ids is given, then the map returned has the customer_list IDs as keys,
// otherwise, they have UUIDs as the keys.
// Note: This is a really weird and awkward API. Ideally, Go Generics
// should've somehow supported generic struct methods.
func (c *Core) GetListTypes(ids []int, uuids []string) (map[any]string, error) {
	res := []listType{}

	out := map[any]string{}
	if err := c.q.GetListTypes.Select(&res, pq.Array(ids), pq.StringArray(uuids)); err != nil {
		c.log.Printf("error fetching customer_list types: %v", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.customer_list}", "error", pqErrMsg(err)))
	}

	isIDs := ids != nil
	for _, r := range res {
		if isIDs {
			out[r.ID] = r.Type
		} else {
			out[r.UUID] = r.Type
		}
	}

	return out, nil
}

// CreateList creates a new customer_list.
func (c *Core) CreateList(l models.CustomerList, scope models.ResourceScope) (models.CustomerList, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return models.CustomerList{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}

	if l.Type == "" {
		l.Type = models.CustomerListTypePrivate
	}
	if l.Optin == "" {
		l.Optin = models.CustomerListOptinSingle
	}
	if l.Status == "" {
		l.Status = models.CustomerListStatusActive
	}

	// Insert and read ID.
	var newID int
	l.UUID = uu.String()
	if err := c.q.CreateList.Get(&newID,
		l.UUID, l.Name, l.Type, l.Optin, l.Status, pq.StringArray(normalizeTags(l.Tags)), l.Description, l.MaskEmails,
		scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility); err != nil {
		c.log.Printf("error creating customer_list: %v", err)
		return models.CustomerList{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.customer_list}", "error", pqErrMsg(err)))
	}
	if l.PoolParentID.Valid {
		if _, err := c.db.Exec(`UPDATE customer_lists SET pool_parent_id=$1 WHERE id=$2`, l.PoolParentID.Int, newID); err != nil {
			return models.CustomerList{}, err
		}
	}

	return c.GetList(newID, "")
}

// CreateListInWorkspace validates that the target organization remains active
// and that the caller still belongs to it while the customer_list is inserted.
func (c *Core) CreateListInWorkspace(access models.WorkspaceAccess, l models.CustomerList, scope models.ResourceScope) (models.CustomerList, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return models.CustomerList{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	if l.Type == "" {
		l.Type = models.CustomerListTypePrivate
	}
	if l.Optin == "" {
		l.Optin = models.CustomerListOptinSingle
	}
	if l.Status == "" {
		l.Status = models.CustomerListStatusActive
	}

	var newID int
	err = c.withWorkspaceCreation(access, func(tx *sqlx.Tx) error {
		if err := tx.Stmtx(c.q.CreateList).Get(&newID,
			uu.String(), l.Name, l.Type, l.Optin, l.Status, pq.StringArray(normalizeTags(l.Tags)), l.Description, l.MaskEmails,
			scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.customer_list}", "error", pqErrMsg(err)))
		}
		if l.PoolParentID.Valid {
			if _, err := tx.Exec(`UPDATE customer_lists SET pool_parent_id=$1 WHERE id=$2`, l.PoolParentID.Int, newID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return models.CustomerList{}, err
	}
	return c.GetWorkspaceList(access, newID)
}

// UpdateList updates a given customer_list.
func (c *Core) UpdateList(id int, l models.CustomerList) (models.CustomerList, error) {
	res, err := c.q.UpdateList.Exec(id, l.Name, l.Type, l.Optin, l.Status, pq.StringArray(normalizeTags(l.Tags)), l.Description, l.MaskEmails)
	if err != nil {
		c.log.Printf("error updating customer_list: %v", err)
		return models.CustomerList{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.customer_list}", "error", pqErrMsg(err)))
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return models.CustomerList{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.customer_list}"))
	}

	return c.GetList(id, "")
}

// DeleteList deletes a customer_list.
func (c *Core) DeleteList(id int) error {
	return c.DeleteLists([]int{id}, "", true, nil)
}

// DeleteLists deletes multiple customer_lists.
func (c *Core) DeleteLists(ids []int, query string, getAll bool, permittedIDs []int) error {
	var queryStr string

	if len(ids) > 0 {
		queryStr = ""
	} else {
		queryStr = makeSearchString(query)
	}

	if _, err := c.q.DeleteLists.Exec(pq.Array(ids), queryStr, getAll, pq.Array(permittedIDs)); err != nil {
		c.log.Printf("error deleting customer_lists: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customer_lists}", "error", pqErrMsg(err)))
	}
	return nil
}
