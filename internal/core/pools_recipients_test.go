package core

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
)

// The "active and not excluded pool member" rule is defined once in pools.go,
// but three code paths expand a pool audience: the first-level resolve
// (ResolvePoolRecipients), the secondary-list resolve
// (resolvePoolRecipientsForSegment) and the campaign snapshot writer shared by
// AttachPoolToCampaign and EnsurePoolCampaignRecipients. These tests need a real
// PostgreSQL server because the rule is a SQL fragment embedded in both a SELECT
// and an INSERT ... SELECT, and because the refresh semantics depend on
// ON CONFLICT ... DO UPDATE ... WHERE plus an enum-typed status column. Run them
// with POOL_RECIPIENTS_TEST_DSN pointing at a disposable database. They create
// and drop their own throwaway schema and never touch the public schema.
const poolRecipientsTestDSNEnv = "POOL_RECIPIENTS_TEST_DSN"

// poolRecipientsTestDDL mirrors the production shapes of the tables these paths
// touch (see schema.sql). Only the columns the audience expansion reads or
// writes are declared, but the keys, enum type and ON DELETE rules are the
// production ones because the refresh semantics depend on them.
const poolRecipientsTestDDL = `
CREATE TYPE campaign_recipient_status AS ENUM ('pending','queued','deferred','sent','cancelled');

CREATE TABLE users (
    id SERIAL PRIMARY KEY
);

CREATE TABLE organizations (
    id     BIGSERIAL PRIMARY KEY,
    name   TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE customer_lists (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    type            TEXT NOT NULL DEFAULT 'private',
    status          TEXT NOT NULL DEFAULT 'active',
    organization_id BIGINT,
    owner_user_id   INTEGER
);

CREATE TABLE reply_mailboxes (
    id              SERIAL PRIMARY KEY,
    email           TEXT NOT NULL,
    organization_id BIGINT,
    status          TEXT NOT NULL DEFAULT 'pending',
    verified_at     TIMESTAMPTZ
);

CREATE TABLE pool_contacts (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    customer_code TEXT NOT NULL DEFAULT '',
    company_name  TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    attribs       JSONB NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE pool_members (
    pool_id    INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_id, contact_id)
);

CREATE TABLE pool_segments (
    id                 BIGSERIAL PRIMARY KEY,
    list_id            INTEGER,
    pool_id            INTEGER REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    reply_mailbox_id   INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_pool_segments_pool_org ON pool_segments(pool_id, organization_id);

CREATE TABLE pool_segment_members (
    segment_id         BIGINT NOT NULL REFERENCES pool_segments(id) ON DELETE CASCADE,
    contact_id         BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','removed')),
    removed_reason     TEXT NOT NULL DEFAULT '',
    removed_at         TIMESTAMPTZ,
    removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (segment_id, contact_id)
);

CREATE TABLE pool_segment_exclusions (
    pool_id            INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_id         BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    segment_id         BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
    reason             TEXT NOT NULL DEFAULT 'manual',
    source             TEXT NOT NULL DEFAULT 'segment',
    removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    removed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    restored_at        TIMESTAMPTZ,
    PRIMARY KEY (pool_id, organization_id, contact_id)
);

CREATE TABLE pool_organization_permissions (
    pool_id            INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    granted_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_id, organization_id)
);

CREATE TABLE campaigns (
    id              SERIAL PRIMARY KEY,
    organization_id BIGINT
);

CREATE TABLE campaign_customer_lists (
    campaign_id               INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    customer_list_id          INTEGER,
    customer_list_name        TEXT NOT NULL DEFAULT '',
    pool_id                   INTEGER REFERENCES customer_lists(id) ON DELETE SET NULL,
    pool_segment_id           BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
    source_organization_id    BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    resolved_reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX idx_campaign_customer_lists_pool_unique ON campaign_customer_lists(campaign_id, pool_id, COALESCE(pool_segment_id, 0)) WHERE pool_id IS NOT NULL;

CREATE TABLE campaign_pool_recipients (
    campaign_id      INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    pool_contact_id  BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    pool_id          INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    segment_id       BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
    organization_id  BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
    status           campaign_recipient_status NOT NULL DEFAULT 'pending',
    email_snapshot   TEXT NOT NULL,
    name_snapshot    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (campaign_id, pool_contact_id)
);
`

