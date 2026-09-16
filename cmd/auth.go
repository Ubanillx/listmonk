package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/internal/tmptokens"
	"github.com/knadh/listmonk/internal/utils"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/pquerna/otp/totp"
	"github.com/zerodha/simplesessions/v3"
	"gopkg.in/volatiletech/null.v6"
)

const (
	passwordResetTTL = 30 * time.Minute
	twofaTokenTTL    = 5 * time.Minute

	// Length of reset and 2FA auth tokens.
	tmpAuthTokenLen = 64
)

type loginTpl struct {
	Title       string
	Description string

	NextURI          string
	Nonce            string
	PasswordEnabled  bool
	OIDCProvider     string
	OIDCProviderLogo string
	Error            string
}

type oidcState struct {
	Nonce string `json:"nonce"`
	Next  string `json:"next"`
}

type forgotPasswordTpl struct {
	Title       string
	Description string
	Error       string
}

type resetPasswordTpl struct {
	Title       string
	Description string
	Token       string
	Email       string
	Error       string
}

type twofaTpl struct {
	Title       string
	Description string
	Token       string
	NextURI     string
	Error       string
}

var (
	oidcProviders = map[string]struct{}{
		"google.com":          {},
		"microsoftonline.com": {},
		"auth0.com":           {},
		"github.com":          {},
	}
)

// setNeedsUserSetup sets the in-memory first-time setup hint. It is only an
// optimization: the authoritative check-and-create is serialized in the database
// (see core.FirstTimeSetup), so the flag can never allow a second bootstrap.
func (a *App) setNeedsUserSetup(v bool) {
	a.Lock()
	a.needsUserSetup = v
	a.Unlock()
}

// getNeedsUserSetup reports whether the app still believes the first-time setup
// has to be run.
func (a *App) getNeedsUserSetup() bool {
	a.Lock()
	defer a.Unlock()
	return a.needsUserSetup
}

// LoginPage renders the login page and handles the login form.
func (a *App) LoginPage(c echo.Context) error {
	// Has the user been setup?
	if a.getNeedsUserSetup() {
		return a.LoginSetupPage(c)
	}

	// Process POST login request.
	var loginErr error
	if c.Request().Method == http.MethodPost {
		loginErr = a.doLogin(c)
		if loginErr == nil {
			return c.Redirect(http.StatusFound, a.workspaceSelectURI(c.FormValue("next")))
		}
		setAuditAction(c, "auth.login_failed")
		setAuditOutcome(c, "failed", "login_failed")
	}

	// Render the page, with or without POST.
	return a.renderLoginPage(c, loginErr)
}

// LoginSetupPage renders the first time user login page and handles the login form.
func (a *App) LoginSetupPage(c echo.Context) error {
	// Process POST login request.
	var loginErr error
	if c.Request().Method == http.MethodPost {
		loginErr = a.doFirstTimeSetup(c)
		if loginErr == nil {
			a.setNeedsUserSetup(false)
			setAuditAction(c, "auth.bootstrap_completed")
			return c.Redirect(http.StatusFound, a.workspaceSelectURI(c.FormValue("next")))
		}
		setAuditAction(c, "auth.bootstrap_failed")
		setAuditOutcome(c, "failed", "bootstrap_failed")

		// Another request bootstrapped the first user while this one was in
		// flight. Creating a second super admin would be a privilege
		// escalation, so fall back to exactly what a POST to /admin/login does
		// on a deployment that is already set up: a normal login with the
		// submitted credentials. Two tabs submitting the same credentials end up
		// logged in; anything else gets the regular login error.
		if errors.Is(loginErr, core.ErrFirstTimeSetupDone) {
			a.setNeedsUserSetup(false)

			loginErr = a.doLogin(c)
			if loginErr == nil {
				setAuditAction(c, "auth.login_succeeded")
				return c.Redirect(http.StatusFound, a.workspaceSelectURI(c.FormValue("next")))
			}

			return a.renderLoginPage(c, loginErr)
		}
	}

	// Render the page, with or without POST.
	return a.renderLoginSetupPage(c, loginErr)
}

