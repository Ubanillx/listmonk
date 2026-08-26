package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	ListID             int             `db:"list_id"`
	SubscriberID       int             `db:"subscriber_id"`
	Email              string          `db:"email"`
	Name               string          `db:"name"`
	Attribs            types.JSONText  `db:"attribs"`
	SubscriberStatus   string          `db:"subscriber_status"`
	SubscriptionStatus string          `db:"subscription_status"`
	Meta               json.RawMessage `db:"meta"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}

// MigratePersonalListsToOrganization copies or moves caller-owned personal
// lists into an organization. Associated subscribers are copied into the
// destination owner scope and merged by e-mail, preventing cross-workspace
// subscriber_links even when a source subscriber belongs to other personal
// lists that are not being migrated.
func (c *Core) MigratePersonalListsToOrganization(sourceUserID, targetOrganizationID, targetUserID int, sourceListIDs []int, move bool) ([]int, error) {
	if sourceUserID < 1 || targetOrganizationID < 1 || targetUserID < 1 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid list migration workspace")
	}
	if sourceUserID != targetUserID {
		return nil, echo.NewHTTPError(http.StatusForbidden, "personal resources can only be migrated by their owner")
	}
	sourceListIDs = uniquePositiveIDs(sourceListIDs)
	if len(sourceListIDs) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "at least one personal list is required")
	}

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, workspaceQueryError("starting list migration", err)
	}
	defer tx.Rollback()

	var lists []models.List
	if err := tx.Select(&lists, `
		SELECT * FROM lists
		WHERE id = ANY($1::INT[]) AND organization_id IS NULL
			AND owner_user_id = $2 AND transfer_pending_at IS NULL
		FOR UPDATE`, pq.Array(sourceListIDs), sourceUserID); err != nil {
		return nil, workspaceQueryError("reading personal lists", err)
	}
	if len(lists) != len(sourceListIDs) {
		return nil, echo.NewHTTPError(http.StatusForbidden, "one or more lists are outside the personal workspace")
	}

	listMap := make(map[int]int, len(lists))
	targetListIDs := make([]int, 0, len(lists))
	for _, source := range lists {
		targetID := source.ID
		// Lists are always owner-private. This also repairs records created
		// before the server-side visibility validation was introduced.
		visibility := models.ResourceVisibilityPrivate
		if move {
			if _, err := tx.Exec(`
				UPDATE lists SET organization_id = $2, owner_user_id = $3,
					original_owner_user_id = COALESCE(original_owner_user_id, owner_user_id),
					visibility = $4, transfer_pending_at = NULL, updated_at = NOW()
				WHERE id = $1`, source.ID, targetOrganizationID, targetUserID, visibility); err != nil {
				return nil, workspaceQueryError("moving list", err)
			}
		} else {
			listUUID, err := uuid.NewV4()
			if err != nil {
				return nil, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
			}
			if err := tx.Get(&targetID, `
				INSERT INTO lists (
					uuid, name, type, optin, status, tags, description,
					organization_id, owner_user_id, original_owner_user_id, visibility
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				RETURNING id`, listUUID, source.Name, source.Type, source.Optin, source.Status,
				source.Tags, source.Description, targetOrganizationID, targetUserID,
				targetUserID, visibility); err != nil {
				return nil, workspaceQueryError("copying list", err)
			}
		}
		listMap[source.ID] = targetID
		targetListIDs = append(targetListIDs, targetID)
	}

	var subscriptions []migratedListSubscription
	if err := tx.Select(&subscriptions, `
		SELECT sl.list_id, s.id AS subscriber_id, s.email, s.name, s.attribs,
			s.status AS subscriber_status, sl.status AS subscription_status,
			sl.meta, sl.created_at, sl.updated_at
		FROM subscriber_lists sl
		JOIN subscribers s ON s.id = sl.subscriber_id
		WHERE sl.list_id = ANY($1::INT[]) AND s.organization_id IS NULL
			AND s.owner_user_id = $2 AND s.transfer_pending_at IS NULL
		FOR UPDATE OF sl, s`, pq.Array(sourceListIDs), sourceUserID); err != nil {
		return nil, workspaceQueryError("reading list subscribers", err)
	}

	for _, subscription := range subscriptions {
		targetSubscriberID, err := c.findOrCopyMigratedSubscriber(tx, subscription, targetOrganizationID, targetUserID)
		if err != nil {
			return nil, err
		}
		if err := upsertMigratedSubscription(tx, targetSubscriberID, listMap[subscription.ListID], subscription); err != nil {
			return nil, err
		}
		if move && targetSubscriberID != subscription.SubscriberID {
			if _, err := tx.Exec(`DELETE FROM subscriber_lists WHERE subscriber_id = $1 AND list_id = $2`, subscription.SubscriberID, subscription.ListID); err != nil {
				return nil, workspaceQueryError("removing moved list subscription", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, workspaceQueryError("committing list migration", err)
	}
	return targetListIDs, nil
}

func (c *Core) findOrCopyMigratedSubscriber(tx *sqlx.Tx, source migratedListSubscription, organizationID, ownerUserID int) (int, error) {
	var targetID int
	err := tx.Get(&targetID, `
		SELECT id FROM subscribers
		WHERE organization_id = $1 AND owner_user_id = $2
			AND LOWER(email) = LOWER($3) AND transfer_pending_at IS NULL
		LIMIT 1 FOR UPDATE`, organizationID, ownerUserID, source.Email)
	if err == nil {
		return targetID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, workspaceQueryError("checking migrated subscriber", err)
	}

	subscriberUUID, err := uuid.NewV4()
	if err != nil {
		return 0, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if err := tx.Get(&targetID, `
		INSERT INTO subscribers (
			uuid, email, name, attribs, status,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'private')
		RETURNING id`, subscriberUUID, strings.ToLower(strings.TrimSpace(source.Email)), source.Name,
		source.Attribs, source.SubscriberStatus, organizationID, ownerUserID, ownerUserID); err != nil {
		return 0, workspaceQueryError("copying list subscriber", err)
	}
	return targetID, nil
}

func upsertMigratedSubscription(tx *sqlx.Tx, subscriberID, listID int, source migratedListSubscription) error {
	if _, err := tx.Exec(`
		INSERT INTO subscriber_lists (subscriber_id, list_id, status, meta, created_at, updated_at)
		VALUES ($1, $2, $3::subscription_status, $4, $5, $6)
		ON CONFLICT (subscriber_id, list_id) DO UPDATE SET
			status = CASE
				WHEN subscriber_lists.status = 'confirmed' OR EXCLUDED.status = 'confirmed' THEN 'confirmed'::subscription_status
				WHEN subscriber_lists.status = 'unconfirmed' OR EXCLUDED.status = 'unconfirmed' THEN 'unconfirmed'::subscription_status
				ELSE 'unsubscribed'::subscription_status END,
			meta = subscriber_lists.meta || EXCLUDED.meta,
			updated_at = NOW()`, subscriberID, listID, source.SubscriptionStatus, source.Meta, source.CreatedAt, source.UpdatedAt); err != nil {
		return workspaceQueryError("copying list subscription", err)
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