// poolRecipientsTestEnv owns one throwaway schema and the single-connection
// session that keeps its search_path stable.
type poolRecipientsTestEnv struct {
	t      *testing.T
	schema string
	db     *sqlx.DB
	core   *Core
}

func newPoolRecipientsTestEnv(t *testing.T) *poolRecipientsTestEnv {
	t.Helper()
	dsn := os.Getenv(poolRecipientsTestDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set", poolRecipientsTestDSNEnv)
	}

	env := &poolRecipientsTestEnv{t: t, schema: fmt.Sprintf("pool_recipients_%d", time.Now().UnixNano())}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	// One connection keeps search_path (the throwaway schema) stable for every
	// statement, including the transactions the core methods open.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	env.db = db
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + env.schema + ` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		_ = db.Close()
	})
	if _, err := db.Exec(`CREATE SCHEMA ` + env.schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := db.Exec(`SET search_path TO ` + env.schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := db.Exec(poolRecipientsTestDDL); err != nil {
		t.Fatalf("install test schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users(id) VALUES(1)`); err != nil {
		t.Fatalf("seed test user: %v", err)
	}
	env.core = &Core{db: db}
	return env
}

func (env *poolRecipientsTestEnv) exec(query string, args ...any) {
	env.t.Helper()
	if _, err := env.db.Exec(query, args...); err != nil {
		env.t.Fatalf("exec %q: %v", query, err)
	}
}

func (env *poolRecipientsTestEnv) id(query string, args ...any) int64 {
	env.t.Helper()
	var id int64
	if err := env.db.Get(&id, query, args...); err != nil {
		env.t.Fatalf("query %q: %v", query, err)
	}
	return id
}

func (env *poolRecipientsTestEnv) seedOrganization(name string) int64 {
	env.t.Helper()
	return env.id(`INSERT INTO organizations(name) VALUES($1) RETURNING id`, name)
}

func (env *poolRecipientsTestEnv) seedPool(name string) int {
	env.t.Helper()
	return int(env.id(`INSERT INTO customer_lists(name,type,status) VALUES($1,$2,'active') RETURNING id`, name, models.CustomerListTypePool))
}

func (env *poolRecipientsTestEnv) seedMailbox(organizationID int64, email string) int64 {
	env.t.Helper()
	return env.id(`INSERT INTO reply_mailboxes(organization_id,email,status,verified_at) VALUES($1,$2,'active',NOW()) RETURNING id`, organizationID, email)
}

func (env *poolRecipientsTestEnv) seedSegment(poolID int, organizationID int64, mailboxID *int64) int64 {
	env.t.Helper()
	return env.id(`INSERT INTO pool_segments(pool_id,organization_id,reply_mailbox_id,created_by_user_id) VALUES($1,$2,$3,1) RETURNING id`, poolID, organizationID, mailboxID)
}

func (env *poolRecipientsTestEnv) seedContact(code, name, email, status string) int64 {
	env.t.Helper()
	return env.id(`INSERT INTO pool_contacts(customer_code,company_name,email,name,status) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		code, "Company "+code, email, name, status)
}

func (env *poolRecipientsTestEnv) joinPool(poolID int, contactID int64) {
	env.t.Helper()
	env.exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, poolID, contactID)
}

func (env *poolRecipientsTestEnv) allocate(segmentID, contactID int64, status string) {
	env.t.Helper()
	env.exec(`INSERT INTO pool_segment_members(segment_id,contact_id,status) VALUES($1,$2,$3)`, segmentID, contactID, status)
}

func (env *poolRecipientsTestEnv) exclude(poolID int, organizationID int64, segmentID, contactID int64, restored bool) {
	env.t.Helper()
	env.exec(`INSERT INTO pool_segment_exclusions(pool_id,organization_id,contact_id,segment_id,reason,restored_at)
		VALUES($1,$2,$3,$4,'manual',CASE WHEN $5 THEN NOW() ELSE NULL END)`, poolID, organizationID, contactID, segmentID, restored)
}

func (env *poolRecipientsTestEnv) seedCampaign() int {
	env.t.Helper()
	return int(env.id(`INSERT INTO campaigns(organization_id) VALUES(NULL) RETURNING id`))
}

// seedAudience writes the campaign relation EnsurePoolCampaignRecipients reads.
// A nil segmentID is a first-level selection; it resolves to the single
// secondary list the organization owns.
func (env *poolRecipientsTestEnv) seedAudience(campaignID, poolID int, organizationID int64, segmentID, mailboxID *int64) {
	env.t.Helper()
	env.exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,pool_segment_id,source_organization_id,resolved_reply_mailbox_id)
		VALUES($1,NULL,'pool audience',$2,$3,$4,$5)`, campaignID, poolID, segmentID, organizationID, mailboxID)
}

