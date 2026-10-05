package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gdgvda/cron"
	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx/types"
	koanfjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/internal/replyai"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

const pwdMask = "•"

type aboutHost struct {
	OS       string `json:"os"`
	Machine  string `json:"arch"`
	Hostname string `json:"hostname"`
}

type aboutSystem struct {
	NumCPU  int    `json:"num_cpu"`
	AllocMB uint64 `json:"memory_alloc_mb"`
	OSMB    uint64 `json:"memory_from_os_mb"`
}

type about struct {
	Version   string         `json:"version"`
	Build     string         `json:"build"`
	GoVersion string         `json:"go_version"`
	GoArch    string         `json:"go_arch"`
	Database  types.JSONText `json:"database"`
	System    aboutSystem    `json:"system"`
	Host      aboutHost      `json:"host"`
}

var (
	reAlphaNum = regexp.MustCompile(`[^a-z0-9\-]`)
)

// GetSettings returns settings from the DB.
func (a *App) GetSettings(c echo.Context) error {
	s, err := a.core.GetSettings()
	if err != nil {
		return err
	}
	if s.SMTPDelivery.MaxConns == 0 {
		s.SMTPDelivery = smtpDeliveryFromLegacy(s.SMTP)
	}
	// Older settings may still contain several platform SMTP rows. Only the
	// system notification sender is exposed; the next save removes the rest.
	for _, server := range s.SMTP {
		if server.Enabled && server.IsPrimary {
			s.SMTP = []models.SMTPServer{server}
			break
		}
	}

	// Empty out passwords.
	for i := range s.SMTP {
		s.SMTP[i].Password = strings.Repeat(pwdMask, utf8.RuneCountInString(s.SMTP[i].Password))
	}
	for i := range s.BounceBoxes {
		s.BounceBoxes[i].Password = strings.Repeat(pwdMask, utf8.RuneCountInString(s.BounceBoxes[i].Password))
	}
	for i := range s.Messengers {
		s.Messengers[i].Password = strings.Repeat(pwdMask, utf8.RuneCountInString(s.Messengers[i].Password))
	}

	s.UploadS3AwsSecretAccessKey = strings.Repeat(pwdMask, utf8.RuneCountInString(s.UploadS3AwsSecretAccessKey))
	s.SendgridKey = strings.Repeat(pwdMask, utf8.RuneCountInString(s.SendgridKey))
	s.BouncePostmark.Password = strings.Repeat(pwdMask, utf8.RuneCountInString(s.BouncePostmark.Password))
	s.BounceForwardEmail.Key = strings.Repeat(pwdMask, utf8.RuneCountInString(s.BounceForwardEmail.Key))
	s.ReplyAI.APIKey = strings.Repeat(pwdMask, utf8.RuneCountInString(s.ReplyAI.APIKey))
	s.SecurityCaptcha.HCaptcha.Secret = strings.Repeat(pwdMask, utf8.RuneCountInString(s.SecurityCaptcha.HCaptcha.Secret))
	s.OIDC.ClientSecret = strings.Repeat(pwdMask, utf8.RuneCountInString(s.OIDC.ClientSecret))

	return c.JSON(http.StatusOK, okResp{s})
}