// TwofaPage renders the 2FA verification page and handles the 2FA form submission.
func (a *App) TwofaPage(c echo.Context) error {
	var token, next string

	if c.Request().Method == http.MethodPost {
		setAuditAction(c, "auth.two_factor_failed")
		token = strings.TrimSpace(c.FormValue("token"))
		next = utils.SanitizeURI(c.FormValue("next"))
	} else {
		token = strings.TrimSpace(c.QueryParam("token"))
		next = utils.SanitizeURI(c.QueryParam("next"))
	}

	// If there's no token, redirect.
	if len(token) < tmpAuthTokenLen {
		if c.Request().Method == http.MethodPost {
			setAuditOutcome(c, "failed", "invalid_two_factor_token")
		}
		return c.Redirect(http.StatusFound, uriAdmin)
	}

	if next == "" || next == "/" {
		next = uriAdmin
	}

	// Validate the 2FA temp token.
	data, err := tmptokens.Check(token)
	if err != nil {
		if c.Request().Method == http.MethodPost {
			setAuditOutcome(c, "failed", "invalid_two_factor_token")
		}
		return c.Redirect(http.StatusFound, uriAdmin)
	}

	userID, ok := data.(int)
	if !ok {
		if c.Request().Method == http.MethodPost {
			setAuditOutcome(c, "failed", "invalid_two_factor_token")
		}
		return a.renderTwofaPage(c, token, next, a.i18n.T("users.invalidRequest"))
	}

	// Process the 2FA verification POST request.
	if c.Request().Method == http.MethodPost {
		return a.doTwofaVerify(c, token, userID, next)
	}

	// Render the 2FA verification page.
	return a.renderTwofaPage(c, token, next, "")
}

