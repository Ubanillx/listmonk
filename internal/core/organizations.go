package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

var (
	ErrOrganizationNotFound    = echo.NewHTTPError(http.StatusNotFound, "organization not found")
	ErrNotOrganizationMember   = echo.NewHTTPError(http.StatusForbidden, "not an active organization member")
	ErrLastOrganizationManager = echo.NewHTTPError(http.StatusBadRequest, "an organization must retain at least one manager")
)

// HashOrganizationInviteCode hashes an invitation code before it is persisted.
// The code itself is intentionally never stored, logged, or returned later.
func HashOrganizationInviteCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

// GetUserOrganizations returns active memberships suitable for a workspace
// switcher. Archived organizations are intentionally excluded.
func (c *Core) GetUserOrganizations(userID int) ([]models.Organization, error) {
	out := []models.Organization{}
	err := c.db.Select(&out, `
		SELECT o.*, om.role AS my_role,
			COUNT(active_members.user_id) AS member_count
		FROM organizations o
		JOIN organization_members om
			ON om.organization_id = o.id AND om.user_id = $1 AND om.removed_at IS NULL
		LEFT JOIN organization_members active_members
			ON active_members.organization_id = o.id AND active_members.removed_at IS NULL
		WHERE o.status = $2
		GROUP BY o.id, om.role
		ORDER BY LOWER(o.name)`, userID, models.OrganizationStatusActive)
	if err != nil {
		return nil, c.organizationDBErr("fetching organizations", err)
	}
	return out, nil
}

// GetOrganizations lists organizations for platform administration.
func (c *Core) GetOrganizations(includeArchived bool) ([]models.Organization, error) {
	out := []models.Organization{}
	err := c.db.Select(&out, `
		SELECT o.*, '' AS my_role, COUNT(om.user_id) AS member_count
		FROM organizations o
		LEFT JOIN organization_members om ON om.organization_id = o.id AND om.removed_at IS NULL
		WHERE ($1 OR o.status = $2)
		GROUP BY o.id
		ORDER BY LOWER(o.name)`, includeArchived, models.OrganizationStatusActive)
	if err != nil {
		return nil, c.organizationDBErr("fetching organizations", err)
	}
	return out, nil
}

func (c *Core) GetOrganization(id int) (models.Organization, error) {
	var out models.Organization
	if err := c.db.Get(&out, `
		SELECT o.*, '' AS my_role,
			(SELECT COUNT(*) FROM organization_members om WHERE om.organization_id = o.id AND om.removed_at IS NULL) AS member_count
		FROM organizations o WHERE o.id = $1`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrOrganizationNotFound
		}
		return out, c.organizationDBErr("fetching organization", err)
	}
	return out, nil
}

// GetOrganizationMembership returns an active membership only.
func (c *Core) GetOrganizationMembership(orgID int, userID int) (models.OrganizationMember, error) {
	var out models.OrganizationMember
	err := c.db.Get(&out, `
		SELECT om.*, u.username, u.name, u.email
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		JOIN organizations o ON o.id = om.organization_id
		WHERE om.organization_id = $1 AND om.user_id = $2
			AND om.removed_at IS NULL AND o.status = $3`, orgID, userID, models.OrganizationStatusActive)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotOrganizationMember
		}
		return out, c.organizationDBErr("fetching organization membership", err)
	}
	return out, nil
}

func (c *Core) GetOrganizationMembers(orgID int) ([]models.OrganizationMember, error) {
	out := []models.OrganizationMember{}
	err := c.db.Select(&out, `
		SELECT om.*, u.username, u.name, u.email
		FROM organization_members om JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1
		ORDER BY om.removed_at NULLS FIRST, om.role DESC, LOWER(u.name), u.id`, orgID)
	if err != nil {
		return nil, c.organizationDBErr("fetching organization members", err)
	}
	return out, nil
}

func (c *Core) CreateOrganizationRequest(userID int, name, description string) (models.OrganizationJoinRequest, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return models.OrganizationJoinRequest{}, echo.NewHTTPError(http.StatusBadRequest, "organization name is required")
	}
	var exists bool
	if err := c.db.Get(&exists, `SELECT EXISTS(SELECT 1 FROM organizations WHERE LOWER(name) = LOWER($1))`, name); err != nil {
		return models.OrganizationJoinRequest{}, c.organizationDBErr("checking organization name", err)
	}
	if exists {
		return models.OrganizationJoinRequest{}, echo.NewHTTPError(http.StatusConflict, "organization name is already in use")
	}

	var out models.OrganizationJoinRequest
	err := c.db.Get(&out, `
		INSERT INTO organization_join_requests (requested_name, description, requested_by_user_id)
		VALUES ($1, $2, $3)
		RETURNING *`, name, description, userID)
	if err != nil {
		if conflict := organizationNameConflictError(err); conflict != nil {
			return out, conflict
		}
		return out, c.organizationDBErr("creating organization request", err)
	}
	return out, nil
}

func (c *Core) GetOrganizationRequests(includeResolved bool) ([]models.OrganizationJoinRequest, error) {
	out := []models.OrganizationJoinRequest{}
	err := c.db.Select(&out, `
		SELECT r.*, u.name AS requested_by_name
		FROM organization_join_requests r
		JOIN users u ON u.id = r.requested_by_user_id
		WHERE ($1 OR r.status = $2)
		ORDER BY r.created_at DESC`, includeResolved, models.OrganizationRequestPending)
	if err != nil {
		return nil, c.organizationDBErr("fetching organization requests", err)
	}
	return out, nil
}

// GetUserOrganizationRequests returns every organization-creation request
// submitted by one account, including reviewed and withdrawn history.
func (c *Core) GetUserOrganizationRequests(userID int) ([]models.OrganizationJoinRequest, error) {
	out := []models.OrganizationJoinRequest{}
	err := c.db.Select(&out, `
		SELECT r.*, u.name AS requested_by_name
		FROM organization_join_requests r
		JOIN users u ON u.id = r.requested_by_user_id
		WHERE r.requested_by_user_id = $1
		ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, c.organizationDBErr("fetching user organization requests", err)
	}
	return out, nil
}

// WithdrawOrganizationRequest keeps a caller's pending request as history
// while making it unavailable for platform review. The row lock serializes a
// withdrawal with a concurrent platform review.
func (c *Core) WithdrawOrganizationRequest(requestID, userID int) (models.OrganizationJoinRequest, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.OrganizationJoinRequest{}, c.organizationDBErr("starting organization request withdrawal", err)
	}
	defer tx.Rollback()

	var req models.OrganizationJoinRequest
	if err := tx.Get(&req, `SELECT * FROM organization_join_requests WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return req, echo.NewHTTPError(http.StatusNotFound, "organization request not found")
		}
		return req, c.organizationDBErr("fetching organization request for withdrawal", err)
	}
	if req.RequestedByUserID != userID {
		return req, echo.NewHTTPError(http.StatusNotFound, "organization request not found")
	}
	if req.Status != models.OrganizationRequestPending {
		return req, echo.NewHTTPError(http.StatusBadRequest, "only pending organization requests can be withdrawn")
	}

	if err := tx.Get(&req, `
		UPDATE organization_join_requests
		SET status = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING *`, requestID, models.OrganizationRequestWithdrawn); err != nil {
		return req, c.organizationDBErr("withdrawing organization request", err)
	}
	if err := tx.Commit(); err != nil {
		return req, c.organizationDBErr("committing organization request withdrawal", err)
	}
	return req, nil
}

