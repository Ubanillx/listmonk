package core

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

var bounceQuerySortFields = []string{"email", "campaign_name", "source", "created_at", "type"}

// QueryBounces retrieves paginated bounce entries based on the given params.
// It also returns the total number of bounce records in the DB.
func (c *Core) QueryBounces(campID, subID int, source, orderBy, order string, offset, limit int) ([]models.Bounce, int, error) {
	if !strSliceContains(orderBy, bounceQuerySortFields) {
		orderBy = "created_at"
	}
	if order != SortAsc && order != SortDesc {
		order = SortDesc
	}

	out := []models.Bounce{}
	stmt := strings.ReplaceAll(c.q.QueryBounces, "%order%", orderBy+" "+order)
	if err := c.db.Select(&out, stmt, 0, campID, subID, source, offset, limit); err != nil {
		c.log.Printf("error fetching bounces: %v", err)
		return nil, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.bounce}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}

	return out, total, nil
}

// GetBounce retrieves bounce entries based on the given params.
func (c *Core) GetBounce(id int) (models.Bounce, error) {
	var out []models.Bounce
	stmt := strings.ReplaceAll(c.q.QueryBounces, "%order%", "id "+SortAsc)
	if err := c.db.Select(&out, stmt, id, 0, 0, "", 0, 1); err != nil {
		c.log.Printf("error fetching bounces: %v", err)
		return models.Bounce{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.bounce}", "error", pqErrMsg(err)))
	}

	if len(out) == 0 {
		return models.Bounce{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.bounce}"))

	}

	return out[0], nil
}

// RecordBounce records a new bounce.
func (c *Core) RecordBounce(b models.Bounce) error {
	action, ok := c.consts.BounceActions[b.Type]
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, c.i18n.Ts("globals.messages.invalidData")+": "+b.Type)
	}
	// Pool delivery uses the imported contact UUID as the provider token and
	// never creates a legacy customers row. Resolve that immutable campaign
	// recipient first; a matching pair is handled with organization-scoped
	// logical exclusion and an auditable bounce row.
	if b.CampaignUUID != "" && b.CustomerUUID != "" {
		var pool struct {
			ContactID    int64         `db:"pool_contact_id"`
			PoolID       int           `db:"pool_id"`
			AllocationID sql.NullInt64 `db:"allocation_id"`
			OrgID        sql.NullInt64 `db:"organization_id"`
			CampID       int           `db:"campaign_id"`
		}
		if err := c.db.Get(&pool, `SELECT cpr.pool_contact_id,cpr.pool_id,cpr.allocation_id,cpr.organization_id,cpr.campaign_id FROM campaigns c JOIN campaign_pool_recipients cpr ON cpr.campaign_id=c.id JOIN pool_contacts pc ON pc.id=cpr.pool_contact_id WHERE c.uuid=$1::UUID AND pc.uuid=$2::UUID LIMIT 1`, b.CampaignUUID, b.CustomerUUID); err == nil {
			return c.recordPoolBounce(b, action.Action, action.Count, pool.ContactID, pool.PoolID, pool.AllocationID, pool.OrgID, pool.CampID)
		}
	}

	_, err := c.q.RecordBounce.Exec(b.CustomerUUID,
		b.Email,
		b.CampaignUUID,
		b.Type,
		b.Source,
		b.Meta,
		b.CreatedAt,
		action.Count,
		action.Action)

	if err != nil {
		// Ignore the error if it complained of no customer.
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Column == "customer_id" {
			c.log.Printf("bounced customer (%s / %s) not found", b.CustomerUUID, b.Email)
			return nil
		}

		c.log.Printf("error recording bounce: %v", err)
	}

	return err
}

func (c *Core) recordPoolBounce(b models.Bounce, configuredAction string, threshold int, contactID int64, poolID int, allocationID, organizationID sql.NullInt64, campaignID int) error {
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	meta := b.Meta
	if len(meta) == 0 {
		meta = []byte(`{}`)
	}
	if _, err = tx.Exec(`INSERT INTO bounces(pool_contact_id,campaign_id,type,source,meta,created_at,source_pool_id,source_allocation_id,source_organization_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, contactID, campaignID, b.Type, b.Source, meta, b.CreatedAt, poolID, nullIntValue(allocationID), nullIntValue(organizationID)); err != nil {
		return err
	}
	var count int
	if err = tx.Get(&count, `SELECT COUNT(*) FROM bounces WHERE pool_contact_id=$1 AND type=$2 AND ($3::BIGINT IS NULL OR source_organization_id=$3)`, contactID, b.Type, nullIntValue(organizationID)); err != nil {
		return err
	}
	if configuredAction == "blocklist" && (threshold < 1 || count >= threshold) && organizationID.Valid {
		if _, err = tx.Exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,source,removed_at,restored_at) VALUES($1,$2,$3,$4,$5,'bounce',NOW(),NULL) ON CONFLICT(pool_id,organization_id,contact_id) DO UPDATE SET allocation_id=EXCLUDED.allocation_id,reason=EXCLUDED.reason,source='bounce',removed_at=NOW(),restored_at=NULL`, poolID, organizationID.Int64, contactID, nullIntValue(allocationID), b.Type); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullIntValue(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}

// BlocklistBouncedCustomers blocklists all bounced customers.
func (c *Core) BlocklistBouncedCustomers() error {
	if _, err := c.q.BlocklistBouncedCustomers.Exec(); err != nil {
		c.log.Printf("error blocklisting bounced customers: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, c.i18n.Ts("customers.errorBlocklisting", "error", err.Error()))
	}

	return nil
}

// DeleteBounce deletes a customer_list.
func (c *Core) DeleteBounce(id int) error {
	return c.DeleteBounces([]int{id}, false)
}

// DeleteBounces deletes multiple customer_lists.
func (c *Core) DeleteBounces(ids []int, all bool) error {
	if _, err := c.q.DeleteBounces.Exec(pq.Array(ids), all); err != nil {
		c.log.Printf("error deleting customer_lists: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.customer_list}", "error", pqErrMsg(err)))
	}
	return nil
}
