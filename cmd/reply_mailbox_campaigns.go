package main

import (
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// validateCampaignReplyMailbox enforces that a campaign can only reference a
// reply mailbox owned by the campaign owner and currently verified/active.
// Organization managers cannot use this path to attach their mailbox to a
// member's campaign because campaign mutation already enforces ownership.
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
	if ownerID != access.UserID {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox is not owned by this account")
	}
	if mailboxOrg.Valid != (access.OrganizationID > 0) || (mailboxOrg.Valid && mailboxOrg.Int != access.OrganizationID) {
		return echo.NewHTTPError(http.StatusForbidden, "reply mailbox belongs to another workspace")
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
