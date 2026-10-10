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

func TestPoolImportAllowsEmptyNames(t *testing.T) {
	for _, mode := range []struct {
		name      string
		blocklist bool
		status    string
	}{
		{"subscribe", false, "active"},
		{"blocklist", true, "blocklisted"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
				ALTER TABLE pool_contacts ADD COLUMN allocation_department TEXT NOT NULL DEFAULT '';
				CREATE TABLE pool_merge_conflicts (
					pool_id INTEGER, contact_id BIGINT, customer_code TEXT,
					existing_snapshot JSONB, incoming_snapshot JSONB, created_by_user_id INTEGER
				);`)
			org := env.seedOrganization("Import department")
			pool := env.seedPool("Empty names")
			allocation := env.seedAllocation(pool, org)
			rows := []models.PoolContactImportRow{
				{Row: 2, CustomerCode: "EMPTY", Email: "empty@example.com", AllocationDepartment: "Import department"},
				{Row: 3, CustomerCode: "EMPTY", Name: " \t ", Email: "EMPTY@example.com", AllocationDepartment: "Import department"},
				{Row: 4, CustomerCode: "SPACE", Name: " \t ", Email: "space@example.com", AllocationDepartment: "Import department"},
				{Row: 5, Email: "code@example.com", AllocationDepartment: "Import department"},
				{Row: 6, CustomerCode: "BAD-EMAIL", Email: "invalid", AllocationDepartment: "Import department"},
				{Row: 7, CustomerCode: "BAD-DEPT", Email: "department@example.com", AllocationDepartment: "Unknown department"},
			}
			result, err := env.core.ImportPoolContacts(pool, 1, rows, mode.blocklist)
			if err != nil {
				t.Fatal(err)
			}
			if result.Total != 6 || result.Created != 3 || result.Valid != 3 || result.Invalid != 2 || result.Duplicates != 1 {
				t.Fatalf("unexpected import result: %+v", result)
			}
			for i, reason := range []string{"invalid_email", "allocation_department_not_found"} {
				if len(result.Issues) != 2 || result.Issues[i].Row != i+6 || result.Issues[i].Reason != reason {
					t.Fatalf("unexpected validation issues: %+v", result.Issues)
				}
			}
			if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE name='' AND status=$1`, mode.status); got != 3 {
				t.Fatalf("contacts with empty names = %d, want 3", got)
			}
			if got := env.countRows(`SELECT COUNT(*) FROM org_pool_allocation_members WHERE allocation_id=$1`, allocation); got != 3 {
				t.Fatalf("allocated contacts = %d, want 3", got)
			}
			result, err = env.core.ImportPoolContacts(pool, 1, rows[:3], mode.blocklist)
			if err != nil || result.Created != 0 || result.Existing != 2 || result.Invalid != 0 || result.Duplicates != 1 {
				t.Fatalf("reimport with empty names: %+v, %v", result, err)
			}
			if mode.blocklist && result.Blocklisted != 2 {
				t.Fatalf("blocklisted contacts = %d, want 2", result.Blocklisted)
			}
			// An empty name remains part of the identity, rather than matching any name.
			rows[0].Name = "Named contact"
			result, err = env.core.ImportPoolContacts(pool, 1, rows[:1], mode.blocklist)
			if err != nil || result.Created != 1 || result.Conflicts != 1 || result.Existing != 0 {
				t.Fatalf("same code with a different name: %+v, %v", result, err)
			}
			if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE name=''`); got != 3 {
				t.Fatalf("original empty names changed: %d contacts, want 3", got)
			}
		})
	}
}

func TestPoolImportAllowsEmptyCustomerCodes(t *testing.T) {
	for _, blocklist := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscribe", true: "blocklist"}[blocklist], func(t *testing.T) {
			env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
				ALTER TABLE pool_contacts ADD COLUMN allocation_department TEXT NOT NULL DEFAULT '';
				CREATE TABLE pool_merge_conflicts (
					pool_id INTEGER, contact_id BIGINT, customer_code TEXT,
					existing_snapshot JSONB, incoming_snapshot JSONB, created_by_user_id INTEGER
				);`)
			org := env.seedOrganization("Import department")
			pool := env.seedPool("Empty codes")
			allocation := env.seedAllocation(pool, org)
			rows := []models.PoolContactImportRow{
				{Row: 2, Email: "one@example.com", AllocationDepartment: "Import department"},
				{Row: 3, CustomerCode: " \t ", Email: "ONE@example.com", AllocationDepartment: "Import department"},
				{Row: 4, Email: "two@example.com", AllocationDepartment: "Import department"},
			}
			result, err := env.core.ImportPoolContacts(pool, 1, rows, blocklist)
			if err != nil || result.Created != 2 || result.Invalid != 0 || result.Duplicates != 1 || result.Conflicts != 0 {
				t.Fatalf("empty code import: %+v, %v", result, err)
			}
			if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE customer_code=''`); got != 2 {
				t.Fatalf("contacts with empty codes = %d, want 2", got)
			}
			if got := env.countRows(`SELECT COUNT(*) FROM org_pool_allocation_members WHERE allocation_id=$1`, allocation); got != 2 {
				t.Fatalf("allocated contacts = %d, want 2", got)
			}
			rows[0].ReplyTo = "reply@example.com"
			result, err = env.core.ImportPoolContacts(pool, 1, rows[:1], blocklist)
			if err != nil || result.Created != 0 || result.Existing != 1 || result.Conflicts != 0 {
				t.Fatalf("empty code reimport: %+v, %v", result, err)
			}
			if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE email='one@example.com' AND reply_to='reply@example.com'`); got != 1 {
				t.Fatal("reimport did not update reply email")
			}
			if got := env.countRows(`SELECT COUNT(*) FROM pool_merge_conflicts`); got != 0 {
				t.Fatalf("empty codes generated %d conflicts", got)
			}
			if blocklist && env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE status='blocklisted'`) != 2 {
				t.Fatal("empty code contacts were not blocklisted")
			}
		})
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