// UpdateSettings returns settings from the DB.
func (a *App) UpdateSettings(c echo.Context) error {
	// Unmarshal and marshal the fields once to sanitize the settings blob.
	var set models.Settings
	if err := c.Bind(&set); err != nil {
		return err
	}

	// Get the existing settings.
	cur, err := a.core.GetSettings()
	if err != nil {
		return err
	}
	// Customer field definitions have their own administrator-only endpoint.
	// Do not let a broad settings payload overwrite them accidentally.
	set.CustomFields = cur.CustomFields

	// One platform SMTP sends system notifications. Account SMTP pools handle
	// campaigns and transactional messages.
	names := map[string]bool{emailMsgr: true}
	if len(set.SMTP) != 1 || !set.SMTP[0].Enabled {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.errorNoSMTP"))
	}
	set.SMTP[0].IsPrimary = true
	if set.SMTPDelivery.MaxConns == 0 {
		set.SMTPDelivery = cur.SMTPDelivery
		if set.SMTPDelivery.MaxConns == 0 {
			set.SMTPDelivery = smtpDeliveryFromLegacy(cur.SMTP)
		}
	}
	delivery := set.SMTPDelivery
	if _, _, err := delivery.SendDelayRange(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if delivery.MaxConns < 1 || delivery.MaxMsgRetries < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP delivery settings")
	}
	if idle, err := time.ParseDuration(delivery.IdleTimeout); err != nil || idle <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP idle timeout")
	}
	if wait, err := time.ParseDuration(delivery.WaitTimeout); err != nil || wait <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP wait timeout")
	}
	set.SMTP[0].MaxConns = delivery.MaxConns
	set.SMTP[0].MaxMsgRetries = delivery.MaxMsgRetries
	set.SMTP[0].IdleTimeout = delivery.IdleTimeout
	set.SMTP[0].WaitTimeout = delivery.WaitTimeout
	set.SMTP[0].EmailHeaders = delivery.EmailHeaders

	for i, s := range set.SMTP {
		if s.TLSType != "none" && s.TLSType != "TLS" && s.TLSType != "STARTTLS" {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP TLS type")
		}
		set.SMTP[i].FromEmail = strings.TrimSpace(s.FromEmail)

		// Sanitize and normalize the SMTP server name.
		name := reAlphaNum.ReplaceAllString(strings.ToLower(strings.TrimSpace(s.Name)), "-")
		if name != "" {
			if !strings.HasPrefix(name, "email-") {
				name = "email-" + name
			}

			if _, ok := names[name]; ok {
				return echo.NewHTTPError(http.StatusBadRequest,
					a.i18n.Ts("settings.duplicateMessengerName", "name", name))
			}

			names[name] = true
		}
		set.SMTP[i].Name = name

		// Assign a UUID. The frontend only sends a password when the user explicitly
		// changes the password. In other cases, the existing password in the DB
		// is copied while updating the settings and the UUID is used to match
		// the incoming array of SMTP blocks with the array in the DB.
		if s.UUID == "" {
			set.SMTP[i].UUID = uuid.Must(uuid.NewV4()).String()
		}

		// Ensure the HOST is trimmed of any whitespace.
		// This is a common mistake when copy-pasting SMTP settings.
		set.SMTP[i].Host = strings.TrimSpace(s.Host)
		if s.DailyLimit < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.errorSMTPDailyLimit"))
		}

		if s.Enabled {
			if set.SMTP[i].FromEmail == "" {
				return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.errorSMTPFromEmail"))
			}
			if !reFromAddress.Match([]byte(set.SMTP[i].FromEmail)) {
				em, err := a.importer.SanitizeEmail(set.SMTP[i].FromEmail)
				if err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.errorSMTPFromEmail"))
				}
				set.SMTP[i].FromEmail = em
			}
		}

		// If there's no password coming in from the frontend, copy the existing
		// password by matching the UUID.
		if s.Password == "" {
			for _, c := range cur.SMTP {
				if s.UUID == c.UUID {
					set.SMTP[i].Password = c.Password
				}
			}
		}
	}

	// Always remove the trailing slash from the app root URL.
	set.AppRootURL = strings.TrimRight(set.AppRootURL, "/")

	// Bounce boxes.
	for i, s := range set.BounceBoxes {
		if s.StartTLS && s.TLSEnabled {
			return echo.NewHTTPError(http.StatusBadRequest, "Choose either SSL/TLS or STARTTLS for the bounce mailbox")
		}
		// Assign a UUID. The frontend only sends a password when the user explicitly
		// changes the password. In other cases, the existing password in the DB
		// is copied while updating the settings and the UUID is used to match
		// the incoming array of blocks with the array in the DB.
		if s.UUID == "" {
			set.BounceBoxes[i].UUID = uuid.Must(uuid.NewV4()).String()
		}

		// Ensure the HOST is trimmed of any whitespace.
		// This is a common mistake when copy-pasting SMTP settings.
		set.BounceBoxes[i].Host = strings.TrimSpace(s.Host)

		if d, _ := time.ParseDuration(s.ScanInterval); d.Minutes() < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.bounces.invalidScanInterval"))
		}

		// If there's no password coming in from the frontend, copy the existing
		// password by matching the UUID.
		if s.Password == "" {
			for _, c := range cur.BounceBoxes {
				if s.UUID == c.UUID {
					set.BounceBoxes[i].Password = c.Password
				}
			}
		}
	}

	// Reply-AI credentials follow the same blank-means-unchanged convention as
	// other settings secrets. The endpoint is validated only when automation is
	// enabled, so administrators can save a disabled draft safely.
	if set.ReplyAI.APIKey == "" || strings.Trim(set.ReplyAI.APIKey, pwdMask) == "" {
		set.ReplyAI.APIKey = cur.ReplyAI.APIKey
	}
	set.ReplyAI.BaseURL = strings.TrimSpace(set.ReplyAI.BaseURL)
	set.ReplyAI.Model = strings.TrimSpace(set.ReplyAI.Model)
	if set.ReplyAI.Timeout == "" {
		set.ReplyAI.Timeout = "15s"
	}
	if set.ReplyAI.MinConfidence == 0 {
		set.ReplyAI.MinConfidence = 0.98
	}
	if set.ReplyAI.Enabled {
		if _, err := replyai.New(replyai.Options{
			Enabled:       set.ReplyAI.Enabled,
			BaseURL:       set.ReplyAI.BaseURL,
			APIKey:        set.ReplyAI.APIKey,
			Model:         set.ReplyAI.Model,
			Timeout:       set.ReplyAI.Timeout,
			MinConfidence: set.ReplyAI.MinConfidence,
		}); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	for i, m := range set.Messengers {
		// UUID to keep track of password changes similar to the SMTP logic above.
		if m.UUID == "" {
			set.Messengers[i].UUID = uuid.Must(uuid.NewV4()).String()
		}

		if m.Password == "" {
			for _, c := range cur.Messengers {
				if m.UUID == c.UUID {
					set.Messengers[i].Password = c.Password
				}
			}
		}

		name := reAlphaNum.ReplaceAllString(strings.ToLower(m.Name), "")
		if _, ok := names[name]; ok {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("settings.duplicateMessengerName", "name", name))
		}
		if len(name) == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.invalidMessengerName"))
		}

		set.Messengers[i].Name = name
		names[name] = true
	}

	// S3 password?
	if set.UploadS3AwsSecretAccessKey == "" {
		set.UploadS3AwsSecretAccessKey = cur.UploadS3AwsSecretAccessKey
	}
	if set.SendgridKey == "" {
		set.SendgridKey = cur.SendgridKey
	}
	if set.BouncePostmark.Password == "" {
		set.BouncePostmark.Password = cur.BouncePostmark.Password
	}
	if set.BounceForwardEmail.Key == "" {
		set.BounceForwardEmail.Key = cur.BounceForwardEmail.Key
	}
	if set.SecurityCaptcha.HCaptcha.Secret == "" {
		set.SecurityCaptcha.HCaptcha.Secret = cur.SecurityCaptcha.HCaptcha.Secret
	}
	if set.OIDC.ClientSecret == "" {
		set.OIDC.ClientSecret = cur.OIDC.ClientSecret
	}

	// OIDC user auto-creation is enabled. Validate.
	if set.OIDC.AutoCreateUsers {
		if set.OIDC.DefaultUserRoleID.Int < auth.SuperAdminRoleID {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", a.i18n.T("settings.security.OIDCDefaultRole")))
		}
	}

	for n, v := range set.UploadExtensions {
		set.UploadExtensions[n] = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v), "."))
	}

	// Domain blocklist / allowlist.
	doms := make([]string, 0, len(set.DomainBlocklist))
	for _, d := range set.DomainBlocklist {
		if d = strings.TrimSpace(strings.ToLower(d)); d != "" {
			doms = append(doms, d)
		}
	}
	set.DomainBlocklist = doms

	doms = make([]string, 0, len(set.DomainAllowlist))
	for _, d := range set.DomainAllowlist {
		if d = strings.TrimSpace(strings.ToLower(d)); d != "" {
			doms = append(doms, d)
		}
	}
	set.DomainAllowlist = doms

	// Validate and clean CORS domains.
	cors := make([]string, 0, len(set.SecurityCORSOrigins))
	for _, d := range set.SecurityCORSOrigins {
		if d = strings.TrimSpace(d); d != "" {
			if d == "*" {
				cors = append(cors, d)
				continue
			}

			// Parse and validate the URL.
			u, err := url.Parse(d)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return echo.NewHTTPError(http.StatusBadRequest,
					a.i18n.Ts("globals.messages.invalidData")+": invalid CORS domain: "+d)
			}
			// Save clean scheme + host
			cors = append(cors, u.Scheme+"://"+u.Host)
		}
	}
	set.SecurityCORSOrigins = cors

	// Validate slow query caching cron.
	if set.CacheSlowQueries {
		if _, err := cron.ParseStandard(set.CacheSlowQueriesInterval); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidData")+": slow query cron: "+err.Error())
		}
	}

	// Update the settings in the DB.
	if err := a.core.UpdateSettings(set); err != nil {
		return err
	}

	return a.handleSettingsRestart(c)
}

