package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/utils"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gopkg.in/volatiletech/null.v6"
)

const MaxPersonalIntegrationTokensPerWorkspace = 10

func (c *Core) GetUsers() ([]auth.User, error) {
	out := []auth.User{}
	if err := c.q.GetUsers.Select(&out); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.users}", "error", pqErrMsg(err)))
	}

	return c.setupUserFields(out), nil
}

// GetUser retrieves a specific user based on any one given identifier.
func (c *Core) GetUser(id int, username, email string) (auth.User, error) {
	var out auth.User
	if err := c.q.GetUser.Get(&out, id, username, email); err != nil {
		if err == sql.ErrNoRows {
			return out, echo.NewHTTPError(http.StatusNotFound,
				c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.user}"))

		}

		return out, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.users}", "error", pqErrMsg(err)))
	}

	return c.setupUserFields([]auth.User{out})[0], nil
}

// CreateUser creates a new user.
func (c *Core) CreateUser(u auth.User) (auth.User, error) {
	var id int

	// If it's an API user, generate a random token for password
	// and set the e-mail to default.
	if u.Type == auth.UserTypeAPI {
		// Generate a random admin password.
		tk, err := utils.GenerateRandomString(32)
		if err != nil {
			return auth.User{}, err
		}

		u.Email = null.String{String: u.Username + "@api", Valid: true}
		u.PasswordLogin = false
		u.Password = null.String{String: tk, Valid: true}
	}

	if err := c.q.CreateUser.Get(&id, u.Username, u.PasswordLogin, u.Password, u.Email, u.Name, u.Type, u.UserRoleID, u.ListRoleID, u.Status); err != nil {
		return auth.User{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	// Hide the password field in the response except for when the user type is an API token,
	// where the frontend shows the token on the UI just once.
	if u.Type != auth.UserTypeAPI {
		u.Password = null.String{Valid: false}
	}

	out, err := c.GetUser(id, "", "")
	return out, err
}

// CreateUsers creates a batch of regular users atomically. Callers are
// responsible for validating the incoming user records before calling this.
func (c *Core) CreateUsers(users []auth.User) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.users}", "error", pqErrMsg(err)))
	}
	defer tx.Rollback()

	for _, u := range users {
		var id int
		if err := tx.Stmtx(c.q.CreateUser).Get(&id, u.Username, u.PasswordLogin, u.Password, u.Email, u.Name, u.Type, u.UserRoleID, u.ListRoleID, u.Status); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
		}
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.users}", "error", pqErrMsg(err)))
	}

	return nil
}

// CreateIntegrationToken creates a new service bearer token for an API user.
func (c *Core) CreateIntegrationToken(userID int, name string) (auth.IntegrationToken, string, error) {
	user, err := c.GetUser(userID, "", "")
	if err != nil {
		return auth.IntegrationToken{}, "", err
	}
	if user.Type != auth.UserTypeAPI {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusBadRequest, "integration tokens are only available for API users")
	}

	token, err := utils.GenerateRandomString(48)
	if err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	token = "lmit_" + token

	var id int
	if err := c.q.CreateIntegrationToken.Get(&id, userID, name, auth.HashIntegrationToken(token)); err != nil {
		if err == sql.ErrNoRows {
			return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusBadRequest, "integration tokens are only available for API users")
		}

		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	out, err := c.GetIntegrationToken(userID, id)
	if err != nil {
		return auth.IntegrationToken{}, "", err
	}

	return out, token, nil
}

