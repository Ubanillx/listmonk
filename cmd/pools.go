package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
)

// Pool allocation file limits. The upload is a CSV or XLSX holding
// customer_code/email pairs for one segment; the archive limits bound what an
// XLSX may expand to, since a spreadsheet is a ZIP archive.
const (
	maxPoolAllocationUploadSize = 25 << 20
	maxPoolAllocationUnzipSize  = 256 << 20
)

var poolContactImportAliases = map[string][]string{
	"customer_code":         {"customer_code", "customer code", "customercode", "客户编码", "客户编号"},
	"name":                  {"name", "fullname", "full name", "联系人", "姓名"},
	"email":                 {"email", "e-mail", "mail", "邮箱", "邮件地址"},
	"allocation_department": {"allocation_department", "allocation department", "department", "部门", "分配部门", "分配部门名称"},
}

func normalizePoolImportHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
}

func parsePoolImportColumnRef(ref string) (int, bool) {
	if ref == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(ref); err == nil {
		if n <= 0 {
			return 0, false
		}
		return n - 1, true
	}
	column := 0
	for _, r := range ref {
		upper := unicode.ToUpper(r)
		if upper < 'A' || upper > 'Z' {
			return 0, false
		}
		column = column*26 + int(upper-'A'+1)
	}
	if column <= 0 {
		return 0, false
	}
	return column - 1, true
}

func resolvePoolContactImportColumns(header []string, fieldMap map[string]string) (map[string]int, error) {
	headerIndexes := make(map[string]int, len(header))
	for i, value := range header {
		normalized := normalizePoolImportHeader(value)
		if normalized != "" {
			headerIndexes[normalized] = i
		}
	}
	normalizedMap := make(map[string]string, len(fieldMap))
	for key, value := range fieldMap {
		normalizedMap[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}

	columns := make(map[string]int, len(poolContactImportAliases))
	for key, aliases := range poolContactImportAliases {
		ref := normalizedMap[key]
		if ref != "" {
			if index, ok := headerIndexes[normalizePoolImportHeader(ref)]; ok {
				columns[key] = index
				continue
			}
			if index, ok := parsePoolImportColumnRef(ref); ok {
				columns[key] = index
				continue
			}
			return nil, fmt.Errorf("字段映射 %s 无法匹配列 %q", key, ref)
		}
		for _, alias := range aliases {
			if index, ok := headerIndexes[normalizePoolImportHeader(alias)]; ok {
				columns[key] = index
				break
			}
		}
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("公海导入首行必须包含客户编号、姓名、邮箱、分配部门列")
		}
	}
	return columns, nil
}

// parsePoolContactImportFile parses the first sheet/CSV of the unified public
// pool import. The source workbook may contain the full business template;
// only the four mapped fields are returned to the domain layer.
func parsePoolContactImportFile(file *multipart.FileHeader, fieldMap map[string]string) ([]models.PoolContactImportRow, error) {
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
				return nil, fmt.Errorf("公海导入文件为空")
			}
			return nil, fmt.Errorf("读取公海导入表头失败：%w", err)
		}
		return parsePoolContactImportRows(header, func() ([]string, error) { return reader.Read() }, fieldMap)
	}
	if strings.HasSuffix(name, ".xlsx") {
		workbook, err := excelize.OpenReader(src, excelize.Options{UnzipSizeLimit: maxPoolAllocationUnzipSize})
		if err != nil {
			return nil, fmt.Errorf("无效 XLSX 文件：%w", err)
		}
		defer workbook.Close()
		sheets := workbook.GetSheetList()
		if len(sheets) == 0 {
			return nil, fmt.Errorf("XLSX 文件没有工作表")
		}
		allRows, err := workbook.GetRows(sheets[0])
		if err != nil {
			return nil, fmt.Errorf("读取 XLSX 工作表失败：%w", err)
		}
		if len(allRows) == 0 {
			return nil, fmt.Errorf("公海导入文件为空")
		}
		index := 1
		return parsePoolContactImportRows(allRows[0], func() ([]string, error) {
			if index >= len(allRows) {
				return nil, io.EOF
			}
			row := allRows[index]
			index++
			return row, nil
		}, fieldMap)
	}
	return nil, fmt.Errorf("公海导入仅支持 .csv 或 .xlsx 文件")
}

