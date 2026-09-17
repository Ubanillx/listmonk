package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// store implements DataSource over the primary
// database.
type store struct {
	queries *models.Queries
	core    *core.Core
	media   media.Store
	db      *sqlx.DB
}

type campaignSendState struct {
	CampaignID      int       `db:"campaign_id"`
	CampaignType    string    `db:"campaign_type"`
	Status          string    `db:"status"`
	Messenger       string    `db:"messenger"`
	OwnerUserID     null.Int  `db:"owner_user_id"`
	DailySendLimit  int       `db:"daily_send_limit"`
	DailyResumeTime string    `db:"daily_resume_time"`
	NextResumeAt    null.Time `db:"next_resume_at"`
	DailySentCount  int       `db:"daily_sent_count"`
	QueuedCount     int       `db:"queued_count"`
	UnsentCount     int       `db:"unsent_count"`
	PoolScope       string    `db:"pool_scope"`
	PoolOrgIndex    int       `db:"pool_next_org_index"`
}

type campaignProgress struct {
	ToSend    int       `db:"to_send"`
	Sent      int       `db:"sent"`
	StartedAt null.Time `db:"started_at"`
}

// db is variadic for backwards compatibility with package-local tests and
// integrations that constructed the manager store before scoped attachment
// loading was added. Production passes the application's DB explicitly.
func newManagerStore(q *models.Queries, c *core.Core, m media.Store, dbs ...*sqlx.DB) *store {
	var db *sqlx.DB
	if len(dbs) > 0 {
		db = dbs[0]
	}
	return &store{
		queries: q,
		core:    c,
		media:   m,
		db:      db,
	}
}

// GetUserSMTPServers loads only enabled account-owned SMTP records and maps
// the persisted settings into the smtppool options used by email.Emailer.
func (s *store) GetUserSMTPServers(userID int) ([]email.Server, error) {
	var rows []models.PersonalSMTPServer
	// Delivery resolution must never include disabled account SMTP rows. The
	// profile API uses GetUserSMTPServers directly because it needs to display
	// those rows, so keep the enabled-only predicate in its own prepared query.
	if err := s.queries.GetEnabledUserSMTPServers.Select(&rows, userID, currentLocalDate()); err != nil {
		return nil, err
	}
	out := make([]email.Server, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		srv, err := mapSMTPServer(row)
		if err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	return out, nil
}

// mapSMTPServer converts one persisted SMTP row into the messenger server
// options used by email.Emailer.
func mapSMTPServer(row models.PersonalSMTPServer) (email.Server, error) {
	idle, err := time.ParseDuration(row.IdleTimeout)
	if err != nil {
		return email.Server{}, fmt.Errorf("invalid SMTP idle timeout: %w", err)
	}
	wait, err := time.ParseDuration(row.WaitTimeout)
	if err != nil {
		return email.Server{}, fmt.Errorf("invalid SMTP wait timeout: %w", err)
	}
	return email.Server{
		Name:          row.Name,
		UUID:          row.UUID,
		FromEmail:     row.FromEmail,
		DailyLimit:    row.DailyLimit,
		Username:      row.Username,
		Password:      row.Password,
		AuthProtocol:  row.AuthProtocol,
		TLSType:       row.TLSType,
		TLSSkipVerify: row.TLSSkipVerify,
		EmailHeaders:  headersToMap(row.EmailHeaders),
		Opt: smtppool.Opt{
			Host:              row.Host,
			Port:              row.Port,
			HelloHostname:     row.HelloHostname,
			MaxConns:          row.MaxConns,
			MaxMessageRetries: row.MaxMsgRetries,
			IdleTimeout:       idle,
			PoolWaitTimeout:   wait,
		},
	}, nil
}

// GetPoolSMTPServerByUUID loads one specific enabled SMTP account by UUID for
// a platform-level public-pool recipient, revalidating that the owning user
// is enabled and still an active member of an active organization. A
// single-server Emailer is returned so the recipient's assigned sender is
// honored exactly.
func (s *store) GetPoolSMTPServerByUUID(uuid string) (*email.Emailer, error) {
	var rows []models.PersonalSMTPServer
	if err := s.queries.GetEnabledUserSMTPServerByUUID.Select(&rows, uuid, currentLocalDate()); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: pool SMTP %s is not available", manager.ErrPoolSMTPUnavailable, uuid)
	}
	srv, err := mapSMTPServer(rows[0])
	if err != nil {
		return nil, err
	}
	msgr, err := email.New(email.MessengerName, srv)
	if err != nil {
		return nil, err
	}
	return msgr, nil
}

func headersToMap(headers models.Headers) map[string]string {
	out := make(map[string]string)
	for _, set := range headers {
		for key, value := range set {
			out[key] = value
		}
	}
	return out
}

