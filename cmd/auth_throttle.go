package main

import (
	"sync"
	"time"
)

const (
	// authThrottleMaxFailures is the number of failed attempts allowed for one
	// key inside the window before it is locked out.
	authThrottleMaxFailures = 10
	// authThrottleWindow is the period failures are counted over.
	authThrottleWindow = 15 * time.Minute
	// authThrottleLockout is how long a key stays locked once the failure count
	// is reached.
	authThrottleLockout = 15 * time.Minute
	// authThrottleMaxEntries bounds the in-memory table. Once exceeded, expired
	// entries are swept on the next update.
	authThrottleMaxEntries = 10000
)

// authThrottle is a small in-process limiter for authentication failures. It
// raises the cost of online password/TOTP guessing, which the 100ms timing
// mitigation alone does not meaningfully constrain. It is deliberately not a
// general rate limiter: it only counts failures per key and resets on success.
//
// Caveats (documented, accepted): state is per process, so deployments running
// several instances get that many times the allowance, and a restart clears the
// table. Keys derived from client IPs should use the proxy-aware RealIP; a
// deployment that does not sanitize forwarding headers can still be evaded by a
// spoofing attacker.
type authThrottle struct {
	mu      sync.Mutex
	entries map[string]*throttleEntry

	// now is replaceable in tests.
	now func() time.Time
}

type throttleEntry struct {
	failures    int
	firstFail   time.Time
	lockedUntil time.Time
}

func (t *authThrottle) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// Allow reports whether an attempt for the key may proceed.
func (t *authThrottle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.entries[key]
	if !ok {
		return true
	}
	return !t.clock().Before(e.lockedUntil)
}

// Failure records a failed attempt and locks the key once the threshold inside
// the window is reached.
func (t *authThrottle) Failure(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.clock()
	e, ok := t.entries[key]
	if !ok || now.Sub(e.firstFail) > authThrottleWindow {
		e = &throttleEntry{firstFail: now}
		if t.entries == nil {
			t.entries = make(map[string]*throttleEntry)
		}
		t.entries[key] = e
	}

	e.failures++
	if e.failures >= authThrottleMaxFailures {
		e.lockedUntil = now.Add(authThrottleLockout)
		e.failures = 0
		e.firstFail = now
	}

	if len(t.entries) > authThrottleMaxEntries {
		t.sweep(now)
	}
}

// Success clears the failure state for a key after a successful login.
func (t *authThrottle) Success(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.entries, key)
}

// sweep drops entries that are neither locked nor inside the failure window.
// The caller must hold the lock.
func (t *authThrottle) sweep(now time.Time) {
	for k, e := range t.entries {
		if now.Before(e.lockedUntil) {
			continue
		}
		if now.Sub(e.firstFail) > authThrottleWindow {
			delete(t.entries, k)
		}
	}
}
