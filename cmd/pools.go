package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
)

func (a *App) GetPoolContacts(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	user := auth.GetUser(c)
	rows, err := a.core.QueryPoolContacts(id, access.OrganizationID, user.IsPlatformAdmin(), c.QueryParam("customer_code"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

func (a *App) GetPoolSegments(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	rows, err := a.core.QueryPoolSegments(id, int64(access.OrganizationID), auth.GetUser(c).IsPlatformAdmin())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

// GetPoolManagementTarget resolves an allocation target for a first-level
// public pool. Unlike a workspace selection, this is not a membership action:
// a highest administrator can select any active organization here while
// remaining in the current workspace.
func (a *App) GetPoolManagementTarget(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may select a pool target organization")
	}
	poolID := getID(c)
	organizationID, err := strconv.Atoi(c.QueryParam("organization_id"))
	if err != nil || organizationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "organization_id is required")
	}
	out, err := a.core.QueryPoolManagementTarget(poolID, organizationID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetPoolImportConflicts(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may inspect pool conflicts")
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	var rows []struct {
		ID               int64       `db:"id"`
		ContactID        *int64      `db:"contact_id"`
		CustomerCode     string      `db:"customer_code"`
		ExistingSnapshot models.JSON `db:"existing_snapshot"`
		IncomingSnapshot models.JSON `db:"incoming_snapshot"`
		CreatedByUserID  *int        `db:"created_by_user_id"`
		CreatedAt        string      `db:"created_at"`
	}
	if err := a.db.Select(&rows, `SELECT id,contact_id,customer_code,existing_snapshot,incoming_snapshot,created_by_user_id,created_at::text FROM pool_merge_conflicts WHERE pool_id=$1 ORDER BY id DESC`, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

type poolOrganizationRequest struct {
	PoolID         int   `json:"pool_id"`
	OrganizationID int64 `json:"organization_id"`
}

func (a *App) GrantPoolOrganization(c echo.Context) error {
	u := auth.GetUser(c)
	if !u.IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may grant pool delivery access")
	}
	var req poolOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.PoolID <= 0 || req.OrganizationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool_id and organization_id are required")
	}
	if err := a.core.GrantPoolOrganization(req.PoolID, req.OrganizationID, u.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) RevokePoolOrganization(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may revoke pool delivery access")
	}
	var req poolOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := a.core.RevokePoolOrganization(req.PoolID, req.OrganizationID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) CreatePoolContact(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may import public-pool contacts")
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	var p models.PoolContact
	if err := c.Bind(&p); err != nil {
		return err
	}
	out, err := a.core.CreatePoolContact(id, p)
	if err != nil {
		return err
	}
	// Never leak the source address through this endpoint to ordinary users.
	if u := auth.GetUser(c); !u.IsPlatformAdmin() {
		return c.JSON(http.StatusOK, okResp{out.Safe()})
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ClearPoolContactEmail(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() && !access.IsOrganizationManager() {
		return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	contactID, err := strconv.ParseInt(c.Param("contact_id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid contact id")
	}
	if err := a.core.ClearPoolContactEmail(poolID, contactID, int64(access.OrganizationID), auth.GetUser(c).IsPlatformAdmin()); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

type poolSegmentRequest struct {
	PoolID         int    `json:"pool_id"`
	OrganizationID int64  `json:"organization_id"`
	Name           string `json:"name"`
	ReplyMailboxID *int   `json:"reply_mailbox_id"`
}

func (a *App) CreatePoolSegment(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() && !access.IsOrganizationManager() {
		return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	var req poolSegmentRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	u := auth.GetUser(c)
	userID := 0
	userID = u.ID
	if u.IsPlatformAdmin() && req.ReplyMailboxID != nil {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox must be configured in the organization workspace")
	}
	if !auth.GetUser(c).IsPlatformAdmin() && req.OrganizationID != int64(access.OrganizationID) {
		return echo.NewHTTPError(http.StatusForbidden, "organization scope mismatch")
	}
	if !auth.GetUser(c).IsPlatformAdmin() {
		var permitted bool
		if err := a.db.Get(&permitted, `SELECT EXISTS(
			SELECT 1 FROM pool_organization_permissions WHERE pool_id=$1 AND organization_id=$2
			UNION ALL
			SELECT 1 FROM pool_segments WHERE pool_id=$1 AND organization_id=$2
		)`, req.PoolID, req.OrganizationID); err != nil {
			return err
		}
		if !permitted {
			return echo.NewHTTPError(http.StatusForbidden, "pool delivery access has not been granted to organization")
		}
	}
	out, err := a.core.CreatePoolSegment(req.PoolID, req.OrganizationID, req.Name, req.ReplyMailboxID, userID, auth.GetUser(c).IsPlatformAdmin())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

type poolSegmentMailboxRequest struct {
	ReplyMailboxID *int `json:"reply_mailbox_id"`
}

func (a *App) UpdatePoolSegmentReplyMailbox(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() && !access.IsOrganizationManager() {
		return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	if auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox must be configured in the organization workspace")
	}
	segmentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid segment id")
	}
	var req poolSegmentMailboxRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() {
		var orgID int64
		if err := a.db.Get(&orgID, `SELECT organization_id FROM pool_segments WHERE id=$1`, segmentID); err != nil {
			return err
		}
		if orgID != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "organization scope mismatch")
		}
	}
	if err := a.core.UpdatePoolSegmentReplyMailbox(segmentID, req.ReplyMailboxID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

type poolMembershipRequest struct {
	SegmentID int64  `json:"segment_id"`
	ContactID int64  `json:"contact_id"`
	Reason    string `json:"reason"`
}

type campaignPoolRequest struct {
	PoolID         int    `json:"pool_id"`
	PoolSegmentID  *int64 `json:"pool_segment_id"`
	OrganizationID int64  `json:"organization_id"`
}

// ImportListIntoPool remains a separate, explicit maintenance operation for
// ordinary lists. It does not create or bind secondary pool lists.
type poolImportRequest struct {
	ListID int `json:"list_id"`
	PoolID int `json:"pool_id"`
}

func (a *App) ImportListIntoPool(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may import lists into a pool")
	}
	var req poolImportRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.ListID <= 0 || req.PoolID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "list_id and pool_id are required")
	}
	if err := a.core.ImportListIntoPool(req.ListID, req.PoolID, auth.GetUser(c).ID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) AttachCampaignPool(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	campaignID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid campaign id")
	}
	if _, err := a.requireManagedWorkspaceCampaign(c, access, campaignID); err != nil {
		return err
	}
	var req campaignPoolRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.PoolID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool_id is required")
	}
	orgID := int64(access.OrganizationID)
	if auth.GetUser(c).IsPlatformAdmin() && req.OrganizationID > 0 {
		orgID = req.OrganizationID
	}
	if err := a.core.AttachPoolToCampaign(campaignID, req.PoolID, req.PoolSegmentID, orgID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) AssignPoolContact(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() && !access.IsOrganizationManager() {
		return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() {
		var segmentOrg int64
		if err := a.db.Get(&segmentOrg, `SELECT organization_id FROM pool_segments WHERE id=$1`, req.SegmentID); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "pool segment not found")
		}
		if segmentOrg != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "organization scope mismatch")
		}
	}
	if err := a.core.AssignPoolContact(req.SegmentID, req.ContactID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// ImportPoolSegmentMembers accepts a CSV or XLSX file with customer_code and
// email columns. The server performs the match against the selected pool so
// clients never need to send thousands of contact IDs over individual calls.
func (a *App) ImportPoolSegmentMembers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	u := auth.GetUser(c)
	if !u.IsPlatformAdmin() {
		if err := requireWritableWorkspace(access); err != nil {
			return err
		}
		if !access.IsOrganizationManager() {
			return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
		}
	}
	segmentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || segmentID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool segment id")
	}
	var segmentOrg int64
	if err := a.db.Get(&segmentOrg, `SELECT organization_id FROM pool_segments WHERE id=$1`, segmentID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "pool segment not found")
	}
	if !u.IsPlatformAdmin() && segmentOrg != int64(access.OrganizationID) {
		return echo.NewHTTPError(http.StatusForbidden, "organization scope mismatch")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "file is required")
	}
	if file.Size > 25<<20 {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "allocation file must be 25 MB or smaller")
	}
	rows, err := parsePoolAllocationFile(file)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	result, err := a.core.ImportPoolSegmentMembers(segmentID, rows)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{result})
}

