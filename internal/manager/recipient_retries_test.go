package manager

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

type unavailableFailureHistoryStore struct{ sendErrorStore }

func (*unavailableFailureHistoryStore) RecordCampaignSendFailure(models.CampaignSendFailure) error {
	return errors.New("failure history unavailable")
}

func TestCampaignStopsWhenRetryHistoryCannotBeRecorded(t *testing.T) {
	for _, poolID := range []int64{0, 41} {
		t.Run(fmt.Sprint(poolID), func(t *testing.T) {
			m := newTestManager()
			defer m.Close()
			m.store = &unavailableFailureHistoryStore{}
			p := &pipe{m: m, camp: &models.Campaign{}, wg: &sync.WaitGroup{}, done: make(chan struct{}),
				messenger: &failedMessenger{err: errors.New("SMTP failure")}}
			p.wg.Add(1)
			go m.worker()
			m.campMsgQ <- CampaignMessage{Campaign: p.camp, Customer: models.Customer{Base: models.Base{ID: 7}}, PoolContactID: poolID, pipe: p}
			done := make(chan struct{})
			go func() { p.wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("worker did not stop")
			}
			if !p.stopped.Load() || !p.withErrors.Load() {
				t.Fatal("missing durable failure history can permit unbounded retries")
			}
		})
	}
}