// recipientIDs returns the resolved pool contact IDs in the order the shared
// rule returns them.
func recipientIDs(recipients []PoolRecipient) []int64 {
	ids := make([]int64, 0, len(recipients))
	for _, r := range recipients {
		ids = append(ids, r.ID)
	}
	return ids
}

func mustResolve(t *testing.T, label string, resolve func() ([]PoolRecipient, error)) []int64 {
	t.Helper()
	recipients, err := resolve()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	return recipientIDs(recipients)
}

// assertSameIDs compares recipient sets regardless of the order the caller
// returned them in.
func assertSameIDs(t *testing.T, label string, got, want []int64) {
	t.Helper()
	gotSorted := append([]int64(nil), got...)
	wantSorted := append([]int64(nil), want...)
	slices.Sort(gotSorted)
	slices.Sort(wantSorted)
	if !slices.Equal(gotSorted, wantSorted) {
		t.Errorf("%s recipients = %v, want %v", label, gotSorted, wantSorted)
	}
}

// snapshotIDs reads the whole campaign snapshot, sorted by pool contact ID, and
// fails on a duplicate recipient.
func (env *poolRecipientsTestEnv) snapshotIDs(campaignID int) []int64 {
	env.t.Helper()
	return env.querySnapshotIDs(campaignID, "")
}

// ownedSnapshotIDs reads only the snapshot rows a refresh owns: the rows that
// have not been handed to delivery yet. The three audience paths must agree on
// this set for every fixture.
func (env *poolRecipientsTestEnv) ownedSnapshotIDs(campaignID int) []int64 {
	env.t.Helper()
	return env.querySnapshotIDs(campaignID, ` AND status IN ('pending','deferred')`)
}

func (env *poolRecipientsTestEnv) querySnapshotIDs(campaignID int, filter string) []int64 {
	env.t.Helper()
	var rows []struct {
		ContactID int64 `db:"pool_contact_id"`
		Status    string
	}
	if err := env.db.Select(&rows, `SELECT pool_contact_id,status::TEXT AS status FROM campaign_pool_recipients WHERE campaign_id=$1`+filter, campaignID); err != nil {
		env.t.Fatalf("read campaign snapshot: %v", err)
	}
	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ContactID]; ok {
			env.t.Fatalf("campaign %d snapshot duplicates pool contact %d", campaignID, row.ContactID)
		}
		seen[row.ContactID] = struct{}{}
		ids = append(ids, row.ContactID)
	}
	slices.Sort(ids)
	return ids
}

// poolSnapshotRow is one campaign_pool_recipients row as the send paths read it.
type poolSnapshotRow struct {
	PoolContactID   int64  `db:"pool_contact_id"`
	PoolID          int    `db:"pool_id"`
	SegmentID       *int64 `db:"segment_id"`
	OrganizationID  *int64 `db:"organization_id"`
	ReplyMailboxID  *int64 `db:"reply_mailbox_id"`
	Status          string `db:"status"`
	EmailSnapshot   string `db:"email_snapshot"`
	NameSnapshot    string `db:"name_snapshot"`
	PoolContactMail string `db:"pool_contact_email"`
}

func (env *poolRecipientsTestEnv) snapshotRows(campaignID int) map[int64]poolSnapshotRow {
	env.t.Helper()
	var rows []poolSnapshotRow
	if err := env.db.Select(&rows, `SELECT cpr.pool_contact_id,cpr.pool_id,cpr.segment_id,cpr.organization_id,cpr.reply_mailbox_id,
			cpr.status::TEXT AS status,cpr.email_snapshot,cpr.name_snapshot,pc.email AS pool_contact_email
		FROM campaign_pool_recipients cpr
		JOIN pool_contacts pc ON pc.id=cpr.pool_contact_id
		WHERE cpr.campaign_id=$1`, campaignID); err != nil {
		env.t.Fatalf("read campaign snapshot rows: %v", err)
	}
	out := make(map[int64]poolSnapshotRow, len(rows))
	for _, row := range rows {
		out[row.PoolContactID] = row
	}
	return out
}

