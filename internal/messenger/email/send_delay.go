package email

import (
	"math/rand/v2"
	"sync"
	"time"
)

// A sender can appear in both an account pool and an organization pool.
// Share its gate across Emailers so concurrency cannot bypass the delay.
var sendGates = struct {
	sync.Mutex
	items map[string]*sendGate
}{items: make(map[string]*sendGate)}

type sendGate struct {
	token chan struct{}
	refs  int
}

func acquireSendGate(key string) *sendGate {
	return acquireSendGateWithCapacity(key, 1)
}

func acquireSendGateWithCapacity(key string, capacity int) *sendGate {
	sendGates.Lock()
	defer sendGates.Unlock()
	g := sendGates.items[key]
	if g == nil {
		g = &sendGate{token: make(chan struct{}, capacity)}
		sendGates.items[key] = g
	}
	g.refs++
	return g
}

func releaseSendGate(key string) {
	sendGates.Lock()
	defer sendGates.Unlock()
	if g := sendGates.items[key]; g != nil {
		g.refs--
		if g.refs == 0 {
			delete(sendGates.items, key)
		}
	}
}

func randomSendDelay(min, max time.Duration) time.Duration {
	if min == max {
		return min
	}
	return min + time.Duration(rand.Int64N(int64((max-min)/time.Millisecond)+1))*time.Millisecond
}

// wait holds the sender until the caller completes its SMTP attempt and
// releases it. A cancelled wait releases its slot without sending mail.
func (g *sendGate) wait(min, max time.Duration, closed, shutdown, campaign <-chan struct{}) error {
	select {
	case g.token <- struct{}{}:
	case <-closed:
		return ErrSMTPUnavailable
	case <-shutdown:
		return ErrSendCancelled
	case <-campaign:
		return ErrSendCancelled
	}
	timer := time.NewTimer(randomSendDelay(min, max))
	defer timer.Stop()
	var err error
	select {
	case <-timer.C:
	case <-closed:
		err = ErrSMTPUnavailable
	case <-shutdown:
		err = ErrSendCancelled
	case <-campaign:
		err = ErrSendCancelled
	}
	// Cancellation may race with a zero/short timer. Recheck before sending.
	if err == nil {
		select {
		case <-closed:
			err = ErrSMTPUnavailable
		case <-shutdown:
			err = ErrSendCancelled
		case <-campaign:
			err = ErrSendCancelled
		default:
		}
	}
	if err != nil {
		g.release()
	}
	return err
}

func (g *sendGate) release() { <-g.token }
