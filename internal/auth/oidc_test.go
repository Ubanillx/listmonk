package auth

import (
	"errors"
	"fmt"
	"io"
	"log"
	"testing"

	"github.com/knadh/listmonk/internal/oidctest"
)

func boolPtr(b bool) *bool { return &b }

// TestVerifyOIDCEmailClaims locks the login/auto-creation policy for the e-mail
// identity of an OIDC login. Only an explicit `email_verified: true` may be
// trusted; `false` and an absent claim are both refused so that a provider that
// hands out an unverified (or attacker-controlled) address cannot be used to log
// into somebody else's account.
func TestVerifyOIDCEmailClaims(t *testing.T) {
	for _, tc := range []struct {
		name      string
		claims    OIDCclaim
		wantEmail string
		wantErr   error
	}{
		{
			name:      "verified",
			claims:    OIDCclaim{Email: "  User@Example.COM ", EmailVerified: boolPtr(true)},
			wantEmail: "user@example.com",
		},
		{
			name:    "explicitly unverified",
			claims:  OIDCclaim{Email: "user@example.com", EmailVerified: boolPtr(false)},
			wantErr: ErrOIDCEmailUnverified,
		},
		{
			name:    "claim absent entirely",
			claims:  OIDCclaim{Email: "user@example.com"},
			wantErr: ErrOIDCEmailUnverified,
		},
		{
			name:    "no address",
			claims:  OIDCclaim{EmailVerified: boolPtr(true)},
			wantErr: ErrOIDCEmailMissing,
		},
		{
			name:    "unverified and no address",
			claims:  OIDCclaim{EmailVerified: boolPtr(false)},
			wantErr: ErrOIDCEmailMissing,
		},
		{
			name:    "malformed address",
			claims:  OIDCclaim{Email: "not-an-address", EmailVerified: boolPtr(true)},
			wantErr: ErrOIDCEmailInvalid,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email, err := VerifyOIDCEmailClaims(tc.claims)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if email != tc.wantEmail {
				t.Fatalf("expected e-mail %q, got %q", tc.wantEmail, email)
			}
		})
	}
}

// TestExchangeOIDCTokenEmailVerification drives a real token exchange against a
// fake provider. It covers both sources of the e-mail claim: the ID token and the
// userinfo fallback that is used when the ID token carries no address.
func TestExchangeOIDCTokenEmailVerification(t *testing.T) {
	for _, tc := range []struct {
		name           string
		idClaims       map[string]any
		userInfoClaims map[string]any
		wantEmail      string
		wantErr        error
		wantUserInfo   int
	}{
		{
			name:      "verified id token claim",
			idClaims:  map[string]any{"email": "User@Example.com", "email_verified": true},
			wantEmail: "user@example.com",
		},
		{
			name:     "unverified id token claim",
			idClaims: map[string]any{"email": "user@example.com", "email_verified": false},
			wantErr:  ErrOIDCEmailUnverified,
		},
		{
			name:     "id token claim without email_verified",
			idClaims: map[string]any{"email": "user@example.com"},
			wantErr:  ErrOIDCEmailUnverified,
		},
		{
			name:     "id token without an address falls back to a verified userinfo claim",
			idClaims: map[string]any{},
			userInfoClaims: map[string]any{
				"sub": "oidctest-subject", "email": "fallback@example.com", "email_verified": true,
			},
			wantEmail:    "fallback@example.com",
			wantUserInfo: 1,
		},
		{
			name:     "userinfo fallback with an unverified address",
			idClaims: map[string]any{},
			userInfoClaims: map[string]any{
				"sub": "oidctest-subject", "email": "fallback@example.com", "email_verified": false,
			},
			wantErr:      ErrOIDCEmailUnverified,
			wantUserInfo: 1,
		},
		{
			name:     "userinfo fallback without email_verified",
			idClaims: map[string]any{},
			userInfoClaims: map[string]any{
				"sub": "oidctest-subject", "email": "fallback@example.com",
			},
			wantErr:      ErrOIDCEmailUnverified,
			wantUserInfo: 1,
		},
		{
			name:           "userinfo fallback without an address",
			idClaims:       map[string]any{},
			userInfoClaims: map[string]any{"sub": "oidctest-subject"},
			wantErr:        ErrOIDCEmailMissing,
			wantUserInfo:   1,
		},
		{
			name:     "unverified address in the id token is not rescued by the userinfo endpoint",
			idClaims: map[string]any{"email": "user@example.com", "email_verified": false},
			userInfoClaims: map[string]any{
				"sub": "oidctest-subject", "email": "user@example.com", "email_verified": true,
			},
			wantErr: ErrOIDCEmailUnverified,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idp := oidctest.New(t)
			idp.IDTokenClaims = tc.idClaims
			idp.UserInfoClaims = tc.userInfoClaims

			a := newOIDCTestAuth(t, idp)

			_, claims, err := a.ExchangeOIDCToken("test-code", oidctest.DefaultNonce)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if err != nil {
				if claims.Email != "" {
					t.Fatalf("expected no claims alongside the error, got %q", claims.Email)
				}
			} else if claims.Email != tc.wantEmail {
				t.Fatalf("expected e-mail %q, got %q", tc.wantEmail, claims.Email)
			}

			// The fallback must only run when the ID token has no address.
			if got := idp.UserInfoCalls(); got != tc.wantUserInfo {
				t.Fatalf("expected %d userinfo call(s), got %d", tc.wantUserInfo, got)
			}
		})
	}
}

// TestExchangeOIDCTokenWithoutUserInfoEndpoint covers a provider that does not
// advertise the userinfo endpoint and an ID token without an address: the login
// must fail rather than proceed without an identity.
func TestExchangeOIDCTokenWithoutUserInfoEndpoint(t *testing.T) {
	idp := oidctest.New(t)
	idp.IDTokenClaims = map[string]any{}

	a := newOIDCTestAuth(t, idp)

	if _, _, err := a.ExchangeOIDCToken("test-code", oidctest.DefaultNonce); err == nil {
		t.Fatal("expected the login to fail without an e-mail claim")
	}
}

// TestExchangeOIDCTokenMalformedEmailVerified covers providers that send
// `email_verified` as a quoted string (AWS Cognito is a known one). Such a claim
// does not decode into the expected type, so the login is refused rather than
// treated as verified.
func TestExchangeOIDCTokenMalformedEmailVerified(t *testing.T) {
	for _, value := range []any{"true", "false", 1, nil} {
		t.Run(fmt.Sprintf("%v", value), func(t *testing.T) {
			idp := oidctest.New(t)
			idp.IDTokenClaims = map[string]any{"email": "user@example.com", "email_verified": value}

			a := newOIDCTestAuth(t, idp)

			if _, _, err := a.ExchangeOIDCToken("test-code", oidctest.DefaultNonce); err == nil {
				t.Fatalf("expected the malformed email_verified claim %#v to be refused", value)
			}
		})
	}
}

// newOIDCTestAuth returns an Auth wired to the given fake provider. It bypasses
// New() on purpose: the session store and the database are irrelevant here.
func newOIDCTestAuth(t *testing.T, idp *oidctest.Provider) *Auth {
	t.Helper()

	a := &Auth{
		cfg: Config{OIDC: OIDCConfig{
			Enabled:      true,
			ProviderURL:  idp.URL,
			ClientID:     idp.ClientID,
			ClientSecret: "test-secret",
			RedirectURL:  "http://localhost/auth/oidc",
		}},
		log: log.New(io.Discard, "", 0),
	}
	if err := a.initOIDC(); err != nil {
		t.Fatalf("initializing OIDC against the test provider: %v", err)
	}

	return a
}
