package main

import (
	"net/url"
	"testing"
)

func TestPoolExportSelection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		values    url.Values
		aggregate bool
		invalid   bool
	}{
		{"unselected", url.Values{}, true, false},
		{"aggregate", url.Values{"contact": {"7:42", "8:42", "7:42"}}, true, false},
		{"scoped", url.Values{"contact": {"0:42"}}, false, false},
		{"empty", url.Values{"contact": {}}, true, true},
		{"blank", url.Values{"contact": {""}}, true, true},
		{"missing pool", url.Values{"contact": {"42"}}, true, true},
		{"zero pool", url.Values{"contact": {"0:42"}}, true, true},
		{"scoped pool", url.Values{"contact": {"7:42"}}, false, true},
		{"zero contact", url.Values{"contact": {"7:0"}}, true, true},
		{"negative contact", url.Values{"contact": {"7:-1"}}, true, true},
		{"overflow", url.Values{"contact": {"7:9223372036854775808"}}, true, true},
		{"partial invalid", url.Values{"contact": {"7:42", "invalid"}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := parsePoolExportSelection(tc.values, tc.aggregate)
			if (err != nil) != tc.invalid {
				t.Fatalf("selection error = %v, invalid = %v", err, tc.invalid)
			}
			if tc.invalid {
				return
			}
			if tc.name == "unselected" {
				if selected != nil || !poolExportIncludes(selected, 99, 99) {
					t.Fatal("omitting selection must preserve full filtered export")
				}
				return
			}
			poolID := int64(0)
			if tc.aggregate {
				poolID = 7
			}
			if !poolExportIncludes(selected, poolID, 42) || poolExportIncludes(selected, poolID, 43) || poolExportIncludes(selected, 9, 42) {
				t.Fatal("selection must match both pool membership and contact")
			}
		})
	}
}