// Logout logs a user out.
func (a *App) Logout(c echo.Context) error {
	// Delete the session from the DB and cookie. Bearer and BasicAuth requests
	// never carry a cookie session, so the lookup must not assume one.
	if sess, ok := c.Get(auth.SessionKey).(*simplesessions.Session); ok {
		_ = sess.Destroy()
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// OIDCLogin initializes an OIDC request and redirects to the OIDC provider for login.
func (a *App) OIDCLogin(c echo.Context) error {
	setAuditAction(c, "auth.oidc_started")
	// Verify that the request came from the login page (CSRF).
	nonce, err := c.Cookie("nonce")
	if err != nil || nonce.Value == "" || nonce.Value != c.FormValue("nonce") {
		return echo.NewHTTPError(http.StatusUnauthorized, a.i18n.T("users.invalidRequest"))
	}

	// Sanitize the URL and make it relative.
	next := utils.SanitizeURI(c.FormValue("next"))
	if next == "/" {
		next = uriAdmin
	}

	// Preparethe OIDC payload to send to the provider.
	state := oidcState{Nonce: nonce.Value, Next: next}

	b, err := json.Marshal(state)
	if err != nil {
		a.log.Printf("error marshalling OIDC state: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	// Redirect to the external OIDC provider.
	return c.Redirect(http.StatusFound, a.auth.GetOIDCAuthURL(base64.URLEncoding.EncodeToString(b), nonce.Value))
}

// OIDCFinish receives the redirect callback from the OIDC provider and completes the handshake.
func (a *App) OIDCFinish(c echo.Context) error {
	setAuditAction(c, "auth.oidc_login_failed")
	setAuditOutcome(c, "failed", "oidc_login_failed")
	// Verify that the request actually originated from the login request (which sets the nonce value).
	nonce, err := c.Cookie("nonce")
	if err != nil || nonce.Value == "" {
		return a.renderLoginPage(c, echo.NewHTTPError(http.StatusUnauthorized, a.i18n.T("users.invalidRequest")))
	}

	// Validate the OIDC token.
	oidcToken, claims, err := a.auth.ExchangeOIDCToken(c.Request().URL.Query().Get("code"), nonce.Value)
	if err != nil {
		return a.renderLoginPage(c, err)
	}

	// Validate the state.
	var state oidcState
	stateB, err := base64.URLEncoding.DecodeString(c.QueryParam("state"))
	if err != nil {
		a.log.Printf("error decoding OIDC state: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}
	if err := json.Unmarshal(stateB, &state); err != nil {
		a.log.Printf("error unmarshalling OIDC state: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}
	if state.Nonce != nonce.Value {
		return a.renderLoginPage(c, echo.NewHTTPError(http.StatusUnauthorized, a.i18n.T("users.invalidRequest")))
	}

	// Validate the e-mail identity from the claims. The address has to be
	// present, parseable and asserted as verified by the provider
	// (`email_verified: true`); a provider that hands out unverified or mutable
	// e-mail claims must not be able to log in as (or auto-create an account for)
	// an address that belongs to somebody else. The claims are checked again
	// here, where login and auto-creation happen, in addition to the check
	// ExchangeOIDCToken performs on both the ID token and the userinfo fallback.
	email, err := auth.VerifyOIDCEmailClaims(claims)
	if err != nil {
		if errors.Is(err, auth.ErrOIDCEmailMissing) {
			err = errors.New(a.i18n.Ts("globals.messages.invalidFields", "name", "email"))
		}
		return a.renderLoginPage(c, err)
	}
	claims.Email = email

	// Get the user by e-mail received from OIDC.
	user, userErr := a.core.GetUser(0, "", email)
	if userErr != nil {
		// If the user doesn't exist, and auto-creation is enabled, create a new user.
		if httpErr, ok := userErr.(*echo.HTTPError); ok && httpErr.Code == http.StatusNotFound && a.cfg.Security.OIDC.AutoCreateUsers {
			u, err := a.createOIDCUser(claims, c)
			if err != nil {
				return a.renderLoginPage(c, err)
			}
			user = u
			userErr = nil
		} else {
			return a.renderLoginPage(c, userErr)
		}
	}

	// A disabled account must not be able to log in through the provider either.
	// The password login path enforces this in SQL, but the OIDC path resolves
	// the account by e-mail and would otherwise hand a session to a disabled
	// user.
	if user.Status != auth.UserStatusEnabled {
		setAuditOutcome(c, "failed", "account_disabled")
		return a.renderLoginPage(c, echo.NewHTTPError(http.StatusForbidden, a.i18n.T("users.accountDisabled")))
	}

	// Update the user login state (avatar, logged in date) in the DB.
	if err := a.core.UpdateUserLogin(user.ID, claims.Picture); err != nil {
		return a.renderLoginPage(c, err)
	}

	// Set the session in the DB and cookie.
	if err := a.auth.SaveSession(user, oidcToken, c); err != nil {
		return a.renderLoginPage(c, err)
	}
	setAuditAction(c, "auth.oidc_login_succeeded")
	setAuditOutcome(c, "success", "")

	// Redirect to the workspace selection page.
	return c.Redirect(http.StatusFound, a.workspaceSelectURI(state.Next))
}

// ForgotPage renders the forgot password page and handles the forgot password form.
func (a *App) ForgotPage(c echo.Context) error {
	// Process the forgot password request.
	if c.Request().Method == http.MethodPost {
		return a.doForgotPassword(c)
	}

	// Render the forgot page.
	out := forgotPasswordTpl{Title: a.i18n.T("users.forgotPassword")}
	return c.Render(http.StatusOK, "admin-forgot-password", out)
}

// ResetPage renders the reset password page and handles the reset password form.
func (a *App) ResetPage(c echo.Context) error {
	var (
		token = strings.TrimSpace(c.QueryParam("token"))
		email = strings.ToLower(strings.TrimSpace(c.QueryParam("email")))
	)

	// Validate token and email (don't delete it yet, as we may need it for POST).
	data, err := tmptokens.Check(email)
	if err != nil {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.invalidResetLink")))
	}

	tk, ok := data.(string)
	if !ok || tk != token {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.invalidResetLink")))
	}

	// Validate that the user exists.
	_, err = a.core.GetUser(0, "", email)
	if err != nil {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.invalidResetLink")))
	}

	// Process the reset password request form with the new passwords.
	if c.Request().Method == http.MethodPost {
		return a.doResetPassword(c, token, email)
	}

	// Render the reset password form for GET request.
	return a.renderResetPasswordPage(c, token, email, "")
}

// renderLoginPage renders the login page and handles the login form.
func (a *App) renderLoginPage(c echo.Context, loginErr error) error {
	next := utils.SanitizeURI(c.FormValue("next"))
	if next == "/" {
		next = uriAdmin
	}

	var (
		oidcProviderName = ""
		oidcLogo         = ""
	)
	if a.cfg.Security.OIDC.Enabled {
		// Defaults.
		oidcProviderName = a.cfg.Security.OIDC.ProviderName
		oidcLogo = "oidc.png"

		u, err := url.Parse(a.cfg.Security.OIDC.ProviderURL)
		if err == nil {
			h := strings.Split(u.Hostname(), ".")

			// Get the last two h for the root domain
			prov := ""
			if len(h) >= 2 {
				prov = h[len(h)-2] + "." + h[len(h)-1]
			} else {
				prov = u.Hostname()
			}

			if oidcProviderName == "" {
				oidcProviderName = prov
			}

			// Lookup the logo in the known providers map.
			if _, ok := oidcProviders[prov]; ok {
				oidcLogo = prov + ".png"
			}
		}
	}

	out := loginTpl{
		Title:            a.i18n.T("users.login"),
		PasswordEnabled:  true,
		OIDCProvider:     oidcProviderName,
		OIDCProviderLogo: oidcLogo,
		NextURI:          next,
	}

	// If there was an error in the previous state (POST reqest), set it to render in the template.
	if loginErr != nil {
		if e, ok := loginErr.(*echo.HTTPError); ok {
			out.Error = e.Message.(string)
		} else {
			out.Error = loginErr.Error()
		}
	}

	// Generate and set a nonce for preventing CSRF requests that will be valided in the subsequent requests.
	nonce, err := utils.GenerateRandomString(16)
	if err != nil {
		a.log.Printf("error generating OIDC nonce: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.internalError"))
	}
	c.SetCookie(&http.Cookie{
		Name:     "nonce",
		Value:    nonce,
		HttpOnly: true,
		Secure:   a.secureCookies(),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	out.Nonce = nonce

	// Render the login page.
	return c.Render(http.StatusOK, "admin-login", out)
}

// renderLoginSetupPage renders the first time user setup page.
func (a *App) renderLoginSetupPage(c echo.Context, loginErr error) error {
	next := utils.SanitizeURI(c.FormValue("next"))
	if next == "/" {
		next = uriAdmin
	}

	out := loginTpl{
		Title:           a.i18n.T("users.login"),
		PasswordEnabled: true,
		NextURI:         next,
	}

	// If there was an error in the previous state (POST reqest), set it to render in the template.
	if loginErr != nil {
		if e, ok := loginErr.(*echo.HTTPError); ok {
			out.Error = e.Message.(string)
		} else {
			out.Error = loginErr.Error()
		}
	}

	return c.Render(http.StatusOK, "admin-login-setup", out)
}

// createOIDCUser creates a new user in the DB with the OIDC claims.
func (a *App) createOIDCUser(claims auth.OIDCclaim, c echo.Context) (auth.User, error) {
	name := claims.Name
	if name == "" {
		name = strings.TrimSpace(claims.PreferredUsername)
	}
	if name == "" {
		name = strings.Split(claims.Email, "@")[0]
	}

	var customerListRoleID *int
	if a.cfg.Security.OIDC.DefaultListRoleID > 0 {
		customerListRoleID = &a.cfg.Security.OIDC.DefaultListRoleID
	}

	user, err := a.core.CreateUser(auth.User{
		Type:               auth.UserTypeUser,
		HasPassword:        false,
		PasswordLogin:      false,
		Username:           claims.Email,
		Name:               name,
		Email:              null.NewString(claims.Email, true),
		UserRoleID:         a.cfg.Security.OIDC.DefaultUserRoleID,
		CustomerListRoleID: customerListRoleID,
		Status:             auth.UserStatusEnabled,
	})

	return user, err
}

// doLogin logs a user in with a username and password.
func (a *App) doLogin(c echo.Context) error {
	var (
		startTime = time.Now()
		username  = strings.TrimSpace(c.FormValue("username"))
		password  = strings.TrimSpace(c.FormValue("password"))
	)

	// Ensure timing mitigation is applied regardless of early returns
	defer func() {
		if elapsed := time.Since(startTime).Milliseconds(); elapsed < 100 {
			time.Sleep(time.Duration(100-elapsed) * time.Millisecond)
		}
	}()

	if !strHasLen(username, 3, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "username"))
	}
	if !strHasLen(password, 8, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "password"))
	}

	// Refuse further attempts for a client/account pair that keeps failing.
	// Success below clears the counter.
	throttleKey := "login|" + c.RealIP() + "|" + strings.ToLower(username)
	if !a.throttle.Allow(throttleKey) {
		setAuditOutcome(c, "failed", "rate_limited")
		return echo.NewHTTPError(http.StatusTooManyRequests, a.i18n.T("users.tooManyAttempts"))
	}

	// Log the user in by fetching and verifying credentials from the DB.
	user, err := a.core.LoginUser(username, password)
	if err != nil {
		a.throttle.Failure(throttleKey)
		return err
	}
	a.throttle.Success(throttleKey)

	// If TOTP is enabled for the user, create a temp token and redirect to the 2FA page.
	if user.TwofaType == models.TwofaTypeTOTP {
		// Generate a random token.
		token, err := generateRandomString(tmpAuthTokenLen)
		if err != nil {
			a.log.Printf("error generating 2FA token: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
		}

		// Set the token.
		tmptokens.Set(token, twofaTokenTTL, user.ID)

		// Redirect to 2FA page.
		setAuditAction(c, "auth.login_challenge_issued")
		next := utils.SanitizeURI(c.FormValue("next"))
		return c.Redirect(http.StatusFound, fmt.Sprintf("%s/login/twofa?token=%s&next=%s", uriAdmin, token, url.QueryEscape(next)))
	}

	// Set the session in the DB and cookie.
	if err := a.auth.SaveSession(user, "", c); err != nil {
		return err
	}
	setAuditAction(c, "auth.login_succeeded")

	return nil
}

// doFirstTimeSetup sets a user up for the first time.
func (a *App) doFirstTimeSetup(c echo.Context) error {
	var (
		email     = strings.TrimSpace(c.FormValue("email"))
		username  = strings.TrimSpace(c.FormValue("username"))
		password  = strings.TrimSpace(c.FormValue("password"))
		password2 = strings.TrimSpace(c.FormValue("password2"))
	)
	if !utils.ValidateEmail(email) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "email"))
	}
	if !strHasLen(username, 3, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "username"))
	}
	if !strHasLen(password, 8, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "password"))
	}
	if password != password2 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("users.passwordMismatch"))
	}

	// Create the default "Super Admin" role and the user in one transaction that
	// is guarded by a PostgreSQL advisory lock, so two concurrent setup requests
	// on a fresh deployment cannot each bootstrap a super admin. The caller that
	// loses the race gets core.ErrFirstTimeSetupDone.
	permissions := make([]string, 0, len(a.cfg.Permissions))
	for p := range a.cfg.Permissions {
		permissions = append(permissions, p)
	}

	u := auth.User{
		Type:          auth.UserTypeUser,
		HasPassword:   true,
		PasswordLogin: true,
		Username:      username,
		Name:          username,
		Password:      null.NewString(password, true),
		Email:         null.NewString(email, true),
		UserRoleID:    auth.SuperAdminRoleID,
		Status:        auth.UserStatusEnabled,
	}
	if _, err := a.core.FirstTimeSetup(u, permissions); err != nil {
		return err
	}

	// Log the user in directly.
	user, err := a.core.LoginUser(username, password)
	if err != nil {
		return err
	}

	// Set the session in the DB and cookie.
	if err := a.auth.SaveSession(user, "", c); err != nil {
		return err
	}

	return nil
}

