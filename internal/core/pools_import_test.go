package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestNormalizePoolAllocationDepartment(t *testing.T) {
	if got := normalizePoolAllocationDepartment("  Sales North  "); got != "sales north" {
		t.Fatalf("normalized department = %q, want %q", got, "sales north")
	}
}

func TestPoolBlocklistImportPreservesSuppressionAndRecipientBoundaries(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
		ALTER TABLE pool_contacts ADD COLUMN allocation_department TEXT NOT NULL DEFAULT '';
		CREATE TABLE pool_merge_conflicts (
			pool_id INTEGER, contact_id BIGINT, customer_code TEXT,
			existing_snapshot JSONB, incoming_snapshot JSONB, created_by_user_id INTEGER
		);`)
	org := env.seedOrganization("Import department")
	mailbox := env.seedMailbox(org, "reply@example.com")
	env.setOrganizationMailbox(org, &mailbox)
	pool := env.seedPool("Selected")
	otherPool := env.seedPool("Other")
	allocation := env.seedAllocation(pool, org)
	blocked := env.seedContact("OLD", "Old name", "blocked@example.com", "active")
	untouched := env.seedContact("OTHER", "Other pool", "blocked@example.com", "active")
	active := env.seedContact("ACTIVE", "Active", "active@example.com", "active")
	env.joinPool(pool, blocked)
	env.joinPool(pool, active)
	env.joinPool(otherPool, untouched)
	env.allocate(allocation, blocked, "active")
	env.allocate(allocation, active, "active")
	rows := []models.PoolContactImportRow{
		{Row: 2, CustomerCode: "NEW", Name: "Changed name", Email: "BLOCKED@example.com", AllocationDepartment: "Import department"},
		{Row: 3, CustomerCode: "UNKNOWN", Name: "New blocked", Email: "new@example.com", AllocationDepartment: "Import department"},
		{Row: 4, CustomerCode: "INVALID", Name: "Invalid", Email: "active@example.com", AllocationDepartment: "Unknown department"},
	}
	result, err := env.core.ImportPoolContacts(pool, 1, rows, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 2 || result.Blocklisted != 3 || result.Invalid != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE id IN ($1,$2) AND status='active'`, untouched, active); got != 2 {
		t.Fatalf("unrelated contacts affected: %d", got)
	}
	result, err = env.core.ImportPoolContacts(pool, 1, rows[:2], false)
	if err != nil || result.Existing != 2 {
		t.Fatalf("normal reimport: %+v, %v", result, err)
	}
	// A changed source identity must not bypass an existing email blocklist.
	rows[0].CustomerCode = "ANOTHER"
	if _, err := env.core.ImportPoolContacts(pool, 1, rows[:1], false); err != nil {
		t.Fatal(err)
	}
	if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE LOWER(email)='blocked@example.com' AND status='blocklisted'`); got != 3 {
		t.Fatalf("blocklist not preserved across import identities: %d", got)
	}
	recipients, err := env.core.ResolvePoolRecipients(pool, org)
	if err != nil || len(recipients) != 1 || recipients[0].ID != active {
		t.Fatalf("blacklisted contact in recipients: %+v, %v", recipients, err)
	}
}
