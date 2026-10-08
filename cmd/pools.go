package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/mail"
	"regexp"
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
	"customer_code": {"customer_code", "customer code", "customercode", "客户编码", "客户编号"},
	"name":          {"name", "fullname", "full name", "联系人", "姓名"},
	"email":         {"email", "e-mail", "mail", "邮箱", "邮件地址"},
	"reply_to":      {"reply_to", "reply to", "reply-to", "reply_email", "回信邮箱", "回件邮箱", "回复邮箱"},
	// Prefer the explicit allocation column when a business template also has
	// a general "department" column. The latter can describe the contact's
	// source data and is not the organization routing target.
	"allocation_department": {"allocation_department", "allocation department", "分配部门", "分配部门名称", "department", "部门"},
}

// Match whole address-like tokens, including malformed ones, so the domain
// validator can reject them without silently importing a valid-looking suffix.
var poolImportEmailPattern = regexp.MustCompile(`[^\s<>()[\]";|]+@[^\s<>()[\]";|]+`)
var poolImportEmailPunctuation = strings.NewReplacer("＠", "@", "．", ".", "；", ";", "，", ",", "｜", "|")

func splitPoolImportEmails(value string) []string {
	value = strings.TrimSpace(poolImportEmailPunctuation.Replace(value))
	// Preserve already-valid bare addresses with characters that are also
	// commonly used as list separators.
	if parsed, err := mail.ParseAddress(value); err == nil && parsed.Address == value {
		return []string{value}
	}

	var emails []string
	appendCell := func(cell string) {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return
		}
		if parsed, err := mail.ParseAddress(cell); err == nil {
			emails = append(emails, parsed.Address)
			return
		}
		candidates := poolImportEmailPattern.FindAllString(cell, -1)
		if len(candidates) == 0 {
			emails = append(emails, cell)
			return
		}
		normalized := make([]string, 0, len(candidates))
		undotted := false
		for _, candidate := range candidates {
			candidate = strings.TrimRight(candidate, ".。")
			if candidate == "" {
				continue
			}
			// An undotted domain cut off at whitespace can be part of a broken
			// address ("user@public 1 example.com"). Keep it intact for validation.
			at := strings.LastIndexByte(candidate, '@')
			if at >= 0 && !strings.Contains(candidate[at+1:], ".") {
				undotted = true
			}
			normalized = append(normalized, candidate)
		}
		if undotted && len(normalized) == 1 {
			emails = append(emails, cell)
			return
		}
		emails = append(emails, normalized...)
	}

	// Commas and slashes after an @ are separators. A comma inside a malformed local part,
	// such as "first,last@example.com", must remain an invalid address rather
	// than becoming "last@example.com". Quoted local parts may contain separators.
	start, hasAt, boundary, quoted, escaped := 0, false, false, false, false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if quoted && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
		}
		if quoted {
			continue
		}
		if r == ';' || r == '|' || r == '\n' || r == '\r' || ((r == ',' || r == '/') && (hasAt || boundary)) {
			appendCell(value[start:i])
			start, hasAt, boundary = i+1, false, false
			continue
		}
		// Track the current token rather than the entire segment: an earlier
		// address must not turn the comma in "one@example.com first,last@example.com"
		// into a separator and silently import "last@example.com".
		if unicode.IsSpace(r) || r == '>' || r == ')' || r == ']' {
			hasAt, boundary = false, true
		} else {
			boundary = false
			if r == '@' {
				hasAt = true
			}
		}
	}
	appendCell(value[start:])
	if len(emails) == 0 {
		// A blank cell remains one row so core reports email_required.
		return []string{value}
	}
	return emails
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
			if key == "reply_to" {
				columns[key] = -1
				continue
			}
			return nil, fmt.Errorf("公海导入首行必须包含客户编号、姓名、邮箱、分配部门列")
		}
	}
	return columns, nil
}

