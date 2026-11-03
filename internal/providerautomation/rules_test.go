package providerautomation

import (
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/bestedm"
)

func validConfig() Config {
	return Config{Types: []int{1, 3, 6, 8, 9}, Schedule: "daily", DailyTime: "02:00", IntervalHours: 24, LookbackDays: 7, ExcludedEmails: []string{}}
}

func TestRuleValidationAndSchedule(t *testing.T) {
	c := validConfig()
	c.Enabled = true
	c.ExcludedEmails = []string{" A&b@example.test ", "a&B@example.test"}
	if err := c.Validate(); err != nil || len(c.ExcludedEmails) != 1 {
		t.Fatalf("normalization: %+v %v", c, err)
	}
	zone := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 3, 2, 0, 0, 0, zone)
	if next := c.Next(now); next == nil || next.Day() != 4 || next.Hour() != 2 {
		t.Fatalf("daily next: %v", next)
	}
	c.Schedule = "interval"
	c.IntervalHours = 6
	if next := c.Next(now); next == nil || next.Sub(now) != 6*time.Hour {
		t.Fatalf("interval next: %v", next)
	}
	c.Enabled = false
	if c.Next(now) != nil {
		t.Fatal("disabled rule scheduled")
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.Types = nil }, func(c *Config) { c.Types = []int{1, 1} }, func(c *Config) { c.Types = []int{11} }, func(c *Config) { c.Schedule = "bad" }, func(c *Config) { c.DailyTime = "2:00" }, func(c *Config) { c.LookbackDays = 0 }, func(c *Config) { c.IntervalHours = 169 }} {
		c := validConfig()
		mutate(&c)
		if c.Validate() == nil {
			t.Fatalf("invalid rule accepted: %+v", c)
		}
	}
}

func TestSelectPreservesAddressesAndFiltersBeforeCounting(t *testing.T) {
	config := validConfig()
	rows := []bestedm.Row{
		{"email": " A&b@example.test ", "error_type": "1"},
		{"email": "a&B@example.test", "error_type": "9"},
		{"email": "a&b@example.test", "error_type": "2"},
		{"email": "complaint@example.test", "error_type": "7"},
		{"email": "", "error_type": "1"},
	}
	got := Select(rows, config)
	if len(got) != 1 || got[0].Email != "a&b@example.test" || got[0].Count != 2 || len(got[0].Types) != 2 {
		t.Fatalf("evidence changed: %+v", got)
	}
	config.Types = []int{7}
	if got := Select(rows, config); len(got) != 1 || got[0].Email != "complaint@example.test" {
		t.Fatalf("independent types ignored: %+v", got)
	}
}
