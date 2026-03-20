package main

import "time"

const dailyResumeLayout = "15:04"

func currentLocalDate() string {
	return time.Now().In(time.Local).Format("2006-01-02")
}

func nextDailyResumeAt(hhmm string, now time.Time) time.Time {
	base := now.In(time.Local)
	t, err := time.ParseInLocation(dailyResumeLayout, hhmm, time.Local)
	if err != nil {
		return base.Add(24 * time.Hour).Truncate(time.Minute)
	}

	next := time.Date(base.Year(), base.Month(), base.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	if !next.After(base) {
		next = next.Add(24 * time.Hour)
	}

	return next
}