// parsePoolContactImportFile parses the first sheet/CSV of the unified public
// pool import. The source workbook may contain the full business template;
// only the mapped business fields are returned to the domain layer.
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
			base := models.PoolContactImportRow{
				Row:                  rowNumber,
				CustomerCode:         valueAt(values, "customer_code"),
				Name:                 valueAt(values, "name"),
				ReplyTo:              valueAt(values, "reply_to"),
				AllocationDepartment: valueAt(values, "allocation_department"),
			}
			for _, email := range splitPoolImportEmails(valueAt(values, "email")) {
				base.Email = email
				rows = append(rows, base)
				if len(rows) > 100000 {
					return nil, fmt.Errorf("文件最多支持 100000 条联系人")
				}
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
	if !canManagePoolMaster(user) {
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
	poolStatus := c.QueryParam("status")
	pg := a.pg.NewFromURL(c.Request().URL.Query())
	rows, total, err := a.core.QueryPoolContacts(id, access.OrganizationID, user.IsPlatformAdmin(),
		poolStatus, search, c.QueryParam("order_by"), c.QueryParam("order"), pg.Offset, pg.Limit, canManagePoolMaster(user))
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

// GetAllPoolContacts is the public-pool landing view. The core query enforces
// organization scope and paginates across pool memberships.
func (a *App) GetAllPoolContacts(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsGet)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("customer_code")
	}
	poolID, department, err := allPoolContactFilterParams(c)
	if err != nil {
		return err
	}
	pg := a.pg.NewFromURL(c.Request().URL.Query())
	rows, total, err := a.core.QueryAllPoolContacts(access.OrganizationID, user.IsPlatformAdmin(),
		c.QueryParam("status"), search, poolID, department, c.QueryParam("order_by"), c.QueryParam("order"), pg.Offset, pg.Limit, canManagePoolMaster(user))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{models.PageResults{
		Results: rows,
		Search:  search,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}})
}

// GetAllPoolContactFilters returns the visible pool/department combinations
// used by the aggregate page's dropdown filters.
func (a *App) GetAllPoolContactFilters(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsGet)
	if err != nil {
		return err
	}
	options, err := a.core.QueryAllPoolContactFilterOptions(access.OrganizationID, canManagePoolMaster(auth.GetUser(c)))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{options})
}

func allPoolContactFilterParams(c echo.Context) (int64, *string, error) {
	params := c.Request().URL.Query()
	var poolID int64
	if values, ok := params["pool_id"]; ok {
		if len(values) == 0 {
			return 0, nil, echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
		}
		id, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 64)
		if err != nil || id <= 0 {
			return 0, nil, echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
		}
		poolID = id
	}
	var department *string
	if values, ok := params["allocation_department"]; ok {
		value := ""
		if len(values) > 0 {
			value = strings.TrimSpace(values[0])
		}
		department = &value
	}
	return poolID, department, nil
}

