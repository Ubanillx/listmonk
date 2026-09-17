package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// maxImportUploadSize caps the uploaded CSV/XLSX/ZIP itself. It mirrors the
// transport limit for this route; the extraction limits in the subimporter
// bound what an archive may expand to.
const maxImportUploadSize = 64 << 20

type importTargetInfo struct {
	PoolIDs              []int
	OrgPoolAllocationIDs []int
	RegularIDs           []int
}

func normalizeImportFieldMap(fieldMap map[string]string) map[string]string {
	if len(fieldMap) == 0 {
		return nil
	}

	out := make(map[string]string, len(fieldMap))
	for key, value := range fieldMap {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

// classifyImportTargets reads only list types. Public-pool lists may be
// delivery-visible outside the active workspace, so this classification is
// deliberately separate from the later write-scope check.
func (a *App) classifyImportTargets(ids []int) (importTargetInfo, error) {
	info := importTargetInfo{}
	for _, id := range ids {
		var typ string
		if err := a.db.Get(&typ, `SELECT type::text FROM customer_lists WHERE id=$1`, id); err != nil {
			if err == sql.ErrNoRows {
				return info, echo.NewHTTPError(http.StatusNotFound, "customer list not found")
			}
			return info, err
		}
		switch typ {
		case models.CustomerListTypePool:
			info.PoolIDs = append(info.PoolIDs, id)
		case models.CustomerListTypeOrgPoolAllocation:
			info.OrgPoolAllocationIDs = append(info.OrgPoolAllocationIDs, id)
		default:
			info.RegularIDs = append(info.RegularIDs, id)
		}
	}
	return info, nil
}

// ImportCustomers handles the uploading and bulk importing of
// a ZIP file of one or more CSV files.
func (a *App) ImportCustomers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersImport); err != nil {
		return err
	}
	// Is an import already running?
	if status := a.importer.GetStats().Status; status == subimporter.StatusImporting || status == subimporter.StatusStopping {
		if err := a.requireImportAccess(access); err != nil {
			return err
		}
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("import.alreadyRunning"))
	}

	// Unmarshal the JSON params.
	var opt subimporter.SessionOpt
	if err := json.Unmarshal([]byte(c.FormValue("params")), &opt); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("import.invalidParams", "error", err.Error()))
	}
	opt.FieldMap = normalizeImportFieldMap(opt.FieldMap)
	targets, err := a.classifyImportTargets(opt.CustomerListIDs)
	if err != nil {
		return err
	}
	isPoolImport := len(targets.PoolIDs) > 0 || len(targets.OrgPoolAllocationIDs) > 0
	// Reject mappings for unsupported import fields.
	if len(opt.FieldMap) > 0 {
		allowed := map[string]bool{"email": true, "name": true, "customer_code": true}
		if isPoolImport {
			allowed["allocation_department"] = true
		}
		for key := range opt.FieldMap {
			if !allowed[strings.ToLower(strings.TrimSpace(key))] {
				return echo.NewHTTPError(http.StatusBadRequest, "unknown custom field: "+key)
			}
		}
	}
	if isPoolImport {
		if len(targets.PoolIDs) != 1 || len(targets.OrgPoolAllocationIDs) > 0 || len(targets.RegularIDs) > 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "public-pool import requires exactly one first-level public pool")
		}
		if !auth.GetUser(c).IsPlatformAdmin() {
			return echo.NewHTTPError(http.StatusForbidden, "only highest administrators may import public-pool contacts")
		}
		if opt.Mode != subimporter.ModeSubscribe {
			return echo.NewHTTPError(http.StatusBadRequest, "public-pool import only supports subscribe mode")
		}
		if opt.Overwrite || opt.OverwriteUserInfo || opt.OverwriteSubStatus {
			return echo.NewHTTPError(http.StatusBadRequest, "overwrite options are not supported for public-pool import")
		}
		if err := a.requireWorkspaceCustomerListIDsForRequestAllowPool(c, access, opt.CustomerListIDs, true); err != nil {
			return err
		}
		return a.importPoolCustomers(c, targets.PoolIDs[0], opt)
	}
	if len(opt.FieldMap) > 0 {
		if _, ok := opt.FieldMap["allocation_department"]; ok && strings.TrimSpace(opt.FieldMap["allocation_department"]) != "" {
			return echo.NewHTTPError(http.StatusBadRequest, "allocation_department is only supported for public-pool import")
		}
	}

	// Validate mode.
	if opt.Mode != subimporter.ModeSubscribe && opt.Mode != subimporter.ModeBlocklist {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("import.invalidMode"))
	}

	// If no status is specified, pick a default one.
	if opt.SubStatus == "" {
		switch opt.Mode {
		case subimporter.ModeSubscribe:
			opt.SubStatus = models.SubscriptionStatusUnconfirmed
		case subimporter.ModeBlocklist:
			opt.SubStatus = models.SubscriptionStatusUnsubscribed
		}
	}

	if opt.SubStatus != models.SubscriptionStatusUnconfirmed &&
		opt.SubStatus != models.SubscriptionStatusConfirmed &&
		opt.SubStatus != models.SubscriptionStatusUnsubscribed {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("import.invalidSubStatus"))
	}
	if err := a.requireOwnedWorkspaceCustomerListIDsForRequest(c, access, opt.CustomerListIDs, true); err != nil {
		return err
	}
	opt.OwnerUserID = access.UserID
	opt.OriginalOwnerUserID = access.UserID
	if access.IsOrganization() {
		organizationID := access.OrganizationID
		opt.OrganizationID = &organizationID
	}

	// Open the HTTP file.
	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("import.invalidFile", "error", err.Error()))
	}

	// Parsing a multipart upload larger than the in-memory threshold spills the
	// part to a temporary file that net/http does not remove on its own. The
	// contents are copied to our own temp file below, so the multipart copy can
	// be reclaimed as soon as this handler returns.
	if mf := c.Request().MultipartForm; mf != nil {
		defer func() {
			if err := mf.RemoveAll(); err != nil {
				a.log.Printf("error removing multipart temporary files: %v", err)
			}
		}()
	}

	filename := strings.ToLower(file.Filename)
	isCSV := strings.HasSuffix(filename, ".csv")
	isXLSX := strings.HasSuffix(filename, ".xlsx")
	isZIP := strings.HasSuffix(filename, ".zip")
	if !isCSV && !isXLSX && !isZIP {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.T("import.invalidFile"))
	}

	// The transport limit already bounds this request, but checking the declared
	// size here keeps the rejection independent of the route configuration and
	// refuses the upload before it is copied to disk.
	if file.Size > maxImportUploadSize {
		return tooLargeErr(a, fmt.Sprintf("file (max %d MB)", maxImportUploadSize>>20))
	}

	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// Everything created for this import has to outlive the request and be
	// removed once the import finishes, fails or is stopped. tempPaths is
	// appended to before the cleanup watcher is armed, and read by the watcher
	// only after the session's completion signal fires. This cleanup is
	// registered before the temp file is opened so that it runs after the file
	// handle is closed (deferred calls run last-registered-first; Windows cannot
	// remove an open file).
	var (
		tempPaths []string
		armed     bool
	)
	defer func() {
		// An early return leaves the paths unclaimed by the watcher: remove them
		// here instead of leaking them.
		if !armed {
			removeTempPaths(a.log, tempPaths...)
		}
	}()

	// Copy it to a temp location. The copy is bounded as well, so a part whose
	// declared size understates its content cannot fill the disk.
	out, err := os.CreateTemp("", "listmonk")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("import.errorCopyingFile", "error", err.Error()))
	}
	defer out.Close()
	tempPaths = append(tempPaths, out.Name())

	if _, err := io.Copy(out, io.LimitReader(src, maxImportUploadSize+1)); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("import.errorCopyingFile", "error", err.Error()))
	}

	// Start the importer session. NewSession is the single admission point, so
	// the pre-check above can lose the race to a concurrent request that was
	// admitted first. That loser has to report the same condition the same way
	// the pre-check does (403 for another workspace's import, 400 for one that
	// is already running) instead of a generic "error starting import".
	opt.Filename = file.Filename
	sess, err := a.importer.NewSession(opt)
	if err != nil {
		if errors.Is(err, subimporter.ErrIsImporting) {
			if err := a.requireImportAccess(access); err != nil {
				return err
			}
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("import.alreadyRunning"))
		}
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("import.errorStarting", "error", err.Error()))
	}

	// Capture this session's completion signal before the loaders run, so the
	// cleanup below can never observe a later session's signal.
	done := a.importer.Done()
	go sess.Start()

	if isCSV {
		go sess.LoadCSV(out.Name())
	} else if isXLSX {
		go sess.LoadXLSX(out.Name())
	} else {
		// Only 1 CSV from the ZIP is considered. If multiple files have
		// to be processed, counting the net number of lines (to track progress),
		// keeping the global import state (failed / successful) etc. across
		// multiple files becomes complex. Instead, it's just easier for the
		// end user to concat multiple CSVs (if there are multiple in the first)
		// place and upload as one in the first place.
		dir, files, err := sess.ExtractZIP(out.Name(), 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				a.i18n.Ts("import.errorProcessingZIP", "error", err.Error()))
		}
		tempPaths = append(tempPaths, dir)

		go sess.LoadCSV(dir + "/" + files[0])
	}

	// The import now owns the temp paths for as long as it runs.
	armed = true
	go func() {
		<-done
		removeTempPaths(a.log, tempPaths...)
	}()

	return c.JSON(http.StatusOK, okResp{a.importer.GetStats()})
}

