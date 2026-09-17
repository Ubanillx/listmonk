package core

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
)

// The "active and not excluded pool member" rule is defined once in pools.go,
// but three code paths expand a pool audience: the first-level resolve
// (ResolvePoolRecipients), the pool-allocation resolve
// (resolvePoolRecipientsForAllocation) and the campaign snapshot writer shared by
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
    id SERIAL PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'enabled'
);

-- The organization SMTP pool readiness check reads these two tables.
CREATE TABLE user_smtp_servers (
    id          SERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    from_email  TEXT NOT NULL DEFAULT '',
    daily_limit INT NOT NULL DEFAULT 0,
    host        TEXT NOT NULL DEFAULT '',
    port        INT NOT NULL DEFAULT 465
);

CREATE TABLE user_smtp_daily_usage (
    smtp_uuid  UUID NOT NULL REFERENCES user_smtp_servers(uuid) ON DELETE CASCADE,
    usage_date DATE NOT NULL,
    sent_count INT NOT NULL DEFAULT 0,
    PRIMARY KEY (smtp_uuid, usage_date)
);

CREATE TABLE organizations (
    id     BIGSERIAL PRIMARY KEY,
    name   TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE organization_members (
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            TEXT NOT NULL DEFAULT 'member',
    removed_at      TIMESTAMPTZ,
    PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE customer_lists (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    type            TEXT NOT NULL DEFAULT 'private',
    status          TEXT NOT NULL DEFAULT 'active',
    organization_id BIGINT,
    owner_user_id   INTEGER,
    original_owner_user_id INTEGER
);

CREATE TABLE reply_mailboxes (
    id              SERIAL PRIMARY KEY,
    email           TEXT NOT NULL,
    organization_id BIGINT,
    status          TEXT NOT NULL DEFAULT 'pending',
    verified_at     TIMESTAMPTZ
);
-- schema.sql adds the organization's unified reply mailbox through a trailing
-- ALTER because organizations is created before reply_mailboxes.
ALTER TABLE organizations ADD COLUMN reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL;

CREATE TABLE pool_contacts (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    customer_code TEXT NOT NULL DEFAULT '',
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

CREATE TABLE org_pool_allocations (
    id                 BIGSERIAL PRIMARY KEY,
    list_id            INTEGER,
    pool_id            INTEGER REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_org_pool_allocations_pool_org ON org_pool_allocations(pool_id, organization_id);

CREATE TABLE org_pool_allocation_members (
    allocation_id         BIGINT NOT NULL REFERENCES org_pool_allocations(id) ON DELETE CASCADE,
    contact_id         BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','removed')),
    removed_reason     TEXT NOT NULL DEFAULT '',
    removed_at         TIMESTAMPTZ,
    removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (allocation_id, contact_id)
);

CREATE TABLE org_pool_allocation_exclusions (
    pool_id            INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_id         BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    allocation_id         BIGINT REFERENCES org_pool_allocations(id) ON DELETE SET NULL,
    reason             TEXT NOT NULL DEFAULT 'manual',
    source             TEXT NOT NULL DEFAULT 'allocation',
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
    organization_id BIGINT,
    pool_scope      TEXT NOT NULL DEFAULT 'organization'
);

CREATE TABLE campaign_pool_org_orders (
    campaign_id     INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    dispatch_order  INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (campaign_id, organization_id)
);

CREATE TABLE campaign_customer_lists (
    campaign_id               INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    customer_list_id          INTEGER,
    customer_list_name        TEXT NOT NULL DEFAULT '',
    pool_id                   INTEGER REFERENCES customer_lists(id) ON DELETE SET NULL,
    org_pool_allocation_id           BIGINT REFERENCES org_pool_allocations(id) ON DELETE SET NULL,
    source_organization_id    BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    resolved_reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX idx_campaign_customer_lists_pool_unique ON campaign_customer_lists(campaign_id, pool_id, COALESCE(org_pool_allocation_id, 0)) WHERE pool_id IS NOT NULL;

CREATE TABLE campaign_pool_recipients (
    campaign_id      INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    pool_contact_id  BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    pool_id          INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    allocation_id       BIGINT REFERENCES org_pool_allocations(id) ON DELETE SET NULL,
    organization_id  BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
    status           campaign_recipient_status NOT NULL DEFAULT 'pending',
    email_snapshot   TEXT NOT NULL,
    name_snapshot    TEXT NOT NULL DEFAULT '',
    sender_smtp_uuid UUID,
    sender_user_id   INTEGER,
    sender_from_snapshot TEXT NOT NULL DEFAULT '',
    sender_assigned_at TIMESTAMPTZ,
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

// setOrganizationMailbox configures the organization's single unified reply
// mailbox. A nil mailboxID clears it, which is how the tests model an
// organization that has not configured one.
func (env *poolRecipientsTestEnv) setOrganizationMailbox(organizationID int64, mailboxID *int64) {
	env.t.Helper()
	env.exec(`UPDATE organizations SET reply_mailbox_id=$2 WHERE id=$1`, organizationID, mailboxID)
}

// seedAllocationListName is the name of the pool-allocation customer list seedAllocation
// creates. Tests compute the same name to assert that the self-diagnosing block
// message names the real list.
func seedAllocationListName(poolID int, organizationID int64) string {
	return fmt.Sprintf("pool allocation %d/%d", poolID, organizationID)
}

// seedAllocation creates the organization's pool allocation and its
// org_pool_allocations row, mirroring the production shape (org_pool_allocations.list_id
// -> customer_lists). An allocation carries no reply mailbox of its own.
func (env *poolRecipientsTestEnv) seedAllocation(poolID int, organizationID int64) int64 {
	env.t.Helper()
	listID := env.id(`INSERT INTO customer_lists(name,type,status) VALUES($1,'private','active') RETURNING id`, seedAllocationListName(poolID, organizationID))
	return env.id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,created_by_user_id) VALUES($1,$2,$3,1) RETURNING id`, listID, poolID, organizationID)
}

func (env *poolRecipientsTestEnv) seedContact(code, name, email, status string) int64 {
	env.t.Helper()
	return env.id(`INSERT INTO pool_contacts(customer_code,email,name,status) VALUES($1,$2,$3,$4) RETURNING id`,
		code, email, name, status)
}

func (env *poolRecipientsTestEnv) joinPool(poolID int, contactID int64) {
	env.t.Helper()
	env.exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, poolID, contactID)
}

func (env *poolRecipientsTestEnv) allocate(allocationID, contactID int64, status string) {
	env.t.Helper()
	env.exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status) VALUES($1,$2,$3)`, allocationID, contactID, status)
}

func (env *poolRecipientsTestEnv) exclude(poolID int, organizationID int64, allocationID, contactID int64, restored bool) {
	env.t.Helper()
	env.exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,restored_at)
		VALUES($1,$2,$3,$4,'manual',CASE WHEN $5 THEN NOW() ELSE NULL END)`, poolID, organizationID, contactID, allocationID, restored)
}

func (env *poolRecipientsTestEnv) seedCampaign() int {
	env.t.Helper()
	return int(env.id(`INSERT INTO campaigns(organization_id) VALUES(NULL) RETURNING id`))
}

// seedAudience writes the campaign relation EnsurePoolCampaignRecipients reads.
// A nil allocationID is a first-level selection; it resolves to the single
// pool allocation the organization owns.
func (env *poolRecipientsTestEnv) seedAudience(campaignID, poolID int, organizationID int64, allocationID, mailboxID *int64) {
	env.t.Helper()
	env.exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
		VALUES($1,NULL,(SELECT name FROM customer_lists WHERE id=$2),$2,$3,$4,$5)`, campaignID, poolID, allocationID, organizationID, mailboxID)
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
	AllocationID    *int64 `db:"allocation_id"`
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
	if err := env.db.Select(&rows, `SELECT cpr.pool_contact_id,cpr.pool_id,cpr.allocation_id,cpr.organization_id,cpr.reply_mailbox_id,
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
// created before the organization manager configured the organization's unified
// reply mailbox. Preview/send validation must use the current organization
// setting and repair the campaign relation instead of keeping the old NULL
// forever.
func TestValidatePoolCampaignAudienceRefreshesRoutes(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	org := env.seedOrganization("org-a")
	poolID := env.seedPool("ws-pool")
	mailbox := env.seedMailbox(org, "pool-a@example.invalid")
	env.seedAllocation(poolID, org)
	campaign := env.seedCampaign()
	env.seedAudience(campaign, poolID, org, nil, nil)

	if err := env.core.ValidatePoolCampaignAudience(campaign); err == nil {
		t.Fatal("ValidatePoolCampaignAudience accepted an organization without a unified reply mailbox")
	} else {
		msg := err.Error()
		prefix := "code=400, message=public-pool audience requires an organization allocation and reply mailbox before previewing or sending: "
		if !strings.HasPrefix(msg, prefix) {
			t.Fatalf("ValidatePoolCampaignAudience error = %q, want the legacy prefix %q", msg, prefix)
		}
		// The message must name the pool list, the organization allocation under
		// it and the organization, state the concrete missing piece, and end with
		// the actionable steps.
		for _, want := range []string{
			fmt.Sprintf("pool list %q", "ws-pool"),
			fmt.Sprintf("organization allocation %q", seedAllocationListName(poolID, org)),
			`(organization "org-a")`,
			"the organization has not configured its unified reply mailbox",
			"Manage organizations -> Organization reply mailboxes",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("ValidatePoolCampaignAudience error = %q, want it to contain %q", msg, want)
			}
		}
		if strings.Contains(msg, "Customer lists -> Public pool management") {
			t.Errorf("ValidatePoolCampaignAudience error = %q, want no allocation step for a mailbox-only failure", msg)
		}
		if strings.Contains(msg, "\n") {
			t.Errorf("ValidatePoolCampaignAudience error = %q, want a single line", msg)
		}
	}

	env.setOrganizationMailbox(org, &mailbox)
	if err := env.core.ValidatePoolCampaignAudience(campaign); err != nil {
		t.Fatalf("ValidatePoolCampaignAudience after mailbox configuration: %v", err)
	}

	var resolved sql.NullInt64
	if err := env.db.Get(&resolved, `SELECT resolved_reply_mailbox_id FROM campaign_customer_lists WHERE campaign_id=$1`, campaign); err != nil {
		t.Fatalf("read resolved mailbox: %v", err)
	}
	if !resolved.Valid || resolved.Int64 != mailbox {
		t.Fatalf("resolved mailbox = %v, want %d", resolved, mailbox)
	}

	env.exec(`UPDATE reply_mailboxes SET status='disabled' WHERE id=$1`, mailbox)
	if err := env.core.ValidatePoolCampaignAudience(campaign); err == nil {
		t.Fatal("ValidatePoolCampaignAudience accepted a disabled mailbox")
	} else {
		want := fmt.Sprintf(`pool list "ws-pool" -> organization allocation %q (organization "org-a"): the organization's unified reply mailbox "pool-a@example.invalid" is not verified and active`, seedAllocationListName(poolID, org))
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidatePoolCampaignAudience error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// TestValidatePoolCampaignAudienceReportsRouteReasons pins the self-diagnosing
// block message: an unresolved pool audience must name the pool, the
// organization and the concrete missing piece instead of the former single
// opaque sentence. Every case gets its own campaign because a campaign may hold
// several unresolved audiences at once.
func TestValidatePoolCampaignAudienceReportsRouteReasons(t *testing.T) {
	cases := []struct {
		name string
		// reason is the expected PoolAudienceRouteIssue.Reason; it is empty for
		// the positive control, which must validate.
		reason string
		// setup seeds one campaign and returns it together with the clause the
		// block message must contain. An empty clause means the campaign must
		// validate.
		setup func(env *poolRecipientsTestEnv) (campaign int, wantClause string)
	}{
		{
			name:   "allocation_missing",
			reason: "allocation_missing",
			setup: func(env *poolRecipientsTestEnv) (int, string) {
				org := env.seedOrganization("org-no-allocation")
				poolID := env.seedPool("pool-no-allocation")
				campaign := env.seedCampaign()
				env.seedAudience(campaign, poolID, org, nil, nil)
				return campaign, `pool list "pool-no-allocation": organization "org-no-allocation" has no organization allocation bound to the pool`
			},
		},
		{
			name:   "mailbox_missing",
			reason: "mailbox_missing",
			setup: func(env *poolRecipientsTestEnv) (int, string) {
				org := env.seedOrganization("org-no-mailbox")
				poolID := env.seedPool("pool-no-mailbox")
				env.seedAllocation(poolID, org)
				campaign := env.seedCampaign()
				env.seedAudience(campaign, poolID, org, nil, nil)
				return campaign, fmt.Sprintf(`pool list "pool-no-mailbox" -> organization allocation %q (organization "org-no-mailbox"): the organization has not configured its unified reply mailbox`, seedAllocationListName(poolID, org))
			},
		},
		{
			name:   "mailbox_unavailable",
			reason: "mailbox_unavailable",
			setup: func(env *poolRecipientsTestEnv) (int, string) {
				org := env.seedOrganization("org-disabled-mailbox")
				poolID := env.seedPool("pool-disabled-mailbox")
				mailbox := env.seedMailbox(org, "disabled@example.invalid")
				env.setOrganizationMailbox(org, &mailbox)
				env.exec(`UPDATE reply_mailboxes SET status='disabled' WHERE id=$1`, mailbox)
				env.seedAllocation(poolID, org)
				campaign := env.seedCampaign()
				env.seedAudience(campaign, poolID, org, nil, nil)
				return campaign, fmt.Sprintf(`pool list "pool-disabled-mailbox" -> organization allocation %q (organization "org-disabled-mailbox"): the organization's unified reply mailbox "disabled@example.invalid" is not verified and active`, seedAllocationListName(poolID, org))
			},
		},
		{
			name:   "organization_missing",
			reason: "organization_missing",
			setup: func(env *poolRecipientsTestEnv) (int, string) {
				poolID := env.seedPool("pool-no-organization")
				campaign := env.seedCampaign()
				env.exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
					VALUES($1,NULL,(SELECT name FROM customer_lists WHERE id=$2),$2,NULL,NULL,NULL)`, campaign, poolID)
				return campaign, `pool list "pool-no-organization" has no target organization, so no reply mailbox can be resolved`
			},
		},
		{
			name: "resolved_audience",
			setup: func(env *poolRecipientsTestEnv) (int, string) {
				org := env.seedOrganization("org-ready")
				poolID := env.seedPool("pool-ready")
				mailbox := env.seedMailbox(org, "ready@example.invalid")
				env.setOrganizationMailbox(org, &mailbox)
				env.seedAllocation(poolID, org)
				campaign := env.seedCampaign()
				env.seedAudience(campaign, poolID, org, nil, nil)
				return campaign, ""
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newPoolRecipientsTestEnv(t)
			campaign, wantClause := tc.setup(env)

			err := env.core.ValidatePoolCampaignAudience(campaign)
			if wantClause == "" {
				if err != nil {
					t.Fatalf("ValidatePoolCampaignAudience on a resolved audience: %v", err)
				}
				if issues, loadErr := env.core.poolAudienceRouteIssues(campaign); loadErr != nil {
					t.Fatalf("poolAudienceRouteIssues: %v", loadErr)
				} else if len(issues) != 0 {
					t.Fatalf("poolAudienceRouteIssues = %+v, want no issue for a resolved audience", issues)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidatePoolCampaignAudience accepted an unresolved pool audience")
			}
			msg := err.Error()
			prefix := "code=400, message=public-pool audience requires an organization allocation and reply mailbox before previewing or sending: "
			if !strings.HasPrefix(msg, prefix) {
				t.Fatalf("ValidatePoolCampaignAudience error = %q, want the legacy prefix %q", msg, prefix)
			}
			if !strings.Contains(msg, wantClause) {
				t.Errorf("ValidatePoolCampaignAudience error = %q, want the clause %q", msg, wantClause)
			}
			if !strings.Contains(msg, "Manage organizations -> Organization reply mailboxes") {
				t.Errorf("ValidatePoolCampaignAudience error = %q, want the unified reply mailbox step", msg)
			}
			// The pool-management step is only useful (and only appended) when
			// a missing organization allocation is one of the reasons.
			wantAllocationStep := tc.reason == "allocation_missing"
			if got := strings.Contains(msg, "Customer lists -> Public pool management"); got != wantAllocationStep {
				t.Errorf("ValidatePoolCampaignAudience error = %q, allocation step present = %v, want %v", msg, got, wantAllocationStep)
			}
			if !strings.HasSuffix(msg, poolAudienceRouteMessageRetry) {
				t.Errorf("ValidatePoolCampaignAudience error = %q, want the actionable retry step as suffix", msg)
			}
			if strings.Contains(msg, "\n") {
				t.Errorf("ValidatePoolCampaignAudience error = %q, want a single line", msg)
			}
			// A mailbox failure must name the mailbox problem instead of
			// claiming that the organization has no allocation at all.
			if tc.reason == "mailbox_missing" || tc.reason == "mailbox_unavailable" {
				if strings.Contains(msg, "has no organization allocation bound") {
					t.Errorf("ValidatePoolCampaignAudience error = %q must not claim a missing organization allocation", msg)
				}
			}

			issues, loadErr := env.core.poolAudienceRouteIssues(campaign)
			if loadErr != nil {
				t.Fatalf("poolAudienceRouteIssues: %v", loadErr)
			}
			if len(issues) != 1 {
				t.Fatalf("poolAudienceRouteIssues = %+v, want exactly one issue", issues)
			}
			if issues[0].Reason != tc.reason {
				t.Errorf("issue reason = %q, want %q", issues[0].Reason, tc.reason)
			}
			if issues[0].PoolName == "" {
				t.Errorf("issue pool name is empty, want the audience pool name")
			}
		})
	}
}

// TestPoolAudienceRouteMessageCapsClauses pins the single-line, capped block
// message. It needs no database because it renders fabricated issues.
func TestPoolAudienceRouteMessageCapsClauses(t *testing.T) {
	issues := make([]PoolAudienceRouteIssue, 0, poolAudienceRouteMessageLimit+2)
	for i := 0; i < poolAudienceRouteMessageLimit+2; i++ {
		issues = append(issues, PoolAudienceRouteIssue{PoolID: i + 1, PoolName: fmt.Sprintf("pool-%d", i+1), Reason: poolAudienceRouteReasonOrganizationMissing})
	}
	msg := poolAudienceRouteMessage(issues)
	if !strings.HasPrefix(msg, poolAudienceRouteMessagePrefix+": ") {
		t.Errorf("message = %q, want the legacy prefix", msg)
	}
	if got := strings.Count(msg, "has no target organization"); got != poolAudienceRouteMessageLimit {
		t.Errorf("message renders %d clauses, want %d", got, poolAudienceRouteMessageLimit)
	}
	if !strings.Contains(msg, " (+2 more)") {
		t.Errorf("message = %q, want the truncated-count suffix", msg)
	}
	if !strings.HasSuffix(msg, poolAudienceRouteMessageRetry) {
		t.Errorf("message = %q, want the actionable retry step as suffix", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("message = %q, want a single line", msg)
	}
}

// TestPoolRecipientPathsAgreeOnDeliverableMembers is the drift test: the same
// fixture is expanded by both resolve paths, by AttachPoolToCampaign for a
// pool-allocation and for a first-level audience, and by
// EnsurePoolCampaignRecipients for a allocation-scoped and a pool-wide relation.
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
	allocationA := env.seedAllocation(poolID, orgA)
	allocationB := env.seedAllocation(poolID, orgB)
	env.setOrganizationMailbox(orgA, &mailboxA)
	env.setOrganizationMailbox(orgB, &mailboxB)

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
	env.allocate(allocationA, deliverable, "active")
	env.allocate(allocationA, excluded, "active")
	env.allocate(allocationA, removedAllocation, "removed")
	env.allocate(allocationA, archivedContact, "active")
	env.allocate(allocationA, restored, "active")
	env.allocate(allocationA, unallocated, "active")
	env.allocate(allocationB, orgBOnly, "active")
	env.allocate(allocationA, shared, "active")
	env.allocate(allocationB, shared, "active")
	env.exclude(poolID, orgA, allocationA, excluded, false)
	env.exclude(poolID, orgA, allocationA, restored, true)

	wantA := []int64{deliverable, restored, shared}
	wantB := []int64{orgBOnly, shared}

	// Path 1: first-level resolve for the organization.
	firstLevel := mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, orgA)
	})
	assertSameIDs(t, "ResolvePoolRecipients", firstLevel, wantA)

	// Path 2: pool-allocation resolve. It must not merely contain the same
	// contacts but return them in the same order for the same audience.
	allocationResolve := mustResolve(t, "resolvePoolRecipientsForAllocation", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForAllocation(poolID, orgA, allocationA)
	})
	assertSameIDs(t, "resolvePoolRecipientsForAllocation", allocationResolve, wantA)
	if !slices.Equal(firstLevel, allocationResolve) {
		t.Errorf("resolve paths disagree on row order/content: first-level=%v allocation=%v", firstLevel, allocationResolve)
	}

	// Path 3: attach an explicitly selected pool allocation.
	allocationCampaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(allocationCampaign, poolID, &allocationA, orgA, false); err != nil {
		t.Fatalf("AttachPoolToCampaign(allocation): %v", err)
	}
	assertSameIDs(t, "AttachPoolToCampaign(allocation)", env.snapshotIDs(allocationCampaign), wantA)
	assertSameIDs(t, "AttachPoolToCampaign(allocation) owned rows", env.ownedSnapshotIDs(allocationCampaign), wantA)

	// Path 4: attach the first-level pool, which resolves to the organization's
	// single pool allocation.
	firstLevelCampaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(firstLevelCampaign, poolID, nil, orgA, false); err != nil {
		t.Fatalf("AttachPoolToCampaign(first-level): %v", err)
	}
	assertSameIDs(t, "AttachPoolToCampaign(first-level)", env.snapshotIDs(firstLevelCampaign), wantA)

	// Path 5: refresh a allocation-scoped campaign_customer_lists relation.
	ensureAllocationCampaign := env.seedCampaign()
	env.seedAudience(ensureAllocationCampaign, poolID, orgA, &allocationA, &mailboxA)
	if err := env.core.EnsurePoolCampaignRecipients(ensureAllocationCampaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients(allocation): %v", err)
	}
	assertSameIDs(t, "EnsurePoolCampaignRecipients(allocation)", env.snapshotIDs(ensureAllocationCampaign), wantA)

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
	env.seedAudience(otherOrgCampaign, poolID, orgB, &allocationB, &mailboxB)
	if err := env.core.EnsurePoolCampaignRecipients(otherOrgCampaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients(org B): %v", err)
	}
	assertSameIDs(t, "EnsurePoolCampaignRecipients(org B)", env.snapshotIDs(otherOrgCampaign), wantB)

	// De-duplication by pool contact id: a campaign that selects both the
	// first-level pool and its pool allocation still holds exactly one row per
	// contact.
	dedupCampaign := env.seedCampaign()
	env.seedAudience(dedupCampaign, poolID, orgA, &allocationA, &mailboxA)
	env.seedAudience(dedupCampaign, poolID, orgA, nil, &mailboxA)
	if err := env.core.EnsurePoolCampaignRecipients(dedupCampaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients(allocation + first-level): %v", err)
	}
	assertSameIDs(t, "EnsurePoolCampaignRecipients(allocation + first-level)", env.snapshotIDs(dedupCampaign), wantA)
	if n := env.countRows(`SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$1`, dedupCampaign); n != len(wantA) {
		t.Errorf("snapshot rows for a doubly-selected audience = %d, want %d", n, len(wantA))
	}

	// Provenance: every snapshot row names the resolved pool allocation,
	// organization and per-list reply mailbox.
	rows := env.snapshotRows(firstLevelCampaign)
	for _, contactID := range wantA {
		row, ok := rows[contactID]
		if !ok {
			t.Fatalf("contact %d is missing from the snapshot", contactID)
		}
		if row.AllocationID == nil || *row.AllocationID != allocationA {
			t.Errorf("contact %d allocation_id = %v, want %d", contactID, row.AllocationID, allocationA)
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
	allocation := env.seedAllocation(poolID, org)
	env.setOrganizationMailbox(org, &mailbox)

	keep := env.seedContact("C-1", "Keep", "keep@example.invalid", "active")
	excludedLater := env.seedContact("C-2", "Excluded later", "later@example.invalid", "active")
	for _, id := range []int64{keep, excludedLater} {
		env.joinPool(poolID, id)
		env.allocate(allocation, id, "active")
	}

	campaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &allocation, org, false); err != nil {
		t.Fatalf("AttachPoolToCampaign: %v", err)
	}
	assertSameIDs(t, "snapshot after attach", env.snapshotIDs(campaign), []int64{keep, excludedLater})

	// The organization removes the contact from its allocation. This is the
	// shipped removal path: it deactivates the allocation and writes the
	// organization-scoped exclusion.
	if err := env.core.RemovePoolContact(allocation, excludedLater, 1, "manual"); err != nil {
		t.Fatalf("RemovePoolContact: %v", err)
	}
	assertSameIDs(t, "ResolvePoolRecipients after exclusion", mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, org)
	}), []int64{keep})
	assertSameIDs(t, "resolvePoolRecipientsForAllocation after exclusion", mustResolve(t, "resolvePoolRecipientsForAllocation", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForAllocation(poolID, org, allocation)
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
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &allocation, org, false); err != nil {
		t.Fatalf("AttachPoolToCampaign after exclusion: %v", err)
	}
	assertSameIDs(t, "snapshot after re-attach", env.snapshotIDs(campaign), []int64{keep})
	assertSameIDs(t, "owned snapshot rows after re-attach", env.ownedSnapshotIDs(campaign), []int64{keep})

	// Restoring the allocation must make the contact deliverable again, and the
	// refresh must re-add the row it pruned.
	if err := env.core.RestorePoolContact(allocation, excludedLater); err != nil {
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
	allocation := env.seedAllocation(poolID, org)
	env.setOrganizationMailbox(org, &mailbox)

	keep := env.seedContact("C-1", "Keep", "keep@example.invalid", "active")
	removedAllocation := env.seedContact("C-2", "Reallocated", "reallocated@example.invalid", "active")
	archived := env.seedContact("C-3", "Archived", "archived@example.invalid", "active")
	for _, id := range []int64{keep, removedAllocation, archived} {
		env.joinPool(poolID, id)
		env.allocate(allocation, id, "active")
	}

	campaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &allocation, org, false); err != nil {
		t.Fatalf("AttachPoolToCampaign: %v", err)
	}
	assertSameIDs(t, "snapshot after attach", env.snapshotIDs(campaign), []int64{keep, removedAllocation, archived})

	env.exec(`UPDATE org_pool_allocation_members SET status='removed',removed_reason='reallocated',removed_at=NOW() WHERE allocation_id=$1 AND contact_id=$2`, allocation, removedAllocation)
	env.exec(`UPDATE pool_contacts SET status='archived',updated_at=NOW() WHERE id=$1`, archived)

	if n := env.countRows(`SELECT COUNT(*) FROM org_pool_allocation_exclusions`); n != 0 {
		t.Fatalf("fixture wrote %d exclusion rows; this case must not rely on one", n)
	}
	assertSameIDs(t, "ResolvePoolRecipients", mustResolve(t, "ResolvePoolRecipients", func() ([]PoolRecipient, error) {
		return env.core.ResolvePoolRecipients(poolID, org)
	}), []int64{keep})
	assertSameIDs(t, "resolvePoolRecipientsForAllocation", mustResolve(t, "resolvePoolRecipientsForAllocation", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForAllocation(poolID, org, allocation)
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
	allocation := env.seedAllocation(poolID, org)
	env.setOrganizationMailbox(org, &mailbox)

	pending := env.seedContact("C-1", "Pending", "pending@example.invalid", "active")
	sent := env.seedContact("C-2", "Sent", "sent@example.invalid", "active")
	queued := env.seedContact("C-3", "Queued", "queued@example.invalid", "active")
	deferred := env.seedContact("C-4", "Deferred", "deferred@example.invalid", "active")
	cancelled := env.seedContact("C-5", "Cancelled", "cancelled@example.invalid", "active")
	stillDeliverable := env.seedContact("C-6", "Still deliverable", "still@example.invalid", "active")
	for _, id := range []int64{pending, sent, queued, deferred, cancelled, stillDeliverable} {
		env.joinPool(poolID, id)
		env.allocate(allocation, id, "active")
	}

	campaign := env.seedCampaign()
	if err := env.core.AttachPoolToCampaign(campaign, poolID, &allocation, org, false); err != nil {
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
		env.exclude(poolID, org, allocation, id, false)
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
	assertSameIDs(t, "resolved audience after exclusion", mustResolve(t, "resolvePoolRecipientsForAllocation", func() ([]PoolRecipient, error) {
		return env.core.resolvePoolRecipientsForAllocation(poolID, org, allocation)
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

// TestQueryPlatformPublicPoolListsExposesActiveFirstLevelPools pins the
// audience-selector grant for platform-level public-pool senders: every active
// first-level pool is offered as a deliverable audience regardless of the
// caller's organization, while archived pools are excluded and no contact
// detail is attached.
func TestQueryPlatformPublicPoolListsExposesActiveFirstLevelPools(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	active := env.seedPool("platform-pool")
	archived := env.seedPool("archived-pool")
	env.exec(`UPDATE customer_lists SET status='archived' WHERE id=$1`, archived)
	allocationList := env.id(`INSERT INTO customer_lists(name,type,status) VALUES('allocation-list','org_pool_allocation','active') RETURNING id`)

	rows, err := env.core.QueryPlatformPublicPoolLists()
	if err != nil {
		t.Fatalf("QueryPlatformPublicPoolLists: %v", err)
	}
	byID := make(map[int]models.CustomerList, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	if _, ok := byID[archived]; ok {
		t.Errorf("archived pool %d was offered as a platform audience", archived)
	}
	if _, ok := byID[int(allocationList)]; ok {
		t.Errorf("pool allocation list %d was offered as a first-level platform audience", allocationList)
	}
	got, ok := byID[active]
	if !ok {
		t.Fatalf("active first-level pool %d is missing from %v", active, rows)
	}
	if !got.PoolDeliveryAllowed {
		t.Error("platform pool list is not marked deliverable")
	}
	if got.Type != models.CustomerListTypePool {
		t.Errorf("list type = %q, want %q", got.Type, models.CustomerListTypePool)
	}
}

// seedAllOrgAudience creates a platform-level ('all_organizations') campaign
// whose single pool audience covers every active organization's allocation.
func (env *poolRecipientsTestEnv) seedAllOrgAudience(campaignID, poolID int) {
	env.t.Helper()
	env.exec(`UPDATE campaigns SET pool_scope=$2 WHERE id=$1`, campaignID, models.CampaignPoolScopeAllOrganizations)
	env.exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id)
		VALUES($1,NULL,(SELECT name FROM customer_lists WHERE id=$2),$2)`, campaignID, poolID)
}

func (env *poolRecipientsTestEnv) setOrgOrder(campaignID int, entries map[int64]int) {
	env.t.Helper()
	env.exec(`DELETE FROM campaign_pool_org_orders WHERE campaign_id=$1`, campaignID)
	for orgID, order := range entries {
		env.exec(`INSERT INTO campaign_pool_org_orders(campaign_id,organization_id,dispatch_order) VALUES($1,$2,$3)`, campaignID, orgID, order)
	}
}

// TestAllOrgPoolSnapshotDeduplicatesByRotationOrder pins the platform-level
// audience rule: every deliverable contact appears exactly once and is owned
// by the organization that comes first in the campaign's persisted rotation,
// the assignment follows a changed rotation while the row is still pending,
// delivered rows keep their original organization, and an organization without
// a usable unified reply mailbox contributes no recipients.
func TestAllOrgPoolSnapshotDeduplicatesByRotationOrder(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	poolID := env.seedPool("all-org-pool")
	orgA := env.seedOrganization("org-a")
	orgB := env.seedOrganization("org-b")
	mailboxA := env.seedMailbox(orgA, "a@example.invalid")
	mailboxB := env.seedMailbox(orgB, "b@example.invalid")
	env.setOrganizationMailbox(orgA, &mailboxA)
	env.setOrganizationMailbox(orgB, &mailboxB)
	allocationA := env.seedAllocation(poolID, orgA)
	allocationB := env.seedAllocation(poolID, orgB)

	shared := env.seedContact("C1", "Shared", "shared@example.invalid", "active")
	onlyB := env.seedContact("C2", "OnlyB", "b-only@example.invalid", "active")
	for _, id := range []int64{shared, onlyB} {
		env.joinPool(poolID, id)
	}
	env.allocate(allocationA, shared, "active")
	env.allocate(allocationB, shared, "active")
	env.allocate(allocationB, onlyB, "active")

	campaign := env.seedCampaign()
	env.seedAllOrgAudience(campaign, poolID)
	env.setOrgOrder(campaign, map[int64]int{orgA: 1, orgB: 2})

	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients: %v", err)
	}
	assertSameIDs(t, "all-org snapshot", env.snapshotIDs(campaign), []int64{shared, onlyB})
	rows := env.snapshotRows(campaign)
	if got := rows[shared]; got.OrganizationID == nil || *got.OrganizationID != orgA {
		t.Errorf("shared contact organization = %v, want %d (first in rotation)", got.OrganizationID, orgA)
	}
	if got := rows[shared]; got.AllocationID == nil || *got.AllocationID != allocationA {
		t.Errorf("shared contact allocation = %v, want %d", got.AllocationID, allocationA)
	}
	if got := rows[shared]; got.ReplyMailboxID == nil || *got.ReplyMailboxID != mailboxA {
		t.Errorf("shared contact reply mailbox = %v, want %d", got.ReplyMailboxID, mailboxA)
	}
	if got := rows[onlyB]; got.OrganizationID == nil || *got.OrganizationID != orgB {
		t.Errorf("single-organization contact = %v, want %d", got.OrganizationID, orgB)
	}

	// Reversing the rotation moves a still-pending row to the new first
	// organization, and it never duplicates the contact.
	env.setOrgOrder(campaign, map[int64]int{orgA: 2, orgB: 1})
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients after rotation change: %v", err)
	}
	assertSameIDs(t, "all-org snapshot after rotation change", env.snapshotIDs(campaign), []int64{shared, onlyB})
	rows = env.snapshotRows(campaign)
	if got := rows[shared]; got.OrganizationID == nil || *got.OrganizationID != orgB {
		t.Errorf("pending row did not follow the new rotation: organization = %v, want %d", got.OrganizationID, orgB)
	}

	// A delivery-history row is never re-owned by a rotation change.
	env.exec(`UPDATE campaign_pool_recipients SET status='sent' WHERE campaign_id=$1 AND pool_contact_id=$2`, campaign, shared)
	env.setOrgOrder(campaign, map[int64]int{orgA: 1, orgB: 2})
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients after delivery: %v", err)
	}
	rows = env.snapshotRows(campaign)
	if got := rows[shared]; got.OrganizationID == nil || *got.OrganizationID != orgB || got.Status != models.CampaignRecipientStatusSent {
		t.Errorf("delivered row was rewritten: organization=%v status=%q", got.OrganizationID, got.Status)
	}

	// An organization without a usable unified reply mailbox contributes no
	// recipients, and a contact that is only allocated there is dropped.
	env.setOrganizationMailbox(orgB, nil)
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatalf("EnsurePoolCampaignRecipients after mailbox removal: %v", err)
	}
	assertSameIDs(t, "all-org snapshot without org-b mailbox", env.snapshotIDs(campaign), []int64{shared})
}

// TestValidateAllOrgPoolCampaignAudienceRequiresEveryOrganization pins the
// platform-level start guard: one unready organization (missing mailbox or no
// eligible SMTP account) blocks the whole campaign and is named in the error.
func TestValidateAllOrgPoolCampaignAudienceRequiresEveryOrganization(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)

	poolID := env.seedPool("all-org-pool")
	org := env.seedOrganization("org-a")
	mailbox := env.seedMailbox(org, "a@example.invalid")
	env.setOrganizationMailbox(org, &mailbox)
	env.seedAllocation(poolID, org)
	// The sender account must belong to an enabled active member.
	env.exec(`INSERT INTO organization_members(organization_id,user_id) VALUES($1,1)`, org)
	env.exec(`INSERT INTO user_smtp_servers(uuid,user_id,name,enabled,from_email,host,port)
		VALUES(gen_random_uuid(),1,'primary',TRUE,'sender@example.invalid','smtp.example.invalid',465)`)

	campaign := env.seedCampaign()
	env.seedAllOrgAudience(campaign, poolID)

	if err := env.core.ValidatePoolCampaignAudience(campaign); err != nil {
		t.Fatalf("ready all-organization campaign rejected: %v", err)
	}

	// The organization's only SMTP row is disabled: the campaign must be
	// blocked and the message must name the SMTP problem, not a mailbox one.
	env.exec(`UPDATE user_smtp_servers SET enabled=FALSE`)
	err := env.core.ValidatePoolCampaignAudience(campaign)
	if err == nil {
		t.Fatal("ValidatePoolCampaignAudience accepted an organization without any enabled SMTP account")
	}
	if msg := err.Error(); !strings.Contains(msg, "no enabled SMTP account") || !strings.Contains(msg, "org-a") {
		t.Errorf("block message = %q, want the organization and the SMTP reason", msg)
	}
}