// CreatePersonalIntegrationToken creates a workspace-bound bearer token for a
// regular user. The token is returned once; only its hash is persisted.
func (c *Core) CreatePersonalIntegrationToken(userID, organizationID int, name string, scopes []string, expiresAt time.Time) (auth.IntegrationToken, string, error) {
	organizationIDValue := nullableOrganizationID(organizationID)
	token, err := utils.GenerateRandomString(48)
	if err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	token = "lmpk_" + token

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback()

	// Serialize key creation for this exact owner/workspace pair so concurrent
	// requests cannot exceed the active-key limit.
	if _, err := tx.Exec("SELECT pg_advisory_xact_lock($1, $2)", userID, organizationID); err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var activeCount int
	if err := tx.Stmtx(c.q.CountActivePersonalIntegrationTokens).Get(&activeCount, userID, organizationIDValue); err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "integration tokens", "error", pqErrMsg(err)))
	}
	if activeCount >= MaxPersonalIntegrationTokensPerWorkspace {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusBadRequest, "maximum active API keys reached for this workspace")
	}

	var id int
	if err := tx.Stmtx(c.q.CreatePersonalIntegrationToken).Get(&id, userID, organizationIDValue, name, auth.HashIntegrationToken(token), pq.StringArray(scopes), expiresAt); err != nil {
		if err == sql.ErrNoRows {
			return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusBadRequest, "personal API keys require an enabled regular user")
		}
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "API key", "error", pqErrMsg(err)))
	}
	if err := tx.Commit(); err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	out, err := c.GetPersonalIntegrationToken(userID, id)
	return out, token, err
}

// UpdateUser updates a given user.
func (c *Core) UpdateUser(id int, u auth.User) (auth.User, error) {
	listRoleID := 0
	if u.ListRoleID == nil {
		listRoleID = -1
	} else {
		listRoleID = *u.ListRoleID
	}

	res, err := c.q.UpdateUser.Exec(id, u.Username, u.PasswordLogin, u.Password, u.Email, u.Name, u.Type, u.UserRoleID, listRoleID, u.Status)
	if err != nil {
		return auth.User{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return auth.User{}, echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("users.needSuper"))
	}

	out, err := c.GetUser(id, "", "")

	return out, err
}

// GetIntegrationTokens retrieves service integration tokens, optionally for a single API user.
func (c *Core) GetIntegrationTokens(userID int) ([]auth.IntegrationToken, error) {
	out := []auth.IntegrationToken{}
	if err := c.q.GetIntegrationTokens.Select(&out, userID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetPersonalIntegrationTokens retrieves a regular user's self-service API keys.
func (c *Core) GetPersonalIntegrationTokens(userID int) ([]auth.IntegrationToken, error) {
	out := []auth.IntegrationToken{}
	if err := c.q.GetPersonalIntegrationTokens.Select(&out, userID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "API keys", "error", pqErrMsg(err)))
	}
	return out, nil
}

// GetActiveIntegrationTokens retrieves active integration tokens for auth cache warmup.
func (c *Core) GetActiveIntegrationTokens() ([]auth.IntegrationToken, error) {
	out := []auth.IntegrationToken{}
	if err := c.q.GetActiveIntegrationTokens.Select(&out); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetIntegrationToken retrieves a specific service integration token by ID for an API user.
func (c *Core) GetIntegrationToken(userID int, tokenID int) (auth.IntegrationToken, error) {
	out, err := c.GetIntegrationTokens(userID)
	if err != nil {
		return auth.IntegrationToken{}, err
	}

	for _, t := range out {
		if t.ID == tokenID {
			return t, nil
		}
	}

	return auth.IntegrationToken{}, echo.NewHTTPError(http.StatusNotFound,
		c.i18n.Ts("globals.messages.notFound", "name", "integration token"))
}

// GetPersonalIntegrationToken retrieves a specific personal API key for its owner.
func (c *Core) GetPersonalIntegrationToken(userID int, tokenID int) (auth.IntegrationToken, error) {
	out, err := c.GetPersonalIntegrationTokens(userID)
	if err != nil {
		return auth.IntegrationToken{}, err
	}
	for _, token := range out {
		if token.ID == tokenID {
			return token, nil
		}
	}
	return auth.IntegrationToken{}, echo.NewHTTPError(http.StatusNotFound,
		c.i18n.Ts("globals.messages.notFound", "name", "API key"))
}

// DeleteIntegrationToken revokes an integration token for a user.
func (c *Core) DeleteIntegrationToken(userID int, tokenID int) error {
	var id int
	if err := c.q.DeleteIntegrationToken.Get(&id, tokenID, userID); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound,
				c.i18n.Ts("globals.messages.notFound", "name", "integration token"))
		}

		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "integration token", "error", pqErrMsg(err)))
	}

	return nil
}

