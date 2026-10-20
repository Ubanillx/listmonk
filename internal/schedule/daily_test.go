package schedule

import (
	"testing"
	"time"
)

func setLocalTZ(t *testing.T, tz string) func() {
	t.Helper()

	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Skipf("timezone %s unavailable: %v", tz, err)
	}

	old := time.Local
	time.Local = loc
	return func() {
		time.Local = old
	}
}

func TestNextDailyResumeAtAlwaysUsesNextDay(t *testing.T) {
	restore := setLocalTZ(t, "UTC")
	defer restore()

	now := time.Date(2026, 3, 23, 8, 30, 0, 0, time.Local)
	got := NextDailyResumeAt("09:00", now)
	want := time.Date(2026, 3, 24, 9, 0, 0, 0, time.Local)

	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNextDailyResumeAtRollsToNextDay(t *testing.T) {
	restore := setLocalTZ(t, "UTC")
	defer restore()

	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.Local)
	got := NextDailyResumeAt("09:00", now)
	want := time.Date(2026, 3, 24, 9, 0, 0, 0, time.Local)

	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNextDailyResumeAtKeepsWallClockAcrossDST(t *testing.T) {
	restore := setLocalTZ(t, "America/New_York")
	defer restore()

	now := time.Date(2026, 3, 7, 23, 30, 0, 0, time.Local)
	got := NextDailyResumeAt("09:00", now)
	want := time.Date(2026, 3, 8, 9, 0, 0, 0, time.Local)

	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

// TestNextDailyResumeAtRendersConfiguredTimeOnEveryClock pins the defect that
// reached production: a process whose time.Local was UTC (rather than the
// deployment zone) persisted a campaign configured for 09:00 as 09:00 UTC, and
// the admin UI then showed 17:00 in Asia/Shanghai. The returned instant must
// always read back as the configured wall clock in the process zone, so the
// resume time is never silently shifted no matter which zone computed it.
func TestNextDailyResumeAtRendersConfiguredTimeOnEveryClock(t *testing.T) {
	for _, tz := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Europe/Berlin"} {
		t.Run(tz, func(t *testing.T) {
			restore := setLocalTZ(t, tz)
			defer restore()

			// The production campaign was deferred at 10:17 local time.
			now := time.Date(2026, 10, 20, 10, 17, 5, 0, time.Local)
			got := NextDailyResumeAt("09:00", now)

			if !got.After(now) {
				t.Fatalf("resume time %s is not after %s", got, now)
			}
			if wall := got.Format(DailyResumeLayout); wall != "09:00" {
				t.Fatalf("resume wall clock = %s, want 09:00", wall)
			}
			if got.Location() != time.Local {
				t.Fatalf("resume location = %s, want the process zone %s", got.Location(), time.Local)
			}
		})
	}
}

// TestCurrentLocalDateUsesProcessZone keeps the daily quota day boundary on the
// same calendar the resume time uses.
func TestCurrentLocalDateUsesProcessZone(t *testing.T) {
	restore := setLocalTZ(t, "Asia/Shanghai")
	defer restore()

	want := time.Now().In(time.Local).Format("2006-01-02")
	if got := CurrentLocalDate(); got != want {
		t.Fatalf("CurrentLocalDate() = %s, want %s", got, want)
	}
}
