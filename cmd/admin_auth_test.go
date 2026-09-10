package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/oidctest"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// The tests in this file drive the admin login and first-time setup handlers end
// to end against a real PostgreSQL server, because both behaviors they guard are
// database-level properties: one initial super admin can be bootstrapped under
// concurrency, and an OIDC login may only bind to a verified e-mail address.
//
// They follow the pattern of the other database-backed tests in this repository:
// the DSN comes from an environment variable (ADMIN_AUTH_TEST_DSN, falling back to
// MIGRATION_TEST_DSN) and the test is skipped when neither is set. Unlike those
// tests, the schema is installed into a throwaway database rather than an isolated
// schema, because the application's user queries depend on pgcrypto's
// crypt()/gen_salt() and an isolated schema would only resolve those by falling
// back to `public` and hence to live data.
//
//	$env:ADMIN_AUTH_TEST_DSN = "postgres://user:pass@host:5432/db?sslmode=disable"   # PowerShell
//	export ADMIN_AUTH_TEST_DSN="postgres://user:pass@host:5432/db?sslmode=disable"   # POSIX
//	go test ./cmd/ -run 'FirstTimeSetup|OIDCLogin' -v

// newAdminAuthTestApp returns an App wired to the real core, auth and i18n stack
// on a throwaway database, so login and setup can be exercised through the
// handlers. When idp is non-nil, OIDC login is enabled against that provider with
// auto-creation of users.
func newAdminAuthTestApp(t *testing.T, idp *oidctest.Provider) *App {
	t.Helper()

	db := newAdminTestDB(t)
	queries := prepareQueries(readTestQueries(t), db, koanf.New("."))

	langB, err := os.ReadFile(filepath.Join("..", "i18n", "en.json"))
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	translator, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}
	logger := log.New(io.Discard, "", 0)

	co := core.New(&core.Opt{DB: db, Queries: queries, I18n: translator, Log: logger}, nil)

	// Session cookie callbacks, mirroring initAuth.
	cb := &auth.Callbacks{
		GetCookie: func(name string, r any) (*http.Cookie, error) {
			return r.(echo.Context).Cookie(name)
		},
		SetCookie: func(cookie *http.Cookie, w any) error {
			cookie.SameSite = http.SameSiteLaxMode
			w.(echo.Context).SetCookie(cookie)
			return nil
		},
		GetUser: func(id int) (auth.User, error) { return co.GetUser(id, "", "") },
	}

	oidcCfg := auth.OIDCConfig{}
	if idp != nil {
		oidcCfg = auth.OIDCConfig{
			Enabled:           true,
			ProviderURL:       idp.URL,
			ClientID:          idp.ClientID,
			ClientSecret:      "test-secret",
			RedirectURL:       "http://localhost/auth/oidc",
			AutoCreateUsers:   true,
			DefaultUserRoleID: auth.SuperAdminRoleID,
		}
	}

	authMod, err := auth.New(auth.Config{OIDC: oidcCfg}, db.DB, cb, logger)
	if err != nil {
		t.Fatalf("initializing auth: %v", err)
	}

	a := &App{
		db:             db,
		core:           co,
		auth:           authMod,
		i18n:           translator,
		log:            logger,
		cfg:            &Config{Permissions: map[string]struct{}{auth.PermUsersManage: {}}},
		needsUserSetup: true,
	}
	a.cfg.Security.OIDC.Enabled = oidcCfg.Enabled
	a.cfg.Security.OIDC.ProviderURL = oidcCfg.ProviderURL
	a.cfg.Security.OIDC.AutoCreateUsers = oidcCfg.AutoCreateUsers
	a.cfg.Security.OIDC.DefaultUserRoleID = oidcCfg.DefaultUserRoleID

	return a
}

// seedAdmin bootstraps the initial super admin, which is the state of any
// deployment that has been set up: the Super Admin role exists and one account
// owns it.
func seedAdmin(t *testing.T, a *App, username, email string) auth.User {
	t.Helper()

	u, err := a.core.FirstTimeSetup(auth.User{
		Type:          auth.UserTypeUser,
		HasPassword:   true,
		PasswordLogin: true,
		Username:      username,
		Name:          username,
		Password:      null.NewString("password123", true),
		Email:         null.NewString(email, true),
		UserRoleID:    auth.SuperAdminRoleID,
		Status:        auth.UserStatusEnabled,
	}, []string{auth.PermUsersManage})
	if err != nil {
		t.Fatalf("seeding the initial admin: %v", err)
	}
	a.setNeedsUserSetup(false)

	return u
}

