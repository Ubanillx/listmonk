package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/types"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

type migratedListSubscription struct {
	CustomerListID     int             `db:"customer_list_id"`
	CustomerID         int             `db:"customer_id"`
	Email              string          `db:"email"`
	Name               string          `db:"name"`
	Attribs            types.JSONText  `db:"attribs"`
	CustomerStatus     string          `db:"customer_status"`
	SubscriptionStatus string          `db:"subscription_status"`
	Meta               json.RawMessage `db:"meta"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}

const (
	personalMigrationResourceLists     = "customer_lists"
	personalMigrationResourceTemplates = "templates"
	personalMigrationResourceCampaigns = "campaigns"
	personalMigrationResourceMedia     = "media"
)

// MigratePersonalResourcesToOrganization is the common server-side entry
// point for copying or moving personal resources into an organization. CustomerLists
// include their customers; templates and campaigns take binary-media
// snapshots so CID/MIME images never keep a hidden dependency on a personal
// workspace after migration.
func (c *Core) MigratePersonalResourcesToOrganization(sourceUserID, targetOrganizationID, targetUserID int, resource string, sourceIDs []int, move bool) ([]int, error) {
	if sourceUserID < 1 || targetOrganizationID < 1 || targetUserID < 1 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid personal resource migration workspace")
	}
	if sourceUserID != targetUserID {
		return nil, echo.NewHTTPError(http.StatusForbidden, "personal resources can only be migrated by their owner")
	}

	switch resource {
	case personalMigrationResourceLists:
		return c.MigratePersonalListsToOrganization(sourceUserID, targetOrganizationID, targetUserID, sourceIDs, move)
	case personalMigrationResourceTemplates:
		return c.migratePersonalTemplatesToOrganization(sourceUserID, targetOrganizationID, targetUserID, sourceIDs, move)
	case personalMigrationResourceCampaigns:
		return c.migratePersonalCampaignsToOrganization(sourceUserID, targetOrganizationID, targetUserID, sourceIDs, move)
	case personalMigrationResourceMedia:
		return c.migratePersonalMediaToOrganization(sourceUserID, targetOrganizationID, targetUserID, sourceIDs, move)
	default:
		return nil, echo.NewHTTPError(http.StatusBadRequest, "unsupported personal resource type")
	}
}

// MigratePersonalListsToOrganization copies or moves caller-owned personal
// customer_lists into an organization. Associated customers are copied into the
// destination owner scope and merged by e-mail, preventing cross-workspace
// customer_links even when a source customer belongs to other personal
// customer_lists that are not being migrated.
func (c *Core) MigratePersonalListsToOrganization(sourceUserID, targetOrganizationID, targetUserID int, sourceCustomerListIDs []int, move bool) ([]int, error) {
	if sourceUserID < 1 || targetOrganizationID < 1 || targetUserID < 1 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid customer_list migration workspace")
	}
	if sourceUserID != targetUserID {
		return nil, echo.NewHTTPError(http.StatusForbidden, "personal resources can only be migrated by their owner")
	}
	sourceCustomerListIDs = uniquePositiveIDs(sourceCustomerListIDs)
	if len(sourceCustomerListIDs) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "at least one personal customer_list is required")
	}

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, workspaceQueryError("starting customer_list migration", err)
	}
	defer tx.Rollback()
	if err := c.lockPersonalMigrationTarget(tx, targetOrganizationID, targetUserID); err != nil {
		return nil, err
	}

	var customer_lists []models.CustomerList
	if err := tx.Select(&customer_lists, `
		SELECT * FROM customer_lists
		WHERE id = ANY($1::INT[]) AND organization_id IS NULL
			AND owner_user_id = $2 AND transfer_pending_at IS NULL
		ORDER BY id FOR UPDATE`, pq.Array(sourceCustomerListIDs), sourceUserID); err != nil {
		return nil, workspaceQueryError("reading personal customer_lists", err)
	}
	if len(customer_lists) != len(sourceCustomerListIDs) {
		return nil, echo.NewHTTPError(http.StatusForbidden, "one or more customer_lists are outside the personal workspace")
	}

	listMap := make(map[int]int, len(customer_lists))
	targetCustomerListIDs := make([]int, 0, len(customer_lists))
	for _, source := range customer_lists {
		targetID := source.ID
		// CustomerLists are always owner-private. This also repairs records created
		// before the server-side visibility validation was introduced.
		visibility := models.ResourceVisibilityPrivate
		if move {
			// campaign_customer_lists deliberately retains customer_list_name as history when a
			// customer_list changes workspace. Disconnect only relationships belonging
			// to a different owner/workspace; otherwise a stale personal
			// campaign could keep sending to a customer_list after it was moved.
			if _, err := tx.Exec(`
				UPDATE campaign_customer_lists cl SET customer_list_id = NULL
				FROM campaigns c
				WHERE cl.customer_list_id = $1 AND c.id = cl.campaign_id
					AND (
						c.organization_id IS DISTINCT FROM $2::BIGINT
						OR c.owner_user_id IS DISTINCT FROM $3
						OR c.transfer_pending_at IS NOT NULL
					)`, source.ID, targetOrganizationID, targetUserID); err != nil {
				return nil, workspaceQueryError("disconnecting migrated customer_list from campaigns", err)
			}
			if _, err := tx.Exec(`
				UPDATE customer_lists SET organization_id = $2, owner_user_id = $3,
					original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
					visibility = $4, transfer_pending_at = NULL, updated_at = NOW()
				WHERE id = $1`, source.ID, targetOrganizationID, targetUserID, visibility); err != nil {
				return nil, workspaceQueryError("moving customer_list", err)
			}
		} else {
			listUUID, err := uuid.NewV4()
			if err != nil {
				return nil, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
			}
			if err := tx.Get(&targetID, `
				INSERT INTO customer_lists (
					uuid, name, type, optin, status, tags, description,
					organization_id, owner_user_id, original_owner_user_id, visibility
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				RETURNING id`, listUUID, source.Name, source.Type, source.Optin, source.Status,
				source.Tags, source.Description, targetOrganizationID, targetUserID,
				targetUserID, visibility); err != nil {
				return nil, workspaceQueryError("copying customer_list", err)
			}
		}
		listMap[source.ID] = targetID
		targetCustomerListIDs = append(targetCustomerListIDs, targetID)
	}

	var subscriptions []migratedListSubscription
	if err := tx.Select(&subscriptions, `
		SELECT sl.customer_list_id, s.id AS customer_id, s.email, s.name, s.attribs,
			s.status AS customer_status, sl.status AS subscription_status,
			sl.meta, sl.created_at, sl.updated_at
		FROM customer_list_memberships sl
		JOIN customers s ON s.id = sl.customer_id
		WHERE sl.customer_list_id = ANY($1::INT[]) AND s.organization_id IS NULL
			AND s.owner_user_id = $2 AND s.transfer_pending_at IS NULL
		ORDER BY sl.customer_list_id, s.id
		FOR UPDATE OF sl, s`, pq.Array(sourceCustomerListIDs), sourceUserID); err != nil {
		return nil, workspaceQueryError("reading customer_list customers", err)
	}

	for _, subscription := range subscriptions {
		targetCustomerID, err := c.findOrCopyMigratedCustomer(tx, subscription, targetOrganizationID, targetUserID)
		if err != nil {
			return nil, err
		}
		if err := upsertMigratedSubscription(tx, targetCustomerID, listMap[subscription.CustomerListID], subscription); err != nil {
			return nil, err
		}
		if move {
			// The source customer may remain personal to preserve other links
			// and delivery history, but it must never retain a link to the customer_list
			// that was moved into the organization. Keeping that stale link can
			// duplicate customer_list statistics and bypass the workspace association
			// invariant.
			if _, err := tx.Exec(`DELETE FROM customer_list_memberships WHERE customer_id = $1 AND customer_list_id = $2`, subscription.CustomerID, subscription.CustomerListID); err != nil {
				return nil, workspaceQueryError("removing moved customer_list subscription", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, workspaceQueryError("committing customer_list migration", err)
	}
	return targetCustomerListIDs, nil
}

// lockPersonalMigrationResources verifies and locks a complete resource set
// before it is copied or moved. Global resources are intentionally excluded:
// they are platform-visible assets, not personal workspace resources, and a
// migration must not silently change their publication contract.
func (c *Core) lockPersonalMigrationResources(tx *sqlx.Tx, table string, sourceUserID int, sourceIDs []int) ([]int, error) {
	if table != "templates" && table != "campaigns" && table != "media" {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "unsupported migration resource table")
	}
	sourceIDs = uniquePositiveIDs(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "at least one personal resource is required")
	}

	var locked []int
	stmt := fmt.Sprintf(`
		SELECT id FROM %s
		WHERE id = ANY($1::INT[]) AND organization_id IS NULL
			AND owner_user_id = $2 AND visibility = 'private'
			AND transfer_pending_at IS NULL
		ORDER BY id FOR UPDATE`, table)
	if err := tx.Select(&locked, stmt, pq.Array(sourceIDs), sourceUserID); err != nil {
		return nil, workspaceQueryError("reading personal resources", err)
	}
	if len(locked) != len(sourceIDs) {
		return nil, echo.NewHTTPError(http.StatusForbidden, "one or more resources are outside the personal workspace")
	}
	return sourceIDs, nil
}

func personalMigrationTargetScope(organizationID, userID int) models.ResourceScope {
	return ApplyWorkspaceScope(models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: organizationID},
		UserID:    userID,
	}, models.ResourceVisibilityPrivate)
}

// lockPersonalMigrationTarget serializes a copy/move with organization
// archival and membership removal. The HTTP layer performs the same check for
// a friendly early response, but this transaction-level check is authoritative
// for the actual INSERT/UPDATE.
func (c *Core) lockPersonalMigrationTarget(tx *sqlx.Tx, organizationID, userID int) error {
	if organizationID < 1 || userID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid organization migration target")
	}
	statuses, err := c.lockWorkspaceOrganizations(tx, []int{organizationID})
	if err != nil {
		return err
	}
	if statuses[organizationID] != models.OrganizationStatusActive ||
		!c.workspaceMembershipActive(tx, organizationID, userID) {
		return workspaceMutationError()
	}
	return nil
}

func (c *Core) migratePersonalTemplatesToOrganization(sourceUserID, targetOrganizationID, targetUserID int, sourceIDs []int, move bool) ([]int, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, workspaceQueryError("starting template migration", err)
	}
	defer tx.Rollback()
	if err := c.lockPersonalMigrationTarget(tx, targetOrganizationID, targetUserID); err != nil {
		return nil, err
	}

	sourceIDs, err = c.lockPersonalMigrationResources(tx, "templates", sourceUserID, sourceIDs)
	if err != nil {
		return nil, err
	}
	if move {
		var references int
		if err := tx.Get(&references, `
			SELECT COUNT(*) FROM campaigns
			WHERE organization_id IS NULL AND owner_user_id = $2
				AND (template_id = ANY($1::INT[]) OR archive_template_id = ANY($1::INT[]))`,
			pq.Array(sourceIDs), sourceUserID); err != nil {
			return nil, workspaceQueryError("checking template references", err)
		}
		if references > 0 {
			return nil, echo.NewHTTPError(http.StatusConflict,
				"a template used by a personal campaign cannot be moved; copy it or migrate the campaign instead")
		}
	}

	target := personalMigrationTargetScope(targetOrganizationID, targetUserID)
	mediaCopies := make(map[int]int)
	result := make([]int, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		targetID := sourceID
		if move {
			if _, err := tx.Exec(`
				UPDATE templates SET organization_id = $2, owner_user_id = $3,
					original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
					visibility = 'private', is_default = FALSE, transfer_pending_at = NULL,
					updated_at = NOW()
				WHERE id = $1`, sourceID, targetOrganizationID, targetUserID); err != nil {
				return nil, workspaceQueryError("moving template", err)
			}
		} else if err := tx.Get(&targetID, `
			INSERT INTO templates (
				name, type, subject, body, body_source, name_fallback, is_default,
				organization_id, owner_user_id, original_owner_user_id, visibility
			)
			SELECT name, type, subject, body, body_source, name_fallback, FALSE,
				$2, $3, $4, 'private'
			FROM templates WHERE id = $1
			RETURNING id`, sourceID, target.OrganizationID, target.OwnerUserID, target.OriginalOwnerUserID); err != nil {
			return nil, workspaceQueryError("copying template", err)
		}

		if err := c.copyTemplateMigrationMedia(tx, sourceID, targetID, target, move, mediaCopies); err != nil {
			return nil, err
		}
		if err := c.rewriteTemplateMediaReferencesTx(tx, targetID, mediaCopies); err != nil {
			return nil, err
		}
		result = append(result, targetID)
	}

	if err := tx.Commit(); err != nil {
		return nil, workspaceQueryError("committing template migration", err)
	}
	return result, nil
}

func (c *Core) copyTemplateMigrationMedia(tx *sqlx.Tx, sourceTemplateID, targetTemplateID int, target models.ResourceScope, move bool, mediaCopies map[int]int) error {
	var refs []mediaAssociation
	if err := tx.Select(&refs, `
		SELECT media_id, filename FROM template_media
		WHERE template_id = $1
		ORDER BY media_id NULLS LAST, filename
		FOR UPDATE`, sourceTemplateID); err != nil {
		return workspaceQueryError("reading template media for migration", err)
	}
	if err := c.lockCloneMediaRows(tx, refs); err != nil {
		return err
	}
	for _, ref := range refs {
		if !ref.MediaID.Valid {
			if !move {
				if err := insertMediaAssociation(tx, "template_media", "template_id", targetTemplateID, ref, mediaCopies); err != nil {
					return err
				}
			}
			continue
		}

		sourceMediaID := int(ref.MediaID.Int)
		copyID, ok := mediaCopies[sourceMediaID]
		if !ok {
			var err error
			copyID, err = cloneMediaRecord(tx, sourceMediaID, target)
			if err != nil {
				return err
			}
			mediaCopies[sourceMediaID] = copyID
		}
		if move {
			if _, err := tx.Exec(`
				UPDATE template_media SET media_id = $3
				WHERE template_id = $1 AND media_id = $2`, targetTemplateID, sourceMediaID, copyID); err != nil {
				return workspaceQueryError("moving template media", err)
			}
			continue
		}
		if err := insertMediaAssociation(tx, "template_media", "template_id", targetTemplateID, ref, mediaCopies); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) migratePersonalMediaToOrganization(sourceUserID, targetOrganizationID, targetUserID int, sourceIDs []int, move bool) ([]int, error) {
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, workspaceQueryError("starting media migration", err)
	}
	defer tx.Rollback()
	if err := c.lockPersonalMigrationTarget(tx, targetOrganizationID, targetUserID); err != nil {
		return nil, err
	}

	sourceIDs, err = c.lockPersonalMigrationResources(tx, "media", sourceUserID, sourceIDs)
	if err != nil {
		return nil, err
	}
	if move {
		var referenced bool
		if err := tx.Get(&referenced, `
			SELECT EXISTS(
				SELECT 1 FROM campaign_media WHERE media_id = ANY($1::INT[])
				UNION ALL
				SELECT 1 FROM template_media WHERE media_id = ANY($1::INT[])
			)`, pq.Array(sourceIDs)); err != nil {
			return nil, workspaceQueryError("checking media references", err)
		}
		if referenced {
			return nil, echo.NewHTTPError(http.StatusConflict,
				"media referenced by a template or campaign cannot be moved; copy it to preserve existing MIME/CID content")
		}
		if _, err := tx.Exec(`
			UPDATE media SET organization_id = $2, owner_user_id = $3,
				original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
				folder_id = NULL,
				visibility = 'private', transfer_pending_at = NULL, updated_at = NOW()
			WHERE id = ANY($1::INT[])`, pq.Array(sourceIDs), targetOrganizationID, targetUserID); err != nil {
			return nil, workspaceQueryError("moving media", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, workspaceQueryError("committing media migration", err)
		}
		return sourceIDs, nil
	}

	target := personalMigrationTargetScope(targetOrganizationID, targetUserID)
	result := make([]int, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		copyID, err := cloneMediaRecord(tx, sourceID, target)
		if err != nil {
			return nil, err
		}
		result = append(result, copyID)
	}
	if err := tx.Commit(); err != nil {
		return nil, workspaceQueryError("committing media migration", err)
	}
	return result, nil
}

func (c *Core) migratePersonalCampaignsToOrganization(sourceUserID, targetOrganizationID, targetUserID int, sourceIDs []int, move bool) ([]int, error) {
	// CloneCampaignForWorkspace owns its own transaction so it can snapshot the
	// template and binary media atomically. Validate all sources first, then
	// delete only an untouched draft after its destination clone succeeds.
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, workspaceQueryError("starting campaign migration", err)
	}
	if err := c.lockPersonalMigrationTarget(tx, targetOrganizationID, targetUserID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	sourceIDs, err = c.lockPersonalMigrationResources(tx, "campaigns", sourceUserID, sourceIDs)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if move {
		var nonDraftOrStarted int
		if err := tx.Get(&nonDraftOrStarted, `
			SELECT COUNT(*) FROM campaigns c
			WHERE c.id = ANY($1::INT[])
				AND (c.status <> 'draft' OR EXISTS (
					SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id
				))`, pq.Array(sourceIDs)); err != nil {
			tx.Rollback()
			return nil, workspaceQueryError("checking campaign migration state", err)
		}
		if nonDraftOrStarted > 0 {
			tx.Rollback()
			return nil, echo.NewHTTPError(http.StatusConflict,
				"only unsent draft campaigns can be moved; copy sent or scheduled campaigns instead")
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, workspaceQueryError("committing campaign migration validation", err)
	}

	target := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: targetOrganizationID},
		UserID:    targetUserID,
	}
	result := make([]int, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceAccess := models.WorkspaceAccess{
			Workspace: models.Workspace{Personal: true},
			UserID:    sourceUserID,
		}
		clone, err := c.CloneCampaignForWorkspaceWithSource(sourceID, sourceAccess, target, "")
		if err != nil {
			return nil, err
		}
		if move {
			res, err := c.db.Exec(`
				DELETE FROM campaigns c
				WHERE c.id = $1 AND c.organization_id IS NULL
					AND c.owner_user_id = $2 AND c.visibility = 'private'
					AND c.status = 'draft'
					AND NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)`, sourceID, sourceUserID)
			if err != nil {
				return nil, workspaceQueryError("moving campaign", err)
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return nil, echo.NewHTTPError(http.StatusConflict,
					"the campaign changed while it was being moved; the destination copy was retained")
			}
		}
		result = append(result, clone.ID)
	}
	return result, nil
}

func (c *Core) findOrCopyMigratedCustomer(tx *sqlx.Tx, source migratedListSubscription, organizationID, ownerUserID int) (int, error) {
	var targetID int
	err := tx.Get(&targetID, `
		SELECT id FROM customers
		WHERE organization_id = $1 AND owner_user_id = $2
			AND LOWER(email) = LOWER($3) AND transfer_pending_at IS NULL
		LIMIT 1 FOR UPDATE`, organizationID, ownerUserID, source.Email)
	if err == nil {
		if err := c.mergeCustomerProfile(tx, source.CustomerID, targetID); err != nil {
			return 0, err
		}
		return targetID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, workspaceQueryError("checking migrated customer", err)
	}

	customerUUID, err := uuid.NewV4()
	if err != nil {
		return 0, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if err := tx.Get(&targetID, `
		INSERT INTO customers (
			uuid, email, name, attribs, status,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'private')
		RETURNING id`, customerUUID, strings.ToLower(strings.TrimSpace(source.Email)), source.Name,
		source.Attribs, source.CustomerStatus, organizationID, ownerUserID, ownerUserID); err != nil {
		return 0, workspaceQueryError("copying customer_list customer", err)
	}
	return targetID, nil
}

func upsertMigratedSubscription(tx *sqlx.Tx, customerID, customerListID int, source migratedListSubscription) error {
	if _, err := tx.Exec(`
		INSERT INTO customer_list_memberships (customer_id, customer_list_id, status, meta, created_at, updated_at)
		VALUES ($1, $2, $3::subscription_status, $4, $5, $6)
		ON CONFLICT (customer_id, customer_list_id) DO UPDATE SET
			status = CASE
				WHEN customer_list_memberships.status = 'confirmed' OR EXCLUDED.status = 'confirmed' THEN 'confirmed'::subscription_status
				WHEN customer_list_memberships.status = 'unconfirmed' OR EXCLUDED.status = 'unconfirmed' THEN 'unconfirmed'::subscription_status
				ELSE 'unsubscribed'::subscription_status END,
			meta = customer_list_memberships.meta || EXCLUDED.meta,
			updated_at = NOW()`, customerID, customerListID, source.SubscriptionStatus, source.Meta, source.CreatedAt, source.UpdatedAt); err != nil {
		return workspaceQueryError("copying customer_list subscription", err)
	}
	return nil
}

func uniquePositiveIDs(ids []int) []int {
	out := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id < 1 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
