// Package oidctest implements a minimal, self-contained OpenID Connect provider
// for tests. It serves a discovery document, a JWKS, a token endpoint and an
// optional userinfo endpoint, so code that talks to a real provider (for
// instance internal/auth.ExchangeOIDCToken) can be exercised end to end without
// a network, an external identity provider or a database.
//
// The provider is deliberately explicit about the claims it hands out: tests set
// IDTokenClaims and UserInfoClaims verbatim, which is what makes it possible to
// assert behavior for a verified, an unverified and an entirely absent
// `email_verified` claim.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const (
	// KeyID is the `kid` advertised in the JWKS and used to sign ID tokens.
	KeyID = "oidctest-key"

	// DefaultClientID is the audience written into signed ID tokens.
	DefaultClientID = "oidctest-client"

	// DefaultNonce is written into the ID token unless overridden.
	DefaultNonce = "oidctest-nonce"

	// AccessToken is the token handed to the userinfo endpoint.
	AccessToken = "oidctest-access-token"
)

// Provider is a fake OIDC provider backed by an httptest server.
type Provider struct {
	// URL is the issuer URL, i.e. the httptest server URL.
	URL string

	// ClientID is the audience (`aud`) written into ID tokens. It must match the
	// client ID the code under test is configured with.
	ClientID string

	// Nonce is written into ID tokens so that callers can compare it against the
	// nonce of their login request.
	Nonce string

	// IDTokenClaims are merged into the signed ID token. Leaving `email` out of
	// this map is what makes a client fall back to the userinfo endpoint.
	IDTokenClaims map[string]any

	// UserInfoClaims is served by the userinfo endpoint. When nil, the endpoint
	// is neither advertised in the discovery document nor served.
	UserInfoClaims map[string]any

	key    *rsa.PrivateKey
	server *httptest.Server

	mu            sync.Mutex
	tokenCalls    int
	userInfoCalls int
}

// New starts a fake provider and closes it when the test finishes.
func New(t testing.TB) *Provider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("oidctest: generating key: %v", err)
	}

	p := &Provider{
		ClientID:      DefaultClientID,
		Nonce:         DefaultNonce,
		IDTokenClaims: map[string]any{},
		key:           key,
	}
	srv := httptest.NewServer(http.HandlerFunc(p.serve))
	p.server = srv
	p.URL = srv.URL
	t.Cleanup(srv.Close)

	return p
}

// Close shuts the provider down. It is also registered as a test cleanup.
func (p *Provider) Close() {
	p.server.Close()
}

// TokenCalls returns the number of requests the token endpoint has served.
func (p *Provider) TokenCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokenCalls
}

// UserInfoCalls returns the number of requests the userinfo endpoint has served.
func (p *Provider) UserInfoCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userInfoCalls
}

func (p *Provider) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		p.serveDiscovery(w)
	case "/keys":
		p.serveKeys(w)
	case "/token":
		p.serveToken(w)
	case "/userinfo":
		p.serveUserInfo(w)
	default:
		http.NotFound(w, r)
	}
}

func (p *Provider) serveDiscovery(w http.ResponseWriter) {
	doc := map[string]any{
		"issuer":                                p.URL,
		"authorization_endpoint":                p.URL + "/auth",
		"token_endpoint":                        p.URL + "/token",
		"jwks_uri":                              p.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
	}
	if p.UserInfoClaims != nil {
		doc["userinfo_endpoint"] = p.URL + "/userinfo"
	}
	writeJSON(w, doc)
}

func (p *Provider) serveKeys(w http.ResponseWriter) {
	pub := p.key.Public().(*rsa.PublicKey)
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": KeyID,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

func (p *Provider) serveToken(w http.ResponseWriter) {
	p.mu.Lock()
	p.tokenCalls++
	p.mu.Unlock()

	writeJSON(w, map[string]any{
		"access_token": AccessToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     p.signIDToken(),
	})
}

func (p *Provider) serveUserInfo(w http.ResponseWriter) {
	if p.UserInfoClaims == nil {
		http.NotFound(w, nil)
		return
	}

	p.mu.Lock()
	p.userInfoCalls++
	p.mu.Unlock()

	writeJSON(w, p.UserInfoClaims)
}

// signIDToken signs a JWT with the claims configured on the provider, using
// RS256 and the key advertised through the JWKS endpoint.
func (p *Provider) signIDToken() string {
	claims := map[string]any{}
	for k, v := range p.IDTokenClaims {
		claims[k] = v
	}
	if _, ok := claims["sub"]; !ok {
		claims["sub"] = "oidctest-subject"
	}
	now := time.Now()
	claims["iss"] = p.URL
	claims["aud"] = p.ClientID
	claims["exp"] = now.Add(time.Hour).Unix()
	claims["iat"] = now.Unix()
	if p.Nonce != "" {
		claims["nonce"] = p.Nonce
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		panic("oidctest: marshalling claims: " + err.Error())
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"` + KeyID + `","typ":"JWT"}`))
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(payload)

	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		panic("oidctest: signing ID token: " + err.Error())
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
