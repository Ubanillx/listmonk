package core

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Keep the preview and the transfer's source selection in sync.
const pendingOrganizationResourcePredicate = `organization_id = $1 AND owner_user_id IS NULL AND transfer_pending_at IS NOT NULL`

type PendingResourceCount struct {
	Kind  string `db:"kind" json:"kind"`
	Count int    `db:"count" json:"count"`
}

type PendingOrganizationResource struct {
	Kind                string    `db:"kind" json:"kind"`
	ID                  int       `db:"id" json:"id"`
	Name                string    `db:"name" json:"name"`
	OriginalOwnerUserID int       `db:"original_owner_user_id" json:"original_owner_user_id"`
	OriginalOwnerName   string    `db:"original_owner_name" json:"original_owner_name"`
	PendingAt           time.Time `db:"pending_at" json:"pending_at"`
}

type PendingOrganizationResources struct {
	Counts  []PendingResourceCount        `json:"counts"`
	Results []PendingOrganizationResource `json:"results"`
	Total   int                           `json:"total"`
	Page    int                           `json:"page"`
	PerPage int                           `json:"per_page"`
}

// PreviewPendingOrganizationResources returns a bounded, stable-order resource
// list and totals from one read-only snapshot. Customer email/attributes and
// template bodies/media URLs are not needed for a transfer decision.
func (c *Core) PreviewPendingOrganizationResources(orgID, page int) (PendingOrganizationResources, error) {
	out := PendingOrganizationResources{Counts: []PendingResourceCount{}, Results: []PendingOrganizationResource{}, Page: page, PerPage: 20}
	tx, err := c.db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, c.organizationDBErr("starting resource preview", err)
	}
	defer tx.Rollback()
	var status string
	if err := tx.Get(&status, `SELECT status FROM organizations WHERE id=$1`, orgID); err != nil {
		return out, c.organizationDBErr("checking preview organization", err)
	}
	if status != "active" {
		return out, echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	var selects []string
	// Table and kind names are application constants, never user input.
	for _, table := range []string{"customer_lists", "customers", "campaigns", "templates", "media"} {
		nameColumn := "name"
		if table == "media" {
			nameColumn = "filename"
		}
		selects = append(selects, fmt.Sprintf(`SELECT '%s'::TEXT AS kind, id, COALESCE(%s, '') AS name,
			COALESCE(original_owner_user_id, 0) AS original_owner_user_id, transfer_pending_at AS pending_at
			FROM %s WHERE %s`, table, nameColumn, table, pendingOrganizationResourcePredicate))
	}
	pending := `WITH pending AS (` + strings.Join(selects, ` UNION ALL `) + `) `
	if err := tx.Select(&out.Counts, pending+`SELECT kind, COUNT(*) AS count FROM pending GROUP BY kind ORDER BY kind`, orgID); err != nil {
		return out, c.organizationDBErr("counting pending resources", err)
	}
	for _, count := range out.Counts {
		out.Total += count.Count
	}
	lastPage := (out.Total + out.PerPage - 1) / out.PerPage
	if lastPage < 1 {
		lastPage = 1
	}
	if out.Page > lastPage {
		out.Page = lastPage
	}
	if err := tx.Select(&out.Results, pending+`SELECT p.*, COALESCE(u.username, '') AS original_owner_name
		FROM pending p LEFT JOIN users u ON u.id=p.original_owner_user_id
		ORDER BY p.kind, p.id LIMIT $2 OFFSET $3`, orgID, out.PerPage, (out.Page-1)*out.PerPage); err != nil {
		return out, c.organizationDBErr("listing pending resources", err)
	}
	return out, tx.Commit()
}
