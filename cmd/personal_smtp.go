package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

var personalSMTPName = regexp.MustCompile(`[^a-z0-9\-]`)

type personalSMTPRequest struct {
	SMTP []models.PersonalSMTPServer `json:"smtp"`
}

type personalSMTPResponse struct {
	SMTP             []models.PersonalSMTPServer `json:"smtp"`
	RunningCampaigns bool                        `json:"running_campaigns,omitempty"`
}

type personalSMTPStatus struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Enabled    bool      `json:"enabled"`
	DailyLimit int       `json:"daily_limit"`
	SentToday  int       `json:"sent_today"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type personalSMTPStatusResponse struct {
	SMTP []personalSMTPStatus `json:"smtp"`
}

type personalSMTPTestRequest struct {
	models.SMTPServer
	ID    int    `json:"id"`
	Email string `json:"email"`
}

// GetPersonalSMTP returns the current account's SMTP servers. Passwords are
// replaced by a fixed mask; the platform administrator endpoint uses the same
// redaction and never exposes credentials.
func (a *App) GetPersonalSMTP(c echo.Context) error {
	return a.getPersonalSMTP(c, auth.GetUser(c).ID)
}

func (a *App) GetUserPersonalSMTP(c echo.Context) error {
	user := auth.GetUser(c)
	if !user.IsPlatformAdmin() {
		return auth.ErrPermDenied
	}
	rows, err := a.loadPersonalSMTP(getID(c))
	if err != nil {
		return err
	}
	out := make([]personalSMTPStatus, 0, len(rows))
	for _, row := range rows {
		out = append(out, personalSMTPStatus{
			ID: row.ID, Name: row.Name, Enabled: row.Enabled,
			DailyLimit: row.DailyLimit, SentToday: row.SentToday,
			UpdatedAt: row.UpdatedAt.Time,
		})
	}
	return c.JSON(http.StatusOK, okResp{personalSMTPStatusResponse{SMTP: out}})
}

func (a *App) getPersonalSMTP(c echo.Context, userID int) error {
	if userID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid user id")
	}
	rows, err := a.loadPersonalSMTP(userID)
	if err != nil {
		return err
	}
	redactPersonalSMTP(rows)
	return c.JSON(http.StatusOK, okResp{personalSMTPResponse{SMTP: rows}})
}

func (a *App) loadPersonalSMTP(userID int) ([]models.PersonalSMTPServer, error) {
	var rows []models.PersonalSMTPServer
	if err := a.queries.GetUserSMTPServers.Select(&rows, userID, currentLocalDate()); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = make([]models.PersonalSMTPServer, 0)
	}
	return rows, nil
}

// UpdatePersonalSMTP replaces the current account's SMTP configuration. A
// running campaign is allowed to continue and picks up the new pool on its
// next message; the response tells the UI to warn the operator.
func (a *App) UpdatePersonalSMTP(c echo.Context) error {
	userID := auth.GetUser(c).ID
	var req personalSMTPRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	running, err := a.userHasRunningCampaigns(userID)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()
	// Hold the manager's SMTP writer lock across the database transaction. This
	// closes the cached pool first and prevents a concurrent worker (or the
	// scheduler's resolver) from borrowing a row that is about to be replaced
	// or deleted. The callback form also keeps this handler testable when an App
	// is constructed without a manager.
	update := func() error {
		tx, err := a.db.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Lock the account's SMTP rows for the complete replacement so concurrent
		// updates cannot interleave password preservation and deletions.
		var existing []models.PersonalSMTPServer
		if err := tx.SelectContext(ctx, &existing, `
			SELECT s.*, FALSE AS is_primary, COALESCE(u.sent_count, 0) AS sent_today
			FROM user_smtp_servers s
			LEFT JOIN user_smtp_daily_usage u
			  ON u.smtp_uuid = s.uuid AND u.usage_date = $2::DATE
			WHERE s.user_id = $1
			ORDER BY s.id
			FOR UPDATE OF s`, userID, currentLocalDate()); err != nil {
			return err
		}
		existingByID := make(map[int]models.PersonalSMTPServer, len(existing))
		for _, row := range existing {
			existingByID[row.ID] = row
		}
		seen := make(map[int]struct{}, len(req.SMTP))
		seenNames := make(map[string]struct{}, len(req.SMTP))

		for i := range req.SMTP {
			item := &req.SMTP[i]
			if item.ID < 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP server id")
			}
			var old models.PersonalSMTPServer
			if item.ID > 0 {
				var ok bool
				old, ok = existingByID[item.ID]
				if !ok {
					return echo.NewHTTPError(http.StatusNotFound, "SMTP server is not owned by this account")
				}
				if _, duplicate := seen[item.ID]; duplicate {
					return echo.NewHTTPError(http.StatusBadRequest, "duplicate SMTP server id")
				}
				seen[item.ID] = struct{}{}
				// UUIDs are server-owned identity fields. Never accept a client
				// supplied UUID for an existing row: preserving it keeps the daily
				// usage history attached to the same SMTP server and prevents a
				// caller from attempting to retarget another row's usage key.
				item.UUID = old.UUID
			}
			if err := validatePersonalSMTP(item, a.importer.SanitizeEmail); err != nil {
				return err
			}
			if item.Name != "" {
				if _, ok := seenNames[item.Name]; ok {
					return echo.NewHTTPError(http.StatusBadRequest, "duplicate SMTP server name")
				}
				seenNames[item.Name] = struct{}{}
			}

			if item.Password == "" || isPasswordMask(item.Password) {
				if item.ID > 0 {
					item.Password = old.Password
				} else if isPasswordMask(item.Password) {
					// A mask is only a read-back placeholder. Never persist it as
					// a new account credential if a client sends it verbatim.
					item.Password = ""
				}
			}
			if item.ID == 0 {
				// New rows always get a server-generated UUID. Ignore any client
				// supplied value so a UUID (and its usage history) can never be
				// reused across accounts.
				item.UUID = uuid.Must(uuid.NewV4()).String()
				if err := tx.Stmtx(a.queries.CreateUserSMTPServer).Get(&item.ID,
					item.UUID, userID, item.Name, item.Enabled, item.FromEmail,
					item.DailyLimit, item.Host, item.HelloHostname, item.Port,
					item.AuthProtocol, item.Username, item.Password, item.EmailHeaders,
					item.MaxConns, item.MaxMsgRetries, item.IdleTimeout,
					item.WaitTimeout, item.TLSType, item.TLSSkipVerify); err != nil {
					return err
				}
			} else {
				res, err := tx.Stmtx(a.queries.UpdateUserSMTPServer).Exec(item.ID, userID,
					item.Name, item.Enabled, item.FromEmail, item.DailyLimit,
					item.Host, item.HelloHostname, item.Port, item.AuthProtocol,
					item.Username, item.Password, item.EmailHeaders, item.MaxConns,
					item.MaxMsgRetries, item.IdleTimeout, item.WaitTimeout,
					item.TLSType, item.TLSSkipVerify)
				if err != nil {
					return err
				}
				if n, _ := res.RowsAffected(); n == 0 {
					return echo.NewHTTPError(http.StatusNotFound, "SMTP server not found")
				}
			}
		}

		for id := range existingByID {
			if _, ok := seen[id]; !ok {
				if _, err := tx.Stmtx(a.queries.DeleteUserSMTPServer).Exec(id, userID); err != nil {
					return err
				}
			}
		}
		return tx.Commit()
	}
	if a.manager != nil {
		err = a.manager.WithPersonalSMTPUpdate(userID, update)
	} else {
		err = update()
	}
	if err != nil {
		return err
	}
	// An account-owned campaign must never remain runnable after its last
	// enabled SMTP has been removed. The manager normally discovers this at
	// the next delivery attempt, but that leaves a window where the campaign is
	// still persisted as running (and may be picked by another worker). Pause
	// those rows immediately after the configuration transaction commits. If
	// at least one server remains enabled, the invalidated pool is rebuilt on
	// the next message and the campaign continues with the new configuration.
	if !hasEnabledPersonalSMTP(req.SMTP) {
		if _, err := a.pauseUserRunningCampaigns(userID); err != nil {
			return err
		}
	}
	rows, err := a.loadPersonalSMTP(userID)
	if err != nil {
		return err
	}
	redactPersonalSMTP(rows)
	return c.JSON(http.StatusOK, okResp{personalSMTPResponse{SMTP: rows, RunningCampaigns: running}})
}

// DeletePersonalSMTP removes one account-owned SMTP server. It is kept as a
// separate endpoint for API clients; the bulk replacement endpoint applies the
// same ownership and running-campaign guard when rows are omitted.
func (a *App) DeletePersonalSMTP(c echo.Context) error {
	userID := auth.GetUser(c).ID
	id := getID(c)
	running, err := a.userHasRunningCampaigns(userID)
	if err != nil {
		return err
	}
	deleteSMTP := func() error {
		res, err := a.queries.DeleteUserSMTPServer.Exec(id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "SMTP server not found")
		}
		return nil
	}
	if a.manager != nil {
		err = a.manager.WithPersonalSMTPUpdate(userID, deleteSMTP)
	} else {
		err = deleteSMTP()
	}
	if err != nil {
		return err
	}
	if !a.hasEnabledPersonalSMTPForUser(userID) {
		if _, err := a.pauseUserRunningCampaigns(userID); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, okResp{personalSMTPResponse{RunningCampaigns: running}})
}

// hasEnabledPersonalSMTP reports whether a replacement request leaves at
// least one usable account SMTP. Validation has already run for every row,
// so this deliberately only inspects the enabled flag.
func hasEnabledPersonalSMTP(rows []models.PersonalSMTPServer) bool {
	for _, row := range rows {
		if row.Enabled {
			return true
		}
	}
	return false
}

func (a *App) hasEnabledPersonalSMTPForUser(userID int) bool {
	var enabled bool
	if err := a.db.Get(&enabled, `
		SELECT EXISTS(
			SELECT 1 FROM user_smtp_servers
			WHERE user_id = $1 AND enabled = TRUE
		)`, userID); err != nil {
		// Treat an unavailable/failed check as unavailable. This keeps the
		// strict no-fallback guarantee intact; the next campaign attempt will
		// also surface the database error in the normal path.
		return false
	}
	return enabled
}

// pauseUserRunningCampaigns stops every account-owned campaign that could be
// picked up by the scheduler when the account has no enabled SMTP. Running
// campaigns become paused (and their workers are signalled); scheduled and
// deferred campaigns become drafts with their scheduling timestamps cleared.
// Queued recipients are reset to pending so an operator can resume after
// configuring SMTP. The manager signal is sent only after commit: workers then
// observe the durable state and cannot be picked up by a concurrent scanner.
func (a *App) pauseUserRunningCampaigns(userID int) (int, error) {
	if userID < 1 {
		return 0, nil
	}
	tx, err := a.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	type campaignState struct {
		ID     int    `db:"id"`
		Status string `db:"status"`
	}
	var campaigns []campaignState
	if err := tx.Select(&campaigns, `
		SELECT id, status FROM campaigns
		WHERE owner_user_id = $1
		  AND status = ANY('{running,scheduled,deferred}'::campaign_status[])
		ORDER BY id FOR UPDATE`, userID); err != nil {
		return 0, err
	}
	if len(campaigns) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	ids := make([]int, 0, len(campaigns))
	runningIDs := make([]int, 0, len(campaigns))
	for _, campaign := range campaigns {
		ids = append(ids, campaign.ID)
		if campaign.Status == models.CampaignStatusRunning {
			runningIDs = append(runningIDs, campaign.ID)
		}
	}
	if _, err := tx.Exec(`
		UPDATE campaigns SET
			status = CASE
				WHEN status IN ('scheduled', 'deferred') THEN 'draft'::campaign_status
				ELSE 'paused'::campaign_status
			END,
			send_at = NULL,
			next_resume_at = NULL,
			updated_at = NOW()
		WHERE id = ANY($1::INT[])
		  AND owner_user_id = $2
		  AND status = ANY('{running,scheduled,deferred}'::campaign_status[])`,
		pq.Array(ids), userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`
		UPDATE campaign_recipients SET status = 'pending', updated_at = NOW()
		WHERE campaign_id = ANY($1::INT[]) AND status = 'queued'`, pq.Array(ids)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	for _, id := range runningIDs {
		// Some package-level tests and maintenance utilities construct an App
		// without starting the campaign manager. The durable transaction above
		// is still the source of truth in that case; avoid turning a successful
		// SMTP configuration update into a nil-pointer panic merely because
		// there is no in-process worker to signal.
		if a.manager != nil {
			a.manager.StopCampaign(id, models.CampaignStatusPaused)
		}
	}
	return len(campaigns), nil
}

func (a *App) userHasRunningCampaigns(userID int) (bool, error) {
	var has bool
	if err := a.queries.HasUserRunningCampaigns.Get(&has, userID); err != nil {
		return false, err
	}
	return has, nil
}

func (a *App) requirePersonalSMTPAvailable(userID int) error {
	var count int
	if err := a.db.Get(&count, `SELECT COUNT(*) FROM user_smtp_servers WHERE user_id = $1 AND enabled = TRUE`, userID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if count == 0 {
		return echo.NewHTTPError(http.StatusConflict, "configure at least one enabled personal SMTP before scheduling or sending")
	}
	return nil
}

func validatePersonalSMTP(item *models.PersonalSMTPServer, sanitizeEmail func(string) (string, error)) error {
	item.Name = personalSMTPName.ReplaceAllString(strings.ToLower(strings.TrimSpace(item.Name)), "-")
	item.Host = strings.TrimSpace(item.Host)
	item.FromEmail = strings.TrimSpace(item.FromEmail)
	item.Username = strings.TrimSpace(item.Username)
	item.AuthProtocol = strings.ToLower(strings.TrimSpace(item.AuthProtocol))
	if item.AuthProtocol == "" {
		item.AuthProtocol = "plain"
	}
	if item.TLSType == "" {
		item.TLSType = "TLS"
	}
	if item.Port < 1 || item.Port > 65535 || item.Host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "SMTP host and port are required")
	}
	if item.DailyLimit < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "SMTP daily limit must be zero or greater")
	}
	if item.AuthProtocol != "plain" && item.AuthProtocol != "login" && item.AuthProtocol != "cram" && item.AuthProtocol != "none" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP authentication protocol")
	}
	if item.TLSType != "none" && item.TLSType != "TLS" && item.TLSType != "STARTTLS" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP TLS type")
	}
	if item.MaxConns < 1 {
		item.MaxConns = 10
	}
	if item.MaxMsgRetries < 1 {
		item.MaxMsgRetries = 2
	}
	if item.IdleTimeout == "" {
		item.IdleTimeout = "15s"
	}
	if item.WaitTimeout == "" {
		item.WaitTimeout = "5s"
	}
	if _, err := time.ParseDuration(item.IdleTimeout); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP idle timeout")
	}
	if _, err := time.ParseDuration(item.WaitTimeout); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP wait timeout")
	}
	if item.Enabled && item.FromEmail == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "enabled SMTP requires a sender address")
	}
	if item.Enabled && item.FromEmail != "" && !reFromAddress.MatchString(item.FromEmail) && sanitizeEmail != nil {
		from, err := sanitizeEmail(item.FromEmail)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP sender address")
		}
		item.FromEmail = from
	}
	if item.EmailHeaders == nil {
		item.EmailHeaders = models.Headers{}
	}
	return nil
}

func redactPersonalSMTP(rows []models.PersonalSMTPServer) {
	for i := range rows {
		if rows[i].Password != "" {
			rows[i].Password = strings.Repeat(pwdMask, len([]rune(rows[i].Password)))
		}
	}
}

// TestPersonalSMTP validates and sends a connection test through the supplied
// account configuration. It never persists the server and never uses the
// platform SMTP fallback.
func (a *App) TestPersonalSMTP(c echo.Context) error {
	var req personalSMTPTestRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Email) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "recipient e-mail is required")
	}
	if req.ID > 0 {
		userID := auth.GetUser(c).ID
		var saved models.PersonalSMTPServer
		if err := a.queries.GetUserSMTPServer.Get(&saved, req.ID, userID, currentLocalDate()); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "SMTP server not found")
		}
		// The row UUID is server-owned; never allow a test request to replace it.
		req.UUID = saved.UUID
		if isPasswordMask(req.Password) || req.Password == "" {
			req.Password = saved.Password
		}
	}
	item := models.PersonalSMTPServer{SMTPServer: req.SMTPServer}
	if err := validatePersonalSMTP(&item, a.importer.SanitizeEmail); err != nil {
		return err
	}
	idle, _ := time.ParseDuration(item.IdleTimeout)
	wait, _ := time.ParseDuration(item.WaitTimeout)
	msgr, err := email.New("personal-test", email.Server{
		Name: item.Name, UUID: item.UUID, FromEmail: item.FromEmail,
		DailyLimit: item.DailyLimit, Username: item.Username, Password: item.Password,
		AuthProtocol: item.AuthProtocol, TLSType: item.TLSType,
		TLSSkipVerify: item.TLSSkipVerify, EmailHeaders: headersToMap(item.EmailHeaders),
		Opt: smtppool.Opt{Host: item.Host, Port: item.Port, HelloHostname: item.HelloHostname,
			MaxConns: 1, MaxMessageRetries: item.MaxMsgRetries, IdleTimeout: idle, PoolWaitTimeout: wait},
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("error creating SMTP connection: %v", err))
	}
	defer msgr.Close()
	if err := msgr.Push(models.Message{From: item.FromEmail, To: []string{req.Email},
		Subject: a.i18n.T("settings.smtp.testConnection"), Body: []byte("SMTP connection test")}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func isPasswordMask(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if string(r) != pwdMask {
			return false
		}
	}
	return true
}
