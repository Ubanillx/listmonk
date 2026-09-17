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
// customer_code/email pairs for one allocation; the archive limits bound what an
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
	access, err := a.requirePoolPermission(c, auth.PermPoolsGet)
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	user := auth.GetUser(c)
	if !user.IsPlatformAdmin() {
		if err := a.core.AuthorizePoolListAccess(id, int64(access.OrganizationID)); err != nil {
			return err
		}
	}
	// `customer_code` is the previous, still documented single-field filter;
	// `search` matches code, name and e-mail.
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("customer_code")
	}
	pg := a.pg.NewFromURL(c.Request().URL.Query())
	rows, total, err := a.core.QueryPoolContacts(id, access.OrganizationID, user.IsPlatformAdmin(),
		search, c.QueryParam("order_by"), c.QueryParam("order"), pg.Offset, pg.Limit)
	if err != nil {
		return err
	}
	out := models.PageResults{
		Results: rows,
		Search:  search,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// ExportPoolContacts streams the filtered pool contacts as CSV. Non-platform
// administrators only reach pools granted to their workspace organization and
// always receive masked e-mail addresses.
func (a *App) ExportPoolContacts(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsExport)
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	user := auth.GetUser(c)
	if !user.IsPlatformAdmin() {
		if err := a.core.AuthorizePoolListAccess(id, int64(access.OrganizationID)); err != nil {
			return err
		}
	}
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("customer_code")
	}
	orderBy, order := c.QueryParam("order_by"), c.QueryParam("order")

	hdr := c.Response().Header()
	hdr.Set(echo.HeaderContentType, echo.MIMEOctetStream)
	hdr.Set("Content-type", "text/csv")
	hdr.Set(echo.HeaderContentDisposition, "attachment; filename=pool-contacts.csv")
	hdr.Set("Content-Transfer-Encoding", "binary")
	hdr.Set("Cache-Control", "no-cache")

	wr := csv.NewWriter(c.Response())
	if err := wr.Write([]string{"customer_code", "name", "email", "allocation_department", "status", "created_at", "updated_at"}); err != nil {
		return err
	}

	batch := a.cfg.DBBatchSize
	if batch <= 0 {
		batch = 1000
	}
	for offset := 0; ; offset += batch {
		rows, _, err := a.core.QueryPoolContacts(id, access.OrganizationID, user.IsPlatformAdmin(), search, orderBy, order, offset, batch)
		if err != nil {
			return err
		}
		count := 0
		switch page := rows.(type) {
		case []models.PoolContact:
			count = len(page)
			for _, r := range page {
				if err := wr.Write([]string{r.CustomerCode, r.Name, r.Email, r.AllocationDepartment, r.Status, r.CreatedAt.String(), r.UpdatedAt.String()}); err != nil {
					a.log.Printf("error streaming pool contact export: %v", err)
					wr.Flush()
					return nil
				}
			}
		case []models.SafePoolContact:
			count = len(page)
			for _, r := range page {
				if err := wr.Write([]string{r.CustomerCode, r.Name, r.Email, r.AllocationDepartment, r.Status, r.CreatedAt.String(), r.UpdatedAt.String()}); err != nil {
					a.log.Printf("error streaming pool contact export: %v", err)
					wr.Flush()
					return nil
				}
			}
		}
		wr.Flush()
		if count < batch {
			break
		}
	}
	return nil
}

func (a *App) GetOrgPoolAllocations(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	// Accept both a first-level pool list and one of its allocation lists; the
	// listing is always keyed by the first-level pool.
	poolID, _, err := a.core.ResolvePoolListPoolID(id)
	if err != nil {
		return err
	}
	rows, err := a.core.QueryOrgPoolAllocations(poolID, int64(access.OrganizationID), auth.GetUser(c).IsPlatformAdmin())
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

// requirePoolPermission resolves the active workspace and requires the given
// pools permission. Platform administrators bypass the role grant; every other
// caller is additionally restricted to the workspace organization by the
// individual handlers.
func (a *App) requirePoolPermission(c echo.Context, perm string) (models.WorkspaceAccess, error) {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return access, err
	}
	u := auth.GetUser(c)
	if u.IsPlatformAdmin() {
		return access, nil
	}
	if !u.HasPerm(perm) {
		return access, echo.NewHTTPError(http.StatusForbidden, "permission denied: "+perm)
	}
	return access, nil
}