// seedUser creates a regular admin account, standing in for an account that
// already exists when an OIDC login with that e-mail address is attempted.
func seedUser(t *testing.T, a *App, username, email string) auth.User {
	t.Helper()

	u, err := a.core.CreateUser(auth.User{
		Type:          auth.UserTypeUser,
		HasPassword:   true,
		PasswordLogin: true,
		Username:      username,
		Name:          username,
		Password:      null.NewString("password123", true),
		Email:         null.NewString(email, true),
		UserRoleID:    auth.SuperAdminRoleID,
		Status:        auth.UserStatusEnabled,
	})
	if err != nil {
		t.Fatalf("seeding the account %q: %v", email, err)
	}

	return u
}

// setupRequest builds a POST /admin/login request as submitted by the first-time
// setup form.
func setupRequest(t *testing.T, email, username, password string) (echo.Context, *httptest.ResponseRecorder, *recordingRenderer) {
	t.Helper()

	e, rr := newTestEcho()
	form := url.Values{
		"email":     {email},
		"username":  {username},
		"password":  {password},
		"password2": {password},
		"next":      {uriAdmin},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()

	return e.NewContext(req, rec), rec, rr
}

// oidcRequest builds the OIDC provider callback request for a login that started
// on the login page, carrying the nonce cookie and the encoded state.
func oidcRequest(t *testing.T, nonce string) (echo.Context, *httptest.ResponseRecorder, *recordingRenderer) {
	t.Helper()

	state, err := json.Marshal(oidcState{Nonce: nonce, Next: uriAdmin})
	if err != nil {
		t.Fatalf("encoding OIDC state: %v", err)
	}

	e, rr := newTestEcho()
	q := url.Values{
		"code":  {"test-code"},
		"state": {base64.URLEncoding.EncodeToString(state)},
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc?"+q.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: "nonce", Value: nonce})
	rec := httptest.NewRecorder()

	return e.NewContext(req, rec), rec, rr
}

// newTestEcho returns an echo instance whose renderer records the templates the
// handlers render instead of executing them.
func newTestEcho() (*echo.Echo, *recordingRenderer) {
	e := echo.New()
	rr := &recordingRenderer{}
	e.Renderer = rr
	return e, rr
}

type renderedTpl struct {
	name string
	data any
}

type recordingRenderer struct {
	mu    sync.Mutex
	calls []renderedTpl
}

func (r *recordingRenderer) Render(w io.Writer, name string, data any, c echo.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, renderedTpl{name: name, data: data})
	return nil
}

// last returns the most recently rendered template name, or "" when none was.
func (r *recordingRenderer) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return ""
	}
	return r.calls[len(r.calls)-1].name
}

// errorOf returns the error message rendered into the login page.
func (r *recordingRenderer) errorOf() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return ""
	}
	if tpl, ok := r.calls[len(r.calls)-1].data.(loginTpl); ok {
		return tpl.Error
	}
	return ""
}

// readTestQueries parses every application query file, mirroring what the running
// app prepares at startup.
func readTestQueries(t *testing.T) goyesql.Queries {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "queries", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("listing query files: %v (%d found)", err, len(files))
	}

	out := goyesql.Queries{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		parsed, err := goyesql.ParseBytes(b)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		maps.Copy(out, parsed)
	}

	return out
}

// newAdminTestDB installs the fresh-install schema into a throwaway database on
// the test server and returns a handle to it.
func newAdminTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := adminTestDSN(t)
	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connecting to the test PostgreSQL server: %v", err)
	}

	name := fmt.Sprintf("listmonk_admin_auth_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name)); err != nil {
		admin.Close()
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "42501" {
			t.Skipf("the test PostgreSQL role may not create databases: %v", err)
		}
		t.Fatalf("creating the test database: %v", err)
	}

	db, err := sqlx.Connect("postgres", dsnWithDatabase(t, dsn, name))
	if err != nil {
		t.Fatalf("connecting to the test database %s: %v", name, err)
	}
	db = db.Unsafe()

	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(name) + " WITH (FORCE)"); err != nil {
			t.Logf("dropping the test database %s: %v", name, err)
		}
		admin.Close()
	})

	schema, err := os.ReadFile(filepath.Join("..", "schema.sql"))
	if err != nil {
		t.Fatalf("reading schema.sql: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("installing the fresh-install schema: %v", err)
	}

	return db
}