// NextCampaigns retrieves active campaigns ready to be processed excluding
// campaigns that are also being processed.
func (s *store) NextCampaigns(currentIDs []int64) ([]*models.Campaign, error) {
	var out []*models.Campaign
	if err := s.queries.NextCampaigns.Select(&out, pq.Int64Array(currentIDs), time.Now().UTC()); err != nil {
		return nil, err
	}

	ready := make([]*models.Campaign, 0, len(out))
	for _, c := range out {
		if s.core != nil {
			if err := s.core.EnsurePoolCampaignRecipients(c.ID); err != nil {
				return nil, err
			}
		}
		// SetCampaignRunning below atomically claims scheduled/deferred rows and
		// changes their persisted status to running. Keep the pre-claim value on
		// the in-memory model so a strict personal-SMTP failure can distinguish a
		// campaign that had not started yet (draft) from one already in flight
		// (paused).
		c.SchedulerStatus = c.Status
		if c.Status == models.CampaignStatusScheduled || c.Status == models.CampaignStatusDeferred {
			res, err := s.queries.SetCampaignRunning.Exec(c.ID)
			if err != nil {
				return nil, err
			}
			// A member can leave, or an organization can be archived, after the
			// scanner selects this row. The conditional transition protects that
			// race; do not create a worker for a campaign that is no longer runnable.
			if n, err := res.RowsAffected(); err != nil {
				return nil, err
			} else if n == 0 {
				continue
			}
			c.Status = models.CampaignStatusRunning
			c.NextResumeAt.Valid = false
		}

		hasRecipients := false
		if err := s.queries.HasCampaignRecipients.Get(&hasRecipients, c.ID); err != nil {
			return nil, err
		}
		if !hasRecipients {
			if _, err := s.queries.EnsureCampaignRecipients.Exec(c.ID); err != nil {
				return nil, err
			}
		}
		if _, err := s.queries.SnapshotCampaignRecipients.Exec(c.ID); err != nil {
			return nil, err
		}

		if _, err := s.queries.ResetCampaignQueuedRecipients.Exec(c.ID, models.CampaignRecipientStatusPending); err != nil {
			return nil, err
		}

		var prog campaignProgress
		if err := s.queries.SyncCampaignProgress.Get(&prog, c.ID); err != nil {
			return nil, err
		}
		c.ToSend = prog.ToSend
		c.Sent = prog.Sent
		c.StartedAt = prog.StartedAt
		c.UnsentCount = max(0, prog.ToSend-prog.Sent)
		if c.UnsentCount == 0 {
			if err := s.UpdateCampaignStatus(c.ID, models.CampaignStatusFinished); err != nil {
				return nil, err
			}
			continue
		}

		ready = append(ready, c)
	}

	return ready, nil
}

// NextCustomers retrieves a subset of customers of a given campaign.
// Since batches are processed sequentially, the retrieval is ordered by customer ID.
func (s *store) NextCustomers(campID, limit int) ([]models.CampaignCustomer, error) {
	var st campaignSendState
	if err := s.queries.GetCampaignSendState.Get(&st, campID, currentLocalDate()); err != nil {
		return nil, err
	}

	if st.Status != models.CampaignStatusRunning {
		return nil, nil
	}

	if limit < 1 {
		limit = 1
	}

	// Keep the same cap in the SQL projection and the batch decision. This is
	// also defensive for legacy rows whose stored limit is still zero.
	smtpRemaining := -1
	if st.CampaignType == models.CampaignTypeRegular && email.IsMessengerName(st.Messenger) {
		if st.PoolScope == models.CampaignPoolScopeAllOrganizations {
			// Platform-level public-pool campaigns draw from every target
			// organization's member SMTP pool. The aggregate remaining
			// capacity is advisory; the authoritative reservation happens at
			// send time through the shared quota tracker.
			if err := s.queries.GetCampaignPoolSMTPRemaining.Get(&smtpRemaining, campID, currentLocalDate()); err != nil {
				return nil, err
			}
		} else if st.OwnerUserID.Valid && st.OwnerUserID.Int > 0 {
			var err error
			smtpRemaining, err = s.userSMTPRemaining(st.OwnerUserID.Int)
			if err != nil {
				return nil, err
			}
		}
	}
	batchLimit, deferred := campaignBatchLimit(
		st.CampaignType,
		st.Messenger,
		st.DailySendLimit,
		st.DailySentCount,
		st.QueuedCount,
		limit,
		smtpRemaining,
	)
	if st.CampaignType == models.CampaignTypeRegular && email.IsMessengerName(st.Messenger) {
		if deferred {
			lo.Printf("campaign %d deferred due to daily limit: limit=%d sent_today=%d queued=%d local_date=%s",
				campID,
				normalizedCampaignDailySendLimit(st.DailySendLimit),
				st.DailySentCount,
				st.QueuedCount,
				currentLocalDate(),
			)
			return nil, manager.ErrCampaignDeferred
		}
		limit = batchLimit
	}

	// Platform-level public-pool campaigns claim recipients through the
	// transactional organization allocator, which rotates organizations
	// fairly and assigns each recipient an SMTP account from the target
	// organization's member SMTP pool.
	if st.PoolScope == models.CampaignPoolScopeAllOrganizations {
		return s.nextPoolCustomers(campID, st, limit)
	}

	var out []models.CampaignCustomer
	err := s.queries.NextCampaignCustomers.Select(&out,
		campID,
		pq.Array([]string{models.CampaignRecipientStatusPending, models.CampaignRecipientStatusDeferred}),
		limit,
	)
	if err != nil {
		return nil, err
	}
	if len(out) < limit && s.queries.NextCampaignPoolCustomers != nil {
		poolRows := []models.CampaignCustomer{}
		if err := s.queries.NextCampaignPoolCustomers.Select(&poolRows, campID,
			pq.Array([]string{models.CampaignRecipientStatusPending, models.CampaignRecipientStatusDeferred}), limit-len(out)); err != nil {
			return nil, err
		}
		out = append(out, poolRows...)
	}
	return out, nil
}

