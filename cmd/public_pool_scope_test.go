package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
)

// TestNormalizeCampaignPoolScope pins the platform-level scope gate: only an
// explicit request with the dedicated permission may select
// 'all_organizations', an unknown value is rejected, and everything else stays
// on the legacy single-organization scope.
func TestNormalizeCampaignPoolScope(t *testing.T) {
	app := &App{}

	plain := auth.User{}
	permitted := auth.User{PermissionsMap: map[string]struct{}{
		auth.PermCampaignsPublicPoolSend: {},
	}}
	platformAdmin := auth.User{UserRoleID: auth.SuperAdminRoleID}

	for _, tc := range []struct {
		name    string
		user    auth.User
		scope   string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to organization", user: plain, scope: "", want: models.CampaignPoolScopeOrganization},
		{name: "explicit organization", user: plain, scope: models.CampaignPoolScopeOrganization, want: models.CampaignPoolScopeOrganization},
		{name: "unknown scope is rejected", user: plain, scope: "everything", wantErr: true},
		{name: "all organizations requires the permission", user: plain, scope: models.CampaignPoolScopeAllOrganizations, wantErr: true},
		{name: "permission grants all organizations", user: permitted, scope: models.CampaignPoolScopeAllOrganizations, want: models.CampaignPoolScopeAllOrganizations},
		{name: "platform admin is implicitly permitted", user: platformAdmin, scope: models.CampaignPoolScopeAllOrganizations, want: models.CampaignPoolScopeAllOrganizations},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := app.normalizeCampaignPoolScope(tc.user, tc.scope)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeCampaignPoolScope(%q) = %q, want an error", tc.scope, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeCampaignPoolScope(%q): %v", tc.scope, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeCampaignPoolScope(%q) = %q, want %q", tc.scope, got, tc.want)
			}
		})
	}
}

// TestPermissionsManifestCoversPublicPoolSend keeps the shipped permission
// manifest, the auth constant and the role UI in sync: a permission that only
// exists in one of them is either ungrantable or unenforceable.
func TestPermissionsManifestCoversPublicPoolSend(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "permissions.json"))
	if err != nil {
		t.Fatalf("read permissions.json: %v", err)
	}
	var groups []struct {
		Group       string   `json:"group"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &groups); err != nil {
		t.Fatalf("parse permissions.json: %v", err)
	}

	found := false
	for _, group := range groups {
		for _, permission := range group.Permissions {
			if permission == auth.PermCampaignsPublicPoolSend {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("permissions.json does not list %q", auth.PermCampaignsPublicPoolSend)
	}
}
