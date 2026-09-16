package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

type mediaFolderRequest struct {
	Name     string `json:"name"`
	ParentID int    `json:"parent_id"`
}

type mediaFolderMoveRequest struct {
	ParentID *int `json:"parent_id"`
}

type mediaMoveRequest struct {
	FolderID *int `json:"folder_id"`
}

func parseMediaFolderID(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id < 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "invalid media folder")
	}
	return id, nil
}

func parseMediaFolderFilter(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := parseMediaFolderID(raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (a *App) GetMediaFolders(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if !access.IsOrganizationManager() && !user.IsPlatformAdmin() {
		if err := requireLegacyPermission(user, auth.PermMediaGet); err != nil {
			return err
		}
	}
	folders, err := a.core.QueryWorkspaceMediaFolders(access)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{folders})
}

func (a *App) CreateMediaFolder(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermMediaManage); err != nil {
		return err
	}
	var req mediaFolderRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	folder, err := a.core.CreateMediaFolderInWorkspace(access, req.Name, req.ParentID)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.Itoa(folder.ID))
	setAuditMetadata(c, map[string]any{"parent_folder_id": req.ParentID})
	return c.JSON(http.StatusOK, okResp{folder})
}

func (a *App) RenameMediaFolder(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermMediaManage); err != nil {
		return err
	}
	var req mediaFolderRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	folder, err := a.core.RenameMediaFolderInWorkspace(access, getID(c), req.Name)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{folder})
}

func (a *App) DeleteMediaFolder(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermMediaManage); err != nil {
		return err
	}
	if err := a.core.DeleteMediaFolderInWorkspace(access, getID(c)); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) MoveMediaFolder(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermMediaManage); err != nil {
		return err
	}
	var req mediaFolderMoveRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	parentID := 0
	if req.ParentID != nil {
		parentID = *req.ParentID
	}
	if err := a.core.MoveMediaFolderInWorkspace(access, getID(c), parentID); err != nil {
		return err
	}
	setAuditMetadata(c, map[string]any{"parent_folder_id": parentID})
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) MoveMediaToFolder(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if _, err := a.requireManagedWorkspaceResource(c, access, resourceMedia, getID(c), auth.PermMediaManage); err != nil {
		return err
	}
	var req mediaMoveRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	folderID := 0
	if req.FolderID != nil {
		folderID = *req.FolderID
	}
	if err := a.core.MoveMediaToFolderInWorkspace(access, getID(c), folderID); err != nil {
		return err
	}
	setAuditMetadata(c, map[string]any{"folder_id": folderID})
	return c.JSON(http.StatusOK, okResp{true})
}