// UpdateSettingsByKey updates a single setting key-value in the DB.
func (a *App) UpdateSettingsByKey(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidData"))
	}
	if key == customFieldsSettingKey {
		return echo.NewHTTPError(http.StatusForbidden, "use the custom fields endpoint")
	}

	// Read the raw JSON body as the value.
	var b json.RawMessage
	if err := c.Bind(&b); err != nil {
		return err
	}
	if key == "smtp_delivery" {
		var delivery models.SMTPDeliverySettings
		if err := json.Unmarshal(b, &delivery); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP delivery settings")
		}
		if _, _, err := delivery.SendDelayRange(); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if delivery.MaxConns < 1 || delivery.MaxMsgRetries < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP delivery settings")
		}
		if d, err := time.ParseDuration(delivery.IdleTimeout); err != nil || d <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP idle timeout")
		}
		if d, err := time.ParseDuration(delivery.WaitTimeout); err != nil || d <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP wait timeout")
		}
		normalized, err := json.Marshal(delivery)
		if err != nil {
			return err
		}
		b = normalized
	}

	// Update the value in the DB.
	if err := a.core.UpdateSettingsByKey(key, b); err != nil {
		return err
	}

	return a.handleSettingsRestart(c)
}

// handleSettingsRestart checks for running campaigns and either triggers an
// immediate app restart or marks the app as needing a restart.
func (a *App) handleSettingsRestart(c echo.Context) error {
	// If there are any active campaigns, don't do an auto reload and
	// warn the user on the frontend.
	if a.manager.HasRunningCampaigns() {
		a.Lock()
		a.needsRestart = true
		a.Unlock()

		return c.JSON(http.StatusOK, okResp{struct {
			NeedsRestart bool `json:"needs_restart"`
		}{true}})
	}

	// No running campaigns. Reload the app.
	go func() {
		<-time.After(time.Millisecond * 500)
		a.chReload <- syscall.SIGHUP
	}()

	return c.JSON(http.StatusOK, okResp{true})
}

