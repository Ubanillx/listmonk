package main

import (
	"fmt"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
)

// ValidateCampaignReplyRoute guards unsent private deliveries, including queued
// messages whose mailbox was disabled or edited after the scheduler loaded it.
// Pool routes use their independent per-recipient validation and snapshots.
func (s *store) ValidateCampaignReplyRoute(campaignID int, poolContactID int64, replyTo string) error {
	if campaignID <= 0 || s.db == nil {
		return nil
	}
	if poolContactID > 0 {
		var valid bool
		if err := s.db.Get(&valid, `SELECT EXISTS(SELECT 1 FROM campaign_pool_recipients cpr
		JOIN pool_contacts pc ON pc.id=cpr.pool_contact_id AND pc.status='active'
		WHERE cpr.campaign_id=$1 AND cpr.pool_contact_id=$2 AND `+core.PoolRecipientAvailableSQL+`
		AND NOT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions ex WHERE ex.pool_id=cpr.pool_id
		AND ex.organization_id=cpr.organization_id AND ex.contact_id=cpr.pool_contact_id AND ex.restored_at IS NULL))`, campaignID, poolContactID); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: pool allocation or delivery access is no longer active", manager.ErrReplyMailboxUnavailable)
		}
		return nil
	}
	var valid bool
	if err := s.db.Get(&valid, `SELECT (c.reply_mailbox_id IS NULL AND $2='') OR EXISTS(
		SELECT 1 FROM reply_mailboxes m WHERE m.id=c.reply_mailbox_id AND m.status='active'
		AND m.organization_id IS NOT DISTINCT FROM c.organization_id AND LOWER(m.email)=LOWER($2)
	) FROM campaigns c WHERE c.id=$1`, campaignID, replyTo); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: inactive mailbox or changed route", manager.ErrReplyMailboxUnavailable)
	}
	return nil
}

func (s *store) validateLoadedCampaignReplyMailbox(campaign *models.Campaign) error {
	return s.ValidateCampaignReplyRoute(campaign.ID, 0, campaign.ReplyMailboxEmail)
}