// ReviewOrganizationRequest approves or rejects a request. Approval creates
// the organization and its first manager in the same transaction.
func (c *Core) ReviewOrganizationRequest(requestID, reviewerID int, approve bool, note string) (models.OrganizationJoinRequest, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.OrganizationJoinRequest{}, c.organizationDBErr("starting organization review", err)
	}
	defer tx.Rollback()

	var req models.OrganizationJoinRequest
	if err := tx.Get(&req, `SELECT * FROM organization_join_requests WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return req, echo.NewHTTPError(http.StatusNotFound, "organization request not found")
		}
		return req, c.organizationDBErr("fetching organization request", err)
	}
	if req.Status != models.OrganizationRequestPending {
		return req, echo.NewHTTPError(http.StatusBadRequest, "organization request has already been reviewed")
	}

	status := models.OrganizationRequestRejected
	var orgID any
	if approve {
		status = models.OrganizationRequestApproved
		var id int
		if err := tx.Get(&id, `
			INSERT INTO organizations (name, description, created_by_user_id)
			VALUES ($1, $2, $3) RETURNING id`, req.RequestedName, req.Description, req.RequestedByUserID); err != nil {
			if conflict := organizationNameConflictError(err); conflict != nil {
				return req, conflict
			}
			return req, c.organizationDBErr("creating organization", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO organization_members (organization_id, user_id, role)
			VALUES ($1, $2, $3)`, id, req.RequestedByUserID, models.OrganizationMemberRoleManager); err != nil {
			return req, c.organizationDBErr("creating organization manager", err)
		}
		orgID = id
	}

	if err := tx.Get(&req, `
		UPDATE organization_join_requests
		SET status = $2, reviewed_by_user_id = $3, reviewed_at = NOW(), review_note = $4,
			organization_id = $5, updated_at = NOW()
		WHERE id = $1 RETURNING *`, requestID, status, reviewerID, strings.TrimSpace(note), orgID); err != nil {
		return req, c.organizationDBErr("reviewing organization request", err)
	}
	if err := tx.Commit(); err != nil {
		return req, c.organizationDBErr("committing organization review", err)
	}
	return req, nil
}

