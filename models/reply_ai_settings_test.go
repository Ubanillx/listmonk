package models

import (
	"testing"
	"time"
)

func TestReplyScanInterval(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"", time.Minute}, {"  ", time.Minute}, {"60s", time.Minute},
		{" 5m ", 5 * time.Minute}, {"10s", 10 * time.Second}, {"24h", 24 * time.Hour},
	} {
		got, err := ReplyScanInterval(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("ReplyScanInterval(%q) = %v, %v; want %v", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{"invalid", "60", "0s", "-1m", "9s", "25h"} {
		if _, err := ReplyScanInterval(raw); err == nil {
			t.Errorf("accepted invalid interval %q", raw)
		}
	}
}
