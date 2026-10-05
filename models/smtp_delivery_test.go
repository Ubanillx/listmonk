package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSMTPDelayRangeCompatibilityAndValidation(t *testing.T) {
	for _, tt := range []struct {
		min, max         int
		wantMin, wantMax time.Duration
		invalid          bool
	}{
		{0, 0, 0, 0, false},
		{500, 2000, 500 * time.Millisecond, 2 * time.Second, false},
		{3600000, 3600000, time.Hour, time.Hour, false},
		{-1, 2000, 0, 0, true},
		{5000, 2000, 0, 0, true},
		{1000, 0, 0, 0, true},
		{0, 3600001, 0, 0, true},
	} {
		min, max, err := (SMTPDeliverySettings{SendDelayMin: tt.min, SendDelayMax: tt.max}).SendDelayRange()
		if (err != nil) != tt.invalid || min != tt.wantMin || max != tt.wantMax {
			t.Fatalf("range = %v, %v, %v", min, max, err)
		}
	}
}

func TestSMTPDelayJSONKeepsMillisecondsAndLegacyDurations(t *testing.T) {
	for _, payload := range []string{
		`{"send_delay_min":500,"send_delay_max":2000,"max_conns":7}`,
		`{"send_delay_min":"500ms","send_delay_max":"2s","max_conns":7}`,
	} {
		var delivery SMTPDeliverySettings
		if err := json.Unmarshal([]byte(payload), &delivery); err != nil {
			t.Fatal(err)
		}
		if delivery.SendDelayMin != 500 || delivery.SendDelayMax != 2000 || delivery.MaxConns != 7 {
			t.Fatalf("wrong delay or other fields lost: %+v", delivery)
		}
		b, err := json.Marshal(delivery)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(b, &wire); err != nil {
			t.Fatal(err)
		}
		if wire["send_delay_min"] != float64(500) || wire["send_delay_max"] != float64(2000) {
			t.Fatalf("delay not emitted in integer milliseconds: %s", b)
		}
	}
	for _, payload := range []string{
		`{"send_delay_min":1.5}`, `{"send_delay_min":"500us"}`,
		`{"send_delay_min":"bad"}`, `{"send_delay_min":true}`,
	} {
		var delivery SMTPDeliverySettings
		if err := json.Unmarshal([]byte(payload), &delivery); err == nil {
			t.Fatalf("accepted invalid millisecond delay: %s", payload)
		}
	}
}