// renderResetPasswordPage renders the reset password page.
func (a *App) renderResetPasswordPage(c echo.Context, token, email, errMsg string) error {
	out := resetPasswordTpl{
		Title: a.i18n.T("users.resetPassword"),
		Token: token,
		Email: email,
		Error: errMsg,
	}
	return c.Render(http.StatusOK, "admin-reset-password", out)
}

// doForgotPassword handles the forgot password form submission.
func (a *App) doForgotPassword(c echo.Context) error {
	setAuditMetadata(c, map[string]any{"channel": "password_reset"})
	var (
		email = strings.ToLower(strings.TrimSpace(c.FormValue("email")))
	)

	// Validate email format.
	if !utils.ValidateEmail(email) {
		return c.Render(http.StatusOK, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.resetLinkSent")))
	}

	// Get the user by email.
	user, err := a.core.GetUser(0, "", email)
	if err != nil {
		return c.Render(http.StatusOK, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.resetLinkSent")))
	}

	// If the password login is disabled, do not proceed, but show success message to prevent email enumeration.
	if !user.PasswordLogin {
		return c.Render(http.StatusOK, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.resetLinkSent")))
	}

	// Generate a random token.
	token, err := generateRandomString(tmpAuthTokenLen)
	if err != nil {
		a.log.Printf("error generating reset token: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	// Store the reset token in tmptokens.
	tmptokens.Set(email, passwordResetTTL, token)

	// Prepare the reset URL.
	resetURL := fmt.Sprintf("%s/admin/reset?token=%s&email=%s", a.urlCfg.RootURL, token, url.QueryEscape(email))

	// Prepare the email.
	var msg bytes.Buffer
	data := struct {
		ResetURL string
		L        *i18n.I18n
	}{
		ResetURL: resetURL,
		L:        a.i18n,
	}

	// Render the email template.
	if err := notifs.Tpls.ExecuteTemplate(&msg, notifs.TplForgotPassword, data); err != nil {
		a.log.Printf("error compiling notification template '%s': %v", notifs.TplForgotPassword, err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	subject, body := utils.GetTplSubject(a.i18n.T("email.forgotPassword.subject"), msg.Bytes())

	// Send the email.
	if err := a.emailMsgr.Push(models.Message{
		From:    a.emailMsgr.DefaultFromEmail(),
		To:      []string{email},
		Subject: subject,
		Body:    body,
	}); err != nil {
		a.log.Printf("error sending reset email: %s", err)
		setAuditOutcome(c, "failed", "reset_email_delivery_failed")
	}

	// Show the success e-mail nonetheless to prevent e-mail enumeration.
	return c.Render(http.StatusOK, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.resetLinkSent")))
}

// doResetPassword handles the reset password form submission.
func (a *App) doResetPassword(c echo.Context, token, email string) error {
	setAuditMetadata(c, map[string]any{"channel": "password_reset"})
	var (
		password  = c.FormValue("password")
		password2 = c.FormValue("password2")
	)

	// Validate password.
	if !strHasLen(password, 8, stdInputMaxLen) {
		return a.renderResetPasswordPage(c, token, email, a.i18n.Ts("globals.messages.invalidFields", "name", "password"))
	}
	if password != password2 {
		return a.renderResetPasswordPage(c, token, email, a.i18n.T("users.passwordMismatch"))
	}

	// Validate and consume the token (this deletes it).
	data, err := tmptokens.Get(email)
	if err != nil {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.invalidResetLink")))
	}

	tk, ok := data.(string)
	if !ok || tk != token {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.invalidResetLink")))
	}

	// Get the user.
	user, err := a.core.GetUser(0, "", email)
	if err != nil {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.invalidResetLink")))
	}

	// Password login is disabled for the user.
	if !user.PasswordLogin {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("public.invalidFeature")))
	}

	// A disabled account must not be able to regain access by resetting its
	// password.
	if user.Status != auth.UserStatusEnabled {
		return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("users.resetPassword"), "", a.i18n.T("users.accountDisabled")))
	}

	user.Password = null.NewString(password, true)
	if _, err := a.core.UpdateUserProfile(user.ID, user); err != nil {
		a.log.Printf("error updating user password: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	// A password reset is the recovery path for a compromised account, so every
	// session issued before it must stop working.
	if err := a.auth.DestroyUserSessions(user.ID); err != nil {
		a.log.Printf("error invalidating sessions after password reset: %v", err)
	}

	// Possession of the reset e-mail must not bypass the second factor. When
	// TOTP is enabled, hand the login over to the regular 2FA challenge
	// instead of creating a session.
	if user.TwofaType == models.TwofaTypeTOTP {
		token, err := generateRandomString(tmpAuthTokenLen)
		if err != nil {
			a.log.Printf("error generating 2FA token: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
		}
		tmptokens.Set(token, twofaTokenTTL, user.ID)
		setAuditAction(c, "auth.password_reset_succeeded")

		return c.Redirect(http.StatusFound, fmt.Sprintf("%s/login/twofa?token=%s&next=%s", uriAdmin, token, url.QueryEscape(uriAdmin)))
	}

	// Log the user in directly without forcing a manual login right after password change.
	if err := a.auth.SaveSession(user, "", c); err != nil {
		return err
	}
	setAuditAction(c, "auth.password_reset_succeeded")

	// Redirect to the workspace selection page.
	return c.Redirect(http.StatusFound, a.workspaceSelectURI(uriAdmin))
}

// renderTwofaPage renders the 2FA verification page.
func (a *App) renderTwofaPage(c echo.Context, token, next, errMsg string) error {
	out := twofaTpl{
		Title:       a.i18n.T("users.twoFA"),
		Description: "",
		Token:       token,
		NextURI:     next,
		Error:       errMsg,
	}
	return c.Render(http.StatusOK, "admin-twofa", out)
}

// doTwofaVerify handles the 2FA verification form submission.
func (a *App) doTwofaVerify(c echo.Context, token string, userID int, next string) error {
	totpCode := strings.TrimSpace(c.FormValue("totp_code"))

	// Validate.
	if !strHasLen(totpCode, 6, 6) {
		setAuditOutcome(c, "failed", "invalid_totp")
		return a.renderTwofaPage(c, token, next, a.i18n.T("globals.messages.invalidValue"))
	}

	// Get the user.
	user, err := a.core.GetUser(userID, "", "")
	if err != nil {
		setAuditOutcome(c, "failed", "invalid_two_factor_user")
		return a.renderTwofaPage(c, token, next, a.i18n.T("users.invalidRequest"))
	}

	// Verify that TOTP is actually enabled for the user.
	if user.TwofaType != models.TwofaTypeTOTP {
		setAuditOutcome(c, "failed", "two_factor_not_enabled")
		return a.renderTwofaPage(c, token, next, a.i18n.T("users.twoFANotEnabled"))
	}

	// A disabled account must not complete the challenge either.
	if user.Status != auth.UserStatusEnabled {
		setAuditOutcome(c, "failed", "account_disabled")
		return a.renderTwofaPage(c, token, next, a.i18n.T("users.accountDisabled"))
	}

	// Throttle repeated wrong codes for this account/client pair. Keying on the
	// account rather than the temp token matters because a fresh login mints a
	// new token and would otherwise reset a per-token counter.
	throttleKey := "twofa|" + c.RealIP() + "|" + strconv.Itoa(user.ID)
	if !a.throttle.Allow(throttleKey) {
		setAuditOutcome(c, "failed", "rate_limited")
		return a.renderTwofaPage(c, token, next, a.i18n.T("users.tooManyAttempts"))
	}

	// Verify the TOTP code.
	valid := totp.Validate(totpCode, user.TwofaKey.String)
	if !valid {
		a.throttle.Failure(throttleKey)
		setAuditOutcome(c, "failed", "invalid_totp")
		return a.renderTwofaPage(c, token, next, a.i18n.T("globals.messages.invalidValue"))
	}
	a.throttle.Success(throttleKey)

	// Invalidate the token.
	tmptokens.Delete(token)

	// Set the session.
	if err := a.auth.SaveSession(user, "", c); err != nil {
		return err
	}
	setAuditAction(c, "auth.two_factor_verified")

	// Redirect to the next page.
	return c.Redirect(http.StatusFound, a.workspaceSelectURI(next))
}

// GenerateTOTPQR generates a TOTP QR code for a user to scan with their authenticator app.
func (a *App) GenerateTOTPQR(c echo.Context) error {
	u := c.Get(auth.UserHTTPCtxKey).(auth.User)

	// If TOTP is already enabled, don't generate a new key.
	if u.TwofaType == models.TwofaTypeTOTP {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("users.twoFAAlreadyEnabled"))
	}

	// Generate a new TOTP key.
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      a.cfg.SiteName,
		AccountName: u.Email.String,
	})
	if err != nil {
		a.log.Printf("error generating TOTP key: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	// Convert the TOTP key to a QR code image.
	img, err := key.Image(200, 200)
	if err != nil {
		a.log.Printf("error generating QR code: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	// Encode the QR code as a PNG and return it as base64.
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		a.log.Printf("error encoding QR code: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
	}

	return c.JSON(http.StatusOK, okResp{struct {
		Secret string `json:"secret"`
		QR     string `json:"qr"`
	}{
		Secret: key.Secret(),
		QR:     base64.StdEncoding.EncodeToString(buf.Bytes()),
	}})
}
