package auth

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

func TestMiddlewareDoesNotIgnoreAuthorizationWhenSessionCookiePresent(t *testing.T) {
	a := &Auth{apiUsers: map[string]User{}}
	e := echo.New()
	req := httptest.NewRequest("GET", "/api/workspace", nil)
	req.Header.Set("Cookie", "session=current-session")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("legacy-v3-user:legacy-v3-password")))
	c := e.NewContext(req, httptest.NewRecorder())

	called := false
	err := a.Middleware(func(ctx echo.Context) error {
		called = true
		return nil
	})(c)
	if err != nil {
		t.Fatalf("middleware returned unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected middleware chain to continue with an auth error in context")
	}
	got, ok := c.Get(UserHTTPCtxKey).(*echo.HTTPError)
	if !ok || got.Code != 403 {
		t.Fatalf("expected invalid explicit BasicAuth to be rejected, got %#v", c.Get(UserHTTPCtxKey))
	}
}

func TestHashIntegrationTokenDeterministic(t *testing.T) {
	tok := "lmit_example_secret"

	a := HashIntegrationToken(tok)
	b := HashIntegrationToken(tok)

	if a == "" {
		t.Fatal("expected non-empty token hash")
	}
	if a != b {
		t.Fatalf("expected deterministic hash, got %q and %q", a, b)
	}
}

func TestGetIntegrationToken(t *testing.T) {
	user := User{Username: "svc-openclaw"}
	tok := "lmit_example_secret"

	a := &Auth{
		integrationTokens: map[string]IntegrationToken{
			HashIntegrationToken(tok): {
				Base: Base{ID: 9},
				User: user,
			},
		},
	}

	gotUser, gotToken, ok := a.GetIntegrationToken(tok)
	if !ok {
		t.Fatal("expected token lookup to succeed")
	}
	if gotToken.ID != 9 {
		t.Fatalf("expected token id 9, got %d", gotToken.ID)
	}
	if gotUser.Username != user.Username {
		t.Fatalf("expected username %q, got %q", user.Username, gotUser.Username)
	}

	if _, _, ok := a.GetIntegrationToken("wrong"); ok {
		t.Fatal("expected invalid token lookup to fail")
	}
}

func TestGetIntegrationTokenRejectsExpiredKey(t *testing.T) {
	tok := "lmpk_expired"
	a := &Auth{
		integrationTokens: map[string]IntegrationToken{
			HashIntegrationToken(tok): {
				Base:      Base{ID: 10},
				ExpiresAt: null.Time{Time: time.Now().Add(-time.Minute), Valid: true},
			},
		},
	}

	if _, _, ok := a.GetIntegrationToken(tok); ok {
		t.Fatal("expected expired token lookup to fail")
	}
}
