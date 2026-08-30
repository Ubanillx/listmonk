package main

import (
	"database/sql"
	"net/http"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// replyForwardRuleView is the manager-facing representation. It deliberately
// includes source/target identities so a manager can tell which former
// member's mailbox is being relayed, without exposing mailbox credentials.
type replyForwardRuleView struct {
	models.ReplyForwardRule
	MailboxEmail string `db:"mailbox_email" json:"mailbox_email"`
	MailboxName  string `db:"mailbox_name" json:"mailbox_name"`
	SourceUserID int    `db:"source_user_id" json:"source_user_id"`
	SourceEmail  string `db:"source_email" json:"source_email"`
	SourceName   string `db:"source_name" json:"source_name"`
}

type replyForwardRuleInput struct {
	Status string `json:"status"`
}

// resolveReplyForwardTarget prefers the organization creator while that
// account is still an active member with a usable email. If the creator has
// left or has no email, the first active organization manager becomes the
// fallback target.
func (a *App) resolveReplyForwardTarget(orgID int) (int, string, error) {
	var target struct {
		ID    int    `db:"id"`
		Email string `db:"email"`
	}
	err := a.db.Get(&target, `
		SELECT u.id, u.email
		FROM organizations o
		JOIN users u ON u.id = o.created_by_user_id
		JOIN organization_members om ON om.organization_id = o.id
			AND om.user_id = u.id AND om.removed_at IS NULL
		WHERE o.id = $1 AND u.status = 'enabled' AND NULLIF(TRIM(u.email), '') IS NOT NULL
		LIMIT 1`, orgID)
	if err == nil {
		return target.ID, target.Email, nil
	}
	if err != sql.ErrNoRows {
		return 0, "", err
	}
	err = a.db.Get(&target, `
		SELECT u.id, u.email
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.removed_at IS NULL AND u.status = 'enabled'
		  AND om.role = $2 AND NULLIF(TRIM(u.email), '') IS NOT NULL
		ORDER BY om.joined_at, om.user_id
		LIMIT 1`, orgID, models.OrganizationMemberRoleManager)
	if err != nil {
		return 0, "", err
	}
	return target.ID, target.Email, nil
}

// activateReplyForwardingForMember retains every dedicated reply mailbox used
// by the member's campaigns in this organization and creates an idempotent
// forwarding rule to the organization creator (or the first active manager
// when the creator is no longer available). Mailboxes are scoped by
// organization, so a member leaving one organization cannot leak replies from
// another organization.
func (a *App) activateReplyForwardingForMember(orgID, userID int) error {
	if orgID < 1 || userID < 1 {
		return nil
	}
	targetUserID, targetEmail, err := a.resolveReplyForwardTarget(orgID)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusConflict, "organization has no active manager with a work email for reply forwarding")
	}
	if err != nil {
		return err
	}
	var mailboxIDs []int
	if err := a.db.Select(&mailboxIDs, `
		SELECT DISTINCT c.reply_mailbox_id
		FROM campaigns c
		JOIN reply_mailboxes m ON m.id = c.reply_mailbox_id
		WHERE c.organization_id = $1
		  AND c.original_owner_user_id = $2
		  AND c.reply_mailbox_id IS NOT NULL
		  AND m.organization_id = $1`, orgID, userID); err != nil {
		return err
	}
	for _, mailboxID := range mailboxIDs {
		if _, err := a.db.Exec(`UPDATE reply_mailboxes SET status = 'retained', updated_at = NOW() WHERE id = $1 AND user_id = $2 AND organization_id = $3`, mailboxID, userID, orgID); err != nil {
			return err
		}
		_, err := a.db.Exec(`
			INSERT INTO reply_forward_rules
				(reply_mailbox_id, organization_id, target_user_id, target_email, status)
			SELECT $1, $2, $3, $4
			ON CONFLICT (reply_mailbox_id, organization_id) DO UPDATE SET
				target_user_id = EXCLUDED.target_user_id,
				target_email = EXCLUDED.target_email,
				status = 'active', disabled_at = NULL, disabled_by = NULL,
				last_error = '', updated_at = NOW()`, mailboxID, orgID, targetUserID, targetEmail)
		if err != nil {
			return err
		}
	}
	return nil
}

// disableReplyForwardRule is used by the organization manager UI to stop a
// retained mailbox from being relayed while leaving the source mailbox intact.
func (a *App) disableReplyForwardRule(orgID, mailboxID, actorID int) error {
	if orgID < 1 || mailboxID < 1 || actorID < 1 {
		return sql.ErrNoRows
	}
	res, err := a.db.Exec(`UPDATE reply_forward_rules SET status = 'disabled', disabled_at = NOW(), disabled_by = $3, updated_at = NOW() WHERE organization_id = $1 AND reply_mailbox_id = $2`, orgID, mailboxID, actorID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetReplyForwardRules lists retained customer-reply forwarding rules for
// the active organization. Only organization managers may inspect them.
func (a *App) GetReplyForwardRules(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	rows := make([]replyForwardRuleView, 0)
	err = a.db.Select(&rows, `
		SELECT r.id, r.reply_mailbox_id, r.organization_id, r.target_user_id,
		       r.target_email, r.status, r.disabled_at, r.disabled_by,
		       r.last_error, r.last_forward_at, r.created_at, r.updated_at,
		       m.email AS mailbox_email, m.name AS mailbox_name,
		       m.user_id AS source_user_id, COALESCE(u.email, '') AS source_email,
		       COALESCE(u.name, '') AS source_name
		FROM reply_forward_rules r
		JOIN reply_mailboxes m ON m.id = r.reply_mailbox_id
		LEFT JOIN users u ON u.id = m.user_id
		WHERE r.organization_id = $1
		ORDER BY r.status = 'active' DESC, r.updated_at DESC, r.id`, ws.OrganizationID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

// UpdateReplyForwardRule lets a manager pause or resume relaying. Resuming
// always refreshes the target address, so a creator who has left is replaced
// by an active organization manager.
func (a *App) UpdateReplyForwardRule(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	var req replyForwardRuleInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	status := req.Status
	if status != models.ReplyForwardStatusActive && status != models.ReplyForwardStatusDisabled {
		return echo.NewHTTPError(http.StatusBadRequest, "status must be active or disabled")
	}
	id := getID(c)
	if status == models.ReplyForwardStatusDisabled {
		if err := a.disableReplyForwardRule(ws.OrganizationID, id, auth.GetUser(c).ID); err != nil {
			if err == sql.ErrNoRows {
				return echo.NewHTTPError(http.StatusNotFound, "reply forwarding rule not found")
			}
			return err
		}
		return c.JSON(http.StatusOK, okResp{true})
	}
	targetUserID, targetEmail, err := a.resolveReplyForwardTarget(ws.OrganizationID)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusConflict, "organization has no active manager with a work email for reply forwarding")
	}
	if err != nil {
		return err
	}
	res, err := a.db.Exec(`
		UPDATE reply_forward_rules
		SET status = 'active', target_user_id = $3, target_email = $4,
		    disabled_at = NULL, disabled_by = NULL, last_error = '', updated_at = NOW()
		WHERE id = $1 AND organization_id = $2`, id, ws.OrganizationID, targetUserID, targetEmail)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "reply forwarding rule not found")
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// DeleteReplyForwardRule is an explicit alias for disabling a rule. The
// source mailbox and original messages are intentionally retained.
func (a *App) DeleteReplyForwardRule(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	if err := a.disableReplyForwardRule(ws.OrganizationID, getID(c), auth.GetUser(c).ID); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "reply forwarding rule not found")
		}
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}
