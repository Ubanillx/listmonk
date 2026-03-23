package schedule

import "time"

const DailyResumeLayout = "15:04"

func CurrentLocalDate() string {
	return time.Now().In(time.Local).Format("2006-01-02")
}

func NextDailyResumeAt(hhmm string, now time.Time) time.Time {
	base := now.In(time.Local)
	t, err := time.ParseInLocation(DailyResumeLayout, hhmm, time.Local)
	if err != nil {
		nextDay := base.AddDate(0, 0, 1)
		return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), base.Hour(), base.Minute(), 0, 0, time.Local)
	}

	next := time.Date(base.Year(), base.Month(), base.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	if !next.After(base) {
		nextDay := base.AddDate(0, 0, 1)
		next = time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	}

	return next
}