// importPoolCustomers is the public-pool branch of the unified customer
// import endpoint. It is intentionally synchronous: one uploaded workbook is
// parsed and committed as one transaction, and the response contains only
// aggregate counts plus safe row numbers/codes.
func (a *App) importPoolCustomers(c echo.Context, poolID int, opt subimporter.SessionOpt) error {
	a.poolImportMu.Lock()
	defer a.poolImportMu.Unlock()

	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("import.invalidFile", "error", err.Error()))
	}
	if mf := c.Request().MultipartForm; mf != nil {
		defer func() {
			if err := mf.RemoveAll(); err != nil {
				a.log.Printf("error removing multipart temporary files: %v", err)
			}
		}()
	}
	if file.Size > maxPoolAllocationUploadSize {
		return tooLargeErr(a, fmt.Sprintf("file (max %d MB)", maxPoolAllocationUploadSize>>20))
	}
	rows, err := parsePoolContactImportFile(file, opt.FieldMap)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	result, err := a.core.ImportPoolContacts(poolID, auth.GetUser(c).ID, rows)
	if err != nil {
		return err
	}
	setAuditObjectID(c, strconv.Itoa(poolID))
	setAuditMetadata(c, map[string]any{
		"target":     result.Target,
		"total":      result.Total,
		"valid":      result.Valid,
		"created":    result.Created,
		"existing":   result.Existing,
		"conflicts":  result.Conflicts,
		"invalid":    result.Invalid,
		"duplicates": result.Duplicates,
	})
	return c.JSON(http.StatusOK, okResp{result})
}