// requirePoolAllocationScope restricts a non-platform-admin caller to the pool
// allocations owned by the active workspace organization.
func (a *App) requirePoolAllocationScope(c echo.Context, access models.WorkspaceAccess, allocationID int64) error {
	if auth.GetUser(c).IsPlatformAdmin() {
		return nil
	}
	if !access.IsOrganization() || access.OrganizationID <= 0 {
		return echo.NewHTTPError(http.StatusForbidden, "public pool is outside the active workspace")
	}
	orgID, err := a.core.PoolAllocationOrganizationID(allocationID)
	if err != nil {
		return err
	}
	if orgID != int64(access.OrganizationID) {
		return echo.NewHTTPError(http.StatusForbidden, "only the owning organization may manage this pool allocation")
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
	access, err := a.requirePoolPermission(c, auth.PermPoolsManage)
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	var p models.PoolContact
	if err := c.Bind(&p); err != nil {
		return err
	}
	if !auth.GetUser(c).IsPlatformAdmin() {
		if err := a.core.AuthorizePoolListAccess(id, int64(access.OrganizationID)); err != nil {
			return err
		}
		// A non-platform-admin may only add contacts for its own
		// organization, so the department is pinned to it: the contact is
		// never placed in another organization's allocation.
		org, err := a.core.GetOrganization(access.OrganizationID)
		if err != nil {
			return err
		}
		p.AllocationDepartment = org.Name
	}
	out, err := a.core.CreatePoolContact(id, p)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(out.ID, 10))
	setAuditMetadata(c, map[string]any{"pool_id": id})
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ClearPoolContactEmail(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsManage)
	if err != nil {
		return err
	}
	listID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	user := auth.GetUser(c)
	if !user.IsPlatformAdmin() {
		if err := a.core.AuthorizePoolListAccess(listID, int64(access.OrganizationID)); err != nil {
			return err
		}
	}
	// The route accepts a first-level pool list or one of its allocation
	// lists; the core update is keyed by the first-level pool.
	poolID, _, err := a.core.ResolvePoolListPoolID(listID)
	if err != nil {
		return err
	}
	contactID, err := strconv.ParseInt(c.Param("contact_id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid contact id")
	}
	if err := a.core.ClearPoolContactEmail(poolID, contactID, int64(access.OrganizationID), user.IsPlatformAdmin()); err != nil {
		return err
	}
	setAuditMetadata(c, map[string]any{"pool_id": poolID})
	return c.JSON(http.StatusOK, okResp{true})
}

type orgPoolAllocationRequest struct {
	PoolID         int    `json:"pool_id"`
	OrganizationID int64  `json:"organization_id"`
	Name           string `json:"name"`
	ReplyMailboxID *int   `json:"reply_mailbox_id"`
}

// CreateOrgPoolAllocation splits a first-level public pool into a new organization
// pool allocation and binds both in a single transaction. Authorization is
// two-tiered: a platform administrator may split a pool for any active
// organization without being a member of it and without switching the current
// workspace, while an organization manager may only split a pool for the
// organization of the workspace it manages. Ordinary organization members and
// non-members are rejected. The core layer re-verifies, while holding the
// organization row lock, that the target organization is active and that a
// non-platform-admin caller is an active member of it.
func (a *App) CreateOrgPoolAllocation(c echo.Context) error {
	var req orgPoolAllocationRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	u := auth.GetUser(c)
	platformAdmin := u.IsPlatformAdmin()
	if req.ReplyMailboxID != nil {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox must be configured in the organization workspace")
	}
	if !platformAdmin {
		access, err := a.workspaceAccess(c)
		if err != nil {
			return err
		}
		if err := requireWritableWorkspace(access); err != nil {
			return err
		}
		if !access.IsOrganization() || !access.IsOrganizationManager() {
			return echo.NewHTTPError(http.StatusForbidden, "only a highest administrator or the target organization's manager may manage public pools")
		}
		if req.OrganizationID != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "organization managers may only manage their own organization")
		}
	}
	setAuditOrganizationID(c, int(req.OrganizationID))
	out, err := a.core.CreateOrgPoolAllocation(req.PoolID, req.OrganizationID, req.Name, nil, u.ID, platformAdmin)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(out.ID, 10))
	return c.JSON(http.StatusOK, okResp{out})
}

type poolMembershipRequest struct {
	AllocationID int64  `json:"allocation_id"`
	ContactID    int64  `json:"contact_id"`
	Reason       string `json:"reason"`
}

type campaignPoolRequest struct {
	PoolID              int    `json:"pool_id"`
	OrgPoolAllocationID *int64 `json:"org_pool_allocation_id"`
	OrganizationID      int64  `json:"organization_id"`
}

// ImportListIntoPool remains a separate, explicit maintenance operation for
// ordinary lists. It does not create or bind pool allocation lists.
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
	if err := a.core.AttachPoolToCampaign(campaignID, req.PoolID, req.OrgPoolAllocationID, orgID, false); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) AssignPoolContact(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsManage)
	if err != nil {
		return err
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := a.requirePoolAllocationScope(c, access, req.AllocationID); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(req.ContactID, 10))
	setAuditMetadata(c, map[string]any{"allocation_id": req.AllocationID})
	if err := a.core.AssignPoolContact(req.AllocationID, req.ContactID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// ImportOrgPoolAllocationMembers accepts a CSV or XLSX file with customer_code and
// email columns. The server performs the match against the selected pool so
// clients never need to send thousands of contact IDs over individual calls.
func (a *App) ImportOrgPoolAllocationMembers(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsManage)
	if err != nil {
		return err
	}
	allocationID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || allocationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool allocation id")
	}
	if err := a.requirePoolAllocationScope(c, access, allocationID); err != nil {
		return err
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
	result, err := a.core.ImportOrgPoolAllocationMembers(allocationID, rows)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(allocationID, 10))
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
	access, err := a.requirePoolPermission(c, auth.PermPoolsManage)
	if err != nil {
		return err
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := a.requirePoolAllocationScope(c, access, req.AllocationID); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(req.ContactID, 10))
	setAuditMetadata(c, map[string]any{"allocation_id": req.AllocationID})
	u := auth.GetUser(c)
	if err := a.core.RemovePoolContact(req.AllocationID, req.ContactID, u.ID, req.Reason); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) RestorePoolContact(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsManage)
	if err != nil {
		return err
	}
	var req poolMembershipRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := a.requirePoolAllocationScope(c, access, req.AllocationID); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(req.ContactID, 10))
	setAuditMetadata(c, map[string]any{"allocation_id": req.AllocationID})
	if err := a.core.RestorePoolContact(req.AllocationID, req.ContactID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}
