package core

import (
	"context"
	"errors"
	"net/http"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gopkg.in/volatiletech/null.v6"
)

// ErrFirstTimeSetupDone is returned by FirstTimeSetup when the initial super
// admin already exists, either because the deployment is set up or because a
// concurrent request won the bootstrap race.
var ErrFirstTimeSetupDone = errors.New("first-time setup has already been completed")

// firstTimeSetupLockKey is the PostgreSQL advisory lock key that serializes
// first-time setup. The value is the ASCII of "listmonk" and is only meaningful
// within this application.
const firstTimeSetupLockKey int64 = 0x6c6973746d6f6e6b

// FirstTimeSetup creates the "Super Admin" role (with the given permissions, if
// it does not exist yet) and the first super admin user, and claims the
// resources seeded by --install for that user. It returns the created user, or
// ErrFirstTimeSetupDone if a user already exists.
//
// The existence check and the creation have to be atomic. Callers only have the
// in-memory `needsUserSetup` flag, which cannot stop two concurrent requests on
// a fresh deployment from each creating a user with role_id=1, i.e. from
// bootstrapping two super admins. Both therefore run in a single transaction
// that first takes a transaction-scoped advisory lock, so exactly one caller can
// bootstrap. The lock is released when the transaction ends; a caller that was
// waiting for it re-reads under READ COMMITTED (the default isolation level) and
// sees the winner's committed user, so it fails with ErrFirstTimeSetupDone
// instead of creating a second user.
func (c *Core) FirstTimeSetup(u auth.User, permissions []string) (auth.User, error) {
	ctx := context.Background()
	tx, err := c.db.BeginTxx(ctx, nil)
	if err != nil {
		return auth.User{}, c.firstTimeSetupErr(err)
	}
	defer tx.Rollback()

	// Serialize setup attempts. Advisory locks are not bound to a table, so they
	// cannot deadlock with the inserts below, and `pg_advisory_xact_lock` is
	// released automatically when this transaction commits or rolls back.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, firstTimeSetupLockKey); err != nil {
		return auth.User{}, c.firstTimeSetupErr(err)
	}

	// Any user at all means this deployment is (or is being) set up: whoever got
	// here first owns the initial account.
	var hasUsers bool
	if err := tx.GetContext(ctx, &hasUsers, `SELECT EXISTS(SELECT 1 FROM users WHERE type = 'user')`); err != nil {
		return auth.User{}, c.firstTimeSetupErr(err)
	}
	if hasUsers {
		return auth.User{}, ErrFirstTimeSetupDone
	}

	// Create the default "Super Admin" role with all permissions if it doesn't exist.
	if permissions == nil {
		// roles.permissions is NOT NULL; an empty permission set has to be an
		// empty array rather than a NULL.
		permissions = []string{}
	}
	var roles []auth.Role
	if err := tx.Stmtx(c.q.GetUserRoles).SelectContext(ctx, &roles, auth.SuperAdminRoleID); err != nil {
		return auth.User{}, c.firstTimeSetupErr(err)
	}
	if len(roles) == 0 {
		r := auth.Role{
			Type:        auth.RoleTypeUser,
			Name:        null.NewString("Super Admin", true),
			Permissions: permissions,
		}
		if err := tx.Stmtx(c.q.CreateRole).GetContext(ctx, &r, r.Name, auth.RoleTypeUser, pq.Array(r.Permissions)); err != nil {
			return auth.User{}, c.firstTimeSetupErr(err)
		}
	}

	// Create the super admin user in the DB.
	var id int
	if err := tx.Stmtx(c.q.CreateUser).GetContext(ctx, &id, u.Username, u.PasswordLogin, u.Password, u.Email, u.Name, u.Type, u.UserRoleID, u.CustomerListRoleID, u.Status); err != nil {
		return auth.User{}, c.firstTimeSetupErr(err)
	}

	// Claim the seed resources created during --install for the first user. This
	// belongs to the same transaction: the account and its resources are created
	// in one step, or not at all.
	if err := claimUnownedResources(ctx, tx, id); err != nil {
		return auth.User{}, err
	}

	if err := tx.Commit(); err != nil {
		return auth.User{}, c.firstTimeSetupErr(err)
	}

	return c.GetUser(id, "", "")
}

// firstTimeSetupErr wraps a setup failure as an HTTP error, matching the errors
// the other user write paths return.
func (c *Core) firstTimeSetupErr(err error) error {
	return echo.NewHTTPError(http.StatusInternalServerError,
		c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.user}", "error", pqErrMsg(err)))
}
