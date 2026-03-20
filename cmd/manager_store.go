package main

import (
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// store implements DataSource over the primary
// database.
type store struct {
	queries *models.Queries
	core    *core.Core
	media   media.Store
}

type campaignSendState struct {
	CampaignID      int       `db:"campaign_id"`
	CampaignType    string    `db:"campaign_type"`
	Status          string    `db:"status"`
	Messenger       string    `db:"messenger"`
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

func newManagerStore(q *models.Queries, c *core.Core, m media.Store) *store {
	return &store{
		queries: q,
		core:    c,
		media:   m,
	}
}

// NextCampaigns retrieves active campaigns ready to be processed excluding
// campaigns that are also being processed.
func (s *store) NextCampaigns(currentIDs []int64) ([]*models.Campaign, error) {
	var out []*models.Campaign
	if err := s.queries.NextCampaigns.Select(&out, pq.Int64Array(currentIDs)); err != nil {
		return nil, err
	}

	ready := make([]*models.Campaign, 0, len(out))
	for _, c := range out {
		if c.Status == models.CampaignStatusScheduled || c.Status == models.CampaignStatusDeferred {
			if _, err := s.queries.SetCampaignRunning.Exec(c.ID); err != nil {
				return nil, err
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

	if st.CampaignType == models.CampaignTypeRegular && strings.HasPrefix(st.Messenger, emailMsgr) && st.DailySendLimit > 0 {
		remaining := st.DailySendLimit - st.DailySentCount - st.QueuedCount
		if remaining <= 0 {
			return nil, manager.ErrCampaignDeferred
		}
		if remaining < limit {
			limit = remaining
		}
	}

	var out []models.CampaignSubscriber
	err := s.queries.NextCampaignSubscribers.Select(&out,
		campID,
		pq.Array([]string{models.CampaignRecipientStatusPending, models.CampaignRecipientStatusDeferred}),
		limit,
	)
	return out, err
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
	}, nil
}

// CreateLink registers a URL with a UUID for tracking clicks and returns the UUID.
func (s *store) CreateLink(url string) (string, error) {
	// Create a new UUID for the URL. If the URL already exists in the DB
	// the UUID in the database is returned.
	uu, err := uuid.NewV4()
	if err != nil {
		return "", err
	}

	var out string
	if err := s.queries.CreateLink.Get(&out, uu, url); err != nil {
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