// GetLogs returns the log entries stored in the log buffer.
func (a *App) GetLogs(c echo.Context) error {
	return c.JSON(http.StatusOK, okResp{a.bufLog.Lines()})
}

// TestSMTPSettings returns the log entries stored in the log buffer.
func (a *App) TestSMTPSettings(c echo.Context) error {
	// Copy the raw JSON post body.
	reqBody, err := io.ReadAll(c.Request().Body)
	if err != nil {
		a.log.Printf("error reading SMTP test: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.internalError"))
	}

	// Load the JSON into koanf to parse SMTP settings properly including timestrings.
	ko := koanf.New(".")
	if err := ko.Load(rawbytes.Provider(reqBody), koanfjson.Parser()); err != nil {
		a.log.Printf("error unmarshalling SMTP test request: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.internalError"))
	}

	req := email.Server{}
	if err := ko.UnmarshalWithConf("", &req, koanf.UnmarshalConf{Tag: "json"}); err != nil {
		a.log.Printf("error scanning SMTP test request: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.internalError"))
	}

	to := ko.String("email")
	if to == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.missingFields", "name", "email"))
	}

	// Initialize a new SMTP pool.
	req.MaxConns = resolveSMTPPlatformDefaults().MaxConns
	req.IdleTimeout = time.Second * 2
	req.PoolWaitTimeout = time.Second * 2
	req.SendDelayMin, req.SendDelayMax = 0, 0
	msgr, err := email.New("", req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorCreating", "name", "SMTP", "error", err.Error()))
	}
	defer msgr.Close()
	if a.manager != nil {
		a.manager.ConfigureSMTP(msgr)
	}

	// Render the test email template body.
	var b bytes.Buffer
	if err := notifs.Tpls.ExecuteTemplate(&b, "smtp-test", nil); err != nil {
		a.log.Printf("error compiling notification template '%s': %v", "smtp-test", err)
		return err
	}

	m := models.Message{}
	m.From = req.FromEmail
	m.To = []string{to}
	m.Subject = a.i18n.T("settings.smtp.testConnection")
	m.Body = b.Bytes()
	if err := msgr.Push(m); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, okResp{a.bufLog.Lines()})
}

func (a *App) GetAboutInfo(c echo.Context) error {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	out := a.about
	out.System.AllocMB = mem.Alloc / 1024 / 1024
	out.System.OSMB = mem.Sys / 1024 / 1024

	return c.JSON(http.StatusOK, out)
}