// adminTestDSN returns the DSN of the PostgreSQL server the database-backed admin
// tests run against.
func adminTestDSN(t *testing.T) string {
	t.Helper()

	for _, name := range []string{"ADMIN_AUTH_TEST_DSN", "MIGRATION_TEST_DSN"} {
		if dsn := os.Getenv(name); dsn != "" {
			return dsn
		}
	}

	t.Skip("ADMIN_AUTH_TEST_DSN (or MIGRATION_TEST_DSN) is not set")
	return ""
}

// dsnWithDatabase points a DSN at another database, supporting both the URL and
// the key/value form.
func dsnWithDatabase(t *testing.T, dsn, dbName string) string {
	t.Helper()

	if u, err := url.Parse(dsn); err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		u.Path = "/" + dbName
		return u.String()
	}

	re := regexp.MustCompile(`(?i)\b(?:dbname|database)\s*=\s*(?:'[^']*'|\S+)`)
	if re.MatchString(dsn) {
		return re.ReplaceAllString(dsn, "dbname="+dbName)
	}

	return strings.TrimSpace(dsn) + " dbname=" + dbName
}

// countRows returns the value of a scalar count query.
func countRows(t *testing.T, db *sqlx.DB, query string, args ...any) int {
	t.Helper()

	var n int
	if err := db.Get(&n, query, args...); err != nil {
		t.Fatalf("counting rows (%s): %v", query, err)
	}

	return n
}

// setupAttempt is one concurrent submission of the first-time setup form.
type setupAttempt struct {
	rec *httptest.ResponseRecorder
	rr  *recordingRenderer
	err error
}

// submitConcurrently posts the setup form from n concurrent requests that all
// start at the same moment.
func submitConcurrently(t *testing.T, a *App, n int, credentials func(i int) (string, string, string)) []setupAttempt {
	t.Helper()

	var (
		start    = make(chan struct{})
		wg       sync.WaitGroup
		attempts = make([]setupAttempt, n)
	)
	for i := 0; i < n; i++ {
		email, username, password := credentials(i)
		c, rec, rr := setupRequest(t, email, username, password)
		attempts[i].rec, attempts[i].rr = rec, rr

		wg.Add(1)
		go func(i int, c echo.Context) {
			defer wg.Done()
			<-start
			attempts[i].err = a.LoginSetupPage(c)
		}(i, c)
	}
	close(start)
	wg.Wait()

	return attempts
}

// assertSingleBootstrap checks the invariant the advisory lock exists for: at most
// one account, one super admin role and one session, no matter how many setup
// submissions raced. It is asserted before the per-attempt assertions because a
// second super admin is the impact that matters.
func assertSingleBootstrap(t *testing.T, a *App, wantSessions int) {
	t.Helper()

	if got := countRows(t, a.db, `SELECT COUNT(*) FROM users`); got != 1 {
		t.Fatalf("expected exactly one bootstrapped user, got %d", got)
	}
	if got := countRows(t, a.db, `SELECT COUNT(*) FROM users WHERE user_role_id = $1`, auth.SuperAdminRoleID); got != 1 {
		t.Fatalf("expected exactly one super admin user, got %d", got)
	}
	if got := countRows(t, a.db, `SELECT COUNT(*) FROM roles`); got != 1 {
		t.Fatalf("expected exactly one role, got %d", got)
	}
	if got := countRows(t, a.db, `SELECT COUNT(*) FROM sessions`); got != wantSessions {
		t.Fatalf("expected %d session(s), got %d", wantSessions, got)
	}
}