func (s *store) MarkPoolCampaignMessageSent(campID int, contactID int64) error {
	_, err := s.db.Exec(`UPDATE campaign_pool_recipients SET status=$3::campaign_recipient_status, updated_at=NOW() WHERE campaign_id=$1 AND pool_contact_id=$2`, campID, contactID, models.CampaignRecipientStatusSent)
	if err != nil {
		return err
	}
	if _, err = s.queries.IncrementCampaignDailyUsage.Exec(campID, currentLocalDate()); err != nil {
		return err
	}
	_, err = s.queries.UpdateCampaignCounts.Exec(campID, 0, 1, 0)
	return err
}

func (s *store) MarkPoolCampaignRecipientStatus(campID int, contactID int64, status string) error {
	_, err := s.db.Exec(`UPDATE campaign_pool_recipients SET status=$3::campaign_recipient_status, updated_at=NOW() WHERE campaign_id=$1 AND pool_contact_id=$2`, campID, contactID, status)
	return err
}

func (s *store) ResetPoolCampaignQueuedRecipients(campID int, toStatus string) error {
	_, err := s.db.Exec(`UPDATE campaign_pool_recipients SET status=$2::campaign_recipient_status, updated_at=NOW() WHERE campaign_id=$1 AND status='queued'`, campID, toStatus)
	return err
}

// userSMTPRemaining returns aggregate remaining capacity for an account's
// enabled SMTP pool. -1 denotes an unlimited server; finite servers are
// summed because the account-level round-robin pool may use any of them.
func (s *store) userSMTPRemaining(userID int) (int, error) {
	var remaining int
	if err := s.queries.GetUserSMTPRemaining.Get(&remaining, userID, currentLocalDate()); err != nil {
		return 0, err
	}
	return remaining, nil
}

// poolOrgSMTPServer is one flattened SMTP slot of an organization's member
// SMTP pool.
type poolOrgSMTPServer struct {
	UUID       string `db:"uuid"`
	UserID     int    `db:"user_id"`
	FromEmail  string `db:"from_email"`
	DailyLimit int    `db:"daily_limit"`
	SentToday  int    `db:"sent_today"`
}

// poolClaimRow is one claim-campaign-pool-org-recipient row.
type poolClaimRow struct {
	PoolContactID    int64       `db:"pool_contact_id"`
	SenderSMTPUUID   null.String `db:"sender_smtp_uuid"`
	SenderUserID     null.Int    `db:"sender_user_id"`
	SenderFrom       string      `db:"sender_from_snapshot"`
	Email            string      `db:"email"`
	Name             string      `db:"name"`
	Attribs          models.JSON `db:"attribs"`
	UUID             string      `db:"uuid"`
	CustomerCode     string      `db:"customer_code"`
	CreatedAt        null.Time   `db:"created_at"`
	UpdatedAt        null.Time   `db:"updated_at"`
	ReplyMailboxID   null.Int    `db:"reply_mailbox_id"`
	AllocationID     int64       `db:"allocation_id"`
	PoolReplyMailbox string      `db:"pool_reply_mailbox_email"`
	PoolOrgID        int64       `db:"pool_organization_id"`
}

// ensurePoolOrgOrders guarantees the campaign has a persisted, fair
// organization rotation inside the given transaction. The order is generated
// once by shuffling the active organizations that own a pool allocation for
// the campaign's pool; afterwards it never changes (stale organizations are
// pruned, the remaining order keeps its sequence).
func (s *store) ensurePoolOrgOrders(tx *sqlx.Tx, campID int) ([]int64, error) {
	var orders []int64
	if err := tx.Select(&orders, `SELECT organization_id FROM campaign_pool_org_orders WHERE campaign_id=$1 ORDER BY dispatch_order`, campID); err != nil {
		return nil, err
	}
	if len(orders) > 0 {
		if _, err := tx.Exec(`DELETE FROM campaign_pool_org_orders oo
			WHERE oo.campaign_id = $1
			  AND NOT EXISTS (
			      SELECT 1 FROM org_pool_allocations s
			      JOIN organizations o ON o.id = s.organization_id AND o.status = 'active'
			      JOIN campaign_customer_lists ccl ON ccl.campaign_id = oo.campaign_id
			          AND ccl.pool_id = s.pool_id AND ccl.pool_id IS NOT NULL
			      WHERE s.organization_id = oo.organization_id
			  )`, campID); err != nil {
			return nil, err
		}
		if err := tx.Select(&orders, `SELECT organization_id FROM campaign_pool_org_orders WHERE campaign_id=$1 ORDER BY dispatch_order`, campID); err != nil {
			return nil, err
		}
	}
	if len(orders) > 0 {
		return orders, nil
	}

	var poolID null.Int
	if err := tx.Get(&poolID, `SELECT pool_id FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL LIMIT 1`, campID); err != nil {
		if err == sql.ErrNoRows {
			return nil, manager.ErrPoolSMTPUnavailable
		}
		return nil, err
	}
	var orgs []int64
	if err := tx.Select(&orgs, `SELECT DISTINCT s.organization_id
		FROM org_pool_allocations s
		JOIN organizations o ON o.id = s.organization_id AND o.status = 'active'
		WHERE s.pool_id = $1
		ORDER BY s.organization_id`, poolID.Int); err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return nil, manager.ErrPoolSMTPUnavailable
	}
	rand.Shuffle(len(orgs), func(i, j int) { orgs[i], orgs[j] = orgs[j], orgs[i] })
	for i, org := range orgs {
		if _, err := tx.Exec(`INSERT INTO campaign_pool_org_orders(campaign_id, organization_id, dispatch_order)
			VALUES($1, $2, $3) ON CONFLICT (campaign_id, organization_id) DO NOTHING`, campID, org, i); err != nil {
			return nil, err
		}
	}
	return orgs, nil
}

