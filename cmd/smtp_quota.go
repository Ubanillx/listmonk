package main

import (
	"sync"

	"github.com/knadh/listmonk/models"
)

type smtpQuotaTracker struct {
	queries  *models.Queries
	mu       sync.Mutex
	reserved map[string]int
}

func newSMTPQuotaTracker(q *models.Queries) *smtpQuotaTracker {
	return &smtpQuotaTracker{
		queries:  q,
		reserved: make(map[string]int),
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

	_, err := t.queries.IncrementSMTPDailyUsage.Exec(uuid, currentLocalDate())
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
	if err := t.queries.GetSMTPDailyUsage.Get(&sent, uuid, currentLocalDate()); err != nil {
		return 0, err
	}
	return sent, nil
}