// TestFirstTimeSetupBootstrapsExactlyOnce submits the first-time setup form from
// concurrent requests on a fresh database, which is the race that used to create a
// second super admin with role_id=1 and its own session. The check-and-create is
// serialized with a PostgreSQL advisory lock inside the creating transaction, so
// exactly one caller may bootstrap; the others must fall back to the regular login
// path instead of creating an account.
func TestFirstTimeSetupBootstrapsExactlyOnce(t *testing.T) {
	a := newAdminAuthTestApp(t, nil)

	attempts := submitConcurrently(t, a, 8, func(i int) (string, string, string) {
		return fmt.Sprintf("admin%d@example.com", i), fmt.Sprintf("admin%d", i), "password123"
	})

	assertSingleBootstrap(t, a, 1)

	for i := range attempts {
		if attempts[i].err != nil {
			t.Fatalf("setup attempt %d returned an error: %v", i, attempts[i].err)
		}
	}

	// The winner is logged in and redirected; the losers get the regular login
	// page (their credentials do not match the account that won the race).
	var winners int
	for i := range attempts {
		if attempts[i].rec.Code == http.StatusFound {
			winners++
			if loc := attempts[i].rec.Header().Get("Location"); !strings.Contains(loc, uriWorkspaceSelect) {
				t.Fatalf("attempt %d redirected to %q", i, loc)
			}
			continue
		}

		if attempts[i].rr.last() != "admin-login" {
			t.Fatalf("attempt %d rendered %q instead of the login page", i, attempts[i].rr.last())
		}
		if attempts[i].rr.errorOf() == "" {
			t.Fatalf("attempt %d rendered the login page without an error", i)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one successful setup, got %d", winners)
	}

	// The app must no longer offer the setup page.
	if a.getNeedsUserSetup() {
		t.Fatal("expected the first-time setup flag to be cleared")
	}
}

// TestFirstTimeSetupWithExistingRoleBootstrapsExactlyOnce repeats the concurrency
// test in the state a bootstrap passes through after its first statement: the
// Super Admin role is already committed but no user exists yet. That state is what
// a competing request observes when it reads the role a moment after the other
// request created it, and it also happens on a database where the role survived an
// interrupted setup. Nothing but the advisory lock can serialize the requests
// there: the roles table's unique index on (type, name) is no longer in the way,
// and the user rows differ, so two callers would each end up with a super admin.
func TestFirstTimeSetupWithExistingRoleBootstrapsExactlyOnce(t *testing.T) {
	a := newAdminAuthTestApp(t, nil)
	seedSuperAdminRole(t, a)

	attempts := submitConcurrently(t, a, 8, func(i int) (string, string, string) {
		return fmt.Sprintf("admin%d@example.com", i), fmt.Sprintf("admin%d", i), "password123"
	})

	assertSingleBootstrap(t, a, 1)

	var winners int
	for i := range attempts {
		if attempts[i].err != nil {
			t.Fatalf("setup attempt %d returned an error: %v", i, attempts[i].err)
		}
		if attempts[i].rec.Code == http.StatusFound {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one successful setup, got %d", winners)
	}
}

// TestFirstTimeSetupConcurrentSameCredentials is the double-submit case: the same
// person submitting the setup form twice must end up with a single account and
// either a redirect or a regular login, never with two super admins.
func TestFirstTimeSetupConcurrentSameCredentials(t *testing.T) {
	a := newAdminAuthTestApp(t, nil)
	seedSuperAdminRole(t, a)

	attempts := submitConcurrently(t, a, 4, func(int) (string, string, string) {
		return "admin@example.com", "admin", "password123"
	})

	assertSingleBootstrap(t, a, len(attempts))

	for i := range attempts {
		if attempts[i].err != nil {
			t.Fatalf("setup attempt %d returned an error: %v", i, attempts[i].err)
		}
		// Every submission carries the credentials of the account that was
		// created, so each caller ends up logged in.
		if attempts[i].rec.Code != http.StatusFound {
			t.Fatalf("setup attempt %d was not redirected (status %d)", i, attempts[i].rec.Code)
		}
	}
}

// seedSuperAdminRole creates the Super Admin role, the state a bootstrap leaves
// behind between creating the role and the user.
func seedSuperAdminRole(t *testing.T, a *App) {
	t.Helper()

	if _, err := a.db.Exec(`INSERT INTO roles (type, name, permissions) VALUES ('user', 'Super Admin', '{}')`); err != nil {
		t.Fatalf("seeding the Super Admin role: %v", err)
	}
}

// TestOIDCLoginRequiresVerifiedEmail drives the OIDC callback against a fake
// provider for the cases that matter for account binding: a verified assertion, an
// explicit `email_verified: false`, an absent claim, and the same three through the
// userinfo fallback, both for an address that already belongs to a user and for an
// unknown address (auto-creation).
func TestOIDCLoginRequiresVerifiedEmail(t *testing.T) {
	verified := func(email string) map[string]any {
		return map[string]any{"email": email, "email_verified": true}
	}
	unverified := func(email string) map[string]any {
		return map[string]any{"email": email, "email_verified": false}
	}
	unasserted := func(email string) map[string]any {
		return map[string]any{"email": email}
	}

	for _, tc := range []struct {
		name            string
		idClaims        map[string]any
		userInfoClaims  map[string]any
		existingEmail   string
		wantAutoCreate  bool
		wantLogin       bool
		wantUserInfoHit int
	}{
		{
			name:          "verified claim for an existing account",
			idClaims:      verified("victim@example.com"),
			existingEmail: "victim@example.com",
			wantLogin:     true,
		},
		{
			name:          "unverified claim for an existing account",
			idClaims:      unverified("victim@example.com"),
			existingEmail: "victim@example.com",
		},
		{
			name:          "absent email_verified claim for an existing account",
			idClaims:      unasserted("victim@example.com"),
			existingEmail: "victim@example.com",
		},
		{
			name:           "verified claim auto-creates a user",
			idClaims:       verified("new@example.com"),
			wantLogin:      true,
			wantAutoCreate: true,
		},
		{
			name:     "unverified claim does not auto-create a user",
			idClaims: unverified("new@example.com"),
		},
		{
			name:            "verified claim through the userinfo fallback",
			idClaims:        map[string]any{},
			userInfoClaims:  verified("fallback@example.com"),
			wantLogin:       true,
			wantAutoCreate:  true,
			wantUserInfoHit: 1,
		},
		{
			name:            "unverified claim through the userinfo fallback",
			idClaims:        map[string]any{},
			userInfoClaims:  unverified("fallback@example.com"),
			wantUserInfoHit: 1,
		},
		{
			name:            "absent email_verified through the userinfo fallback",
			idClaims:        map[string]any{},
			userInfoClaims:  unasserted("fallback@example.com"),
			wantUserInfoHit: 1,
		},
		{
			name:            "verified userinfo fallback for an existing account",
			idClaims:        map[string]any{},
			userInfoClaims:  verified("victim@example.com"),
			existingEmail:   "victim@example.com",
			wantLogin:       true,
			wantUserInfoHit: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idp := oidctest.New(t)
			a := newAdminAuthTestApp(t, idp)

			// Baseline: a deployment that has been set up, plus the account the
			// claim collides with, when the case has one.
			seedAdmin(t, a, "owner", "owner@example.com")
			if tc.existingEmail != "" {
				seedUser(t, a, "existing-admin", tc.existingEmail)
			}
			idp.IDTokenClaims = tc.idClaims
			idp.UserInfoClaims = tc.userInfoClaims

			usersBefore := countRows(t, a.db, `SELECT COUNT(*) FROM users`)

			c, rec, rr := oidcRequest(t, idp.Nonce)
			if err := a.OIDCFinish(c); err != nil {
				t.Fatalf("OIDC callback returned an error: %v", err)
			}

			if got := idp.UserInfoCalls(); got != tc.wantUserInfoHit {
				t.Fatalf("expected %d userinfo call(s), got %d", tc.wantUserInfoHit, got)
			}

			wantUsers := usersBefore
			if tc.wantAutoCreate {
				wantUsers++
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM users`); got != wantUsers {
				t.Fatalf("expected %d user(s) after the login, got %d", wantUsers, got)
			}

			if !tc.wantLogin {
				// The login must be refused before any account is touched.
				if rec.Code != http.StatusOK {
					t.Fatalf("expected the login page (200), got %d", rec.Code)
				}
				if rr.last() != "admin-login" {
					t.Fatalf("expected the login page to be rendered, got %q", rr.last())
				}
				if rr.errorOf() == "" {
					t.Fatal("expected an error message on the login page")
				}
				if got := countRows(t, a.db, `SELECT COUNT(*) FROM sessions`); got != 0 {
					t.Fatalf("expected no session, got %d", got)
				}
				if tc.existingEmail != "" {
					if got := countRows(t, a.db, `SELECT COUNT(*) FROM users WHERE email = $1 AND loggedin_at IS NULL`, tc.existingEmail); got != 1 {
						t.Fatalf("expected the existing account %q to be untouched", tc.existingEmail)
					}
				}
				return
			}

			// A verified address logs the caller in.
			if rec.Code != http.StatusFound {
				t.Fatalf("expected a redirect after login, got %d (%s)", rec.Code, rr.errorOf())
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, uriWorkspaceSelect) {
				t.Fatalf("expected a redirect to the workspace picker, got %q", loc)
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM sessions`); got != 1 {
				t.Fatalf("expected one session, got %d", got)
			}

			// The login must have landed on the account for the verified address,
			// existing or freshly created.
			loginEmail, _ := tc.idClaims["email"].(string)
			if loginEmail == "" {
				loginEmail, _ = tc.userInfoClaims["email"].(string)
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM users WHERE email = $1 AND loggedin_at IS NOT NULL`, loginEmail); got != 1 {
				t.Fatalf("expected the account for %q to have been logged in", loginEmail)
			}
		})
	}
}