func (env *poolRecipientsTestEnv) countRows(query string, args ...any) int {
	env.t.Helper()
	var n int
	if err := env.db.Get(&n, query, args...); err != nil {
		env.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// TestValidatePoolCampaignAudienceRefreshesRoutes covers drafts that were
// created before the organization manager configured the secondary-list
// mailbox. Preview/send validation must use the current segment setting and
// repair the campaign relation instead of keeping the old NULL forever.
func TestValidatePoolCampaignAudienceRefreshesRoutes(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	org := env.seedOrganization("org-a")
	poolID := env.seedPool("ws-pool")
	mailbox := env.seedMailbox(org, "pool-a@example.invalid")
	segment := env.seedSegment(poolID, org, nil)
	campaign := env.seedCampaign()
	env.seedAudience(campaign, poolID, org, nil, nil)

	if err := env.core.ValidatePoolCampaignAudience(campaign); err == nil {
		t.Fatal("ValidatePoolCampaignAudience accepted an unconfigured segment mailbox")
	} else if err.Error() != "code=400, message=public-pool audience requires an organization segment and reply mailbox before previewing or sending" {
		t.Fatalf("ValidatePoolCampaignAudience error = %v, want the unresolved preview/send message", err)
	}

	env.exec(`UPDATE pool_segments SET reply_mailbox_id=$2 WHERE id=$1`, segment, mailbox)
	if err := env.core.ValidatePoolCampaignAudience(campaign); err != nil {
		t.Fatalf("ValidatePoolCampaignAudience after mailbox configuration: %v", err)
	}

	var resolved int64
	if err := env.db.Get(&resolved, `SELECT resolved_reply_mailbox_id FROM campaign_customer_lists WHERE campaign_id=$1`, campaign); err != nil {
		t.Fatalf("read resolved mailbox: %v", err)
	}
	if resolved != mailbox {
		t.Fatalf("resolved mailbox = %d, want %d", resolved, mailbox)
	}

	env.exec(`UPDATE reply_mailboxes SET status='disabled' WHERE id=$1`, mailbox)
	if err := env.core.ValidatePoolCampaignAudience(campaign); err == nil {
		t.Fatal("ValidatePoolCampaignAudience accepted a disabled mailbox")
	}
}

// TestPoolRecipientPathsAgreeOnDeliverableMembers is the drift test: the same
// fixture is expanded by both resolve paths, by AttachPoolToCampaign for a
// secondary-list and for a first-level audience, and by
// EnsurePoolCampaignRecipients for a segment-scoped and a pool-wide relation.
// All of them must produce the same recipient set, including the boundary
// members: an excluded contact, a restored exclusion, a removed allocation, an
// archived contact, an allocation without pool membership, and a contact that
// belongs to a second organization's audience.
func TestPoolRecipientPathsAgreeOnDeliverableMembers(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	orgA := env.seedOrganization("org-a")
	orgB := env.seedOrganization("org-b")
	poolID := env.seedPool("ws-pool")
	mailboxA := env.seedMailbox(orgA, "pool-a@example.invalid")
	mailboxB := env.seedMailbox(orgB, "pool-b@example.invalid")
	segmentA := env.seedSegment(poolID, orgA, &mailboxA)
	segmentB := env.seedSegment(poolID, orgB, &mailboxB)

	deliverable := env.seedContact("C-1", "Deliverable", "keep@example.invalid", "active")
	excluded := env.seedContact("C-2", "Excluded", "excluded@example.invalid", "active")
	removedAllocation := env.seedContact("C-3", "Removed allocation", "removed@example.invalid", "active")
	archivedContact := env.seedContact("C-4", "Archived contact", "archived@example.invalid", "archived")
	restored := env.seedContact("C-5", "Restored exclusion", "restored@example.invalid", "active")
	unallocated := env.seedContact("C-6", "Allocation without pool membership", "unallocated@example.invalid", "active")
	orgBOnly := env.seedContact("C-7", "Other organization", "other-org@example.invalid", "active")
	shared := env.seedContact("C-8", "Both organizations", "shared@example.invalid", "active")

	for _, id := range []int64{deliverable, excluded, removedAllocation, archivedContact, restored, orgBOnly, shared} {
		env.joinPool(poolID, id)
	}
	env.allocate(segmentA, deliverable, "active")
	env.allocate(segmentA, excluded, "active")
	env.allocate(segmentA, removedAllocation, "removed")
	env.allocate(segmentA, archivedContact, "active")
	env.allocate(segmentA, restored, "active")
	env.allocate(segmentA, unallocated, "active")
	env.allocate(segmentB, orgBOnly, "active")
	env.allocate(segmentA, shared, "active")
	env.allocate(segmentB, shared, "active")
	env.exclude(poolID, orgA, segmentA, excluded, false)
	env.exclude(poolID, orgA, segmentA, restored, true)

	wantA := []int64{deliverable, restored, shared}
	wantB := []int64{orgBOnly, shared}

	// Path 1: first-level resolve for the organization.
	firstLevel := mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, orgA)
	})
	assertSameIDs(t, "ResolvePoolRecipients", firstLevel, wantA)

	// Path 2: secondary-list resolve. It must not merely contain the same
	// contacts but return them in the same order for the same audience.
	segmentResolve := mustResolve(t, "resolvePoolRecipientsForSegment", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForSegment(poolID, orgA, segmentA)
	})
	assertSameIDs(t, "resolvePoolRecipientsForSegment", segmentResolve, wantA)
	if !slices.Equal(firstLevel, segmentResolve) {
		t.Errorf("resolve paths disagree on row order/content: first-level=%v segment=%v", firstLevel, segmentResolve)
	}

	// Path 3: attach an explicitly selected secondary list.
	segmentCampaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(segmentCampaign, poolID, &segmentA, orgA); err != nil {
		t.Fatalf("AttachPoolToCampaign(segment): %v", err)
	}
	assertSameIDs(t, "AttachPoolToCampaign(segment)", env.snapshotIDs(segmentCampaign), wantA)
	assertSameIDs(t, "AttachPoolToCampaign(segment) owned rows", env.ownedSnapshotIDs(segmentCampaign), wantA)

	// Path 4: attach the first-level pool, which resolves to the organization's
	// single secondary list.
	firstLevelCampaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(firstLevelCampaign, poolID, nil, orgA); err != nil {
		t.Fatalf("AttachPoolToCampaign(first-level): %v", err)
	}
	assertSameIDs(t, "AttachPoolToCampaign(first-level)", env.snapshotIDs(firstLevelCampaign), wantA)

	// Path 5: refresh a segment-scoped campaign_customer_lists relation.
	ensureSegmentCampaign := env.seedCampaign()
	env.seedAudience(ensureSegmentCampaign, poolID, orgA, &segmentA, &mailboxA)
	if err := env.core.EnsurePoolCampaignRecipients(ensureSegmentCampaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients(segment): %v", err)
	}
	assertSameIDs(t, "EnsurePoolCampaignRecipients(segment)", env.snapshotIDs(ensureSegmentCampaign), wantA)

	// Path 6: refresh a pool-wide relation, twice, to prove idempotence.
	ensurePoolCampaign := env.seedCampaign()
	env.seedAudience(ensurePoolCampaign, poolID, orgA, nil, &mailboxA)
	for i := 0; i < 2; i++ {
		if err := env.core.EnsurePoolCampaignRecipients(ensurePoolCampaign); err != nil {
			t.Fatalf("EnsurePoolCampaignRecipients(first-level) run %d: %v", i+1, err)
		}
		assertSameIDs(t, fmt.Sprintf("EnsurePoolCampaignRecipients(first-level) run %d", i+1), env.snapshotIDs(ensurePoolCampaign), wantA)
	}

	// Organization scoping: the same first-level pool resolved for another
	// organization yields only that organization's allocation, and a campaign
	// snapshot never mixes the two.
	otherOrg := mustResolve(t, "ResolvePoolRecipients(org B)", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, orgB)
	})
	assertSameIDs(t, "ResolvePoolRecipients(org B)", otherOrg, wantB)
	otherOrgCampaign := env.seedCampaign()
	env.seedAudience(otherOrgCampaign, poolID, orgB, &segmentB, &mailboxB)
	if err := env.core.EnsurePoolCampaignRecipients(otherOrgCampaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients(org B): %v", err)
	}
	assertSameIDs(t, "EnsurePoolCampaignRecipients(org B)", env.snapshotIDs(otherOrgCampaign), wantB)

	// De-duplication by pool contact id: a campaign that selects both the
	// first-level pool and its secondary list still holds exactly one row per
	// contact.
	dedupCampaign := env.seedCampaign()
	env.seedAudience(dedupCampaign, poolID, orgA, &segmentA, &mailboxA)
	env.seedAudience(dedupCampaign, poolID, orgA, nil, &mailboxA)
	if err := env.core.EnsurePoolCampaignRecipients(dedupCampaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients(segment + first-level): %v", err)
	}
	assertSameIDs(t, "EnsurePoolCampaignRecipients(segment + first-level)", env.snapshotIDs(dedupCampaign), wantA)
	if n := env.countRows(`SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$1`, dedupCampaign); n != len(wantA) {
		t.Errorf("snapshot rows for a doubly-selected audience = %d, want %d", n, len(wantA))
	}

	// Provenance: every snapshot row names the resolved secondary list,
	// organization and per-list reply mailbox.
	rows := env.snapshotRows(firstLevelCampaign)
	for _, contactID := range wantA {
		row, ok := rows[contactID]
		if !ok {
			t.Fatalf("contact %d is missing from the snapshot", contactID)
		}
		if row.SegmentID == nil || *row.SegmentID != segmentA {
			t.Errorf("contact %d segment_id = %v, want %d", contactID, row.SegmentID, segmentA)
		}
		if row.OrganizationID == nil || *row.OrganizationID != orgA {
			t.Errorf("contact %d organization_id = %v, want %d", contactID, row.OrganizationID, orgA)
		}
		if row.ReplyMailboxID == nil || *row.ReplyMailboxID != mailboxA {
			t.Errorf("contact %d reply_mailbox_id = %v, want %d", contactID, row.ReplyMailboxID, mailboxA)
		}
		if row.EmailSnapshot != row.PoolContactMail {
			t.Errorf("contact %d email_snapshot = %q, want %q", contactID, row.EmailSnapshot, row.PoolContactMail)
		}
	}
}

