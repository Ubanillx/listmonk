package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
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

func auditMediaFolderName(folders []media.MediaFolder, id int) (string, bool) {
	for _, folder := range folders {
		if folder.ID == id {
			return folder.Name, true
		}
	}
	return "", false
}

// setAuditMediaMoveDetails captures the file and both sides of a media-folder
// move before the mutation runs, so failed requests are understandable too.
func (a *App) setAuditMediaMoveDetails(c echo.Context, access models.WorkspaceAccess, mediaID, targetFolderID int) {
	details := map[string]any{"target_folder_id": targetFolderID}
	if targetFolderID == 0 {
		details["target_folder_root"] = true
	}

	current, err := a.core.GetWorkspaceMediaByID(access, mediaID)
	if err == nil {
		details["filename"] = current.Filename
		if current.FolderID.Valid {
			details["source_folder_id"] = current.FolderID.Int
		} else {
			details["source_folder_root"] = true
		}
	}

	if folders, err := a.core.QueryWorkspaceMediaFolders(access); err == nil {
		if current.FolderID.Valid {
			if name, ok := auditMediaFolderName(folders, int(current.FolderID.Int)); ok {
				details["source_folder_name"] = name
			}
		}
		if targetFolderID > 0 {
			if name, ok := auditMediaFolderName(folders, targetFolderID); ok {
				details["target_folder_name"] = name
			}
		}
	}

	setAuditObjectDetails(c, details)
}

func (a *App) setAuditMediaFolderMoveDetails(c echo.Context, access models.WorkspaceAccess, id, parentID int) {
	details := map[string]any{
		"target_folder_id": parentID,
	}
	if parentID == 0 {
		details["target_folder_root"] = true
	}

	if folders, err := a.core.QueryWorkspaceMediaFolders(access); err == nil {
		if name, ok := auditMediaFolderName(folders, id); ok {
			details["name"] = name
		}
		if parentID > 0 {
			if name, ok := auditMediaFolderName(folders, parentID); ok {
				details["target_folder_name"] = name
			}
		}
	}
	setAuditObjectDetails(c, details)
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
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
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
	setAuditObjectDetails(c, map[string]any{"name": folder.Name})
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
	setAuditObjectDetails(c, map[string]any{"name": folder.Name})
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
	if folders, err := a.core.QueryWorkspaceMediaFolders(access); err == nil {
		if name, ok := auditMediaFolderName(folders, getID(c)); ok {
			setAuditObjectDetails(c, map[string]any{"name": name})
		}
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
	a.setAuditMediaFolderMoveDetails(c, access, getID(c), parentID)
	setAuditMetadata(c, map[string]any{"parent_folder_id": parentID})
	if err := a.core.MoveMediaFolderInWorkspace(access, getID(c), parentID); err != nil {
		return err
	}
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
	a.setAuditMediaMoveDetails(c, access, getID(c), folderID)
	setAuditMetadata(c, map[string]any{"folder_id": folderID})
	if err := a.core.MoveMediaToFolderInWorkspace(access, getID(c), folderID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}