// ExportAllPoolContacts streams the same organization-scoped aggregate rows.
func (a *App) ExportAllPoolContacts(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsExport)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	poolID, department, err := allPoolContactFilterParams(c)
	if err != nil {
		return err
	}
	selected, err := parsePoolExportSelection(c.Request().URL.Query(), true)
	if err != nil {
		return err
	}
	hdr := c.Response().Header()
	hdr.Set(echo.HeaderContentType, "text/csv")
	hdr.Set(echo.HeaderContentDisposition, "attachment; filename=pool-contacts.csv")
	hdr.Set("Cache-Control", "no-cache")
	wr := csv.NewWriter(c.Response())
	if err := wr.Write([]string{"pool_id", "pool_name", "customer_code", "name", "email", "reply_to", "allocation_department", "status", "created_at", "updated_at"}); err != nil {
		return err
	}
	batch := a.cfg.DBBatchSize
	if batch <= 0 {
		batch = 1000
	}
	for offset := 0; ; offset += batch {
		rows, _, err := a.core.QueryAllPoolContacts(access.OrganizationID, user.IsPlatformAdmin(),
			c.QueryParam("status"), c.QueryParam("search"), poolID, department,
			c.QueryParam("order_by"), c.QueryParam("order"), offset, batch, canManagePoolMaster(user))
		if err != nil {
			return err
		}
		count := 0
		write := func(poolID int64, poolName, code, name, email, replyTo, department, status, createdAt, updatedAt string) error {
			return wr.Write([]string{strconv.FormatInt(poolID, 10), poolName, code, name, email, replyTo, department, status, createdAt, updatedAt})
		}
		switch page := rows.(type) {
		case []models.PoolContact:
			count = len(page)
			for _, row := range page {
				if !poolExportIncludes(selected, row.PoolID, row.ID) {
					continue
				}
				if err := write(row.PoolID, row.PoolName, row.CustomerCode, row.Name, row.Email, row.ReplyTo, row.AllocationDepartment, row.Status, row.CreatedAt.String(), row.UpdatedAt.String()); err != nil {
					return err
				}
			}
		case []models.SafePoolContact:
			count = len(page)
			for _, row := range page {
				if !poolExportIncludes(selected, row.PoolID, row.ID) {
					continue
				}
				if err := write(row.PoolID, row.PoolName, row.CustomerCode, row.Name, row.Email, row.ReplyTo, row.AllocationDepartment, row.Status, row.CreatedAt.String(), row.UpdatedAt.String()); err != nil {
					return err
				}
			}
		}
		wr.Flush()
		if err := wr.Error(); err != nil {
			return err
		}
		if count < batch {
			break
		}
	}
	return nil
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
	if !canManagePoolMaster(user) {
		if err := a.core.AuthorizePoolListAccess(id, int64(access.OrganizationID)); err != nil {
			return err
		}
	}
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("customer_code")
	}
	poolStatus := c.QueryParam("status")
	orderBy, order := c.QueryParam("order_by"), c.QueryParam("order")
	selected, err := parsePoolExportSelection(c.Request().URL.Query(), false)
	if err != nil {
		return err
	}

	hdr := c.Response().Header()
	hdr.Set(echo.HeaderContentType, echo.MIMEOctetStream)
	hdr.Set("Content-type", "text/csv")
	hdr.Set(echo.HeaderContentDisposition, "attachment; filename=pool-contacts.csv")
	hdr.Set("Content-Transfer-Encoding", "binary")
	hdr.Set("Cache-Control", "no-cache")

	wr := csv.NewWriter(c.Response())
	if err := wr.Write([]string{"customer_code", "name", "email", "reply_to", "allocation_department", "status", "created_at", "updated_at"}); err != nil {
		return err
	}

	batch := a.cfg.DBBatchSize
	if batch <= 0 {
		batch = 1000
	}
	for offset := 0; ; offset += batch {
		rows, _, err := a.core.QueryPoolContacts(id, access.OrganizationID, user.IsPlatformAdmin(), poolStatus, search, orderBy, order, offset, batch, canManagePoolMaster(user))
		if err != nil {
			return err
		}
		count := 0
		switch page := rows.(type) {
		case []models.PoolContact:
			count = len(page)
			for _, r := range page {
				if !poolExportIncludes(selected, 0, r.ID) {
					continue
				}
				if err := wr.Write([]string{r.CustomerCode, r.Name, r.Email, r.ReplyTo, r.AllocationDepartment, r.Status, r.CreatedAt.String(), r.UpdatedAt.String()}); err != nil {
					a.log.Printf("error streaming pool contact export: %v", err)
					wr.Flush()
					return nil
				}
			}
		case []models.SafePoolContact:
			count = len(page)
			for _, r := range page {
				if !poolExportIncludes(selected, 0, r.ID) {
					continue
				}
				if err := wr.Write([]string{r.CustomerCode, r.Name, r.Email, r.ReplyTo, r.AllocationDepartment, r.Status, r.CreatedAt.String(), r.UpdatedAt.String()}); err != nil {
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
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermPoolsGet, auth.PermPoolsManage, auth.PermPoolsDeliveryManage); err != nil {
		return err
	}
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
	rows, err := a.core.QueryOrgPoolAllocations(poolID, int64(access.OrganizationID), canManagePoolDelivery(auth.GetUser(c)))
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
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermPoolsManage, auth.PermPoolsDeliveryManage); err != nil {
		return err
	}
	poolID := getID(c)
	organizationID, err := strconv.Atoi(c.QueryParam("organization_id"))
	if err != nil || organizationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "organization_id is required")
	}
	if !canManagePoolDelivery(auth.GetUser(c)) {
		access, err := a.workspaceAccess(c)
		if err != nil {
			return err
		}
		if organizationID != access.OrganizationID || !access.IsOrganization() {
			return echo.NewHTTPError(http.StatusForbidden, "pool allocation is outside the active workspace")
		}
	}
	out, err := a.core.QueryPoolManagementTarget(poolID, organizationID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func requirePoolAdministrator(c echo.Context) error {
	return requireLegacyPermission(auth.GetUser(c), auth.PermPoolsMasterManage)
}

// Only a delivery administrator can inspect the platform organization picker.
// It returns names and IDs and grants no membership or organization management.
func (a *App) GetPoolOrganizations(c echo.Context) error {
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermPoolsDeliveryManage); err != nil {
		return err
	}
	var rows []struct {
		ID   int    `db:"id" json:"id"`
		Name string `db:"name" json:"name"`
	}
	if err := a.db.Select(&rows, `SELECT id,name FROM organizations WHERE status='active' ORDER BY name,id`); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

// requirePoolPermission resolves the active workspace and requires the given
// pools permission. Platform administrators bypass the role grant; every other
// caller is restricted by the individual handlers. Master-data maintenance
// widens only first-level pool scope; allocation maintenance stays organization-bound.
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
	if perm != auth.PermPoolsGet && perm != auth.PermPoolsExport {
		if err := requireWritableWorkspace(access); err != nil {
			return access, err
		}
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
	if err := requirePoolAdministrator(c); err != nil {
		return err
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
	if !auth.GetUser(c).IsPlatformAdmin() {
		for i := range rows {
			for _, snapshot := range []models.JSON{rows[i].ExistingSnapshot, rows[i].IncomingSnapshot} {
				delete(snapshot, "uuid")
				delete(snapshot, "attribs")
				for _, field := range []string{"email", "reply_to"} {
					if email, ok := snapshot[field].(string); ok {
						snapshot[field] = models.PoolContact{Email: email}.Safe().Email
					}
				}
			}
		}
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

type poolOrganizationRequest struct {
	PoolID         int   `json:"pool_id"`
	OrganizationID int64 `json:"organization_id"`
}

func (a *App) GrantPoolOrganization(c echo.Context) error {
	u := auth.GetUser(c)
	if err := requireLegacyPermission(u, auth.PermPoolsDeliveryManage); err != nil {
		return err
	}
	var req poolOrganizationRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.PoolID <= 0 || req.OrganizationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool_id and organization_id are required")
	}
	if _, err := a.core.QueryPoolManagementTarget(req.PoolID, int(req.OrganizationID)); err != nil {
		return err
	}
	setAuditOrganizationID(c, int(req.OrganizationID))
	setAuditObjectID(c, strconv.Itoa(req.PoolID))
	if err := a.core.GrantPoolOrganization(req.PoolID, req.OrganizationID, u.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) RevokePoolOrganization(c echo.Context) error {
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermPoolsDeliveryManage); err != nil {
		return err
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
	access, err := a.requirePoolPermission(c, auth.PermPoolsMasterManage)
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
	if _, err := a.core.RequireManageResource(access, resourceLists, id); err != nil {
		return err
	}
	out, err := a.core.CreatePoolContact(id, p)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(out.ID, 10))
	setAuditMetadata(c, map[string]any{"pool_id": id})
	if !auth.GetUser(c).IsPlatformAdmin() {
		return c.JSON(http.StatusOK, okResp{out.Safe()})
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ClearPoolContactEmail(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsMasterManage)
	if err != nil {
		return err
	}
	listID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	user := auth.GetUser(c)
	// The route accepts a first-level pool list or one of its allocation
	// lists; the core update is keyed by the first-level pool.
	poolID, _, err := a.core.ResolvePoolListPoolID(listID)
	if err != nil {
		return err
	}
	if _, err := a.core.RequireManageResource(access, resourceLists, poolID); err != nil {
		return err
	}
	contactID, err := strconv.ParseInt(c.Param("contact_id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid contact id")
	}
	if err := a.core.ClearPoolContactEmail(poolID, contactID, int64(access.OrganizationID), canManagePoolMaster(user)); err != nil {
		return err
	}
	setAuditMetadata(c, map[string]any{"pool_id": poolID})
	return c.JSON(http.StatusOK, okResp{true})
}

// DeletePoolContact permanently removes a contact from a first-level public
// pool. This destructive action requires master-data maintenance and does not
// accept organization allocation list IDs.
func (a *App) DeletePoolContact(c echo.Context) error {
	access, err := a.requirePoolPermission(c, auth.PermPoolsMasterManage)
	if err != nil {
		return err
	}
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pool id")
	}
	if _, err := a.core.RequireManageResource(access, resourceLists, poolID); err != nil {
		return err
	}
	contactID, err := strconv.ParseInt(c.Param("contact_id"), 10, 64)
	if err != nil || contactID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid contact id")
	}
	if err := a.core.DeletePoolContact(poolID, contactID); err != nil {
		return err
	}
	setAuditObjectID(c, strconv.FormatInt(contactID, 10))
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
// two-tiered: allocation management requires pools:manage; a caller with
// delivery administration can choose any active organization. Other callers
// must use their own organization and an existing delivery grant. Core locks
// the target organization and rechecks its state and membership.
func (a *App) CreateOrgPoolAllocation(c echo.Context) error {
	var req orgPoolAllocationRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	u := auth.GetUser(c)
	platformAdmin := u.IsPlatformAdmin()
	if err := requireLegacyPermission(u, auth.PermPoolsManage); err != nil {
		return err
	}
	platformAdmin = platformAdmin || canManagePoolDelivery(u)
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
		if !access.IsOrganization() {
			return echo.NewHTTPError(http.StatusForbidden, "select an organization to manage its pool allocations")
		}
		if req.OrganizationID != int64(access.OrganizationID) {
			return echo.NewHTTPError(http.StatusForbidden, "pool allocation management is limited to the active organization")
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
	if err := requirePoolAdministrator(c); err != nil {
		return err
	}
	var req poolImportRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.ListID <= 0 || req.PoolID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "list_id and pool_id are required")
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if _, err := a.core.RequireManageResource(access, resourceLists, req.ListID); err != nil {
		return err
	}
	if _, err := a.core.RequireManageResource(access, resourceLists, req.PoolID); err != nil {
		return err
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
