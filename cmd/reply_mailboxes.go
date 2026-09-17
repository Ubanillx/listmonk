package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/knadh/go-pop3"
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
	replyMailboxRequest
	ID int `json:"id"`
}

func (a *App) GetReplyMailboxes(c echo.Context) error {
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
	for i := range rows {
		rows[i].Deletable = rows[i].UserID == userID ||
			(managesOrganization && rows[i].OrganizationID.Valid &&
				rows[i].OrganizationID.Int == access.OrganizationID)
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

func (a *App) CreateReplyMailbox(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID := auth.GetUser(c).ID
	var req replyMailboxRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Password) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "password or client authorization code is required")
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
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID, id := auth.GetUser(c).ID, getID(c)
	var req replyMailboxRequest
	if err := c.Bind(&req); err != nil {
		return err
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
// previously verified mailbox becomes active immediately; a mailbox that has
// never passed a connection test remains pending and must be tested first.
func (a *App) EnableReplyMailbox(c echo.Context) error {
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

// TestReplyMailbox verifies a 263 mailbox using POP3-over-TLS. 263 exposes
// both IMAP and POP3; POP3 is used here because it is already supported by
// the server and this endpoint only needs an authentication/connection test.
func (a *App) TestReplyMailbox(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID := auth.GetUser(c).ID
	var req replyMailboxTestRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.ID > 0 {
		var saved models.ReplyMailbox
		if err := a.queries.GetReplyMailbox.Get(&saved, req.ID, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "reply mailbox not found")
		}
		if req.Email == "" {
			req.Email = saved.Email
		}
		if req.Username == "" {
			req.Username = saved.Username
		}
		if req.Password == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "password or client authorization code is required")
		}
		if req.IMAPHost == "" {
			req.IMAPHost = saved.IMAPHost
		}
		if req.IMAPPort == 0 {
			req.IMAPPort = saved.IMAPPort
		}
	}
	if err := validateReplyMailboxRequest(&req.replyMailboxRequest); err != nil {
		return err
	}
	host, port := req.IMAPHost, req.IMAPPort
	if strings.HasPrefix(strings.ToLower(host), "imap.") {
		host = "pop." + strings.TrimPrefix(host, "imap.")
		port = 995
	}
	// Refuse a blocked target before any connection is attempted, and let the
	// dialer apply the same policy with the address it validated.
	if _, err := resolveMailboxHost(c.Request().Context(), host); err != nil {
		if errors.Is(err, errMailboxHostBlocked) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	client := pop3.New(pop3.Opt{Host: host, Port: port, TLSEnabled: true, Dialer: &mailboxPolicyDialer{}})
	conn, err := client.NewConn()
	if err != nil {
		if errors.Is(err, errMailboxHostBlocked) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("mailbox connection failed: %v", err))
	}
	defer conn.Quit()
	if err := conn.Auth(req.Username, req.Password); err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("mailbox authentication failed: %v", err))
	}
	if req.ID > 0 {
		if _, err := a.db.Exec(`UPDATE reply_mailboxes SET status = 'active', verified_at = COALESCE(verified_at, NOW()), updated_at = NOW(), last_sync_error = '' WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3`, req.ID, userID, nullableOrganizationID(access.OrganizationID)); err != nil {
			return err
		}
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
	return c.JSON(http.StatusOK, okResp{row})
}

func validateReplyMailboxRequest(req *replyMailboxRequest) error {
	req.Email = strings.TrimSpace(req.Email)
	addr, err := mail.ParseAddress(req.Email)
	if err != nil || addr.Address != req.Email || !strings.Contains(addr.Address, "@") {
		return echo.NewHTTPError(http.StatusBadRequest, "valid reply mailbox email is required")
	}
	req.Name = strings.TrimSpace(req.Name)
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
