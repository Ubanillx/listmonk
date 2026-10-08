package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

type replyMailboxRequest struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	IMAPHost  string `json:"imap_host"`
	IMAPPort  int    `json:"imap_port"`
	IMAPTLS   *bool  `json:"imap_tls"`
	Folder    string `json:"folder"`
	IsDefault bool   `json:"is_default"`
	AIEnabled bool   `json:"ai_enabled"`
}

type replyMailboxTestRequest struct {
	ID int `json:"id"`
}

func (a *App) GetReplyMailboxes(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesUse, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID := auth.GetUser(c).ID
	rows := make([]models.ReplyMailbox, 0)
	if err := a.queries.GetReplyMailboxes.Select(&rows, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
		return err
	}
	// The listing is what the management UI renders, so each row carries the
	// deletion right the delete endpoint enforces: the owner, or a manager of
	// the organization the mailbox belongs to. Rows without it stay without a
	// delete button instead of offering a request the server answers with 404.
	managesOrganization := access.OrganizationID > 0 && access.IsOrganizationManager()
	user := auth.GetUser(c)
	canConfigure := user.HasPerm(auth.PermMailboxesManage)
	for i := range rows {
		rows[i].Manageable = rows[i].Manageable && canConfigure
		rows[i].Deletable = canConfigure && (rows[i].UserID == userID ||
			(managesOrganization && rows[i].OrganizationID.Valid &&
				rows[i].OrganizationID.Int == access.OrganizationID))
		if !canConfigure {
			rows[i].Username = ""
			rows[i].IMAPHost = ""
			rows[i].IMAPPort = 0
			rows[i].Folder = ""
			rows[i].LastSyncErr = ""
		}
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

func (a *App) CreateReplyMailbox(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID := auth.GetUser(c).ID
	var req replyMailboxRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.AIEnabled && strings.TrimSpace(req.Password) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "password or client authorization code is required")
	}
	if !req.AIEnabled {
		req.Password = ""
	}
	if err := validateReplyMailboxRequest(&req); err != nil {
		return err
	}
	tx, err := a.db.BeginTxx(c.Request().Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if req.IsDefault {
		if _, err := tx.Exec(`UPDATE reply_mailboxes SET is_default = FALSE, updated_at = NOW() WHERE user_id = $1 AND organization_id IS NOT DISTINCT FROM $2`, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
			return err
		}
	}
	var id int
	if err := tx.Stmtx(a.queries.CreateReplyMailbox).Get(&id, userID, nullableOrganizationID(access.OrganizationID), req.Email, req.Name,
		req.Username, req.IMAPHost, req.IMAPPort, boolValue(req.IMAPTLS), req.Folder,
		req.Password, req.IsDefault, req.AIEnabled); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.getReplyMailboxResponse(c, userID, id, access.OrganizationID)
}

func (a *App) UpdateReplyMailbox(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID, id := auth.GetUser(c).ID, getID(c)
	var req replyMailboxRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.AIEnabled && req.Password == "" {
		var hasPassword bool
		if err := a.db.Get(&hasPassword, `SELECT password <> '' FROM reply_mailboxes
			WHERE id=$1 AND user_id=$2 AND organization_id IS NOT DISTINCT FROM $3`,
			id, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
		}
		if !hasPassword {
			return echo.NewHTTPError(http.StatusBadRequest, "password or client authorization code is required for AI reply processing")
		}
	}
	if err := validateReplyMailboxRequest(&req); err != nil {
		return err
	}
	tx, err := a.db.BeginTxx(c.Request().Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if req.IsDefault {
		if _, err := tx.Exec(`UPDATE reply_mailboxes SET is_default = FALSE, updated_at = NOW() WHERE user_id = $1 AND id <> $2 AND organization_id IS NOT DISTINCT FROM $3`, userID, id, nullableOrganizationID(access.OrganizationID)); err != nil {
			return err
		}
	}
	var updatedID int
	if err := tx.Stmtx(a.queries.UpdateReplyMailbox).Get(&updatedID, id, userID, req.Email,
		req.Name, req.Username, req.IMAPHost, req.IMAPPort, boolValue(req.IMAPTLS), req.Folder,
		req.Password, req.IsDefault, req.AIEnabled, nullableOrganizationID(access.OrganizationID)); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.getReplyMailboxResponse(c, userID, updatedID, access.OrganizationID)
}

func (a *App) DisableReplyMailbox(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID, id := auth.GetUser(c).ID, getID(c)
	var disabledID int
	if err := a.queries.DisableReplyMailbox.Get(&disabledID, id, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteReplyMailbox removes a mailbox for good. The owner can always remove
// their own mailbox, and a manager of the owning organization can remove a
// stale one, which is how a mailbox left behind by a former member is cleaned
// up; everybody else receives the same 404 the rest of this surface returns.
// Two still-in-use cases are refused instead of silently breaking routing: the
// organization's unified reply mailbox and a mailbox an active reply forwarding
// rule depends on.
func (a *App) DeleteReplyMailbox(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	user, id := auth.GetUser(c), getID(c)

	var row struct {
		OwnerID        int   `db:"user_id"`
		OrganizationID int64 `db:"organization_id"`
	}
	if err := a.db.Get(&row, `SELECT user_id,COALESCE(organization_id,0) AS organization_id FROM reply_mailboxes WHERE id=$1`, id); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}
	// A mailbox is addressed from its own workspace, exactly like update,
	// disable and enable: a personal mailbox from the caller's personal
	// workspace and an organization mailbox from that organization's workspace.
	matchesWorkspace := (row.OrganizationID == 0 && int64(access.OrganizationID) == 0) ||
		(row.OrganizationID > 0 && row.OrganizationID == int64(access.OrganizationID))
	managesOwningOrganization := row.OrganizationID > 0 &&
		matchesWorkspace && access.IsOrganizationManager()
	if !matchesWorkspace || (row.OwnerID != user.ID && !managesOwningOrganization) {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}

	var inUse bool
	if err := a.db.Get(&inUse, `SELECT EXISTS(SELECT 1 FROM organizations WHERE reply_mailbox_id=$1)`, id); err != nil {
		return err
	}
	if inUse {
		return echo.NewHTTPError(http.StatusConflict, "reply mailbox is the organization's unified reply mailbox; select another one first")
	}
	if err := a.db.Get(&inUse, `SELECT EXISTS(SELECT 1 FROM reply_forward_rules WHERE reply_mailbox_id=$1 AND status='active')`, id); err != nil {
		return err
	}
	if inUse {
		return echo.NewHTTPError(http.StatusConflict, "reply mailbox has an active reply forwarding rule; remove it first")
	}

	var deleted int
	if err := a.queries.DeleteReplyMailbox.Get(&deleted, id); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// EnableReplyMailbox restores a mailbox that was manually disabled. A
// reply address becomes active immediately; an AI-enabled mailbox requires a
// previously verified connection or remains pending until tested.
func (a *App) EnableReplyMailbox(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID, id := auth.GetUser(c).ID, getID(c)
	var enabledID int
	if err := a.queries.EnableReplyMailbox.Get(&enabledID, id, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}
	return a.getReplyMailboxResponse(c, userID, enabledID, access.OrganizationID)
}

// TestReplyMailbox verifies the saved mailbox configuration. The browser only
// supplies its ID; credentials are loaded with the same owner/workspace scope
// as updates and never returned to the browser.
func (a *App) TestReplyMailbox(c echo.Context) error {
	if err := requireMailboxPermission(c, auth.PermMailboxesManage); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID := auth.GetUser(c).ID
	var req replyMailboxTestRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("replyMailbox.saveBeforeTest"))
	}
	var saved struct {
		replyAIMailboxSource
		Status    string `db:"status"`
		AIEnabled bool   `db:"ai_enabled"`
	}
	if err := a.db.Get(&saved, `SELECT id, user_id, organization_id, email, username, password,
		imap_host, imap_port, imap_tls, folder, status, ai_enabled FROM reply_mailboxes
		WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3`,
		req.ID, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
	}
	if !saved.AIEnabled {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("replyMailbox.testRequiresAI"))
	}
	if saved.Password == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "password or client authorization code is required")
	}
	if err := withReplyIMAPBudget(replyAIScanBudget{dial: mailboxDialTimeout, read: replyAIMailboxReadTimeout, scan: replyAIMailboxTimeout}, func(dialer *replyAIDialer) error {
		client, _, err := connectReplyIMAP(saved.replyAIMailboxSource, dialer)
		if err != nil {
			return err
		}
		return client.Logout()
	}); err != nil {
		if errors.Is(err, errMailboxHostBlocked) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("mailbox connection failed: %v", err))
	}
	// A concurrent edit/disable must not be marked verified by a test of the old
	// configuration. Retained mailboxes keep their forwarding lifecycle state.
	res, err := a.db.Exec(`UPDATE reply_mailboxes
		SET status = CASE WHEN status IN ('disabled', 'retained') THEN status ELSE 'active' END,
		verified_at = COALESCE(verified_at, NOW()), updated_at = NOW(), last_sync_error = ''
		WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3
		AND ai_enabled = TRUE
		AND (email, username, password, imap_host, imap_port, imap_tls, folder, status)
		IS NOT DISTINCT FROM ($4, $5, $6, $7, $8, $9, $10, $11)`,
		req.ID, userID, nullableOrganizationID(access.OrganizationID), saved.Email,
		saved.Username, saved.Password, saved.Host, saved.Port, saved.TLSEnabled, saved.Folder, saved.Status)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return echo.NewHTTPError(http.StatusConflict, a.i18n.T("replyMailbox.changedDuringTest"))
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) getReplyMailboxResponse(c echo.Context, userID, id, organizationID int) error {
	var row models.ReplyMailbox
	if err := a.queries.GetReplyMailbox.Get(&row, id, userID, nullableOrganizationID(organizationID)); err != nil {
		return err
	}
	// The row comes from an owner-scoped read, so the caller may always delete
	// it; the flag keeps the card rendered after a save in step with the
	// listing, which computes the same right for every row.
	row.Deletable = true
	row.Manageable = true
	return c.JSON(http.StatusOK, okResp{row})
}

func validateReplyMailboxRequest(req *replyMailboxRequest) error {
	req.Email = strings.TrimSpace(req.Email)
	addr, err := mail.ParseAddress(req.Email)
	if err != nil || addr.Address != req.Email || !strings.Contains(addr.Address, "@") {
		return echo.NewHTTPError(http.StatusBadRequest, "valid reply mailbox email is required")
	}
	req.Name = strings.TrimSpace(req.Name)
	// Address-only mailboxes do not require or validate receiving parameters.
	// Unused defaults keep the existing storage shape; updates preserve any
	// previously saved connection configuration while AI is turned off.
	if !req.AIEnabled {
		req.Username, req.IMAPHost, req.IMAPPort, req.Folder = req.Email, "imap.263.net", 993, "INBOX"
		return nil
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		req.Username = req.Email
	}
	if req.IMAPHost == "" {
		req.IMAPHost = "imap.263.net"
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = 993
	}
	if req.IMAPPort < 1 || req.IMAPPort > 65535 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid IMAP port")
	}
	if req.Folder == "" {
		req.Folder = "INBOX"
	}
	return nil
}

func boolValue(v *bool) bool { return v == nil || *v }

func nullableOrganizationID(id int) any {
	if id < 1 {
		return nil
	}
	return id
}