func (c *Core) CreateOrganizationInvite(orgID, createdBy int, name, codeHash string, expiresAt null.Time, maxUses null.Int) (models.OrganizationInvite, error) {
	// The HTTP handler verifies manager access before entering this method, but
	// the organization may be archived between that check and the INSERT. Lock
	// and re-check the organization here so a stale request can never create a
	// usable invite for an archived tenant.
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.OrganizationInvite{}, c.organizationDBErr("starting organization invitation", err)
	}
	defer tx.Rollback()
	if err := c.lockActiveOrganization(tx, orgID); err != nil {
		return models.OrganizationInvite{}, err
	}
	var canManage bool
	if err := tx.Get(&canManage, `
		SELECT EXISTS(
			SELECT 1 FROM users u
			WHERE u.id = $2 AND u.user_role_id = 1
		) OR EXISTS(
			SELECT 1 FROM organization_members om
			WHERE om.organization_id = $1 AND om.user_id = $2
			  AND om.role = $3 AND om.removed_at IS NULL
		)`, orgID, createdBy, models.OrganizationMemberRoleManager); err != nil {
		return models.OrganizationInvite{}, c.organizationDBErr("checking organization invitation manager", err)
	}
	if !canManage {
		return models.OrganizationInvite{}, echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}

	// Insert first, then load the row through the same organization join used by
	// the listing endpoint.  INSERT ... RETURNING * cannot populate the
	// denormalized organization_name field on OrganizationInvite by itself.
	var inviteID int
	if err := tx.Get(&inviteID, `
		INSERT INTO organization_invites (organization_id, name, code_hash, created_by_user_id, expires_at, max_uses)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, orgID, strings.TrimSpace(name), codeHash, createdBy, expiresAt, maxUses); err != nil {
		return models.OrganizationInvite{}, c.organizationDBErr("creating organization invitation", err)
	}
	var out models.OrganizationInvite
	if err := tx.Get(&out, `
		SELECT i.*, o.name AS organization_name
		FROM organization_invites i
		JOIN organizations o ON o.id = i.organization_id
		WHERE i.id = $1`, inviteID); err != nil {
		return out, c.organizationDBErr("fetching created organization invitation", err)
	}
	if err := tx.Commit(); err != nil {
		return out, c.organizationDBErr("committing organization invitation", err)
	}
	return out, nil
}

func (c *Core) GetOrganizationInvites(orgID int) ([]models.OrganizationInvite, error) {
	out := []models.OrganizationInvite{}
	err := c.db.Select(&out, `
		SELECT i.*, o.name AS organization_name
		FROM organization_invites i JOIN organizations o ON o.id = i.organization_id
		WHERE i.organization_id = $1 ORDER BY i.created_at DESC`, orgID)
	if err != nil {
		return nil, c.organizationDBErr("fetching organization invitations", err)
	}
	return out, nil
}

func (c *Core) RevokeOrganizationInvite(orgID, inviteID int) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting organization invitation revocation", err)
	}
	defer tx.Rollback()
	if err := c.lockActiveOrganization(tx, orgID); err != nil {
		return err
	}
	res, err := tx.Exec(`
		UPDATE organization_invites SET revoked_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND revoked_at IS NULL`, inviteID, orgID)
	if err != nil {
		return c.organizationDBErr("revoking organization invitation", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "active organization invitation not found")
	}
	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing organization invitation revocation", err)
	}
	return nil
}

// JoinOrganizationByInvite atomically consumes a reusable invitation and
// restores a prior membership if the same account had left the organization.
func (c *Core) JoinOrganizationByInvite(userID int, codeHash string) (models.Organization, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.Organization{}, c.organizationDBErr("starting organization join", err)
	}
	defer tx.Rollback()

	var invite models.OrganizationInvite
	err = tx.Get(&invite, `
		SELECT i.*, o.name AS organization_name
		FROM organization_invites i JOIN organizations o ON o.id = i.organization_id
		WHERE i.code_hash = $1 AND i.revoked_at IS NULL AND o.status = $2
		FOR UPDATE`, codeHash, models.OrganizationStatusActive)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Organization{}, echo.NewHTTPError(http.StatusNotFound, "invitation is invalid or revoked")
		}
		return models.Organization{}, c.organizationDBErr("fetching organization invitation", err)
	}
	if invite.ExpiresAt.Valid && !invite.ExpiresAt.Time.After(time.Now()) {
		return models.Organization{}, echo.NewHTTPError(http.StatusBadRequest, "invitation has expired")
	}
	if invite.MaxUses.Valid && invite.UseCount >= int(invite.MaxUses.Int) {
		return models.Organization{}, echo.NewHTTPError(http.StatusBadRequest, "invitation has reached its usage limit")
	}

	var existing models.OrganizationMember
	err = tx.Get(&existing, `SELECT * FROM organization_members WHERE organization_id = $1 AND user_id = $2 FOR UPDATE`, invite.OrganizationID, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return models.Organization{}, c.organizationDBErr("checking organization membership", err)
	}
	if err == nil && !existing.RemovedAt.Valid {
		return models.Organization{}, echo.NewHTTPError(http.StatusBadRequest, "user is already an organization member")
	}

	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
			invite.OrganizationID, userID, models.OrganizationMemberRoleMember)
	} else {
		_, err = tx.Exec(`
			UPDATE organization_members SET role = $3, joined_at = NOW(), removed_at = NULL, removed_by_user_id = NULL
			WHERE organization_id = $1 AND user_id = $2`, invite.OrganizationID, userID, models.OrganizationMemberRoleMember)
	}
	if err != nil {
		return models.Organization{}, c.organizationDBErr("joining organization", err)
	}
	if _, err = tx.Exec(`UPDATE organization_invites SET use_count = use_count + 1, updated_at = NOW() WHERE id = $1`, invite.ID); err != nil {
		return models.Organization{}, c.organizationDBErr("consuming organization invitation", err)
	}

	var org models.Organization
	if err := tx.Get(&org, `
		SELECT o.*, $2::TEXT AS my_role,
			(SELECT COUNT(*) FROM organization_members om
			 WHERE om.organization_id = o.id AND om.removed_at IS NULL) AS member_count
		FROM organizations o
		WHERE o.id = $1`, invite.OrganizationID, models.OrganizationMemberRoleMember); err != nil {
		return org, c.organizationDBErr("fetching organization", err)
	}
	if err := tx.Commit(); err != nil {
		return org, c.organizationDBErr("committing organization join", err)
	}
	return org, nil
}

// AddOrganizationMember adds an existing account directly. Re-adding a former
// member restores the relationship and intentionally does not consume an invite.
func (c *Core) AddOrganizationMember(orgID, userID int, role string) (models.OrganizationMember, error) {
	if role != models.OrganizationMemberRoleMember && role != models.OrganizationMemberRoleManager {
		return models.OrganizationMember{}, echo.NewHTTPError(http.StatusBadRequest, "invalid organization member role")
	}
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.OrganizationMember{}, c.organizationDBErr("starting organization member add", err)
	}
	defer tx.Rollback()
	if err := c.lockActiveOrganization(tx, orgID); err != nil {
		return models.OrganizationMember{}, err
	}

	var current string
	err = tx.Get(&current, `
		SELECT role FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL
		FOR UPDATE`, orgID, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return models.OrganizationMember{}, c.organizationDBErr("checking existing organization member", err)
	}
	if err == nil && current == models.OrganizationMemberRoleManager && role == models.OrganizationMemberRoleMember {
		count, err := c.countOrganizationManagers(tx, orgID)
		if err != nil {
			return models.OrganizationMember{}, err
		}
		if count <= 1 {
			return models.OrganizationMember{}, ErrLastOrganizationManager
		}
	}

	var out models.OrganizationMember
	err = tx.Get(&out, `
		INSERT INTO organization_members (organization_id, user_id, role)
		SELECT $1, u.id, $3 FROM users u WHERE u.id = $2
		ON CONFLICT (organization_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, joined_at = NOW(), removed_at = NULL, removed_by_user_id = NULL
		RETURNING organization_id, user_id, role, joined_at, removed_at, removed_by_user_id,
			''::TEXT AS username, ''::TEXT AS name, ''::TEXT AS email`, orgID, userID, role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return out, c.organizationDBErr("adding organization member", err)
	}
	if err := tx.Commit(); err != nil {
		return out, c.organizationDBErr("committing organization member add", err)
	}
	return c.GetOrganizationMembership(orgID, userID)
}

// FindUserIDByAccount resolves an already registered username or e-mail
// address without exposing a directory of platform accounts to organization
// managers.
func (c *Core) FindUserIDByAccount(account string) (int, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "registered account is required")
	}
	var id int
	if err := c.db.Get(&id, `
		SELECT id FROM users
		WHERE LOWER(username) = LOWER($1) OR LOWER(COALESCE(email, '')) = LOWER($1)
		ORDER BY id LIMIT 1`, account); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, echo.NewHTTPError(http.StatusNotFound, "registered account not found")
		}
		return 0, c.organizationDBErr("finding registered account", err)
	}
	return id, nil
}

func (c *Core) UpdateOrganizationMemberRole(orgID, userID int, role string) error {
	if role != models.OrganizationMemberRoleMember && role != models.OrganizationMemberRoleManager {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid organization member role")
	}
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting organization member update", err)
	}
	defer tx.Rollback()
	if err := c.lockActiveOrganization(tx, orgID); err != nil {
		return err
	}

	var current string
	if err := tx.Get(&current, `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL FOR UPDATE`, orgID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotOrganizationMember
		}
		return c.organizationDBErr("checking organization manager", err)
	}
	if role == models.OrganizationMemberRoleMember && current == models.OrganizationMemberRoleManager {
		count, err := c.countOrganizationManagers(tx, orgID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastOrganizationManager
		}
	}
	if _, err := tx.Exec(`UPDATE organization_members SET role = $3 WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL`, orgID, userID, role); err != nil {
		return c.organizationDBErr("updating organization member", err)
	}
	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing organization member update", err)
	}
	return nil
}

// RemoveOrganizationMember revokes access and turns all member-owned
// organization resources into pending-transfer resources. Scheduled campaigns
// become drafts and running campaigns become paused for the caller to stop.
func (c *Core) RemoveOrganizationMember(orgID, userID, removedBy int) ([]models.Campaign, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, c.organizationDBErr("starting organization member removal", err)
	}
	defer tx.Rollback()
	if err := c.lockActiveOrganization(tx, orgID); err != nil {
		return nil, err
	}

	var member models.OrganizationMember
	if err := tx.Get(&member, `SELECT * FROM organization_members WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL FOR UPDATE`, orgID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotOrganizationMember
		}
		return nil, c.organizationDBErr("fetching organization member", err)
	}
	if member.Role == models.OrganizationMemberRoleManager {
		count, err := c.countOrganizationManagers(tx, orgID)
		if err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, ErrLastOrganizationManager
		}
	}

	if _, err := tx.Exec(`
		UPDATE organization_members SET removed_at = NOW(), removed_by_user_id = $3
		WHERE organization_id = $1 AND user_id = $2`, orgID, userID, removedBy); err != nil {
		return nil, c.organizationDBErr("removing organization member", err)
	}

	// A global template belongs to its creator rather than to the organization
	// in which it was first authored. Keep it usable and maintainable after the
	// creator leaves by moving it, and private copies of its inline media, into
	// that user's personal workspace before the remaining organization resources
	// are marked pending for transfer.
	if err := c.detachGlobalTemplatesForFormerMember(tx, orgID, userID); err != nil {
		return nil, err
	}

	for _, table := range []string{"lists", "subscribers", "templates", "media"} {
		stmt := fmt.Sprintf(`
			UPDATE %s SET owner_user_id = NULL,
				original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
				transfer_pending_at = NOW(), updated_at = NOW()
			WHERE organization_id = $1 AND owner_user_id = $2`, table)
		if _, err := tx.Exec(stmt, orgID, userID); err != nil {
			return nil, c.organizationDBErr("preparing member resources for transfer", err)
		}
	}

	// A campaign pipe marks recipient rows queued while it is preparing a
	// message.  The manager cleanup normally returns those rows to pending
	// after StopCampaign, but a member can be removed when no in-process pipe
	// exists (or while the pipe is between batches).  Reset them in the same
	// transaction as the ownership transition so a later transfer/resume can
	// never inherit a stranded queued row.
	if _, err := tx.Exec(`
		UPDATE campaign_recipients cr SET status = 'pending', updated_at = NOW()
		WHERE cr.status = 'queued'
		  AND EXISTS (
			SELECT 1 FROM campaigns c
			WHERE c.id = cr.campaign_id
			  AND c.organization_id = $1
			  AND c.owner_user_id = $2
		  )`, orgID, userID); err != nil {
		return nil, c.organizationDBErr("resetting member campaign recipients", err)
	}

	var stopped []models.Campaign
	if err := tx.Select(&stopped, `
		UPDATE campaigns SET
			owner_user_id = NULL,
			original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
			transfer_pending_at = NOW(),
			send_at = CASE WHEN status IN ('scheduled', 'deferred') THEN NULL ELSE send_at END,
			next_resume_at = CASE WHEN status = 'deferred' THEN NULL ELSE next_resume_at END,
			status = CASE WHEN status IN ('scheduled', 'deferred') THEN 'draft'::campaign_status
				WHEN status = 'running' THEN 'paused'::campaign_status ELSE status END,
			updated_at = NOW()
		WHERE organization_id = $1 AND owner_user_id = $2
		RETURNING id, status`, orgID, userID); err != nil {
		return nil, c.organizationDBErr("preparing member campaigns for transfer", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, c.organizationDBErr("committing organization member removal", err)
	}
	return stopped, nil
}

// TransferPendingOrganizationResources gives all pending resources from a
// former member to an active member. Subscriber conflicts are merged by scoped
// email, preserving subscriptions and historical analytics before deletion.
func (c *Core) TransferPendingOrganizationResources(orgID, targetUserID int) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting organization resource transfer", err)
	}
	defer tx.Rollback()
	if err := c.lockOrganization(tx, orgID); err != nil {
		return err
	}
	var organizationStatus string
	if err := tx.Get(&organizationStatus, `SELECT status FROM organizations WHERE id = $1`, orgID); err != nil {
		return c.organizationDBErr("checking organization transfer status", err)
	}
	if organizationStatus != models.OrganizationStatusActive {
		return echo.NewHTTPError(http.StatusConflict, "archived organization resources must be transferred through the archive cleanup flow")
	}

	var role string
	if err := tx.Get(&role, `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL`, orgID, targetUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotOrganizationMember
		}
		return c.organizationDBErr("checking transfer target", err)
	}

	// Transfer lists first so merged subscriber subscriptions point at target
	// owned lists when a recipient record is merged below.
	for _, table := range []string{"lists", "campaigns", "media"} {
		stmt := fmt.Sprintf(`
			UPDATE %s SET owner_user_id = $2, transfer_pending_at = NULL, updated_at = NOW()
			WHERE organization_id = $1 AND owner_user_id IS NULL AND transfer_pending_at IS NOT NULL`, table)
		if _, err := tx.Exec(stmt, orgID, targetUserID); err != nil {
			return c.organizationDBErr("transferring organization resources", err)
		}
	}
	// A transferred template must not displace the target owner's existing
	// workspace default. The target can explicitly select it as the default
	// afterwards. Clearing this flag also avoids a conflict with the scoped
	// unique default-template index during the ownership update.
	if _, err := tx.Exec(`
		UPDATE templates SET owner_user_id = $2, transfer_pending_at = NULL,
			is_default = FALSE, updated_at = NOW()
		WHERE organization_id = $1 AND owner_user_id IS NULL AND transfer_pending_at IS NOT NULL`, orgID, targetUserID); err != nil {
		return c.organizationDBErr("transferring organization templates", err)
	}

	type pendingSubscriber struct {
		ID    int    `db:"id"`
		Email string `db:"email"`
	}
	var pending []pendingSubscriber
	if err := tx.Select(&pending, `
		SELECT id, email FROM subscribers
		WHERE organization_id = $1 AND owner_user_id IS NULL AND transfer_pending_at IS NOT NULL
		FOR UPDATE`, orgID); err != nil {
		return c.organizationDBErr("fetching pending subscribers", err)
	}
	for _, source := range pending {
		var targetID int
		err := tx.Get(&targetID, `
			SELECT id FROM subscribers
			WHERE organization_id = $1 AND owner_user_id = $2 AND LOWER(email) = LOWER($3)
			LIMIT 1 FOR UPDATE`, orgID, targetUserID, source.Email)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.Exec(`UPDATE subscribers SET owner_user_id = $2, transfer_pending_at = NULL, updated_at = NOW() WHERE id = $1`, source.ID, targetUserID); err != nil {
				return c.organizationDBErr("transferring subscriber", err)
			}
			continue
		}
		if err != nil {
			return c.organizationDBErr("checking subscriber transfer conflict", err)
		}
		if err := c.mergeSubscriber(tx, source.ID, targetID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing organization resource transfer", err)
	}
	return nil
}

// TransferArchivedOrganizationResourcesToPersonal completes the archive
// lifecycle by moving every remaining pending resource into one active
// member's personal workspace. It intentionally preserves data relations
// (lists, subscribers, campaigns, templates, media, and CID associations),
// while converting organization-only visibility to private. Global templates
// detached during archive remain globally shared and are not included here.
func (c *Core) TransferArchivedOrganizationResourcesToPersonal(orgID, targetUserID int) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting archived organization transfer", err)
	}
	defer tx.Rollback()
	if err := c.lockOrganization(tx, orgID); err != nil {
		return err
	}

	var status string
	if err := tx.Get(&status, `SELECT status FROM organizations WHERE id = $1`, orgID); err != nil {
		return c.organizationDBErr("checking archived organization status", err)
	}
	if status != models.OrganizationStatusArchived {
		return echo.NewHTTPError(http.StatusConflict, "archive the organization before transferring its resources")
	}

	var memberExists bool
	if err := tx.Get(&memberExists, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL
		)`, orgID, targetUserID); err != nil {
		return c.organizationDBErr("checking archived organization transfer target", err)
	}
	if !memberExists {
		return ErrNotOrganizationMember
	}

	// Move lists before subscribers. If a scoped email collides with an
	// existing personal subscriber, the merge below then points every retained
	// subscription at the already moved list IDs.
	if _, err := tx.Exec(`
		UPDATE lists SET organization_id = NULL, owner_user_id = $2,
			visibility = 'private', transfer_pending_at = NULL, updated_at = NOW()
		WHERE organization_id = $1 AND transfer_pending_at IS NOT NULL`, orgID, targetUserID); err != nil {
		return c.organizationDBErr("moving archived organization lists", err)
	}
	if _, err := tx.Exec(`
		UPDATE campaigns SET organization_id = NULL, owner_user_id = $2,
			visibility = CASE WHEN visibility = 'global' THEN 'global' ELSE 'private' END,
			transfer_pending_at = NULL, updated_at = NOW()
		WHERE organization_id = $1 AND transfer_pending_at IS NOT NULL`, orgID, targetUserID); err != nil {
		return c.organizationDBErr("moving archived organization campaigns", err)
	}
	if _, err := tx.Exec(`
		UPDATE templates SET organization_id = NULL, owner_user_id = $2,
			visibility = CASE WHEN visibility = 'global' THEN 'global' ELSE 'private' END,
			is_default = FALSE, transfer_pending_at = NULL, updated_at = NOW()
		WHERE organization_id = $1 AND transfer_pending_at IS NOT NULL`, orgID, targetUserID); err != nil {
		return c.organizationDBErr("moving archived organization templates", err)
	}
	if _, err := tx.Exec(`
		UPDATE media SET organization_id = NULL, owner_user_id = $2,
			visibility = 'private', transfer_pending_at = NULL, updated_at = NOW()
		WHERE organization_id = $1 AND transfer_pending_at IS NOT NULL`, orgID, targetUserID); err != nil {
		return c.organizationDBErr("moving archived organization media", err)
	}

	type pendingSubscriber struct {
		ID    int    `db:"id"`
		Email string `db:"email"`
	}
	var pending []pendingSubscriber
	if err := tx.Select(&pending, `
		SELECT id, email FROM subscribers
		WHERE organization_id = $1 AND transfer_pending_at IS NOT NULL
		ORDER BY id
		FOR UPDATE`, orgID); err != nil {
		return c.organizationDBErr("fetching archived organization subscribers", err)
	}

	// A single organization can contain the same address under several owners.
	// Once all resources are moved to one personal owner, that address must be
	// merged rather than bulk-updated or the personal scoped-email index would
	// reject the transfer. Keep the first pending row for an address when no
	// personal row already exists, then merge every later row into it.
	targetsByEmail := make(map[string]int, len(pending))
	for _, source := range pending {
		email := strings.ToLower(strings.TrimSpace(source.Email))
		targetID, ok := targetsByEmail[email]
		if !ok {
			err := tx.Get(&targetID, `
				SELECT id FROM subscribers
				WHERE organization_id IS NULL AND owner_user_id = $1
					AND LOWER(email) = LOWER($2)
				LIMIT 1 FOR UPDATE`, targetUserID, source.Email)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return c.organizationDBErr("checking archived subscriber transfer conflict", err)
			}
			if errors.Is(err, sql.ErrNoRows) {
				if _, err := tx.Exec(`
					UPDATE subscribers SET organization_id = NULL, owner_user_id = $2,
						visibility = 'private', transfer_pending_at = NULL, updated_at = NOW()
					WHERE id = $1`, source.ID, targetUserID); err != nil {
					return c.organizationDBErr("moving archived organization subscriber", err)
				}
				targetID = source.ID
			} else if err := c.mergeSubscriber(tx, source.ID, targetID); err != nil {
				return err
			}
			targetsByEmail[email] = targetID
			continue
		}
		if source.ID != targetID {
			if err := c.mergeSubscriber(tx, source.ID, targetID); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing archived organization transfer", err)
	}
	return nil
}