func parsePoolAllocationFile(file *multipart.FileHeader) ([]models.PoolImportRow, error) {
	name := strings.ToLower(file.Filename)
	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	if strings.HasSuffix(name, ".csv") {
		reader := csv.NewReader(src)
		reader.FieldsPerRecord = -1
		header, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("allocation file is empty")
			}
			return nil, fmt.Errorf("invalid CSV header: %w", err)
		}
		return parsePoolAllocationRows(header, func() ([]string, error) { return reader.Read() })
	}
	if strings.HasSuffix(name, ".xlsx") {
		workbook, err := excelize.OpenReader(src)
		if err != nil {
			return nil, fmt.Errorf("invalid XLSX file: %w", err)
		}
		defer workbook.Close()
		sheets := workbook.GetSheetList()
		if len(sheets) == 0 {
			return nil, fmt.Errorf("XLSX file has no worksheet")
		}
		allRows, err := workbook.GetRows(sheets[0])
		if err != nil {
			return nil, fmt.Errorf("cannot read XLSX worksheet: %w", err)
		}
		if len(allRows) == 0 {
			return nil, fmt.Errorf("allocation file is empty")
		}
		index := 1
		return parsePoolAllocationRows(allRows[0], func() ([]string, error) {
			if index >= len(allRows) {
				return nil, io.EOF
			}
			row := allRows[index]
			index++
			return row, nil
		})
	}
	return nil, fmt.Errorf("only .csv and .xlsx files are supported")
}

