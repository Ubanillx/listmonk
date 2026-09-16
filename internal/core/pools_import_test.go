package core

import "testing"

func TestNormalizePoolAllocationDepartment(t *testing.T) {
	if got := normalizePoolAllocationDepartment("  Sales North  "); got != "sales north" {
		t.Fatalf("normalized department = %q, want %q", got, "sales north")
	}
}