// TransferOrganizationTemplate lets an organization manager hand an
// organization-shared template to another active member. Private and global
// templates deliberately do not use this path: their owners retain control.
func (c *Core) TransferOrganizationTemplate(orgID, templateID, targetUserID int) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting organization template transfer", err)
	}
	defer tx.Rollback()
	if err := c.lockOrganization(tx, orgID); err != nil {
		return err
	}
	var organizationStatus string
	if err := tx.Get(&organizationStatus, `SELECT status FROM organizations WHERE id = $1`, orgID); err != nil {
		return c.organizationDBErr("checking organization template transfer status", err)
	}
	if organizationStatus != models.OrganizationStatusActive {
		return echo.NewHTTPError(http.StatusConflict, "archived organization templates must be handled through the archive cleanup flow")
	}

	var targetRole string
	if err := tx.Get(&targetRole, `
		SELECT role FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL`, orgID, targetUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotOrganizationMember
		}
		return c.organizationDBErr("checking template transfer target", err)
	}

	var templateOwner null.Int
	if err := tx.Get(&templateOwner, `
		SELECT owner_user_id FROM templates
		WHERE id = $2 AND organization_id = $1
		  AND visibility = 'organization' AND transfer_pending_at IS NULL
		FOR UPDATE`, orgID, templateID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "organization-shared template not found")
		}
		return c.organizationDBErr("locking organization template", err)
	}

	// A template transfer is a complete ownership hand-off. Private media
	// referenced by the template must not remain owned by the departing author,
	// otherwise the new owner could see the template but could neither send it
	// nor edit its media references. Clone those binaries into the target
	// workspace while retaining the source rows for any other resources that
	// still use them. Organization-shared media can be reused directly.
	var refs []mediaAssociation
	if err := tx.Select(&refs, `
		SELECT media_id, filename FROM template_media
		WHERE template_id = $1 ORDER BY media_id NULLS LAST, filename
		FOR UPDATE`, templateID); err != nil {
		return c.organizationDBErr("locking organization template media", err)
	}
	mediaIDs := make([]int, 0, len(refs))
	for _, ref := range refs {
		if ref.MediaID.Valid {
			mediaIDs = append(mediaIDs, int(ref.MediaID.Int))
		}
	}
	mediaIDs = uniqueMutationIDs(mediaIDs)
	sort.Ints(mediaIDs)
	type mediaOwner struct {
		ID             int       `db:"id"`
		OrganizationID null.Int  `db:"organization_id"`
		OwnerUserID    null.Int  `db:"owner_user_id"`
		Visibility     string    `db:"visibility"`
		Pending        null.Time `db:"transfer_pending_at"`
	}
	owners := make(map[int]mediaOwner, len(mediaIDs))
	if len(mediaIDs) > 0 {
		var rows []mediaOwner
		if err := tx.Select(&rows, `
			SELECT id, organization_id, owner_user_id, visibility, transfer_pending_at
			FROM media WHERE id = ANY($1::INT[]) ORDER BY id FOR UPDATE`, pq.Array(mediaIDs)); err != nil {
			return c.organizationDBErr("locking organization template media rows", err)
		}
		if len(rows) != len(mediaIDs) {
			return workspaceMutationError()
		}
		for _, row := range rows {
			owners[row.ID] = row
		}
	}
	targetScope := ApplyWorkspaceScope(models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: orgID},
		UserID:    targetUserID,
	}, models.ResourceVisibilityPrivate)
	mediaCopies := make(map[int]int)
	for _, ref := range refs {
		if !ref.MediaID.Valid {
			continue
		}
		sourceID := int(ref.MediaID.Int)
		row, ok := owners[sourceID]
		if !ok {
			return workspaceMutationError()
		}
		// A shared organization binary is already usable by the target. A
		// private binary is reusable only when it is owned by that target in
		// this organization; all other cases receive an independent copy.
		keep := row.Visibility == models.ResourceVisibilityOrganization &&
			row.OrganizationID.Valid && row.OrganizationID.Int == orgID &&
			!row.Pending.Valid
		if row.OwnerUserID.Valid && row.OwnerUserID.Int == targetUserID &&
			row.OrganizationID.Valid && row.OrganizationID.Int == orgID &&
			!row.Pending.Valid {
			keep = true
		}
		if keep {
			continue
		}
		copyID, ok := mediaCopies[sourceID]
		if !ok {
			var copyErr error
			copyID, copyErr = cloneMediaRecord(tx, sourceID, targetScope)
			if copyErr != nil {
				return copyErr
			}
			mediaCopies[sourceID] = copyID
		}
		if _, err := tx.Exec(`
			UPDATE template_media SET media_id = $3
			WHERE template_id = $1 AND media_id = $2`, templateID, sourceID, copyID); err != nil {
			return c.organizationDBErr("copying organization template media", err)
		}
	}
	if err := c.rewriteTemplateMediaReferencesTx(tx, templateID, mediaCopies); err != nil {
		return err
	}

	res, err := tx.Exec(`
		UPDATE templates
		SET owner_user_id = $3, is_default = FALSE, transfer_pending_at = NULL, updated_at = NOW()
		WHERE id = $2 AND organization_id = $1
			AND visibility = 'organization' AND transfer_pending_at IS NULL`, orgID, templateID, targetUserID)
	if err != nil {
		return c.organizationDBErr("transferring organization template", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "organization-shared template not found")
	}
	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing organization template transfer", err)
	}
	return nil
}

