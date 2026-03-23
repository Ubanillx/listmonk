package main

import "github.com/knadh/listmonk/internal/schedule"

const dailyResumeLayout = schedule.DailyResumeLayout

func currentLocalDate() string {
	return schedule.CurrentLocalDate()
}
