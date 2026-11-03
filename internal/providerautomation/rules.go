// Package providerautomation applies platform-managed, independent private and
// public-pool blocklist rules to date-scoped provider failure records.
package providerautomation

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/knadh/listmonk/internal/bestedm"
)

type Config struct {
	Enabled        bool     `json:"enabled"`
	Types          []int    `json:"types"`
	Schedule       string   `json:"schedule"`
	DailyTime      string   `json:"daily_time"`
	IntervalHours  int      `json:"interval_hours"`
	LookbackDays   int      `json:"lookback_days"`
	ExcludedEmails []string `json:"excluded_emails"`
}

type Rule struct {
	Scope     string     `json:"scope" db:"scope"`
	Version   int64      `json:"version" db:"version"`
	Config    Config     `json:"config"`
	NextRunAt *time.Time `json:"next_run_at" db:"next_run_at"`
	LastRunAt *time.Time `json:"last_run_at" db:"last_run_at"`
}

func ValidScope(scope string) bool { return scope == "private" || scope == "pool" }

func (c *Config) Validate() error {
	if len(c.Types) == 0 || len(c.Types) > 10 {
		return fmt.Errorf("select at least one failure type")
	}
	seen := map[int]bool{}
	for _, typ := range c.Types {
		if typ < 1 || typ > 10 || seen[typ] {
			return fmt.Errorf("invalid or repeated failure type")
		}
		seen[typ] = true
	}
	if c.Schedule != "daily" && c.Schedule != "interval" {
		return fmt.Errorf("invalid schedule")
	}
	if t, err := time.Parse("15:04", c.DailyTime); err != nil || t.Format("15:04") != c.DailyTime {
		return fmt.Errorf("daily time must be HH:MM")
	}
	if c.IntervalHours < 1 || c.IntervalHours > 168 || c.LookbackDays < 1 || c.LookbackDays > 31 {
		return fmt.Errorf("interval must be 1..168 hours and lookback 1..31 days")
	}
	if len(c.ExcludedEmails) > 1000 {
		return fmt.Errorf("at most 1000 excluded addresses")
	}
	addresses := make([]string, 0, len(c.ExcludedEmails))
	emails := map[string]bool{}
	for _, email := range c.ExcludedEmails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" || len(email) > 320 || strings.ContainsAny(email, "\r\n\x00") {
			return fmt.Errorf("invalid exclusion")
		}
		if !emails[email] {
			addresses = append(addresses, email)
			emails[email] = true
		}
	}
	c.ExcludedEmails = addresses
	sort.Ints(c.Types)
	return nil
}

func (c Config) Next(now time.Time) *time.Time {
	if !c.Enabled {
		return nil
	}
	next := now.Add(time.Duration(c.IntervalHours) * time.Hour)
	if c.Schedule == "daily" {
		t, _ := time.Parse("15:04", c.DailyTime)
		next = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
	}
	return &next
}

// Evidence preserves classifications and occurrence counts; no provider row is
// interpreted as an SMTP attempt and no malformed address is guessed or fixed.
type Evidence struct {
	Email string `json:"email"`
	Types []int  `json:"types"`
	Count int    `json:"count"`
}

func Select(rows []bestedm.Row, config Config) []Evidence {
	types := map[string]int{}
	for _, typ := range config.Types {
		types[fmt.Sprint(typ)] = typ
	}
	positions := map[string]int{}
	out := []Evidence{}
	for _, row := range rows {
		typ, ok := types[row["error_type"]]
		email := strings.ToLower(strings.TrimSpace(row["email"]))
		if !ok || email == "" {
			continue
		}
		index, exists := positions[email]
		if !exists {
			index = len(out)
			positions[email] = index
			out = append(out, Evidence{Email: email, Types: []int{}})
		}
		out[index].Count++
		found := false
		for _, t := range out[index].Types {
			found = found || t == typ
		}
		if !found {
			out[index].Types = append(out[index].Types, typ)
		}
	}
	return out
}