// removeTempPaths removes files and directories created for an import. Removing
// them is best effort: a failure is logged rather than returned, because the
// import result has already been reported to the caller.
func removeTempPaths(logger *log.Logger, paths ...string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			logger.Printf("error removing import temporary path '%s': %v", p, err)
		}
	}
}

// GetImportCustomers returns import statistics.
func (a *App) GetImportCustomers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersImport); err != nil {
		return err
	}
	if err := a.requireImportAccess(access); err != nil {
		return err
	}
	s := a.importer.GetStats()
	return c.JSON(http.StatusOK, okResp{s})
}

// GetImportCustomerStats returns import statistics.
func (a *App) GetImportCustomerStats(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersImport); err != nil {
		return err
	}
	if err := a.requireImportAccess(access); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{string(a.importer.GetLogs())})
}

// StopImportCustomers sends a stop signal to the importer.
// If there's an ongoing import, it'll be stopped, and if an import
// is finished, it's state is cleared.
func (a *App) StopImportCustomers(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCustomersImport); err != nil {
		return err
	}
	if err := a.requireImportAccess(access); err != nil {
		return err
	}
	a.importer.Stop()
	return c.JSON(http.StatusOK, okResp{a.importer.GetStats()})
}

func (a *App) requireImportAccess(access models.WorkspaceAccess) error {
	status := a.importer.GetStats()
	if status.OwnerUserID == 0 || access.PlatformAdmin {
		return nil
	}
	workspaceID := 0
	if access.IsOrganization() {
		workspaceID = access.OrganizationID
	}
	if status.OwnerUserID != access.UserID || status.OrganizationID != workspaceID {
		return echo.NewHTTPError(http.StatusForbidden, "import belongs to another workspace")
	}
	return nil
}
