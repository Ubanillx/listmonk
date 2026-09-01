package main

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestCampaignBatchLimit(t *testing.T) {
	tests := []struct {
		name          string
		campaignType  string
		messenger     string
		dailyLimit    int
		dailySent     int
		queued        int
		requested     int
		smtpRemaining int
		wantLimit     int
		wantDeferred  bool
	}{
		{
			name:          "caps batch at campaign limit",
			campaignType:  models.CampaignTypeRegular,
			messenger:     "email",
			dailyLimit:    300,
			requested:     500,
			smtpRemaining: -1,
			wantLimit:     300,
		},
		{
			name:          "subtracts sent and queued recipients",
			campaignType:  models.CampaignTypeRegular,
			messenger:     "email",
			dailyLimit:    300,
			dailySent:     120,
			queued:        30,
			requested:     500,
			smtpRemaining: -1,
			wantLimit:     150,
		},
		{
			name:          "uses smaller SMTP capacity",
			campaignType:  models.CampaignTypeRegular,
			messenger:     "email",
			dailyLimit:    300,
			requested:     500,
			smtpRemaining: 40,
			wantLimit:     40,
		},
		{
			name:          "defers at exact campaign limit",
			campaignType:  models.CampaignTypeRegular,
			messenger:     "email",
			dailyLimit:    300,
			dailySent:     290,
			queued:        10,
			requested:     1,
			smtpRemaining: -1,
			wantDeferred:  true,
		},
		{
			name:          "legacy zero uses default cap",
			campaignType:  models.CampaignTypeRegular,
			messenger:     "email",
			dailyLimit:    0,
			requested:     500,
			smtpRemaining: -1,
			wantLimit:     defaultCampaignDailySendLimit,
		},
		{
			name:          "non email campaign is not capped",
			campaignType:  models.CampaignTypeRegular,
			messenger:     "postback",
			dailyLimit:    10,
			requested:     500,
			smtpRemaining: 2,
			wantLimit:     500,
		},
		{
			name:          "optin email is not capped",
			campaignType:  models.CampaignTypeOptin,
			messenger:     "email",
			dailyLimit:    10,
			requested:     500,
			smtpRemaining: 2,
			wantLimit:     500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLimit, gotDeferred := campaignBatchLimit(
				tt.campaignType,
				tt.messenger,
				tt.dailyLimit,
				tt.dailySent,
				tt.queued,
				tt.requested,
				tt.smtpRemaining,
			)
			if gotLimit != tt.wantLimit || gotDeferred != tt.wantDeferred {
				t.Fatalf("campaignBatchLimit() = (%d, %t), want (%d, %t)", gotLimit, gotDeferred, tt.wantLimit, tt.wantDeferred)
			}
		})
	}
}
