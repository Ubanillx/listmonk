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

func TestNextDailyResumeAtSameDay(t *testing.T) {
	restore := setLocalTZ(t, "UTC")
	defer restore()

	now := time.Date(2026, 3, 23, 8, 30, 0, 0, time.Local)
	got := NextDailyResumeAt("09:00", now)
	want := time.Date(2026, 3, 23, 9, 0, 0, 0, time.Local)

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