func parsePoolAllocationRows(header []string, next func() ([]string, error)) ([]models.PoolImportRow, error) {
	codeIndex, emailIndex := -1, -1
	for i, value := range header {
		normalized := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
		switch normalized {
		case "customer_code", "customer code", "客户编码":
			codeIndex = i
		case "email", "mail", "邮箱", "邮件地址":
			emailIndex = i
		}
	}
	if codeIndex < 0 || emailIndex < 0 {
		return nil, fmt.Errorf("首行必须包含 customer_code 和 email 列")
	}
	rows := make([]models.PoolImportRow, 0)
	rowNumber := 2
	for {
		values, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 行失败: %w", rowNumber, err)
		}
		code, email := "", ""
		if codeIndex < len(values) {
			code = strings.TrimSpace(values[codeIndex])
		}
		if emailIndex < len(values) {
			email = strings.TrimSpace(values[emailIndex])
		}
		if code == "" && email == "" {
			rowNumber++
			continue
		}
		rows = append(rows, models.PoolImportRow{Row: rowNumber, CustomerCode: code, Email: email})
		rowNumber++
		if len(rows) > 100000 {
			return nil, fmt.Errorf("文件最多支持 100000 条联系人")
		}
	}
	return rows, nil
}

func (a *App) RemovePoolContact(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() && !access.IsOrganizationManager() {
		return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	u := auth.GetUser(c)
	userID := 0
	userID = u.ID
	if !u.IsPlatformAdmin() {
		var segmentOrg int64
		if err := a.db.Get(&segmentOrg, `SELECT organization_id FROM pool_segments WHERE id=$1`, req.SegmentID); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "pool segment not found")
		}
		if segmentOrg != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "organization scope mismatch")
		}
	}
	if err := a.core.RemovePoolContact(req.SegmentID, req.ContactID, userID, req.Reason); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) RestorePoolContact(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() && !access.IsOrganizationManager() {
		return echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() {
		var segmentOrg int64
		if err := a.db.Get(&segmentOrg, `SELECT organization_id FROM pool_segments WHERE id=$1`, req.SegmentID); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "pool segment not found")
		}
		if segmentOrg != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "organization scope mismatch")
		}
	}
	if err := a.core.RestorePoolContact(req.SegmentID, req.ContactID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}
