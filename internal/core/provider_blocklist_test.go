package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

// providerBlocklistTestDDL adds the customer table shape the failure-address
// matcher reads. The pool tables and the throwaway-schema bootstrap come from
// the shared pool test harness.
const providerBlocklistTestDDL = `
CREATE TABLE customers (
    id              SERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    customer_code   TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'enabled',
    attribs         JSONB NOT NULL DEFAULT '{}',
    organization_id BIGINT,
    owner_user_id   INTEGER,
    original_owner_user_id INTEGER,
    visibility      TEXT NOT NULL DEFAULT 'private',
    transfer_pending_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

type providerBlocklistEnv struct {
	*poolRecipientsTestEnv
	pool       int
	orgA, orgB int64
	contactA   int64
	contactB   int64
	archived   int64
}

func newProviderBlocklistEnv(t *testing.T) *providerBlocklistEnv {
	t.Helper()
	env := &providerBlocklistEnv{poolRecipientsTestEnv: newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+providerBlocklistTestDDL)}
	env.orgA = env.seedOrganization("Org A")
	env.orgB = env.seedOrganization("Org B")
	env.pool = env.seedPool("Shared pool")
	env.exec(`INSERT INTO users(id) VALUES(2)`)
	env.exec(`INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,1,'member')`, env.orgA)
	env.exec(`INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,2,'member')`, env.orgB)
	env.contactA = env.id(`INSERT INTO pool_contacts(email,name,customer_code) VALUES('Blocked@Example.com','Alpha','C1') RETURNING id`)
	env.contactB = env.id(`INSERT INTO pool_contacts(email,name,customer_code) VALUES('blocked@example.com','Beta','C2') RETURNING id`)
	env.archived = env.id(`INSERT INTO pool_contacts(email,name,status) VALUES('old@example.com','Archived','archived') RETURNING id`)
	for _, contact := range []int64{env.contactA, env.contactB, env.archived} {
		env.exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, env.pool, contact)
	}
	env.exec(`INSERT INTO org_pool_allocations(pool_id,organization_id) VALUES($1,$2)`, env.pool, env.orgA)
	env.exec(`INSERT INTO org_pool_allocations(pool_id,organization_id) VALUES($1,$2)`, env.pool, env.orgB)
	env.exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id)
		SELECT id,$2 FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$3`, env.pool, env.contactA, env.orgA)
	env.exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id)
		SELECT id,$2 FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$3`, env.pool, env.contactB, env.orgB)
	// One private customer owned by user 1 with the same address, so a match can
	// return both a private and a pool target.
	env.exec(`INSERT INTO customers(email,name,customer_code,organization_id,owner_user_id,visibility)
		VALUES('blocked@example.com','Private','P1',NULL,1,'private')`)
	return env
}

func (env *providerBlocklistEnv) match(t *testing.T, access models.WorkspaceAccess) []ProviderFailureMatch {
	t.Helper()
	matches, err := env.core.MatchProviderFailureEmails(access, []string{" BLOCKED@example.com "})
	if err != nil {
		t.Fatalf("match failed addresses: %v", err)
	}
	return matches["blocked@example.com"]
}

func countKind(matches []ProviderFailureMatch, kind string) int {
	n := 0
	for _, match := range matches {
		if match.Kind == kind {
			n++
		}
	}
	return n
}

// TestMatchProviderFailureEmailsRespectsWorkspaceScope pins the boundary that
// makes the report safe: an address is only resolved to targets the caller may
// already act on, never to another workspace's rows.
func TestMatchProviderFailureEmailsRespectsWorkspaceScope(t *testing.T) {
	env := newProviderBlocklistEnv(t)

	platform := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: 1}
	if matches := env.match(t, platform); len(matches) != 3 || countKind(matches, "pool") != 2 || countKind(matches, "private") != 1 {
		t.Fatalf("platform admin matches = %#v, want 2 pool + 1 private", matches)
	}

	// Organization A only holds allocation membership for contactA.
	orgA := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(env.orgA)}, UserID: 1}
	matches := env.match(t, orgA)
	if len(matches) != 1 || matches[0].Kind != "pool" || matches[0].ID != env.contactA {
		t.Fatalf("organization A matches = %#v, want only contactA", matches)
	}

	// Personal workspace of user 1 sees the private customer but no pool rows.
	personal := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 1}
	matches = env.match(t, personal)
	if len(matches) != 1 || matches[0].Kind != "private" {
		t.Fatalf("personal matches = %#v, want only the private customer", matches)
	}

	// Another user must not see user 1's private customer.
	other := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 2}
	if matches := env.match(t, other); len(matches) != 0 {
		t.Fatalf("other user matches = %#v, want none", matches)
	}
}

// TestManagedWorkspaceCustomerIDsFiltersWholeBatch pins the single-query
// boundary used by the batch blocklist action.
func TestManagedWorkspaceCustomerIDsFiltersWholeBatch(t *testing.T) {
	env := newProviderBlocklistEnv(t)
	ownedByOne := int(env.id(`SELECT id FROM customers WHERE email='blocked@example.com'`))
	ownedByTwo := int(env.id(`INSERT INTO customers(email,organization_id,owner_user_id,visibility)
		VALUES('two@example.com',NULL,2,'private') RETURNING id`))
	ids := []int{ownedByOne, ownedByTwo}

	platform := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: 1}
	if got, err := env.core.ManagedWorkspaceCustomerIDs(platform, ids); err != nil || len(got) != 2 {
		t.Fatalf("platform managed ids = %v (err %v), want both", got, err)
	}
	userOne := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 1}
	if got, err := env.core.ManagedWorkspaceCustomerIDs(userOne, ids); err != nil || len(got) != 1 || got[0] != ownedByOne {
		t.Fatalf("user one managed ids = %v (err %v), want only %d", got, err, ownedByOne)
	}
	userTwo := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 2}
	if got, err := env.core.ManagedWorkspaceCustomerIDs(userTwo, ids); err != nil || len(got) != 1 || got[0] != ownedByTwo {
		t.Fatalf("user two managed ids = %v (err %v), want only %d", got, err, ownedByTwo)
	}
}

// TestSetPoolContactsBlocklistKeepsArchivedRows protects the invalid-address
// lifecycle: blocklisting (or removing) must never resurrect a cleared address.
func TestSetPoolContactsBlocklistKeepsArchivedRows(t *testing.T) {
	env := newProviderBlocklistEnv(t)

	changed, err := env.core.SetPoolContactsBlocklist([]int64{env.contactA, env.archived, env.contactA}, true)
	if err != nil {
		t.Fatalf("blocklist pool contacts: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed rows = %d, want 1 (archived and duplicate excluded)", changed)
	}
	var status string
	if err := env.db.Get(&status, `SELECT status FROM pool_contacts WHERE id=$1`, env.contactA); err != nil || status != models.CustomerStatusBlockListed {
		t.Fatalf("contact A status = %q (err %v), want blocklisted", status, err)
	}
	if err := env.db.Get(&status, `SELECT status FROM pool_contacts WHERE id=$1`, env.archived); err != nil || status != "archived" {
		t.Fatalf("archived status = %q (err %v), want archived", status, err)
	}

	platform := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: 1}
	for _, match := range env.match(t, platform) {
		if match.Kind == "pool" && match.ID == env.contactA && !match.Blocked {
			t.Fatalf("match after blocklist = %#v, want blocked", match)
		}
	}

	if _, err := env.core.SetPoolContactsBlocklist([]int64{env.contactA}, false); err != nil {
		t.Fatalf("unblocklist pool contacts: %v", err)
	}
	if err := env.db.Get(&status, `SELECT status FROM pool_contacts WHERE id=$1`, env.contactA); err != nil || status != "active" {
		t.Fatalf("contact A status after unblock = %q (err %v), want active", status, err)
	}
}