func parsePoolContactImportRows(header []string, next func() ([]string, error), fieldMap map[string]string) ([]models.PoolContactImportRow, error) {
	columns, err := resolvePoolContactImportColumns(header, fieldMap)
	if err != nil {
		return nil, err
	}
	valueAt := func(values []string, key string) string {
		index := columns[key]
		if index < 0 || index >= len(values) {
			return ""
		}
		return strings.TrimSpace(values[index])
	}
	rows := make([]models.PoolContactImportRow, 0)
	rowNumber := 2
	for {
		values, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 行失败：%w", rowNumber, err)
		}
		isEmpty := true
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				isEmpty = false
				break
			}
		}
		if !isEmpty {
			rows = append(rows, models.PoolContactImportRow{
				Row:                  rowNumber,
				CustomerCode:         valueAt(values, "customer_code"),
				Name:                 valueAt(values, "name"),
				Email:                valueAt(values, "email"),
				AllocationDepartment: valueAt(values, "allocation_department"),
			})
			if len(rows) > 100000 {
				return nil, fmt.Errorf("文件最多支持 100000 条联系人")
			}
		}
		rowNumber++
	}
	return rows, nil
}

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
	if err := requirePoolAdministrator(c); err != nil {
		return err
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

func requirePoolAdministrator(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may manage public pools")
	}
	return nil
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
	setAuditOrganizationID(c, int(req.OrganizationID))
	setAuditObjectID(c, strconv.Itoa(req.PoolID))
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
	setAuditOrganizationID(c, int(req.OrganizationID))
	setAuditObjectID(c, strconv.Itoa(req.PoolID))
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
	setAuditObjectID(c, strconv.FormatInt(out.ID, 10))
	setAuditMetadata(c, map[string]any{"pool_id": id})
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
	if err := requirePoolAdministrator(c); err != nil {
		return err
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
	setAuditMetadata(c, map[string]any{"pool_id": poolID})
	return c.JSON(http.StatusOK, okResp{true})
}

type poolSegmentRequest struct {
	PoolID         int    `json:"pool_id"`
	OrganizationID int64  `json:"organization_id"`
	Name           string `json:"name"`
	ReplyMailboxID *int   `json:"reply_mailbox_id"`
}

func (a *App) CreatePoolSegment(c echo.Context) error {
	if err := requirePoolAdministrator(c); err != nil {
		return err
	}
	var req poolSegmentRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	u := auth.GetUser(c)
	if req.ReplyMailboxID != nil {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox must be configured in the organization workspace")
	}
	setAuditOrganizationID(c, int(req.OrganizationID))
	out, err := a.core.CreatePoolSegment(req.PoolID, req.OrganizationID, req.Name, nil, u.ID, true)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(out.ID, 10))
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
	setAuditObjectID(c, strconv.Itoa(req.PoolID))
	setAuditMetadata(c, map[string]any{"source_list_id": req.ListID})
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
	if err := requirePoolAdministrator(c); err != nil {
		return err
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(req.ContactID, 10))
	setAuditMetadata(c, map[string]any{"segment_id": req.SegmentID})
	if err := a.core.AssignPoolContact(req.SegmentID, req.ContactID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// ImportPoolSegmentMembers accepts a CSV or XLSX file with customer_code and
// email columns. The server performs the match against the selected pool so
// clients never need to send thousands of contact IDs over individual calls.
func (a *App) ImportPoolSegmentMembers(c echo.Context) error {
	if err := requirePoolAdministrator(c); err != nil {
		return err
	}
	segmentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || segmentID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool segment id")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "file is required")
	}

	// A multipart upload larger than the in-memory threshold is spilled by
	// net/http to a temporary file it does not remove on its own.
	if mf := c.Request().MultipartForm; mf != nil {
		defer func() {
			if err := mf.RemoveAll(); err != nil {
				a.log.Printf("error removing multipart temporary files: %v", err)
			}
		}()
	}

	if file.Size > maxPoolAllocationUploadSize {
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
	setAuditObjectID(c, strconv.FormatInt(segmentID, 10))
	setAuditMetadata(c, map[string]any{
		"total":       result.Total,
		"valid":       result.Valid,
		"created":     result.Created,
		"reactivated": result.Reactivated,
		"unmatched":   result.Unmatched,
		"ambiguous":   result.Ambiguous,
		"invalid":     result.Invalid,
		"duplicates":  result.Duplicates,
	})
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
		// An XLSX is a ZIP archive: bound its extraction rather than accepting
		// excelize's 16 GB default.
		workbook, err := excelize.OpenReader(src, excelize.Options{UnzipSizeLimit: maxPoolAllocationUnzipSize})
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
	if err := requirePoolAdministrator(c); err != nil {
		return err
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(req.ContactID, 10))
	setAuditMetadata(c, map[string]any{"segment_id": req.SegmentID})
	u := auth.GetUser(c)
	if err := a.core.RemovePoolContact(req.SegmentID, req.ContactID, u.ID, req.Reason); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) RestorePoolContact(c echo.Context) error {
	if err := requirePoolAdministrator(c); err != nil {
		return err
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(req.ContactID, 10))
	setAuditMetadata(c, map[string]any{"segment_id": req.SegmentID})
	if err := a.core.RestorePoolContact(req.SegmentID, req.ContactID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}