// UpdatePersonalIntegrationToken changes a personal API key's display name,
// scopes, and expiry without exposing or replacing its secret.
func (c *Core) UpdatePersonalIntegrationToken(userID, tokenID int, name string, scopes []string, expiresAt time.Time) (auth.IntegrationToken, error) {
	var id int
	if err := c.q.UpdatePersonalIntegrationToken.Get(&id, tokenID, userID, name, pq.StringArray(scopes), expiresAt); err != nil {
		if err == sql.ErrNoRows {
			return auth.IntegrationToken{}, echo.NewHTTPError(http.StatusNotFound,
				c.i18n.Ts("globals.messages.notFound", "name", "API key"))
		}
		return auth.IntegrationToken{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "API key", "error", pqErrMsg(err)))
	}
	return c.GetPersonalIntegrationToken(userID, id)
}

// DeletePersonalIntegrationToken revokes a personal API key.
func (c *Core) DeletePersonalIntegrationToken(userID, tokenID int) error {
	var id int
	if err := c.q.DeletePersonalIntegrationToken.Get(&id, tokenID, userID); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound,
				c.i18n.Ts("globals.messages.notFound", "name", "API key"))
		}
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "API key", "error", pqErrMsg(err)))
	}
	return nil
}

// RotatePersonalIntegrationToken immediately revokes the old key and creates
// a replacement with the same workspace and scopes in one transaction.
func (c *Core) RotatePersonalIntegrationToken(userID, tokenID int, expiresAt time.Time) (auth.IntegrationToken, string, error) {
	old, err := c.GetPersonalIntegrationToken(userID, tokenID)
	if err != nil {
		return auth.IntegrationToken{}, "", err
	}
	if old.RevokedAt.Valid || !old.ExpiresAt.Valid || !old.ExpiresAt.Time.After(time.Now()) {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusBadRequest, "API key is not active")
	}

	token, err := utils.GenerateRandomString(48)
	if err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	token = "lmpk_" + token

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback()

	var revokedID int
	if err := tx.Stmtx(c.q.DeletePersonalIntegrationToken).Get(&revokedID, tokenID, userID); err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusConflict, "API key is no longer active")
	}

	var id int
	if err := tx.Stmtx(c.q.CreatePersonalIntegrationToken).Get(&id, userID, nullableOrganizationID(int(old.WorkspaceOrganizationID.Int)), old.Name, auth.HashIntegrationToken(token), pq.StringArray(old.Scopes), expiresAt); err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "API key", "error", pqErrMsg(err)))
	}

	if err := tx.Commit(); err != nil {
		return auth.IntegrationToken{}, "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	out, err := c.GetPersonalIntegrationToken(userID, id)
	return out, token, err
}

func nullableOrganizationID(organizationID int) any {
	if organizationID < 1 {
		return nil
	}
	return organizationID
}

// UpdateUserProfile updates the basic fields of a given uesr (name, email, password).
func (c *Core) UpdateUserProfile(id int, u auth.User) (auth.User, error) {
	res, err := c.q.UpdateUserProfile.Exec(id, u.Name, u.Email, u.PasswordLogin, u.Password, u.Attribs)
	if err != nil {
		return auth.User{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return auth.User{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.user}"))
	}

	return c.GetUser(id, "", "")
}

// UpdateUserLogin updates a user's record post-login.
func (c *Core) UpdateUserLogin(id int, avatar string) error {
	if _, err := c.q.UpdateUserLogin.Exec(id, avatar); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	return nil
}

// TouchIntegrationToken updates the last-used timestamp for a bearer integration token.
func (c *Core) TouchIntegrationToken(id int) error {
	if _, err := c.q.UpdateIntegrationTokenUsage.Exec(id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "integration token", "error", pqErrMsg(err)))
	}

	return nil
}

// SetTwoFA sets or clears the 2FA configuration for a user.
func (c *Core) SetTwoFA(id int, twofaType, twofaKey string) error {
	if _, err := c.q.SetUserTwoFA.Exec(id, twofaType, twofaKey); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
	}

	return nil
}

