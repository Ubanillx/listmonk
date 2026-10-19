package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func (a *App) changeLegacyAllocationMember(c echo.Context, req poolMembershipRequest, action string) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	err = a.core.ChangeAllocationMembers(access, req.AllocationID, action, req.Reason, []int64{req.ContactID})
	if err == nil {
		c.Set("allocation_audit_recorded", true)
	}
	return err
}

func (a *App) allocationManagementAccess(c echo.Context) (core.AllocationAccess, error) {
	u := auth.GetUser(c)
	across := canManagePoolAllocationsAcrossOrganizations(u)
	out := core.AllocationAccess{UserID: u.ID, Manage: u.HasPerm(auth.PermPoolsManage) || across, Across: across,
		Read: u.HasPerm(auth.PermPoolsGet), Audit: u.HasPerm(auth.PermAuditGet), Highest: u.IsPlatformAdmin()}
	if !out.Manage {
		return out, echo.NewHTTPError(http.StatusForbidden, "allocation management permission is required")
	}
	if !across {
		access, err := a.workspaceAccess(c)
		if err != nil {
			return out, err
		}
		if err := requireWritableWorkspace(access); err != nil {
			return out, err
		}
		out.OrganizationID = access.OrganizationID
	}
	return out, nil
}

func allocationManagementID(c echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "invalid allocation id")
	}
	return id, nil
}

func decodeAllocationBody(c echo.Context, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 64*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid allocation request")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid allocation request")
	}
	return nil
}

func allocationPage(c echo.Context) (int, int, error) {
	page, size := 1, 20
	for name, target := range map[string]*int{"page": &page, "per_page": &size} {
		if raw := c.QueryParam(name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v <= 0 || v > 1000000 {
				return 0, 0, echo.NewHTTPError(http.StatusBadRequest, "invalid pagination")
			}
			*target = v
		}
	}
	if size > 100 {
		size = 100
	}
	return page, size, nil
}

func (a *App) GetAllocationManagement(c echo.Context) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	id, err := allocationManagementID(c)
	if err != nil {
		return err
	}
	d, err := a.core.GetAllocationDetail(access, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{d})
}

func (a *App) ChangeAllocationManagement(c echo.Context) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	id, err := allocationManagementID(c)
	if err != nil {
		return err
	}
	var in core.AllocationChange
	if err := decodeAllocationBody(c, &in); err != nil {
		return err
	}
	d, err := a.core.GetAllocationDetail(access, id)
	if err != nil {
		return err
	}
	setAuditOrganizationID(c, d.OrganizationID)
	if err := a.core.ChangeAllocation(access, id, in); err != nil {
		return err
	}
	c.Set("allocation_audit_recorded", true)
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) GetAllocationImpact(c echo.Context) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	id, err := allocationManagementID(c)
	if err != nil {
		return err
	}
	n, err := a.core.AllocationImpact(access, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{map[string]int{"unfinished_campaigns": n}})
}

func (a *App) GetAllocationMembers(c echo.Context) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	id, err := allocationManagementID(c)
	if err != nil {
		return err
	}
	page, size, err := allocationPage(c)
	if err != nil {
		return err
	}
	rows, total, err := a.core.AllocationContacts(access, id, c.QueryParam("status"), c.QueryParam("search"), (page-1)*size, size)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{models.PageResults{Results: rows, Total: total, Page: page, PerPage: size}})
}

func (a *App) ChangeAllocationMembers(c echo.Context) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	id, err := allocationManagementID(c)
	if err != nil {
		return err
	}
	var in struct {
		Action     string  `json:"action"`
		Reason     string  `json:"reason"`
		ContactIDs []int64 `json:"contact_ids"`
	}
	if err := decodeAllocationBody(c, &in); err != nil {
		return err
	}
	d, err := a.core.GetAllocationDetail(access, id)
	if err != nil {
		return err
	}
	setAuditOrganizationID(c, d.OrganizationID)
	if err := a.core.ChangeAllocationMembers(access, id, in.Action, in.Reason, in.ContactIDs); err != nil {
		return err
	}
	c.Set("allocation_audit_recorded", true)
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) GetAllocationEvents(c echo.Context) error {
	access, err := a.allocationManagementAccess(c)
	if err != nil {
		return err
	}
	id, err := allocationManagementID(c)
	if err != nil {
		return err
	}
	page, size, err := allocationPage(c)
	if err != nil {
		return err
	}
	rows, total, err := a.core.AllocationEvents(access, id, (page-1)*size, size)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{models.PageResults{Results: rows, Total: total, Page: page, PerPage: size}})
}
