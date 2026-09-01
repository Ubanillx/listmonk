package schedule

import "time"

const DailyResumeLayout = "15:04"

func CurrentLocalDate() string {
	return time.Now().In(time.Local).Format("2006-01-02")
}

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
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
}
