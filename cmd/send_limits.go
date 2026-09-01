package main

import (
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/schedule"
	"github.com/knadh/listmonk/models"
)

const dailyResumeLayout = schedule.DailyResumeLayout

const defaultCampaignDailySendLimit = 300

func currentLocalDate() string {
	return schedule.CurrentLocalDate()
}

// campaignBatchLimit returns the maximum number of recipients that may be
// queued in this batch and whether the campaign must be deferred first.
// smtpRemaining is -1 when the account SMTP pool has no finite daily quota.
// Keeping this decision independent from SQL makes the campaign cap explicit
// and prevents a zero/legacy value from accidentally becoming unlimited.
func campaignBatchLimit(campaignType, messenger string, dailyLimit, dailySent, queued, requested, smtpRemaining int) (int, bool) {
	if requested < 1 {
		requested = 1
	}

	if campaignType != models.CampaignTypeRegular || !email.IsMessengerName(messenger) {
		return requested, false
	}

	dailyLimit = normalizedCampaignDailySendLimit(dailyLimit)
	remaining := dailyLimit - dailySent - queued
	if smtpRemaining >= 0 && smtpRemaining < remaining {
		remaining = smtpRemaining
	}
	if remaining <= 0 {
		return 0, true
	}
	if remaining < requested {
		return remaining, false
	}
	return requested, false
}

func normalizedCampaignDailySendLimit(dailyLimit int) int {
	if dailyLimit < 1 {
		return defaultCampaignDailySendLimit
	}
	return dailyLimit
}
