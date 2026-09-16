package main

import (
	"testing"
	"time"
)

// TestAuthThrottleLocksAfterThreshold verifies the failure counter locks a key
// once the threshold is reached and lets it through again after the lockout.
func TestAuthThrottleLocksAfterThreshold(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	th := &authThrottle{now: func() time.Time { return now }}

	key := "login|10.0.0.1|admin"
	for i := 0; i < authThrottleMaxFailures-1; i++ {
		if !th.Allow(key) {
			t.Fatalf("attempt %d should be allowed before the threshold", i+1)
		}
		th.Failure(key)
	}
	if !th.Allow(key) {
		t.Fatal("attempt at the threshold should still be allowed")
	}
	th.Failure(key)

	if th.Allow(key) {
		t.Fatal("expected the key to be locked after reaching the threshold")
	}

	// Another key is unaffected.
	if !th.Allow("login|10.0.0.2|admin") {
		t.Fatal("expected an unrelated key to stay allowed")
	}

	// The lockout expires.
	now = now.Add(authThrottleLockout + time.Second)
	if !th.Allow(key) {
		t.Fatal("expected the key to be allowed again after the lockout")
	}
}

// TestAuthThrottleSuccessResets verifies a successful login clears failures.
func TestAuthThrottleSuccessResets(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	th := &authThrottle{now: func() time.Time { return now }}

	key := "login|10.0.0.1|admin"
	for i := 0; i < authThrottleMaxFailures-1; i++ {
		th.Failure(key)
	}
	th.Success(key)

	for i := 0; i < authThrottleMaxFailures-1; i++ {
		th.Failure(key)
	}
	if !th.Allow(key) {
		t.Fatal("failures before a success must not count towards the threshold")
	}
}

// TestAuthThrottleWindowExpiry verifies old failures fall out of the window.
func TestAuthThrottleWindowExpiry(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	th := &authThrottle{now: func() time.Time { return now }}

	key := "login|10.0.0.1|admin"
	for i := 0; i < authThrottleMaxFailures-1; i++ {
		th.Failure(key)
	}

	now = now.Add(authThrottleWindow + time.Second)

	// The counter restarts in the new window; the threshold is not reached
	// until the full count lands inside one window.
	for i := 0; i < authThrottleMaxFailures-1; i++ {
		th.Failure(key)
	}
	if !th.Allow(key) {
		t.Fatal("failures from an expired window must not count")
	}
}

// TestAuthThrottleZeroValueIsUsable guards against test App literals panicking
// on a nil map.
func TestAuthThrottleZeroValueIsUsable(t *testing.T) {
	var th authThrottle
	if !th.Allow("x") {
		t.Fatal("expected an empty throttle to allow")
	}
	th.Failure("x")
	th.Success("x")
	if !th.Allow("x") {
		t.Fatal("expected the key to be allowed after a success reset")
	}
}