// DeleteUsers permanently deletes users and their account-scoped data. Organization
// memberships are stored as soft-deleted audit records, so they and other explicit
// organization references must be handled before the users row can be removed.
func (c *Core) DeleteUsers(ids []int) error {
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("globals.messages.invalidID"))
	}

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.userDeletionDBErr(err)
	}
	defer tx.Rollback()

	// Serializes account deletion with role changes and other account writes so
	// two concurrent requests can never remove the final enabled Super Admin.
	if _, err := tx.Exec(`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return c.userDeletionDBErr(err)
	}

	var remainingSuperAdmins int
	if err := tx.Get(&remainingSuperAdmins, `
		SELECT COUNT(*) FROM users
		WHERE id != ALL($1)
			AND user_role_id = 1 AND type = 'user' AND status = 'enabled'`, pq.Array(ids)); err != nil {
		return c.userDeletionDBErr(err)
	}
	if remainingSuperAdmins == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("users.needSuper"))
	}

	if err := c.prepareUsersForDeletion(tx, ids); err != nil {
		return err
	}

	res, err := tx.Stmtx(c.q.DeleteUsers).Exec(pq.Array(ids))
	if err != nil {
		return c.userDeletionDBErr(err)
	}
	if num, err := res.RowsAffected(); err != nil || num == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("users.needSuper"))
	}
	if err := tx.Commit(); err != nil {
		return c.userDeletionDBErr(err)
	}

	return nil
}

// prepareUsersForDeletion removes relationships that cannot survive a physical
// account deletion. Account-owned organization resources are left pending for
// transfer, matching the ownership guarantees used when a member leaves.
func (c *Core) prepareUsersForDeletion(tx *sqlx.Tx, ids []int) error {
	userIDs := pq.Array(ids)
	exec := func(query string) error {
		if _, err := tx.Exec(query, userIDs); err != nil {
			return c.userDeletionDBErr(err)
		}
		return nil
	}

	// Organization membership, transfer, and archive flows lock this same row.
	// Locking all affected organizations keeps an account deletion from racing a
	// resource transfer or a member removal in the same workspace.
	var organizationIDs []int
	if err := tx.Select(&organizationIDs, `
		SELECT o.id FROM organizations o
		WHERE o.created_by_user_id = ANY($1)
			OR EXISTS (
				SELECT 1 FROM organization_members om
				WHERE om.organization_id = o.id AND om.user_id = ANY($1)
			)
		ORDER BY o.id
		FOR UPDATE`, userIDs); err != nil {
		return c.userDeletionDBErr(err)
	}

	// Do this before clearing campaign ownership so no recipient remains queued
	// after the campaign is handed over for transfer.
	if err := exec(`
		UPDATE campaign_recipients cr SET status = 'pending', updated_at = NOW()
		WHERE cr.status = 'queued'
			AND EXISTS (
				SELECT 1 FROM campaigns c
				WHERE c.id = cr.campaign_id
					AND c.organization_id IS NOT NULL
					AND c.owner_user_id = ANY($1)
			)`); err != nil {
		return err
	}

	for _, table := range []string{"lists", "subscribers", "templates", "media"} {
		query := fmt.Sprintf(`
			UPDATE %s SET owner_user_id = NULL,
				original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
				transfer_pending_at = NOW(), updated_at = NOW()
			WHERE organization_id IS NOT NULL AND owner_user_id = ANY($1)`, table)
		if err := exec(query); err != nil {
			return err
		}
	}

	if err := exec(`
		UPDATE campaigns SET
			owner_user_id = NULL,
			original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
			transfer_pending_at = NOW(),
			send_at = CASE WHEN status IN ('scheduled', 'deferred') THEN NULL ELSE send_at END,
			next_resume_at = CASE WHEN status = 'deferred' THEN NULL ELSE next_resume_at END,
			status = CASE WHEN status IN ('scheduled', 'deferred') THEN 'draft'::campaign_status
				WHEN status = 'running' THEN 'paused'::campaign_status ELSE status END,
			updated_at = NOW()
		WHERE organization_id IS NOT NULL AND owner_user_id = ANY($1)`); err != nil {
		return err
	}

	// An organization itself must stay available after its creator is deleted.
	// Prefer a remaining active manager, then fall back to a remaining Super Admin.
	if err := exec(`
		UPDATE organizations o SET created_by_user_id = COALESCE(
			(
				SELECT om.user_id FROM organization_members om
				WHERE om.organization_id = o.id
					AND om.removed_at IS NULL
					AND om.user_id != ALL($1)
				ORDER BY (om.role = 'manager') DESC, om.joined_at, om.user_id
				LIMIT 1
			),
			(
				SELECT u.id FROM users u
				WHERE u.id != ALL($1)
					AND u.user_role_id = 1 AND u.type = 'user' AND u.status = 'enabled'
				ORDER BY u.id
				LIMIT 1
			)
		)
		WHERE o.created_by_user_id = ANY($1)`); err != nil {
		return err
	}

	// These records have no valid principal after an account is deleted. Removing
	// them also revokes invitations and forwarding destinations tied to the user.
	for _, query := range []string{
		`DELETE FROM reply_forward_rules WHERE target_user_id = ANY($1)`,
		`DELETE FROM organization_join_requests WHERE requested_by_user_id = ANY($1)`,
		`DELETE FROM organization_invites WHERE created_by_user_id = ANY($1)`,
		`DELETE FROM organization_members WHERE user_id = ANY($1)`,
	} {
		if err := exec(query); err != nil {
			return err
		}
	}

	return nil
}

func (c *Core) userDeletionDBErr(err error) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return echo.NewHTTPError(http.StatusConflict,
			c.i18n.T("users.cantDeleteReferenced")).SetInternal(err)
	}

	return echo.NewHTTPError(http.StatusInternalServerError,
		c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.user}", "error", pqErrMsg(err))).SetInternal(err)
}

// LoginUser attempts to log the given user_id in by matching the password.
func (c *Core) LoginUser(username, password string) (auth.User, error) {
	var out auth.User
	if err := c.q.LoginUser.Get(&out, username, password); err != nil {
		if err == sql.ErrNoRows {
			return out, echo.NewHTTPError(http.StatusForbidden, c.i18n.T("users.invalidLogin"))
		}

		return out, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.users}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// setupUserFields prepares and sets up various user fields.
func (c *Core) setupUserFields(users []auth.User) []auth.User {
	for n, u := range users {
		u := u

		if u.Password.String != "" {
			u.HasPassword = true
			u.PasswordLogin = true
		}

		if u.Type == auth.UserTypeAPI {
			u.Email = null.String{}
		}

		u.UserRole.ID = u.UserRoleID
		u.UserRole.Name = u.UserRoleName
		u.UserRole.Permissions = u.UserRolePerms
		u.UserRoleID = 0

		// Prepare lookup maps.
		u.ListPermissionsMap = make(map[int]map[string]struct{})
		u.PermissionsMap = make(map[string]struct{})
		for _, p := range u.UserRolePerms {
			u.PermissionsMap[p] = struct{}{}
		}

		if u.ListRoleID != nil {
			// Unmarshall the raw list perms map.
			var listPerms []auth.ListPermission
			if u.ListsPermsRaw != nil {
				if err := json.Unmarshal(*u.ListsPermsRaw, &listPerms); err != nil {
					c.log.Printf("error unmarshalling list permissions for role %d: %v", u.ID, err)
				}
			}

			u.ListRole = &auth.ListRolePermissions{ID: *u.ListRoleID, Name: u.ListRoleName.String, Lists: listPerms}

			// Iterate each list in the list permissions and setup get/manage list IDs.
			for _, p := range listPerms {
				u.ListPermissionsMap[p.ID] = make(map[string]struct{})

				for _, perm := range p.Permissions {
					u.ListPermissionsMap[p.ID][perm] = struct{}{}

					// List IDs with get / manage permissions.
					if perm == auth.PermListGet {
						u.GetListIDs = append(u.GetListIDs, p.ID)
					}
					if perm == auth.PermListManage {
						u.ManageListIDs = append(u.ManageListIDs, p.ID)
					}
				}
			}
		}

		users[n] = u
	}

	return users
}
