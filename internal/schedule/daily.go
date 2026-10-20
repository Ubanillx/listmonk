package schedule

import (
	"strings"
	"time"
)

const DailyResumeLayout = "15:04"

func CurrentLocalDate() string {
	return time.Now().In(time.Local).Format("2006-01-02")
}

// NextDailyResumeAt returns the instant of the next occurrence of the hhmm
// wall clock on the process's time zone, after now.
//
// The result is an absolute instant, and its wall clock in time.Local is the
// configured resume time. That property is what the admin UI reads back, so it
// must hold for every zone the process might run in: a server that mistakenly
// ran on UTC used to persist 09:00 UTC for a 09:00 Asia/Shanghai campaign, and
// the UI then showed a resume at 17:00.
func NextDailyResumeAt(hhmm string, now time.Time) time.Time {
	base := now.In(time.Local)
	nextDay := base.AddDate(0, 0, 1)

	t, err := time.ParseInLocation(DailyResumeLayout, hhmm, time.Local)
	if err != nil {
		return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), base.Hour(), base.Minute(), 0, 0, time.Local)
	}

	// Daily usage is partitioned by local calendar date. A campaign that has
	// exhausted today's allowance must always wait for tomorrow's configured
	// resume time, even if that time has not occurred yet today.
	next := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	if next.Format(DailyResumeLayout) != t.Format(DailyResumeLayout) {
		// A zone transition that skips the configured wall clock (a DST gap)
		// would otherwise silently store a different time than the campaign
		// asks for. Moving past the gap keeps the resume on the following day
		// rather than surprising the operator with an unconfigured hour.
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// ResumeTimeMatchesWallClock reports whether an absolute resume instant reads
// back as the configured hhmm on the given zone. Deferral uses it so a stored
// time is never accepted on a different clock than the operator configured.
//
// An empty or unparsable hhmm expresses no expectation - NextDailyResumeAt
// falls back to the current clock for those - so the check is vacuously true
// and callers keep their previous behavior.
func ResumeTimeMatchesWallClock(at time.Time, hhmm string, loc *time.Location) bool {
	t, err := time.Parse(DailyResumeLayout, strings.TrimSpace(hhmm))
	if err != nil {
		return true
	}
	if loc == nil {
		loc = time.Local
	}
	return at.In(loc).Format(DailyResumeLayout) == t.Format(DailyResumeLayout)
}
