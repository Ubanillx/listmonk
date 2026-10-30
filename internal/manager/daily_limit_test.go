package manager

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

func TestDailyAllowanceWaitRequeuesAfterWorkerCompletion(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	p := &pipe{m: m, wg: &sync.WaitGroup{}, done: make(chan struct{}), progress: make(chan struct{}, 1)}
	p.wg.Add(2) // Scheduler reference and a queued delivery.
	// Completion before the waiter starts must also wake it; otherwise a fast
	// worker can strand a campaign at the daily allowance boundary.
	p.messageDone()
	go p.waitForProgress()
	select {
	case got := <-m.nextPipes:
		if got != p {
			t.Fatal("requeued another campaign")
		}
	case <-time.After(time.Second):
		t.Fatal("worker completion did not wake the campaign")
	}
	p.wg.Done()
	p.wg.Wait()
}

func TestDailyAllowanceWaitStopsOnPauseAndShutdown(t *testing.T) {
	for _, reason := range []string{"pause", "shutdown"} {
		t.Run(reason, func(t *testing.T) {
			m := newTestManager()
			defer m.Close()
			p := &pipe{m: m, wg: &sync.WaitGroup{}, done: make(chan struct{}), progress: make(chan struct{}, 1)}
			p.wg.Add(1)
			done := make(chan struct{})
			go func() { p.waitForProgress(); p.wg.Wait(); close(done) }()
			if reason == "pause" {
				p.Stop(stopReasonPause, false)
			} else {
				m.Close()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stopped campaign retained its scheduler reference")
			}
		})
	}
}

type unrecordedSendStore struct{ sendErrorStore }

func (*unrecordedSendStore) MarkCampaignMessageSent(int, int) error {
	return errors.New("daily counter unavailable")
}

func (*unrecordedSendStore) MarkPoolCampaignMessageSent(int, int64) error {
	return errors.New("daily counter unavailable")
}

func TestCampaignPausesWhenSuccessfulSendCannotBeRecorded(t *testing.T) {
	for _, poolID := range []int64{0, 41} {
		t.Run(fmt.Sprint(poolID), func(t *testing.T) {
			m := newTestManager()
			defer m.Close()
			m.store = &unrecordedSendStore{}
			p := &pipe{m: m, camp: &models.Campaign{}, messenger: &testMessenger{},
				wg: &sync.WaitGroup{}, done: make(chan struct{}), rate: ratecounter.NewRateCounter(time.Minute)}
			p.wg.Add(1)
			go m.worker()
			m.campMsgQ <- CampaignMessage{Campaign: p.camp, PoolContactID: poolID, pipe: p}
			done := make(chan struct{})
			go func() { p.wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("worker did not finish")
			}
			if !p.stopped.Load() || !p.withErrors.Load() {
				t.Fatal("campaign can hang waiting for an unrecorded delivery")
			}
		})
	}
}