// pickPoolOrgSMTP advances the organization's durable round-robin cursor and
// returns the next SMTP slot that still has local-day quota. A returned nil
// means every server in the pool is quota-exhausted for today (advisory; the
// authoritative reservation happens at send time). A structurally empty pool
// (no eligible SMTP rows at all) is reported as ErrPoolSMTPUnavailable.
func (s *store) pickPoolOrgSMTP(tx *sqlx.Tx, orgID int64, servers []poolOrgSMTPServer) (*poolOrgSMTPServer, error) {
	if len(servers) == 0 {
		return nil, manager.ErrPoolSMTPUnavailable
	}
	if _, err := tx.Exec(`INSERT INTO org_pool_smtp_cursors(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, orgID); err != nil {
		return nil, err
	}
	var cursor null.String
	if err := tx.Get(&cursor, `SELECT next_smtp_uuid FROM org_pool_smtp_cursors WHERE organization_id=$1 FOR UPDATE`, orgID); err != nil {
		return nil, err
	}
	start := 0
	if cursor.Valid {
		for i := range servers {
			if servers[i].UUID == cursor.String {
				start = i + 1
				break
			}
		}
	}
	for i := range servers {
		srv := servers[(start+i)%len(servers)]
		if srv.DailyLimit > 0 && srv.SentToday >= srv.DailyLimit {
			continue
		}
		if _, err := tx.Exec(`UPDATE org_pool_smtp_cursors SET next_smtp_uuid=$2::UUID, updated_at=NOW() WHERE organization_id=$1`, orgID, srv.UUID); err != nil {
			return nil, err
		}
		return &srv, nil
	}
	return nil, nil
}

// nextPoolCustomers claims one batch of recipients for a platform-level
// public-pool campaign. Organizations are served round-robin in the
// campaign's persisted random order: each visit claims one deliverable
// recipient for that organization and assigns an SMTP account from that
// organization's member SMTP pool, advancing the organization's durable
// cursor. A recipient whose previously assigned SMTP is still eligible keeps
// it (an in-flight retry never crosses to a different account); a recipient
// whose assigned SMTP is gone or quota-exhausted is reassigned from the
// cursor position.
func (s *store) nextPoolCustomers(campID int, st campaignSendState, limit int) ([]models.CampaignCustomer, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	orgOrders, err := s.ensurePoolOrgOrders(tx, campID)
	if err != nil {
		return nil, err
	}
	if len(orgOrders) == 0 {
		return nil, manager.ErrPoolSMTPUnavailable
	}

	orgServers := make(map[int64][]poolOrgSMTPServer, len(orgOrders))
	loadServers := func(orgID int64) ([]poolOrgSMTPServer, error) {
		if rows, ok := orgServers[orgID]; ok {
			return rows, nil
		}
		var rows []poolOrgSMTPServer
		if err := s.queries.GetOrgPoolSMTPServers.Select(&rows, orgID, currentLocalDate()); err != nil {
			return nil, err
		}
		orgServers[orgID] = rows
		return rows, nil
	}

	quotaBlocked := make(map[int64]bool)
	statuses := []string{models.CampaignRecipientStatusPending, models.CampaignRecipientStatusDeferred}
	out := make([]models.CampaignCustomer, 0, limit)
	orgIdx := st.PoolOrgIndex
	visited := 0
	for len(out) < limit && visited < len(orgOrders) {
		orgID := orgOrders[orgIdx%len(orgOrders)]
		orgIdx++
		visited++

		if quotaBlocked[orgID] {
			continue
		}
		servers, err := loadServers(orgID)
		if err != nil {
			return nil, err
		}
		if len(servers) == 0 {
			// Structural emptiness: the organization has no eligible SMTP
			// account at all. The whole campaign pauses.
			return nil, manager.ErrPoolSMTPUnavailable
		}

		var claim poolClaimRow
		if err := tx.Get(&claim, `SELECT cpr.pool_contact_id,
				cpr.sender_smtp_uuid, cpr.sender_user_id, cpr.sender_from_snapshot,
				COALESCE(cpr.email_snapshot, pc.email) AS email,
				COALESCE(cpr.name_snapshot, pc.name) AS name,
				pc.attribs, pc.uuid, pc.customer_code, pc.created_at, pc.updated_at,
				cpr.reply_mailbox_id, COALESCE(cpr.allocation_id, 0) AS allocation_id,
				COALESCE(rm.email, '') AS pool_reply_mailbox_email,
				cpr.organization_id AS pool_organization_id
			FROM campaign_pool_recipients cpr
			JOIN pool_contacts pc ON pc.id = cpr.pool_contact_id
			LEFT JOIN reply_mailboxes rm ON rm.id = cpr.reply_mailbox_id
			LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id = cpr.pool_id
				AND ex.organization_id = cpr.organization_id
				AND ex.contact_id = cpr.pool_contact_id
				AND ex.restored_at IS NULL
			WHERE cpr.campaign_id = $1 AND cpr.organization_id = $2
				AND cpr.status = ANY($3::campaign_recipient_status[])
				AND pc.status = 'active' AND ex.contact_id IS NULL
			ORDER BY cpr.pool_contact_id
			FOR UPDATE OF cpr SKIP LOCKED
			LIMIT 1`, campID, orgID, pq.Array(statuses)); err != nil {
			if err == sql.ErrNoRows {
				// The organization has no deliverable recipients left; move
				// on to the next organization in the rotation.
				continue
			}
			return nil, err
		}

		var srv *poolOrgSMTPServer
		if claim.SenderSMTPUUID.Valid {
			// Keep an existing assignment when its SMTP account is still in
			// the organization's active pool and has quota left. Retrying a
			// failed send must not cross accounts.
			for i := range servers {
				if servers[i].UUID == claim.SenderSMTPUUID.String {
					if servers[i].DailyLimit > 0 && servers[i].SentToday >= servers[i].DailyLimit {
						break
					}
					srv = &servers[i]
					break
				}
			}
		}
		if srv == nil {
			srv, err = s.pickPoolOrgSMTP(tx, orgID, servers)
			if err != nil {
				return nil, err
			}
			if srv == nil {
				// Every server in this organization is quota-exhausted for
				// today. Leave the recipient claimable and try other
				// organizations; the campaign defers when the whole pool is
				// exhausted.
				quotaBlocked[orgID] = true
				continue
			}
		}

		res, err := tx.Exec(`UPDATE campaign_pool_recipients
			SET status = 'queued', updated_at = NOW(),
				sender_smtp_uuid = $3::UUID, sender_user_id = $4,
				sender_from_snapshot = $5, sender_assigned_at = NOW()
			WHERE campaign_id = $1 AND pool_contact_id = $2
				AND status = ANY('{pending,deferred}'::campaign_recipient_status[])`,
			campID, claim.PoolContactID, srv.UUID, srv.UserID, srv.FromEmail)
		if err != nil {
			return nil, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if n == 0 {
			continue
		}

		out = append(out, models.CampaignCustomer{
			Customer: models.Customer{
				Base: models.Base{CreatedAt: claim.CreatedAt, UpdatedAt: claim.UpdatedAt},
				UUID: claim.UUID, Email: claim.Email, Name: claim.Name,
				Attribs: claim.Attribs, Status: "enabled",
				CustomerCode: claim.CustomerCode,
			},
			RecipientStatus:       models.CampaignRecipientStatusQueued,
			PoolContactID:         claim.PoolContactID,
			PoolID:                int(claim.AllocationID), // unused for pool delivery; kept for parity
			OrgPoolAllocationID:   claim.AllocationID,
			ReplyMailboxID:        claim.ReplyMailboxID,
			PoolReplyMailboxEmail: claim.PoolReplyMailbox,
			PoolOrganizationID:    claim.PoolOrgID,
			PoolSenderSMTPUUID:    srv.UUID,
			PoolSenderUserID:      int64(srv.UserID),
			PoolSenderFrom:        srv.FromEmail,
		})
	}

	// Persist the rotation position so a paused/restarted campaign resumes
	// where it stopped and concurrent batches rotate instead of restarting.
	if _, err := tx.Exec(`UPDATE campaigns SET pool_next_org_index = $2, updated_at = NOW() WHERE id = $1`,
		campID, orgIdx%len(orgOrders)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCampaign fetches a campaign from the database.
func (s *store) GetCampaign(campID int) (*models.Campaign, error) {
	var out = &models.Campaign{}
	err := s.queries.GetCampaign.Get(out, campID, nil, nil, "default")
	return out, err
}

// GetCampaignOptinListUUIDs returns only double-opt-in list UUIDs attached to
// the campaign. The campaign relation is already workspace-authorized when
// it is queued, so this does not expose unrelated private lists.
func (s *store) GetCampaignOptinListUUIDs(campID int) ([]string, error) {
	var out []string
	err := s.db.Select(&out, `
		SELECT l.uuid
		FROM campaign_customer_lists cl
		JOIN customer_lists l ON l.id=cl.customer_list_id
		WHERE cl.campaign_id=$1 AND cl.pool_id IS NULL
		  AND l.optin='double' AND l.status='active'
		ORDER BY l.id`, campID)
	return out, err
}

// UpdateCampaignStatus updates a campaign's status.
func (s *store) UpdateCampaignStatus(campID int, status string) error {
	_, err := s.queries.UpdateCampaignStatus.Exec(campID, status)
	return err
}

// MarkCampaignSMTPUnavailable atomically applies the strict no-fallback state
// after the scheduler has claimed a campaign but cannot resolve its owner's
// personal SMTP pool. A claim that originated from scheduled/deferred is
// returned to draft; a campaign that was already running is paused. The
// conditional current-status check avoids overwriting a concurrent manual
// pause/cancel, and queued recipients are made retryable.
func (s *store) MarkCampaignSMTPUnavailable(campID int, previousStatus string) error {
	return s.MarkCampaignStartFailure(campID, previousStatus)
}

// MarkCampaignStartFailure atomically applies the safe lifecycle transition
// after the scheduler has claimed a campaign but template/media initialization
// failed. A claim that originated from scheduled/deferred is returned to
// draft; a campaign that was already running is paused. The conditional
// current-status check avoids overwriting a concurrent manual pause/cancel,
// and queued recipients are made retryable in the same transaction.
func (s *store) MarkCampaignStartFailure(campID int, previousStatus string) error {
	if s.db == nil || campID < 1 {
		if err := s.ResetCampaignQueuedRecipients(campID, models.CampaignRecipientStatusPending); err != nil {
			return err
		}
		status := models.CampaignStatusDraft
		if previousStatus == models.CampaignStatusRunning {
			status = models.CampaignStatusPaused
		}
		return s.UpdateCampaignStatus(campID, status)
	}
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err := tx.Get(&current, `SELECT status FROM campaigns WHERE id = $1 FOR UPDATE`, campID); err != nil {
		return err
	}
	// Only a row still eligible for processing may be changed. This preserves a
	// user's explicit pause/cancel made while the SMTP resolver was running.
	if current != models.CampaignStatusRunning {
		return tx.Commit()
	}
	target := models.CampaignStatusPaused
	if previousStatus == models.CampaignStatusScheduled || previousStatus == models.CampaignStatusDeferred {
		target = models.CampaignStatusDraft
	}
	if _, err := tx.Exec(`
		UPDATE campaigns SET status = $2::campaign_status,
			send_at = NULL, next_resume_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'running'`, campID, target); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE campaign_recipients SET status = 'pending', updated_at = NOW()
		WHERE campaign_id = $1 AND status = 'queued'`, campID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE campaign_pool_recipients SET status = 'pending', updated_at = NOW()
		WHERE campaign_id = $1 AND status = 'queued'`, campID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateCampaignCounts updates a campaign's status.
func (s *store) UpdateCampaignCounts(campID int, toSend int, sent int, lastSubID int) error {
	_, err := s.queries.UpdateCampaignCounts.Exec(campID, toSend, sent, lastSubID)
	return err
}

func (s *store) MarkCampaignMessageSent(campID int, subID int) error {
	if _, err := s.queries.MarkCampaignRecipientSent.Exec(campID, subID); err != nil {
		return err
	}
	if _, err := s.queries.IncrementCampaignDailyUsage.Exec(campID, currentLocalDate()); err != nil {
		return err
	}
	_, err := s.queries.UpdateCampaignCounts.Exec(campID, 0, 1, 0)
	return err
}

func (s *store) MarkCampaignRecipientStatus(campID int, subID int, status string) error {
	_, err := s.queries.MarkCampaignRecipientStatus.Exec(campID, subID, status)
	return err
}

func (s *store) ResetCampaignQueuedRecipients(campID int, toStatus string) error {
	_, err := s.queries.ResetCampaignQueuedRecipients.Exec(campID, toStatus)
	if err != nil {
		return err
	}
	return s.ResetPoolCampaignQueuedRecipients(campID, toStatus)
}

func (s *store) UpdateCampaignRecipientStatuses(campID int, toStatus string, fromStatuses []string) error {
	_, err := s.queries.UpdateCampaignRecipientStatuses.Exec(campID, toStatus, pq.Array(fromStatuses))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE campaign_pool_recipients SET status=$2::campaign_recipient_status, updated_at=NOW() WHERE campaign_id=$1 AND status=ANY($3::campaign_recipient_status[])`, campID, toStatus, pq.Array(fromStatuses))
	return err
}