// UnpublishOrganizationTemplate removes the organization-wide visibility of
// a shared template while leaving it with its current owner as a private
// template. This is intentionally unavailable for global templates.
func (c *Core) UnpublishOrganizationTemplate(orgID, templateID int) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting organization template unpublish", err)
	}
	defer tx.Rollback()
	if err := c.lockOrganization(tx, orgID); err != nil {
		return err
	}
	var status string
	if err := tx.Get(&status, `SELECT status FROM organizations WHERE id = $1`, orgID); err != nil {
		return c.organizationDBErr("checking organization template unpublish status", err)
	}
	if status != models.OrganizationStatusActive {
		return echo.NewHTTPError(http.StatusConflict, "archived organization templates cannot be unpublished here")
	}
	res, err := tx.Exec(`
		UPDATE templates SET visibility = 'private', updated_at = NOW()
		WHERE id = $2 AND organization_id = $1
			AND visibility = 'organization' AND transfer_pending_at IS NULL`, orgID, templateID)
	if err != nil {
		return c.organizationDBErr("unpublishing organization template", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "organization-shared template not found")
	}
	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing organization template unpublish", err)
	}
	return nil
}

// ArchiveOrganization freezes an organization before its resources are
// transferred or cleaned up. Scheduled work becomes draft work, and any
// in-flight campaign is paused so the caller can stop its active worker after
// this transaction commits.
func (c *Core) ArchiveOrganization(orgID int) ([]models.Campaign, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, c.organizationDBErr("starting organization archive", err)
	}
	defer tx.Rollback()
	if err := c.lockOrganization(tx, orgID); err != nil {
		return nil, err
	}

	// Global templates are platform-wide assets. Keep them usable by their
	// original creator even though the organization that once hosted them is
	// being archived. Their inline media receives independent personal records
	// so later archive cleanup cannot break CID/MIME rendering.
	var globalTemplateOwners []int
	if err := tx.Select(&globalTemplateOwners, `
		SELECT DISTINCT owner_user_id FROM templates
		WHERE organization_id = $1 AND visibility = 'global' AND owner_user_id IS NOT NULL
		ORDER BY owner_user_id`, orgID); err != nil {
		return nil, c.organizationDBErr("reading global template owners", err)
	}
	for _, ownerUserID := range globalTemplateOwners {
		if err := c.detachGlobalTemplatesForFormerMember(tx, orgID, ownerUserID); err != nil {
			return nil, err
		}
	}

	res, err := tx.Exec(`
		UPDATE organizations SET status = $2, archived_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status != $2`, orgID, models.OrganizationStatusArchived)
	if err != nil {
		return nil, c.organizationDBErr("archiving organization", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, echo.NewHTTPError(http.StatusConflict, "organization is already archived")
	}

	// Make every remaining organization resource an explicit cleanup item.
	// Members can no longer select the workspace; platform administration later
	// moves this coherent resource set to one member's personal workspace
	// before the organization metadata can be permanently deleted.
	for _, table := range []string{"lists", "subscribers", "templates", "media"} {
		stmt := fmt.Sprintf(`
			UPDATE %s SET owner_user_id = NULL,
				original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
				transfer_pending_at = NOW(), updated_at = NOW()
			WHERE organization_id = $1`, table)
		if _, err := tx.Exec(stmt, orgID); err != nil {
			return nil, c.organizationDBErr("preparing archived organization resources for transfer", err)
		}
	}
	if _, err := tx.Exec(`
		UPDATE campaign_recipients cr SET status = 'pending', updated_at = NOW()
		WHERE cr.status = 'queued'
		  AND EXISTS (
			SELECT 1 FROM campaigns c
			WHERE c.id = cr.campaign_id
			  AND c.organization_id = $1
		  )`, orgID); err != nil {
		return nil, c.organizationDBErr("resetting archived campaign recipients", err)
	}

	var stopped []models.Campaign
	if err := tx.Select(&stopped, `
		UPDATE campaigns SET
			owner_user_id = NULL,
			original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
			transfer_pending_at = NOW(),
			send_at = CASE WHEN status IN ('scheduled', 'deferred') THEN NULL ELSE send_at END,
			next_resume_at = CASE WHEN status = 'deferred' THEN NULL ELSE next_resume_at END,
			status = CASE WHEN status IN ('scheduled', 'deferred') THEN 'draft'::campaign_status
				WHEN status = 'running' THEN 'paused'::campaign_status ELSE status END,
			updated_at = NOW()
		WHERE organization_id = $1
		RETURNING id, status`, orgID); err != nil {
		return nil, c.organizationDBErr("stopping organization campaigns", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, c.organizationDBErr("committing organization archive", err)
	}
	return stopped, nil
}

// PurgeArchivedOrganization is the final step of the organization lifecycle.
// It intentionally refuses an active organization and any archived
// organization that still owns resources. The creation request remains as an
// audit record, with its foreign key cleared by the schema's ON DELETE SET
// NULL rule.
func (c *Core) PurgeArchivedOrganization(orgID int) error {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return c.organizationDBErr("starting organization purge", err)
	}
	defer tx.Rollback()
	if err := c.lockOrganization(tx, orgID); err != nil {
		return err
	}

	var status string
	if err := tx.Get(&status, `SELECT status FROM organizations WHERE id = $1 FOR UPDATE`, orgID); err != nil {
		return c.organizationDBErr("checking organization purge status", err)
	}
	if status != models.OrganizationStatusArchived {
		return echo.NewHTTPError(http.StatusConflict, "archive the organization before permanently deleting it")
	}

	for _, table := range []string{"lists", "subscribers", "templates", "campaigns", "media"} {
		var count int
		if err := tx.Get(&count, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE organization_id = $1", table), orgID); err != nil {
			return c.organizationDBErr("checking organization resources", err)
		}
		if count > 0 {
			return echo.NewHTTPError(http.StatusConflict, "transfer or clean all organization resources before permanently deleting it")
		}
	}

	if _, err := tx.Exec(`DELETE FROM organization_invites WHERE organization_id = $1`, orgID); err != nil {
		return c.organizationDBErr("removing organization invitations", err)
	}
	if _, err := tx.Exec(`DELETE FROM organization_members WHERE organization_id = $1`, orgID); err != nil {
		return c.organizationDBErr("removing organization members", err)
	}
	if _, err := tx.Exec(`DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
		return c.organizationDBErr("permanently deleting organization", err)
	}
	if err := tx.Commit(); err != nil {
		return c.organizationDBErr("committing organization purge", err)
	}
	return nil
}

// ClaimUnownedResources assigns resources created during first-time setup to
// the initial system administrator. This is needed on fresh installs because
// seed data is installed before the first user record exists.
func (c *Core) ClaimUnownedResources(userID int) error {
	if userID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid resource owner")
	}

	for _, table := range []string{"lists", "subscribers", "templates", "campaigns", "media"} {
		stmt := fmt.Sprintf(`
			UPDATE %s
			SET owner_user_id = $1, original_owner_user_id = $1
			WHERE owner_user_id IS NULL AND organization_id IS NULL`, table)
		if _, err := c.db.Exec(stmt, userID); err != nil {
			return c.organizationDBErr("claiming initial resources", err)
		}
	}

	return nil
}

func (c *Core) countOrganizationManagers(q sqlx.QueryerContext, orgID int) (int, error) {
	var count int
	if err := sqlx.GetContext(context.Background(), q, &count, `
		SELECT COUNT(*) FROM organization_members
		WHERE organization_id = $1 AND role = $2 AND removed_at IS NULL`, orgID, models.OrganizationMemberRoleManager); err != nil {
		return 0, c.organizationDBErr("counting organization managers", err)
	}
	return count, nil
}

// lockOrganization serializes role changes and removals for one organization.
// Without this lock, two concurrent manager removals could both observe two
// managers and leave an organization with none.
func (c *Core) lockOrganization(tx *sqlx.Tx, orgID int) error {
	var id int
	if err := tx.Get(&id, `SELECT id FROM organizations WHERE id = $1 FOR UPDATE`, orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOrganizationNotFound
		}
		return c.organizationDBErr("locking organization", err)
	}
	return nil
}

// lockActiveOrganization is used by membership and invitation mutations.
// The status check happens while the organization row is locked, closing the
// race where an HTTP request is authorized against an active workspace and an
// archive commits before its INSERT/UPDATE acquires the lock.
func (c *Core) lockActiveOrganization(tx *sqlx.Tx, orgID int) error {
	var status string
	if err := tx.Get(&status, `SELECT status FROM organizations WHERE id = $1 FOR UPDATE`, orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOrganizationNotFound
		}
		return c.organizationDBErr("locking active organization", err)
	}
	if status != models.OrganizationStatusActive {
		return echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	return nil
}

func (c *Core) mergeSubscriber(tx *sqlx.Tx, sourceID, targetID int) error {
	if sourceID == targetID {
		return nil
	}
	// Campaign messages retain the UUID that was present when they were sent.
	// Store the source UUID and any earlier aliases before deleting the source
	// record so historical message, click, view, and unsubscribe links resolve.
	if _, err := tx.Exec(`
		UPDATE subscriber_uuid_aliases
		SET subscriber_id = $2
		WHERE subscriber_id = $1`, sourceID, targetID); err != nil {
		return c.organizationDBErr("preserving subscriber UUID aliases", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO subscriber_uuid_aliases (uuid, subscriber_id)
		SELECT uuid, $2 FROM subscribers WHERE id = $1
		ON CONFLICT (uuid) DO UPDATE SET subscriber_id = EXCLUDED.subscriber_id`, sourceID, targetID); err != nil {
		return c.organizationDBErr("preserving subscriber UUID alias", err)
	}
	if err := c.mergeSubscriberProfile(tx, sourceID, targetID); err != nil {
		return err
	}
	// Preserve the strongest subscription status when the same pair exists.
	if _, err := tx.Exec(`
		INSERT INTO subscriber_lists (subscriber_id, list_id, status, meta, created_at, updated_at)
		SELECT $2, list_id, status, meta, created_at, updated_at
		FROM subscriber_lists WHERE subscriber_id = $1
		ON CONFLICT (subscriber_id, list_id) DO UPDATE SET
			status = CASE
				WHEN subscriber_lists.status = 'confirmed' OR EXCLUDED.status = 'confirmed' THEN 'confirmed'::subscription_status
				WHEN subscriber_lists.status = 'unconfirmed' OR EXCLUDED.status = 'unconfirmed' THEN 'unconfirmed'::subscription_status
				ELSE 'unsubscribed'::subscription_status END,
			meta = subscriber_lists.meta || EXCLUDED.meta,
			updated_at = NOW()`, sourceID, targetID); err != nil {
		return c.organizationDBErr("merging subscriber lists", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM campaign_recipients source
		USING campaign_recipients target
		WHERE source.subscriber_id = $1 AND target.subscriber_id = $2
			AND source.campaign_id = target.campaign_id`, sourceID, targetID); err != nil {
		return c.organizationDBErr("merging campaign recipients", err)
	}
	for _, table := range []string{"campaign_recipients", "campaign_views", "link_clicks", "bounces"} {
		stmt := fmt.Sprintf(`UPDATE %s SET subscriber_id = $2 WHERE subscriber_id = $1`, table)
		if _, err := tx.Exec(stmt, sourceID, targetID); err != nil {
			return c.organizationDBErr("merging subscriber history", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM subscribers WHERE id = $1`, sourceID); err != nil {
		return c.organizationDBErr("removing merged subscriber", err)
	}
	return nil
}

// mergeSubscriberProfile preserves the useful parts of a source profile before
// its scoped subscriptions and history are merged into another record. The
// target keeps explicit values, while a blocklist state always wins.
func (c *Core) mergeSubscriberProfile(tx *sqlx.Tx, sourceID, targetID int) error {
	if sourceID == targetID {
		return nil
	}
	if _, err := tx.Exec(`
		UPDATE subscribers AS target
		SET status = CASE
				WHEN target.status = 'blocklisted' OR source.status = 'blocklisted'
					THEN 'blocklisted'::subscriber_status
				ELSE target.status
			END,
			name = CASE WHEN target.name = '' AND source.name <> '' THEN source.name ELSE target.name END,
			attribs = source.attribs || target.attribs,
			updated_at = GREATEST(target.updated_at, source.updated_at)
		FROM subscribers AS source
		WHERE source.id = $1 AND target.id = $2`, sourceID, targetID); err != nil {
		return c.organizationDBErr("merging subscriber profiles", err)
	}
	return nil
}

// detachGlobalTemplatesForFormerMember preserves globally shared templates
// when their creator leaves an organization. Inline media is cloned as a
// separate media record in the creator's personal space; the physical object
// can remain shared because DeleteMedia keeps it until every record is gone.
func (c *Core) detachGlobalTemplatesForFormerMember(tx *sqlx.Tx, orgID, userID int) error {
	type templateMediaRef struct {
		TemplateID int      `db:"template_id"`
		MediaID    null.Int `db:"media_id"`
	}

	var refs []templateMediaRef
	if err := tx.Select(&refs, `
		SELECT tm.template_id, tm.media_id
		FROM template_media tm
		JOIN templates t ON t.id = tm.template_id
		WHERE t.organization_id = $1 AND t.owner_user_id = $2
			AND t.visibility = 'global' AND tm.media_id IS NOT NULL
		FOR UPDATE OF tm, t`, orgID, userID); err != nil {
		return c.organizationDBErr("reading global template media", err)
	}

	target := ApplyWorkspaceScope(models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    userID,
	}, models.ResourceVisibilityPrivate)
	copies := make(map[int]int)
	templateIDs := make(map[int]struct{})
	for _, ref := range refs {
		sourceID := int(ref.MediaID.Int)
		copyID, ok := copies[sourceID]
		if !ok {
			var err error
			copyID, err = cloneMediaRecord(tx, sourceID, target)
			if err != nil {
				return err
			}
			copies[sourceID] = copyID
		}
		if _, err := tx.Exec(`
			UPDATE template_media SET media_id = $3
			WHERE template_id = $1 AND media_id = $2`, ref.TemplateID, sourceID, copyID); err != nil {
			return c.organizationDBErr("copying global template media", err)
		}
		templateIDs[ref.TemplateID] = struct{}{}
	}
	for templateID := range templateIDs {
		if err := c.rewriteTemplateMediaReferencesTx(tx, templateID, copies); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
		UPDATE templates SET organization_id = NULL, transfer_pending_at = NULL,
			is_default = FALSE, updated_at = NOW()
		WHERE organization_id = $1 AND owner_user_id = $2 AND visibility = 'global'`, orgID, userID); err != nil {
		return c.organizationDBErr("preserving global templates", err)
	}
	return nil
}

func (c *Core) organizationDBErr(action string, err error) error {
	return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("error %s: %s", action, pqErrMsg(err)))
}

// organizationNameConflictError converts the two case-insensitive name
// uniqueness violations into a stable 409 response.  The preflight checks in
// the request/review flows are useful for friendly errors, but they cannot
// close the race with a concurrent request or approval; PostgreSQL remains
// the final arbiter and must not leak that race as a generic HTTP 500.
func organizationNameConflictError(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
		return nil
	}
	switch pqErr.Constraint {
	case "idx_organizations_name_lower", "idx_organization_join_requests_pending_name":
		return echo.NewHTTPError(http.StatusConflict, "organization name is already in use")
	default:
		return nil
	}
}