// TestPoolRecipientSnapshotRefreshAfterExclusion covers the boundary the drift
// defect names directly: a contact that is excluded after the snapshot was
// written must lose its snapshot row on the next refresh, and re-attaching the
// audience must not resurrect it.
func TestPoolRecipientSnapshotRefreshAfterExclusion(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	org := env.seedOrganization("org-a")
	poolID := env.seedPool("ws-pool")
	mailbox := env.seedMailbox(org, "pool-a@example.invalid")
	segment := env.seedSegment(poolID, org, &mailbox)

	keep := env.seedContact("C-1", "Keep", "keep@example.invalid", "active")
	excludedLater := env.seedContact("C-2", "Excluded later", "later@example.invalid", "active")
	for _, id := range []int64{keep, excludedLater} {
		env.joinPool(poolID, id)
		env.allocate(segment, id, "active")
	}

	campaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &segment, org); err != nil {
		t.Fatalf("AttachPoolToCampaign: %v", err)
	}
	assertSameIDs(t, "snapshot after attach", env.snapshotIDs(campaign), []int64{keep, excludedLater})

	// The organization removes the contact from its allocation. This is the
	// shipped removal path: it deactivates the allocation and writes the
	// organization-scoped exclusion.
	if err := env.core.RemovePoolContact(segment, excludedLater, 1, "manual"); err != nil {
		t.Fatalf("RemovePoolContact: %v", err)
	}
	assertSameIDs(t, "ResolvePoolRecipients after exclusion", mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, org)
	}), []int64{keep})
	assertSameIDs(t, "resolvePoolRecipientsForSegment after exclusion", mustResolve(t, "resolvePoolRecipientsForSegment", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForSegment(poolID, org, segment)
	}), []int64{keep})
	// The snapshot is still stale at this point: nothing has refreshed it yet.
	assertSameIDs(t, "snapshot before refresh", env.snapshotIDs(campaign), []int64{keep, excludedLater})

	// The refresh must drop the newly excluded row, not merely stop rewriting it.
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients after exclusion: %v", err)
	}
	assertSameIDs(t, "snapshot after refresh", env.snapshotIDs(campaign), []int64{keep})
	if n := env.countRows(`SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, campaign, excludedLater); n != 0 {
		t.Errorf("excluded contact kept %d snapshot rows, want 0", n)
	}

	// Re-attaching the same audience is a refresh too, so it cannot bring the
	// excluded contact back.
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &segment, org); err != nil {
		t.Fatalf("AttachPoolToCampaign after exclusion: %v", err)
	}
	assertSameIDs(t, "snapshot after re-attach", env.snapshotIDs(campaign), []int64{keep})
	assertSameIDs(t, "owned snapshot rows after re-attach", env.ownedSnapshotIDs(campaign), []int64{keep})

	// Restoring the allocation must make the contact deliverable again, and the
	// refresh must re-add the row it pruned.
	if err := env.core.RestorePoolContact(segment, excludedLater); err != nil {
		t.Fatalf("RestorePoolContact: %v", err)
	}
	assertSameIDs(t, "ResolvePoolRecipients after restore", mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, org)
	}), []int64{keep, excludedLater})
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients after restore: %v", err)
	}
	assertSameIDs(t, "snapshot after restore", env.snapshotIDs(campaign), []int64{keep, excludedLater})
}

// TestPoolRecipientSnapshotRefreshDropsNonExclusionRemovals covers the boundary
// that no exclusion row records: an allocation that was set to removed and a
// pool contact that was archived. The queue path only re-checks exclusions, so
// the snapshot refresh is the only guard for these two.
func TestPoolRecipientSnapshotRefreshDropsNonExclusionRemovals(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	org := env.seedOrganization("org-a")
	poolID := env.seedPool("ws-pool")
	mailbox := env.seedMailbox(org, "pool-a@example.invalid")
	segment := env.seedSegment(poolID, org, &mailbox)

	keep := env.seedContact("C-1", "Keep", "keep@example.invalid", "active")
	removedAllocation := env.seedContact("C-2", "Reallocated", "reallocated@example.invalid", "active")
	archived := env.seedContact("C-3", "Archived", "archived@example.invalid", "active")
	for _, id := range []int64{keep, removedAllocation, archived} {
		env.joinPool(poolID, id)
		env.allocate(segment, id, "active")
	}

	campaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &segment, org); err != nil {
		t.Fatalf("AttachPoolToCampaign: %v", err)
	}
	assertSameIDs(t, "snapshot after attach", env.snapshotIDs(campaign), []int64{keep, removedAllocation, archived})

	env.exec(`UPDATE pool_segment_members SET status='removed',removed_reason='reallocated',removed_at=NOW() WHERE segment_id=$1 AND contact_id=$2`, segment, removedAllocation)
	env.exec(`UPDATE pool_contacts SET status='archived',updated_at=NOW() WHERE id=$1`, archived)

	if n := env.countRows(`SELECT COUNT(*) FROM pool_segment_exclusions`); n != 0 {
		t.Fatalf("fixture wrote %d exclusion rows; this case must not rely on one", n)
	}
	assertSameIDs(t, "ResolvePoolRecipients", mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, org)
	}), []int64{keep})
	assertSameIDs(t, "resolvePoolRecipientsForSegment", mustResolve(t, "resolvePoolRecipientsForSegment", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForSegment(poolID, org, segment)
	}), []int64{keep})

	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients: %v", err)
	}
	assertSameIDs(t, "snapshot after refresh", env.snapshotIDs(campaign), []int64{keep})
}

// TestPoolRecipientSnapshotRefreshRewritesOwnedRowsOnly pins the chosen upsert
// semantics. DO UPDATE must rewrite the rows a refresh still owns (the contact
// was edited after the snapshot was written), and the shared status predicate
// must leave rows that were already handed to delivery exactly as delivered.
func TestPoolRecipientSnapshotRefreshRewritesOwnedRowsOnly(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	org := env.seedOrganization("org-a")
	poolID := env.seedPool("ws-pool")
	mailbox := env.seedMailbox(org, "pool-a@example.invalid")
	segment := env.seedSegment(poolID, org, &mailbox)

	pending := env.seedContact("C-1", "Pending", "pending@example.invalid", "active")
	sent := env.seedContact("C-2", "Sent", "sent@example.invalid", "active")
	queued := env.seedContact("C-3", "Queued", "queued@example.invalid", "active")
	deferred := env.seedContact("C-4", "Deferred", "deferred@example.invalid", "active")
	cancelled := env.seedContact("C-5", "Cancelled", "cancelled@example.invalid", "active")
	stillDeliverable := env.seedContact("C-6", "Still deliverable", "still@example.invalid", "active")
	for _, id := range []int64{pending, sent, queued, deferred, cancelled, stillDeliverable} {
		env.joinPool(poolID, id)
		env.allocate(segment, id, "active")
	}

	campaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &segment, org); err != nil {
		t.Fatalf("AttachPoolToCampaign: %v", err)
	}
	if n := env.countRows(`SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$1`, campaign); n != 6 {
		t.Fatalf("snapshot rows after attach = %d, want 6", n)
	}
	for contactID, status := range map[int64]string{
		sent:      models.CampaignRecipientStatusSent,
		queued:    models.CampaignRecipientStatusQueued,
		deferred:  models.CampaignRecipientStatusDeferred,
		cancelled: models.CampaignRecipientStatusCancelled,
	} {
		env.exec(`UPDATE campaign_pool_recipients SET status=$3::campaign_recipient_status WHERE campaign_id=$1 AND pool_contact_id=$2`, campaign, contactID, status)
	}

	// Every contact except the last one is excluded afterwards.
	for _, id := range []int64{pending, sent, queued, deferred, cancelled} {
		env.exclude(poolID, org, segment, id, false)
	}
	// The contact that is still deliverable was edited after the snapshot was
	// written; the sent row's contact was edited too, but its snapshot must stay
	// exactly what was delivered.
	env.exec(`UPDATE pool_contacts SET email='moved@example.invalid',name='Moved' WHERE id=$1`, stillDeliverable)
	env.exec(`UPDATE pool_contacts SET email='sent-moved@example.invalid' WHERE id=$1`, sent)

	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients: %v", err)
	}

	assertSameIDs(t, "snapshot after refresh", env.snapshotIDs(campaign), []int64{sent, queued, cancelled, stillDeliverable})
	// The refresh-owned rows equal the resolved audience exactly, which is the
	// invariant the three paths share.
	assertSameIDs(t, "owned snapshot rows after refresh", env.ownedSnapshotIDs(campaign), []int64{stillDeliverable})
	assertSameIDs(t, "resolved audience after exclusion", mustResolve(t, "resolvePoolRecipientsForSegment", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForSegment(poolID, org, segment)
	}), []int64{stillDeliverable})

	rows := env.snapshotRows(campaign)
	if got := rows[stillDeliverable]; got.EmailSnapshot != "moved@example.invalid" || got.NameSnapshot != "Moved" {
		t.Errorf("owned row was not refreshed: email_snapshot=%q name_snapshot=%q", got.EmailSnapshot, got.NameSnapshot)
	}
	if got := rows[sent]; got.EmailSnapshot != "sent@example.invalid" || got.Status != models.CampaignRecipientStatusSent {
		t.Errorf("delivered row was rewritten: email_snapshot=%q status=%q", got.EmailSnapshot, got.Status)
	}
	if got := rows[queued]; got.Status != models.CampaignRecipientStatusQueued {
		t.Errorf("queued row status = %q, want %q", got.Status, models.CampaignRecipientStatusQueued)
	}
	if got := rows[cancelled]; got.Status != models.CampaignRecipientStatusCancelled {
		t.Errorf("cancelled row status = %q, want %q", got.Status, models.CampaignRecipientStatusCancelled)
	}
	for _, id := range []int64{pending, deferred} {
		if n := env.countRows(`SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, campaign, id); n != 0 {
			t.Errorf("refresh-owned row for excluded contact %d survived: %d rows", id, n)
		}
	}

	// A second refresh is idempotent.
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("second EnsurePoolCampaignRecipients: %v", err)
	}
	assertSameIDs(t, "snapshot after second refresh", env.snapshotIDs(campaign), []int64{sent, queued, cancelled, stillDeliverable})
	rows = env.snapshotRows(campaign)
	if got := rows[sent]; got.EmailSnapshot != "sent@example.invalid" {
		t.Errorf("delivered row changed on the second refresh: email_snapshot=%q", got.EmailSnapshot)
	}
}