func (s *store) DeferCampaign(campID int, nextResumeAt time.Time) error {
	if _, err := s.queries.SetCampaignDeferred.Exec(campID, nextResumeAt); err != nil {
		return err
	}
	return s.UpdateCampaignRecipientStatuses(campID, models.CampaignRecipientStatusDeferred, []string{models.CampaignRecipientStatusPending})
}

// GetAttachment fetches a media attachment blob.
func (s *store) GetAttachment(mediaID int) (models.Attachment, error) {
	m, err := s.core.GetMedia(mediaID, "", "", s.media)
	if err != nil {
		return models.Attachment{}, err
	}

	b, err := s.media.GetBlob(m.URL)
	if err != nil {
		return models.Attachment{}, err
	}

	return models.Attachment{
		Name:    m.Filename,
		Content: b,
		Header:  manager.MakeAttachmentHeader(m.Filename, "base64", m.ContentType),
		MediaID: mediaID,
		// SourceURL is an internal matching key used while preparing HTML mail.
		// Use the ID-qualified protected route so duplicate provider filenames
		// (which are normal after a campaign/template clone) remain distinct.
		SourceURL: personalMediaSourceURL(mediaID, m.Filename),
	}, nil
}

// GetTemplateAttachments resolves the media snapshot for a transactional
// template in the active workspace.  Transactional sends do not have a
// campaign row that can be used by GetCampaignAttachments, so the template
// association is checked explicitly and every media record is re-authorized
// at send time.  This is important for shared templates that intentionally
// carry a private image owned by the template author, as well as for rows that
// may have been transferred or removed since the template was cached.
func (s *store) GetTemplateAttachments(access models.WorkspaceAccess, templateID int, mediaIDs []int64) ([]models.Attachment, error) {
	if templateID < 1 || len(mediaIDs) == 0 {
		return nil, nil
	}
	// Keep package-local tests and older integrations that construct a store
	// without a database working. Production always passes db and uses the
	// scoped path below.
	if s.db == nil {
		return s.loadLegacyAttachments(mediaIDs)
	}

	type attachmentRow struct {
		ID          int    `db:"id"`
		Filename    string `db:"filename"`
		ContentType string `db:"content_type"`
	}
	var rows []attachmentRow
	if err := s.db.Select(&rows, `
		SELECT DISTINCT m.id, m.filename, m.content_type
		FROM template_media tm
		JOIN media m ON m.id = tm.media_id
		WHERE tm.template_id = $1
		  AND tm.media_id = ANY($2::BIGINT[])
		ORDER BY m.id`, templateID, pq.Array(mediaIDs)); err != nil {
		return nil, err
	}

	byID := make(map[int]attachmentRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	seen := make(map[int64]struct{}, len(mediaIDs))
	attachments := make([]models.Attachment, 0, len(mediaIDs))
	for _, rawID := range mediaIDs {
		if rawID < 1 {
			continue
		}
		if _, ok := seen[rawID]; ok {
			continue
		}
		seen[rawID] = struct{}{}

		row, ok := byID[int(rawID)]
		if !ok {
			return nil, fmt.Errorf("media %d is not attached to template %d", rawID, templateID)
		}
		allowed, err := s.core.CanUseTemplateMedia(access, templateID, row.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("media %d is not usable by template %d", rawID, templateID)
		}
		blob, err := s.media.GetBlob(s.media.GetURL(row.Filename))
		if err != nil {
			return nil, fmt.Errorf("error fetching attachment %d: %w", rawID, err)
		}
		attachments = append(attachments, models.Attachment{
			Name:      row.Filename,
			Content:   blob,
			Header:    manager.MakeAttachmentHeader(row.Filename, "base64", row.ContentType),
			MediaID:   row.ID,
			SourceURL: personalMediaSourceURL(row.ID, row.Filename),
		})
	}
	return attachments, nil
}

func (s *store) loadLegacyAttachments(mediaIDs []int64) ([]models.Attachment, error) {
	attachments := make([]models.Attachment, 0, len(mediaIDs))
	seen := make(map[int64]struct{}, len(mediaIDs))
	for _, id := range mediaIDs {
		if id < 1 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		a, err := s.GetAttachment(int(id))
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, nil
}

// GetCampaignAttachments resolves the media snapshot for a scheduled
// campaign.  The scheduler is intentionally global (it processes campaigns
// for every account), therefore an ID-only media lookup is unsafe: a media row
// may have been transferred, deleted, or replaced after next-campaigns read
// the association.  Re-evaluate both the campaign/template relationship and
// the resource visibility here before reading any provider blob.
func (s *store) GetCampaignAttachments(campaign *models.Campaign, mediaIDs []int64) ([]models.Attachment, error) {
	if campaign == nil || campaign.ID < 1 || len(mediaIDs) == 0 {
		return nil, nil
	}
	// Package-local tests and older integrations may construct a store without
	// passing the application DB (the constructor keeps that form compatible).
	// Preserve their historical attachment behavior; the production constructor
	// always supplies db and therefore takes the scoped path below.
	if s.db == nil {
		return s.loadLegacyAttachments(mediaIDs)
	}

	type attachmentRow struct {
		ID          int    `db:"id"`
		Filename    string `db:"filename"`
		ContentType string `db:"content_type"`
	}
	var rows []attachmentRow
	// A direct campaign media reference is usable only when it is global,
	// owned by the campaign owner in the same workspace, or organization-shared
	// in that workspace.  A template reference follows the same rule for the
	// template itself, with the documented exception that a shared template may
	// carry a private image owned by its author.
	const query = `
		WITH campaign AS (
			SELECT c.id, c.organization_id, c.owner_user_id, c.template_id
			FROM campaigns c
			LEFT JOIN organizations co ON co.id = c.organization_id
			WHERE c.id = $1
				AND c.transfer_pending_at IS NULL
				AND (c.organization_id IS NULL OR co.status = 'active')
		)
		SELECT DISTINCT m.id, m.filename, m.content_type
		FROM media m
		JOIN campaign c ON TRUE
		WHERE m.id = ANY($2::BIGINT[])
			AND m.transfer_pending_at IS NULL
			AND (
				(
					EXISTS (
						SELECT 1 FROM campaign_media cm
						WHERE cm.campaign_id = c.id AND cm.media_id = m.id
					)
					AND (
						m.visibility = 'global'
						OR (
							m.organization_id IS NOT DISTINCT FROM c.organization_id
							AND m.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
						)
						OR (
							m.organization_id IS NOT DISTINCT FROM c.organization_id
							AND m.visibility = 'organization'
						)
					)
				)
				OR EXISTS (
					SELECT 1
					FROM template_media tm
					JOIN templates t ON t.id = tm.template_id
					LEFT JOIN organizations to_org ON to_org.id = t.organization_id
					WHERE t.id = c.template_id
						AND tm.media_id = m.id
						AND t.transfer_pending_at IS NULL
						AND (t.organization_id IS NULL OR to_org.status = 'active')
						AND (
							t.visibility = 'global'
						OR (
							 t.organization_id IS NOT DISTINCT FROM c.organization_id
								AND (t.owner_user_id IS NOT DISTINCT FROM c.owner_user_id OR t.visibility = 'organization')
							)
						)
						AND (
							m.visibility = 'global'
							OR (
								m.organization_id IS NOT DISTINCT FROM c.organization_id
								AND m.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
							)
							OR (
								m.organization_id IS NOT DISTINCT FROM c.organization_id
								AND m.visibility = 'organization'
							)
							OR (
								m.organization_id IS NOT DISTINCT FROM t.organization_id
								AND m.owner_user_id IS NOT DISTINCT FROM t.owner_user_id
								AND m.visibility <> 'organization'
							)
						)
				)
			)
		ORDER BY m.id`
	if err := s.db.Select(&rows, query, campaign.ID, pq.Array(mediaIDs)); err != nil {
		return nil, err
	}

	byID := make(map[int]attachmentRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	seen := make(map[int64]struct{}, len(mediaIDs))
	attachments := make([]models.Attachment, 0, len(mediaIDs))
	for _, rawID := range mediaIDs {
		if rawID < 1 {
			continue
		}
		if _, ok := seen[rawID]; ok {
			continue
		}
		seen[rawID] = struct{}{}
		row, ok := byID[int(rawID)]
		if !ok {
			return nil, fmt.Errorf("media %d is not usable by campaign %d", rawID, campaign.ID)
		}
		blob, err := s.media.GetBlob(s.media.GetURL(row.Filename))
		if err != nil {
			return nil, fmt.Errorf("error fetching attachment %d: %w", rawID, err)
		}
		attachments = append(attachments, models.Attachment{
			Name:      row.Filename,
			Content:   blob,
			Header:    manager.MakeAttachmentHeader(row.Filename, "base64", row.ContentType),
			MediaID:   row.ID,
			SourceURL: personalMediaSourceURL(row.ID, row.Filename),
		})
	}
	return attachments, nil
}

func personalMediaSourceURL(mediaID int, filename string) string {
	if mediaID < 1 {
		return ""
	}
	return "/api/media/file/" + fmt.Sprintf("%d", mediaID) + "/" + url.PathEscape(filename)
}

// CreateLink registers a URL with a UUID, associates it with the campaign
// rendering it, and returns the UUID used in the tracking URL.
func (s *store) CreateLink(campUUID, url string) (string, error) {
	// Create a new UUID for the URL. If the URL already exists in the DB
	// the UUID in the database is returned.
	uu, err := uuid.NewV4()
	if err != nil {
		return "", err
	}

	var out string
	if err := s.queries.CreateLink.Get(&out, uu, url, campUUID); err != nil {
		return "", err
	}

	return out, nil
}

// RecordBounce records a bounce event and returns the bounce count.
func (s *store) RecordBounce(b models.Bounce) (int64, int, error) {
	var res = struct {
		CustomerID int64 `db:"customer_id"`
		Num        int   `db:"num"`
	}{}

	err := s.queries.UpdateCampaignStatus.Select(&res,
		b.CustomerUUID,
		b.Email,
		b.CampaignUUID,
		b.Type,
		b.Source,
		b.Meta)

	return res.CustomerID, res.Num, err
}

// BlocklistCustomer blocklists a customer permanently.
func (s *store) BlocklistCustomer(id int64) error {
	_, err := s.queries.BlocklistCustomers.Exec(pq.Int64Array{id})
	return err
}

// DeleteCustomer deletes a customer from the DB.
func (s *store) DeleteCustomer(id int64) error {
	_, err := s.queries.DeleteCustomers.Exec(pq.Int64Array{id})
	return err
}
