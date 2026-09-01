package main

import (
	"fmt"
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
		idle, err := time.ParseDuration(row.IdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid SMTP idle timeout: %w", err)
		}
		wait, err := time.ParseDuration(row.WaitTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid SMTP wait timeout: %w", err)
		}
		out = append(out, email.Server{
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
		})
	}
	return out, nil
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

// NextSubscribers retrieves a subset of subscribers of a given campaign.
// Since batches are processed sequentially, the retrieval is ordered by subscriber ID.
func (s *store) NextSubscribers(campID, limit int) ([]models.CampaignSubscriber, error) {
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
	if st.CampaignType == models.CampaignTypeRegular && email.IsMessengerName(st.Messenger) &&
		st.OwnerUserID.Valid && st.OwnerUserID.Int > 0 {
		var err error
		smtpRemaining, err = s.userSMTPRemaining(st.OwnerUserID.Int)
		if err != nil {
			return nil, err
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

	var out []models.CampaignSubscriber
	err := s.queries.NextCampaignSubscribers.Select(&out,
		campID,
		pq.Array([]string{models.CampaignRecipientStatusPending, models.CampaignRecipientStatusDeferred}),
		limit,
	)
	return out, err
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

// GetCampaign fetches a campaign from the database.
func (s *store) GetCampaign(campID int) (*models.Campaign, error) {
	var out = &models.Campaign{}
	err := s.queries.GetCampaign.Get(out, campID, nil, nil, "default")
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
	return err
}

func (s *store) UpdateCampaignRecipientStatuses(campID int, toStatus string, fromStatuses []string) error {
	_, err := s.queries.UpdateCampaignRecipientStatuses.Exec(campID, toStatus, pq.Array(fromStatuses))
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
		SubscriberID int64 `db:"subscriber_id"`
		Num          int   `db:"num"`
	}{}

	err := s.queries.UpdateCampaignStatus.Select(&res,
		b.SubscriberUUID,
		b.Email,
		b.CampaignUUID,
		b.Type,
		b.Source,
		b.Meta)

	return res.SubscriberID, res.Num, err
}

// BlocklistSubscriber blocklists a subscriber permanently.
func (s *store) BlocklistSubscriber(id int64) error {
	_, err := s.queries.BlocklistSubscribers.Exec(pq.Int64Array{id})
	return err
}

// DeleteSubscriber deletes a subscriber from the DB.
func (s *store) DeleteSubscriber(id int64) error {
	_, err := s.queries.DeleteSubscribers.Exec(pq.Int64Array{id})
	return err
}
