package main

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestCampaignSMTPSourceScope(t *testing.T) {
	for _, tc := range []struct {
		source    string
		orgID     int
		poolScope string
		wantErr   bool
	}{
		{"", 0, "organization", false},
		{"personal", 1, "organization", false},
		{"organization", 1, "organization", false},
		{"organization", 0, "organization", true},
		{"organization", 0, "all_organizations", false},
		{"system", 1, "organization", true},
	} {
		camp := models.Campaign{SMTPSource: tc.source, PoolScope: tc.poolScope}
		err := normalizeCampaignSMTPSource(&camp, tc.orgID)
		if (err != nil) != tc.wantErr {
			t.Errorf("source=%q org=%d pool=%s: %v", tc.source, tc.orgID, tc.poolScope, err)
		}
		if tc.source == "" && camp.SMTPSource != "personal" {
			t.Fatal("legacy default changed")
		}
	}
}
