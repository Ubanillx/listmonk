package main

import (
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// validateCampaignReplyMailbox enforces that a campaign can only reference a
// reply mailbox of the workspace the campaign belongs to, and that the mailbox
// is currently verified/active.
//
// Selecting is deliberately NOT an ownership check. An organization workspace
// shares its customer reply mailboxes: the listing
// (queries/replies.sql get-reply-mailboxes) returns every mailbox of the
// organization so that any member can attach the organization's company
// address to a campaign of their own, and marks the caller's own rows with
// `manageable`. Editing, disabling, re-enabling and connection testing stay
// with the mailbox owner in cmd/reply_mailboxes.go, so a member who selected a
// shared mailbox cannot manage it.
//
// The ownership requirement is kept where it still matters: a personal
// mailbox may only be attached by the user who owns it. Organization managers
// cannot use this path to attach their mailbox to a member's campaign because
// campaign mutation already enforces campaign ownership.
func (a *App) validateCampaignReplyMailbox(access models.WorkspaceAccess, campaign *models.Campaign) error {
	if !campaign.ReplyMailboxID.Valid || campaign.ReplyMailboxID.Int < 1 {
		return nil
	}
	var status string
	var ownerID int
	var mailboxOrg null.Int
	if err := a.db.QueryRow(`SELECT status, user_id, organization_id FROM reply_mailboxes WHERE id = $1`, campaign.ReplyMailboxID.Int).Scan(&status, &ownerID, &mailboxOrg); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "reply mailbox not found")
	}
	// The mailbox must live in the active workspace: NULL (personal) there and
	// there only, or exactly the current organization.
	if mailboxOrg.Valid != (access.OrganizationID > 0) || (mailboxOrg.Valid && mailboxOrg.Int != access.OrganizationID) {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox belongs to another workspace")
	}
	// A personal mailbox is private to its owner. Organization mailboxes are
	// shared with every member of that organization and only become
	// manageable for the member who created them.
	if !mailboxOrg.Valid && ownerID != access.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox is not owned by this account")
	}
	if status != models.ReplyMailboxStatusActive {
		return echo.NewHTTPError(http.StatusConflict, "reply mailbox must be verified and active")
	}
	return nil
}

func (a *App) persistCampaignReplyMailbox(campaignID, userID int, mailboxID null.Int) error {
	// This helper intentionally accepts the null.Int shape without exposing it
	// in the request layer. A NULL value clears an existing selection.
	if campaignID < 1 || userID < 1 {
		return nil
	}
	var id any
	if mailboxID.Valid && mailboxID.Int > 0 {
		id = mailboxID.Int
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET reply_mailbox_id = $1, updated_at = NOW() WHERE id = $2 AND owner_user_id = $3`, id, campaignID, userID); err != nil {
		return err
	}
	return nil
}
