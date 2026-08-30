package main

import (
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

type smtpQuotaTracker struct {
	getUsageStmt  *sqlx.Stmt
	incrementStmt *sqlx.Stmt
	mu            sync.Mutex
	reserved      map[string]int
}

func newSMTPQuotaTracker(q *models.Queries) *smtpQuotaTracker {
	return &smtpQuotaTracker{
		getUsageStmt:  q.GetSMTPDailyUsage,
		incrementStmt: q.IncrementSMTPDailyUsage,
		reserved:      make(map[string]int),
	}
}

func newUserSMTPQuotaTracker(q *models.Queries) *smtpQuotaTracker {
	return &smtpQuotaTracker{
		getUsageStmt:  q.GetUserSMTPDailyUsage,
		incrementStmt: q.IncrementUserSMTPDailyUsage,
		reserved:      make(map[string]int),
	}
}

func (t *smtpQuotaTracker) HasServerQuota(uuid string, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	sent, err := t.getUsage(uuid)
	if err != nil {
		return false, err
	}

	return sent+t.reserved[uuid] < limit, nil
}

func (t *smtpQuotaTracker) ReserveServer(uuid string, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	sent, err := t.getUsage(uuid)
	if err != nil {
		return false, err
	}
	if sent+t.reserved[uuid] >= limit {
		return false, nil
	}

	t.reserved[uuid]++
	return true, nil
}

func (t *smtpQuotaTracker) CommitServer(uuid string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.reserved[uuid] > 0 {
		t.reserved[uuid]--
	}

	_, err := t.incrementStmt.Exec(uuid, currentLocalDate())
	if err != nil {
		// A server may be deleted immediately after a delivery starts. The
		// manager serializes normal configuration changes with sends, but this
		// guard also protects integrations that remove rows directly: the SMTP
		// message was already accepted by the remote server, and a missing usage
		// row is no reason to report a delivery failure (which would retry and
		// duplicate the message).
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23503" {
			return nil
		}
	}
	return err
}

func (t *smtpQuotaTracker) ReleaseServer(uuid string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.reserved[uuid] > 0 {
		t.reserved[uuid]--
	}
}

func (t *smtpQuotaTracker) getUsage(uuid string) (int, error) {
	var sent int
	if err := t.getUsageStmt.Get(&sent, uuid, currentLocalDate()); err != nil {
		return 0, err
	}
	return sent, nil
}
