package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// GetLists retrieves customer_lists with additional metadata like customer counts.
func (a *App) GetLists(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Minimal query simply returns the customer_list of all customer_lists without JOIN customer counts. This is fast.
	minimal, _ := strconv.ParseBool(c.FormValue("minimal"))
	if minimal {
		status := c.FormValue("status")
		res, _, err := a.queryReadableWorkspaceLists(c, access, "", "", "", status, nil, "name", core.SortAsc, 0, 0)
		if err != nil {
			return err
		}
		if len(res) == 0 {
			return c.JSON(http.StatusOK, okResp{[]struct{}{}})
		}

		// Meta.
		total := len(res)
		out := models.PageResults{
			Results: res,
			Total:   total,
			Page:    1,
			PerPage: total,
		}

		return c.JSON(http.StatusOK, okResp{out})
	}

	// Full customer_list query.
	var (
		query   = strings.TrimSpace(c.FormValue("query"))
		tags    = c.QueryParams()["tag"]
		orderBy = c.FormValue("order_by")
		typ     = c.FormValue("type")
		optin   = c.FormValue("optin")
		status  = c.FormValue("status")
		order   = c.FormValue("order")

		pg = a.pg.NewFromURL(c.Request().URL.Query())
	)
	res, total, err := a.queryReadableWorkspaceLists(c, access, query, typ, optin, status, tags, orderBy, order, pg.Offset, pg.Limit)
	if err != nil {
		return err
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

// GetList retrieves a single customer_list by id.
// It's permission checked by the listPerm middleware.
func (a *App) GetList(c echo.Context) error {
	id := getID(c)
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Public pools are selectable through an independent delivery grant and may
	// live in the platform administrator's workspace. Expose only list metadata
	// here; pool contact endpoints enforce the separate masked-detail policy.
	var poolType string
	if err := a.db.Get(&poolType, `SELECT type::text FROM customer_lists WHERE id=$1`, id); err == nil && (poolType == models.CustomerListTypePool || poolType == models.CustomerListTypePoolSegment) {
		if !access.PlatformAdmin {
			if !access.IsOrganization() || access.OrganizationID <= 0 {
				return echo.NewHTTPError(http.StatusForbidden, "public pool is outside the active workspace")
			}
			var allowed bool
			if poolType == models.CustomerListTypePool {
				if err := a.db.Get(&allowed, `SELECT EXISTS(SELECT 1 FROM pool_organization_permissions WHERE pool_id=$1 AND organization_id=$2)`, id, access.OrganizationID); err != nil {
					return err
				}
				if !allowed {
					if err := a.db.Get(&allowed, `SELECT EXISTS(SELECT 1 FROM pool_segments WHERE pool_id=$1 AND organization_id=$2)`, id, access.OrganizationID); err != nil {
						return err
					}
				}
			} else if err := a.db.Get(&allowed, `SELECT EXISTS(SELECT 1 FROM pool_segments s WHERE s.list_id=$1 AND s.organization_id=$2 AND s.pool_id IS NOT NULL)`, id, access.OrganizationID); err != nil {
				return err
			}
			if !allowed {
				return echo.NewHTTPError(http.StatusForbidden, "public pool is not authorized for this organization")
			}
		}
		out, err := a.core.GetList(id, "")
		if err != nil {
			return err
		}
		out.PoolDeliveryAllowed = true
		return c.JSON(http.StatusOK, okResp{out})
	}
	if _, err := a.requireReadableWorkspaceList(c, access, id); err != nil {
		return err
	}

	// Get the customer_list from the DB.
	out, err := a.core.GetWorkspaceList(access, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// CreateList handles customer_list creation.
func (a *App) CreateList(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	l := models.CustomerList{}
	if err := c.Bind(&l); err != nil {
		return err
	}
	// Secondary public-pool lists are created only by the first-level pool split
	// transaction. They must never be created as standalone customer lists.
	if l.Type == models.CustomerListTypePool {
		if !auth.GetUser(c).IsPlatformAdmin() {
			return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may create a public pool")
		}
	} else if l.Type == models.CustomerListTypePoolSegment {
		return echo.NewHTTPError(http.StatusForbidden, "secondary lists can only be created by splitting a first-level public pool")
	} else if err := requireLegacyPermission(auth.GetUser(c), auth.PermListManageAll); err != nil {
		return err
	}

	// Validate.
	if !strHasLen(l.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customer_lists.invalidName"))
	}

	visibility, err := normalizeResourceVisibility(access, resourceLists, l.Visibility)
	if err != nil {
		return err
	}
	out, err := a.core.CreateListInWorkspace(access, l, core.ApplyWorkspaceScope(access, visibility))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// UpdateList handles customer_list modification.
// It's permission checked by the listPerm middleware.
func (a *App) UpdateList(c echo.Context) error {
	id := getID(c)
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if _, err := a.requireManagedWorkspaceList(c, access, id); err != nil {
		return err
	}

	// Incoming params.
	var l models.CustomerList
	if err := c.Bind(&l); err != nil {
		return err
	}
	if l.Type == models.CustomerListTypePool && !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may manage a public pool")
	}

	// Validate.
	if !strHasLen(l.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("customer_lists.invalidName"))
	}
	visibility := ""
	if l.Visibility != "" {
		visibility, err = normalizeResourceVisibility(access, resourceLists, l.Visibility)
		if err != nil {
			return err
		}
	}

	// Update the customer_list in the DB.
	out, err := a.core.UpdateListInWorkspace(access, id, l, visibility)
	if err != nil {
		return err
	}
	if visibility != "" {
		out.Visibility = visibility
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// DeleteList deletes a single customer_list by ID.
func (a *App) DeleteList(c echo.Context) error {
	id := getID(c)
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if _, err := a.requireManagedWorkspaceList(c, access, id); err != nil {
		return err
	}

	// Delete the customer_list from the DB.
	// Pass getAll=true since we've already verified permissions above.
	if err := a.core.DeleteListsInWorkspace(access, []int{id}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteLists deletes multiple customer_lists by IDs or by query.
func (a *App) DeleteLists(c echo.Context) error {
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

	// The workspace ownership check applies before legacy customer_list-role checks, so
	// no per-customer_list role can widen the active organization boundary.
	if len(ids) > 0 {
		for _, id := range ids {
			if _, err := a.requireManagedWorkspaceList(c, access, id); err != nil {
				return err
			}
		}

		// Delete the customer_lists from the DB.
		// Pass getAll=true since we've already verified permissions above.
		if err := a.core.DeleteListsInWorkspace(access, ids); err != nil {
			return err
		}
	} else {
		if err := requireLegacyPermission(auth.GetUser(c), auth.PermListManageAll); err != nil {
			return err
		}
		managed, err := a.core.CustomerListManagedWorkspaceResources(access, "customer_lists")
		if err != nil {
			return err
		}
		// Keep the page filter when all=true. Managed IDs are an ownership
		// boundary, not a replacement for the user-selected result set.
		visible, _, err := a.core.QueryWorkspaceLists(access, query, "", "", "", nil, "id", "asc", 0, 0)
		if err != nil {
			return err
		}
		allowed := make(map[int]struct{}, len(managed))
		for _, id := range managed {
			allowed[id] = struct{}{}
		}
		for _, customer_list := range visible {
			if _, ok := allowed[customer_list.ID]; ok {
				ids = append(ids, customer_list.ID)
			}
		}
		// DeleteLists' legacy query treats an empty ID array as an
		// unrestricted search. A filter that finds no manageable customer_lists must be
		// a successful no-op, never a broad delete.
		if len(ids) > 0 {
			if err := a.core.DeleteListsInWorkspace(access, ids); err != nil {
				return err
			}
		}
	}

	return c.JSON(http.StatusOK, okResp{true})
}
