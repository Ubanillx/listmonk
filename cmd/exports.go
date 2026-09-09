package main

import (
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/dataexport"
	"github.com/labstack/echo/v4"
)

func (a *App) exportService() *dataexport.Service { return &dataexport.Service{DB: a.db, Log: a.log} }
func (a *App) exportAccess(c echo.Context) (dataexport.Access, error) {
	ws, err := a.workspaceAccess(c)
	if err != nil {
		return dataexport.Access{}, err
	}
	access, _, err := a.exportService().Access(c.Request().Context(), auth.GetUser(c).ID, ws.OrganizationID)
	if err != nil {
		return access, echo.NewHTTPError(http.StatusForbidden, "仅最高管理员或当前组织管理员可导出")
	}
	return access, nil
}
func (a *App) CreateExport(c echo.Context) error {
	access, err := a.exportAccess(c)
	if err != nil {
		return err
	}
	var req dataexport.Request
	if err = c.Bind(&req); err != nil {
		return echo.NewHTTPError(400, "无效导出请求")
	}
	name := "个人空间"
	if access.OrganizationID > 0 {
		if err = a.db.Get(&name, `SELECT name FROM organizations WHERE id=$1`, access.OrganizationID); err != nil {
			return err
		}
	}
	job, err := a.exportService().Create(c.Request().Context(), access, req, name)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}
	return c.JSON(http.StatusAccepted, okResp{job})
}
func (a *App) ListExports(c echo.Context) error {
	access, err := a.exportAccess(c)
	if err != nil {
		return err
	}
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return echo.NewHTTPError(400, "页码过大")
	}
	jobs, err := a.exportService().List(c.Request().Context(), access, page)
	if err != nil {
		return err
	}
	return c.JSON(200, okResp{jobs})
}

func (a *App) ExportOptions(c echo.Context) error {
	access, err := a.exportAccess(c)
	if err != nil {
		return err
	}
	type option struct {
		ID     int    `db:"id" json:"id"`
		Name   string `db:"name" json:"name"`
		Status string `db:"status" json:"status"`
	}
	lists, campaigns := []option{}, []option{}
	if err = a.db.Select(&lists, `SELECT id,name,status FROM customer_lists WHERE COALESCE(organization_id,0)=$1 ORDER BY name,id`, access.OrganizationID); err != nil {
		return err
	}
	if err = a.db.Select(&campaigns, `SELECT id,name,status FROM campaigns WHERE COALESCE(organization_id,0)=$1 ORDER BY name,id`, access.OrganizationID); err != nil {
		return err
	}
	return c.JSON(200, okResp{map[string]any{"lists": lists, "campaigns": campaigns}})
}
func (a *App) DownloadExport(c echo.Context) error {
	access, err := a.exportAccess(c)
	if err != nil {
		return err
	}
	id := c.Param("id")
	if _, err = uuid.FromString(id); err != nil {
		return echo.NewHTTPError(400, "无效任务编号")
	}
	job, rows, err := a.exportService().Download(c.Request().Context(), access, id)
	if err != nil {
		return echo.NewHTTPError(403, "文件不可下载：可能已过期、权限或脱敏设置已变化，请重新生成")
	}
	defer rows.Close()
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	c.Response().Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": job.Filename}))
	c.Response().Header().Set("Content-Type", "application/octet-stream")
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return err
		}
		if _, err = c.Response().Write(b); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("streaming export: %w", err)
	}
	_, err = a.db.Exec(`UPDATE data_export_jobs SET download_count=download_count+1,last_downloaded_at=NOW() WHERE id=$1`, id)
	return err
}
