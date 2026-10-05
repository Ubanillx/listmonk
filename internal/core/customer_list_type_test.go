package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestCanUpdateCustomerListType(t *testing.T) {
	tests := []struct {
		name, current, requested string
		want                     bool
	}{
		{"private unchanged", models.CustomerListTypePrivate, models.CustomerListTypePrivate, true},
		{"ordinary visibility change", models.CustomerListTypePrivate, models.CustomerListTypePublic, true},
		{"public to private", models.CustomerListTypePublic, models.CustomerListTypePrivate, true},
		{"pool unchanged", models.CustomerListTypePool, models.CustomerListTypePool, true},
		{"allocation unchanged", models.CustomerListTypeOrgPoolAllocation, models.CustomerListTypeOrgPoolAllocation, true},
		{"ordinary to pool", models.CustomerListTypePrivate, models.CustomerListTypePool, false},
		{"ordinary to allocation", models.CustomerListTypePrivate, models.CustomerListTypeOrgPoolAllocation, false},
		{"pool to ordinary", models.CustomerListTypePool, models.CustomerListTypePrivate, false},
		{"allocation to ordinary", models.CustomerListTypeOrgPoolAllocation, models.CustomerListTypePrivate, false},
		{"pool to allocation", models.CustomerListTypePool, models.CustomerListTypeOrgPoolAllocation, false},
		{"missing type", models.CustomerListTypePrivate, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canUpdateCustomerListType(tt.current, tt.requested); got != tt.want {
				t.Fatalf("canUpdateCustomerListType(%q, %q) = %v, want %v", tt.current, tt.requested, got, tt.want)
			}
		})
	}
}
