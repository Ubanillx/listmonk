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

// GetLists retrieves lists with additional metadata like subscriber counts.
func (a *App) GetLists(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Minimal query simply returns the list of all lists without JOIN subscriber counts. This is fast.
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

	// Full list query.
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

// GetList retrieves a single list by id.
// It's permission checked by the listPerm middleware.
func (a *App) GetList(c echo.Context) error {
	id := getID(c)
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if _, err := a.requireReadableWorkspaceList(c, access, id); err != nil {
		return err
	}

	// Get the list from the DB.
	out, err := a.core.GetWorkspaceList(access, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// CreateList handles list creation.
func (a *App) CreateList(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermListManageAll); err != nil {
		return err
	}
	l := models.List{}
	if err := c.Bind(&l); err != nil {
		return err
	}

	// Validate.
	if !strHasLen(l.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("lists.invalidName"))
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

// UpdateList handles list modification.
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
	var l models.List
	if err := c.Bind(&l); err != nil {
		return err
	}

	// Validate.
	if !strHasLen(l.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("lists.invalidName"))
	}
	visibility := ""
	if l.Visibility != "" {
		visibility, err = normalizeResourceVisibility(access, resourceLists, l.Visibility)
		if err != nil {
			return err
		}
	}

	// Update the list in the DB.
	out, err := a.core.UpdateListInWorkspace(access, id, l, visibility)
	if err != nil {
		return err
	}
	if visibility != "" {
		out.Visibility = visibility
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// DeleteList deletes a single list by ID.
func (a *App) DeleteList(c echo.Context) error {
	id := getID(c)
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if _, err := a.requireManagedWorkspaceList(c, access, id); err != nil {
		return err
	}

	// Delete the list from the DB.
	// Pass getAll=true since we've already verified permissions above.
	if err := a.core.DeleteListsInWorkspace(access, []int{id}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteLists deletes multiple lists by IDs or by query.
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

	// The workspace ownership check applies before legacy list-role checks, so
	// no per-list role can widen the active organization boundary.
	if len(ids) > 0 {
		for _, id := range ids {
			if _, err := a.requireManagedWorkspaceList(c, access, id); err != nil {
				return err
			}
		}

		// Delete the lists from the DB.
		// Pass getAll=true since we've already verified permissions above.
		if err := a.core.DeleteListsInWorkspace(access, ids); err != nil {
			return err
		}
	} else {
		if err := requireLegacyPermission(auth.GetUser(c), auth.PermListManageAll); err != nil {
			return err
		}
		managed, err := a.core.ListManagedWorkspaceResources(access, "lists")
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
		for _, list := range visible {
			if _, ok := allowed[list.ID]; ok {
				ids = append(ids, list.ID)
			}
		}
		// DeleteLists' legacy query treats an empty ID array as an
		// unrestricted search. A filter that finds no manageable lists must be
		// a successful no-op, never a broad delete.
		if len(ids) > 0 {
			if err := a.core.DeleteListsInWorkspace(access, ids); err != nil {
				return err
			}
		}
	}

	return c.JSON(http.StatusOK, okResp{true})
}
