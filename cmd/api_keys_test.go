package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

func TestNormalizePersonalAPIKeyScopes(t *testing.T) {
	all, err := normalizePersonalAPIKeyScopes(nil)
	if err != nil {
		t.Fatalf("expected default scopes: %v", err)
	}
	if len(all) != len(personalAPIKeyScopeOptions) {
		t.Fatalf("expected %d default scopes, got %d", len(personalAPIKeyScopeOptions), len(all))
	}

	if _, err := normalizePersonalAPIKeyScopes([]string{apiKeyScopeListsRead, "not:a-scope"}); err == nil {
		t.Fatal("expected unknown scope to be rejected")
	}
}

func TestParsePersonalAPIKeyExpiry(t *testing.T) {
	if _, err := parsePersonalAPIKeyExpiry("2026-99"); err == nil {
		t.Fatal("expected invalid month to be rejected")
	}

	now := time.Now().In(time.Local)
	month := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.Local).Format("2006-01")
	expiresAt, err := parsePersonalAPIKeyExpiry(month)
	if err != nil {
		t.Fatalf("expected next month to be valid: %v", err)
	}
	if expiresAt.Month() != now.AddDate(0, 1, 0).Month() || expiresAt.Day() < 28 {
		t.Fatalf("expected month-end expiry, got %v", expiresAt)
	}
}

func TestPersonalAPIKeyWorkspaceMatch(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest("GET", "/api/lists", nil), httptest.NewRecorder())
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{
		Kind:                    auth.IntegrationTokenKindPersonal,
		WorkspaceOrganizationID: null.Int{Int: 42, Valid: true},
		Scopes:                  pq.StringArray{apiKeyScopeListsRead},
	})

	if !personalAPIKeyWorkspaceMatches(c, 42) {
		t.Fatal("expected matching workspace")
	}
	if personalAPIKeyWorkspaceMatches(c, 0) {
		t.Fatal("expected personal workspace to be rejected")
	}
	if err := requireAPIKeyScope(c, apiKeyScopeListsRead); err != nil {
		t.Fatalf("expected allowed scope: %v", err)
	}
	if err := requireAPIKeyScope(c, apiKeyScopeCampaignsSend); err == nil {
		t.Fatal("expected missing scope to be rejected")
	}
}

func TestRejectUnsupportedPersonalAPIKey(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest("GET", "/api/profile", nil), httptest.NewRecorder())
	c.SetPath("/api/profile")
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal})

	nextCalled := false
	err := rejectUnsupportedPersonalAPIKey(func(echo.Context) error {
		nextCalled = true
		return nil
	})(c)
	if err == nil {
		t.Fatal("expected personal API key to be blocked from profile endpoint")
	}
	if nextCalled {
		t.Fatal("expected unsupported route to stop before its handler")
	}

	c = e.NewContext(httptest.NewRequest("GET", "/api/lists", nil), httptest.NewRecorder())
	c.SetPath("/api/lists")
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal})
	nextCalled = false
	if err := rejectUnsupportedPersonalAPIKey(func(echo.Context) error {
		nextCalled = true
		return nil
	})(c); err != nil {
		t.Fatalf("expected business endpoint to be allowed: %v", err)
	}
	if !nextCalled {
		t.Fatal("expected business route handler to run")
	}
}
