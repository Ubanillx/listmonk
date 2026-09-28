package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// The campaign audience write runs the production update/campaign SQL and the
// production workspace mutation locks on the throwaway schema of
// poolRecipientsTestEnv. These tests are gated by the same DSN environment
// variable as the pool recipient tests (POOL_RECIPIENTS_TEST_DSN) because they
// exercise the same pool membership rule, and they extend that schema instead of
// opening a second database bootstrap.
//
// campaignAudienceTestDDL declares the campaign columns, enum and companion
// tables the executed statements read or write: queries/campaigns.sql
// update-campaign (campaigns, campaign_customer_lists, campaign_media, media),
// the recipient-immutability check (campaign_recipients) and the resource lock
// (customer_lists.transfer_pending_at). The types and keys mirror schema.sql;
// nothing else is declared, so a statement that grows a new dependency fails
// loudly here instead of drifting.
const campaignAudienceTestDDL = poolRecipientsTestDDL + `
CREATE TYPE content_type AS ENUM ('richtext', 'html', 'plain', 'markdown', 'visual');

ALTER TABLE campaigns
    ADD COLUMN name              TEXT NOT NULL DEFAULT '',
    ADD COLUMN subject           TEXT NOT NULL DEFAULT '',
    ADD COLUMN from_email        TEXT NOT NULL DEFAULT '',
    ADD COLUMN body              TEXT NOT NULL DEFAULT '',
    ADD COLUMN body_source       TEXT NULL,
    ADD COLUMN altbody           TEXT NULL,
    ADD COLUMN content_type      content_type NOT NULL DEFAULT 'richtext',
    ADD COLUMN send_at           TIMESTAMPTZ,
    ADD COLUMN headers           JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN attribs           JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN status            TEXT NOT NULL DEFAULT 'draft',
    ADD COLUMN daily_send_limit  INT NOT NULL DEFAULT 300,
    ADD COLUMN daily_resume_time TEXT NOT NULL DEFAULT '09:00',
    ADD COLUMN tags              VARCHAR(100)[],
    ADD COLUMN messenger         TEXT NOT NULL DEFAULT 'email',
    ADD COLUMN template_id       INTEGER,
    ADD COLUMN archive           BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN archive_slug      TEXT,
    ADD COLUMN archive_template_id INTEGER,
    ADD COLUMN archive_meta      JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN auto_track_links  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN owner_user_id     INTEGER,
    ADD COLUMN original_owner_user_id INTEGER,
    ADD COLUMN visibility        TEXT NOT NULL DEFAULT 'private',
    ADD COLUMN transfer_pending_at TIMESTAMPTZ,
    ADD COLUMN reply_mailbox_id  INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
    ADD COLUMN updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Campaign updates lock the requested customer_lists exactly like the
-- production table, so the workspace lock predicate needs this column.
ALTER TABLE customer_lists
    ADD COLUMN transfer_pending_at TIMESTAMPTZ;

-- update-campaign upserts both relationship tables, which needs the same unique
-- keys schema.sql declares.
CREATE UNIQUE INDEX campaign_audience_test_customer_lists_unique ON campaign_customer_lists(campaign_id, customer_list_id);

CREATE TABLE campaign_recipients (
    campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    customer_id INTEGER NOT NULL,
    status      campaign_recipient_status NOT NULL DEFAULT 'pending',
    PRIMARY KEY (campaign_id, customer_id)
);

CREATE TABLE media (
    id                  SERIAL PRIMARY KEY,
    filename            TEXT NOT NULL DEFAULT '',
    organization_id     BIGINT,
    owner_user_id       INTEGER,
    visibility          TEXT NOT NULL DEFAULT 'private',
    transfer_pending_at TIMESTAMPTZ
);

CREATE TABLE campaign_media (
    campaign_id INTEGER REFERENCES campaigns(id) ON DELETE CASCADE,
    media_id    INTEGER REFERENCES media(id) ON DELETE SET NULL,
    filename    TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX campaign_audience_test_campaign_media_unique ON campaign_media(campaign_id, media_id);
`

// newCampaignAudienceTestEnv builds the harness Core for the production campaign
// write path: the throwaway schema plus the production update-campaign statement
// and the translator the shared update body consults.
func newCampaignAudienceTestEnv(t *testing.T) *poolRecipientsTestEnv {
	t.Helper()

	env := newPoolRecipientsTestEnvWithDDL(t, campaignAudienceTestDDL)
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating queries/campaigns.sql")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "..", "..", "queries", "campaigns.sql"))
	if err != nil {
		t.Fatalf("read queries/campaigns.sql: %v", err)
	}
	parsed, err := goyesql.ParseBytes(body)
	if err != nil {
		t.Fatalf("parse queries/campaigns.sql: %v", err)
	}
	query, ok := parsed["update-campaign"]
	if !ok {
		t.Fatal("queries/campaigns.sql no longer defines update-campaign")
	}
	statement, err := env.db.Preparex(query.Query)
	if err != nil {
		t.Fatalf("prepare update-campaign: %v", err)
	}
	t.Cleanup(func() { _ = statement.Close() })
	translator, err := i18n.New([]byte(`{"_.code":"en","_.name":"English"}`))
	if err != nil {
		t.Fatalf("build translator: %v", err)
	}
	env.core.q = &models.Queries{UpdateCampaign: statement}
	env.core.i18n = translator
	env.core.log = log.New(io.Discard, "", 0)
	return env
}

// platformAccess is the workspace the tests write with: a platform
// administrator acting inside one organization, which is the access shape that
// reaches the campaign row lock without a membership check while still giving
// the pool audiences a target organization.
func platformAccess(organizationID int) models.WorkspaceAccess {
	return models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: organizationID, PlatformAdmin: true},
		UserID:    1,
	}
}

// seedWritableCampaign inserts the campaign row shape the production update
// statement writes: an organization-owned draft with a reply mailbox and no
// pending transfer.
func (env *poolRecipientsTestEnv) seedWritableCampaign(organizationID int64, name, subject string, ownerUserID int, replyMailboxID int64) int {
	env.t.Helper()
	return int(env.id(`INSERT INTO campaigns(organization_id,owner_user_id,reply_mailbox_id,name,subject,from_email,body,status)
		VALUES($1,$2,$3,$4,$5,'sender@example.invalid','body','draft') RETURNING id`,
		organizationID, ownerUserID, replyMailboxID, name, subject))
}

// seedRegularCampaignList writes one legacy (non-pool) campaign/customer_list
// relation, the rows a campaign update must rewrite while the pool rebuild must
// leave alone.
func (env *poolRecipientsTestEnv) seedRegularCampaignList(campaignID, customerListID int) {
	env.t.Helper()
	env.exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name)
		VALUES($1,$2,'regular audience')`, campaignID, customerListID)
}

// seedCampaignSnapshot writes one existing pool recipient snapshot row, the
// delivery state a draft rebuild replaces and a failed write must restore.
func (env *poolRecipientsTestEnv) seedCampaignSnapshot(campaignID int, poolID int, organizationID int64, allocationID, mailboxID int64, contactID int64) {
	env.t.Helper()
	env.exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,reply_mailbox_id,email_snapshot,name_snapshot)
		VALUES($1,$2,$3,$4,$5,$6,(SELECT email FROM pool_contacts WHERE id=$2),(SELECT name FROM pool_contacts WHERE id=$2))`,
		campaignID, contactID, poolID, allocationID, organizationID, mailboxID)
}

// campaignWriteState renders every row one campaign write owns — the
// campaign_customer_lists relations, the pool recipient snapshots and the
// organization rotation rows — as text, so a test can compare the complete state
// before and after a failed write.
func (env *poolRecipientsTestEnv) campaignWriteState(campaignID int) []string {
	env.t.Helper()
	var rows []string
	if err := env.db.Select(&rows, `
		SELECT line FROM (
			SELECT format('list|%s|%s|%s|%s', COALESCE(customer_list_id,-1), COALESCE(pool_id,-1),
				COALESCE(org_pool_allocation_id,-1), customer_list_name) AS line
				FROM campaign_customer_lists WHERE campaign_id=$1
			UNION ALL
			SELECT format('snapshot|%s|%s|%s|%s', pool_contact_id, pool_id, COALESCE(allocation_id,-1), status::TEXT)
				FROM campaign_pool_recipients WHERE campaign_id=$1
			UNION ALL
			SELECT format('order|%s|%s', organization_id, dispatch_order)
				FROM campaign_pool_org_orders WHERE campaign_id=$1
		) state ORDER BY line`, campaignID); err != nil {
		env.t.Fatalf("read campaign write state: %v", err)
	}
	return rows
}

// commitTx runs fn on one committed transaction, which is how the tests drive
// the pieces the campaign entry points run inside their own transaction.
func (env *poolRecipientsTestEnv) commitTx(label string, fn func(tx *sqlx.Tx) error) {
	env.t.Helper()
	tx, err := env.db.Beginx()
	if err != nil {
		env.t.Fatalf("%s: begin: %v", label, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		env.t.Fatalf("%s: %v", label, err)
	}
	if err := tx.Commit(); err != nil {
		env.t.Fatalf("%s: commit: %v", label, err)
	}
}

// TestCampaignAudienceUpdateRollsBackOnPoolFailure proves the atomicity contract
// of UpdateCampaignWithAudienceInWorkspace: when an audience fails inside the
// pool step, the campaign row, its customer_list relations and its reply mailbox
// are exactly what they were before the request.
//
// The failing audience is an allocation ID that does not belong to the pool and
// organization (the production "invalid pool allocation" rejection), placed
// after a valid audience. The campaign row update, the reply mailbox update and
// the first pool attachment have therefore all already been written inside the
// transaction when the failure happens.
func TestCampaignAudienceUpdateRollsBackOnPoolFailure(t *testing.T) {
	env := newCampaignAudienceTestEnv(t)

	org := env.seedOrganization("org-a")
	pool := env.seedPool("pool-a")
	mailbox := env.seedMailbox(org, "org-a-reply@example.invalid")
	env.setOrganizationMailbox(org, &mailbox)
	env.exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id) VALUES($1,$2)`, pool, org)
	allocation := env.seedAllocation(pool, org)
	contact := env.seedContact("cust-1", "Customer One", "one@example.invalid", "active")
	env.joinPool(pool, contact)
	env.allocate(allocation, contact, "active")

	// The legacy customer lists the update rewrites. They are scoped to the
	// campaign's organization and owner because update-campaign only relates a
	// customer_list that the campaign workspace already owns.
	newListID := int(env.id(`INSERT INTO customer_lists(name,type,status,organization_id,owner_user_id)
		VALUES('new audience','private','active',$1,1) RETURNING id`, org))

	campaignID := env.seedWritableCampaign(org, "before", "before", 1, mailbox)
	oldListID := int(env.id(`INSERT INTO customer_lists(name,type,status,organization_id,owner_user_id)
		VALUES('regular audience','private','active',$1,1) RETURNING id`, org))
	env.seedRegularCampaignList(campaignID, oldListID)
	env.seedAudience(campaignID, pool, org, &allocation, &mailbox)
	env.seedCampaignSnapshot(campaignID, pool, org, allocation, mailbox, contact)
	env.exec(`INSERT INTO campaign_pool_org_orders(campaign_id,organization_id,dispatch_order) VALUES($1,$2,0)`, campaignID, org)

	type campaignRow struct {
		Name           string    `db:"name"`
		Subject        string    `db:"subject"`
		OwnerUserID    int       `db:"owner_user_id"`
		ReplyMailboxID int64     `db:"reply_mailbox_id"`
		UpdatedAt      time.Time `db:"updated_at"`
		Status         string    `db:"status"`
	}
	readCampaign := func() campaignRow {
		var row campaignRow
		if err := env.db.Get(&row, `SELECT name,subject,owner_user_id,reply_mailbox_id,updated_at,status FROM campaigns WHERE id=$1`, campaignID); err != nil {
			t.Fatalf("read campaign: %v", err)
		}
		return row
	}
	before := readCampaign()
	beforeState := env.campaignWriteState(campaignID)
	if len(beforeState) != 4 {
		t.Fatalf("fixture must own 4 campaign rows (regular list, pool list, snapshot, rotation), got %d: %v", len(beforeState), beforeState)
	}

	// A second, valid mailbox: the update would have moved the campaign to it.
	replacementMailbox := env.seedMailbox(org, "replacement@example.invalid")
	bogusAllocation := int64(987654321)
	update := models.Campaign{
		Name:           "after",
		Subject:        "after",
		FromEmail:      "sender@example.invalid",
		Body:           "after",
		ContentType:    models.CampaignContentTypeHTML,
		Messenger:      "email",
		Headers:        models.Headers{},
		Attribs:        models.JSON{},
		ArchiveMeta:    json.RawMessage(`{}`),
		ReplyMailboxID: null.IntFrom(int(replacementMailbox)),
	}

	_, err := env.core.UpdateCampaignWithAudienceInWorkspace(platformAccess(int(org)), campaignID, update, []int{newListID}, nil, "", CampaignAudiencePlan{
		Audiences: []CampaignAudience{
			{PoolID: pool, AllocationID: &allocation},
			{PoolID: pool, AllocationID: &bogusAllocation},
		},
		RebuildAll: true,
	})
	if err == nil {
		t.Fatal("an audience with an allocation outside the pool must fail the update")
	}
	// The failure must come from the pool attachment step, not from an earlier
	// lock or validation step; otherwise this test would not prove that the
	// already-written campaign and audience changes were rolled back.
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest || httpErr.Message != "invalid pool allocation" {
		t.Fatalf("expected the pool attachment rejection, got %v", err)
	}

	after := readCampaign()
	if after.Name != "before" || after.Subject != "before" || after.Status != before.Status {
		t.Errorf("campaign row changed: name=%q subject=%q status=%q, want %q/%q/%q",
			after.Name, after.Subject, after.Status, before.Name, before.Subject, before.Status)
	}
	if after.ReplyMailboxID != mailbox || after.OwnerUserID != before.OwnerUserID {
		t.Errorf("reply mailbox/owner changed: mailbox=%d owner=%d, want %d/%d",
			after.ReplyMailboxID, after.OwnerUserID, mailbox, before.OwnerUserID)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("campaign updated_at changed to %s, want %s", after.UpdatedAt, before.UpdatedAt)
	}

	afterState := env.campaignWriteState(campaignID)
	if !slices.Equal(beforeState, afterState) {
		t.Errorf("campaign audience state changed:\n before %v\n after  %v", beforeState, afterState)
	}
	if got := env.id(`SELECT COUNT(*) FROM campaign_customer_lists WHERE campaign_id=$1 AND customer_list_id=$2`, campaignID, newListID); got != 0 {
		t.Errorf("the rolled-back update left %d rows for the newly selected customer list", got)
	}
	if got := env.id(`SELECT COUNT(*) FROM campaigns WHERE id=$1 AND reply_mailbox_id=$2`, campaignID, replacementMailbox); got != 0 {
		t.Error("the rolled-back update left the replacement reply mailbox attached")
	}

	// Positive control: the same statements do change the campaign when their
	// transaction commits. Without it the assertions above would also pass if the
	// write path silently stopped writing anything at all.
	env.commitTx("control campaign update", func(tx *sqlx.Tx) error {
		return env.core.updateCampaignTx(tx, platformAccess(int(org)), campaignID, update, []int{newListID}, nil, "")
	})
	if got := readCampaign(); got.Name != "after" || got.Subject != "after" {
		t.Fatalf("control update did not write the campaign row: name=%q subject=%q", got.Name, got.Subject)
	}
	if got := env.id(`SELECT COUNT(*) FROM campaign_customer_lists WHERE campaign_id=$1 AND customer_list_id=$2`, campaignID, newListID); got != 1 {
		t.Fatalf("control update did not rewrite the campaign customer_lists (new rows: %d)", got)
	}
	env.commitTx("control reply mailbox update", func(tx *sqlx.Tx) error {
		return env.core.updateCampaignReplyMailboxTx(tx, campaignID, 1, null.IntFrom(int(replacementMailbox)))
	})
	if got := env.id(`SELECT COUNT(*) FROM campaigns WHERE id=$1 AND reply_mailbox_id=$2`, campaignID, replacementMailbox); got != 1 {
		t.Fatal("control reply mailbox update did not move the campaign to the new mailbox")
	}
}

// TestSyncCampaignAudienceTxAppliesTheHandlerBranches pins the three audience
// branches the campaign API decides. syncCampaignAudienceTx must apply the
// caller's plan verbatim:
//
//	RebuildAll=false, pools selected  -> only the pools the request dropped are
//	                                     removed; delivery history for the kept
//	                                     pools and the rotation rows survive.
//	RebuildAll=true,  pools selected  -> every pool association is rebuilt.
//	RebuildAll=true,  no pool selected -> every pool association is removed while
//	                                     legacy customer_list rows survive.
func TestSyncCampaignAudienceTxAppliesTheHandlerBranches(t *testing.T) {
	env := newCampaignAudienceTestEnv(t)

	org := env.seedOrganization("org-a")
	access := platformAccess(int(org))
	mailbox := env.seedMailbox(org, "org-a-reply@example.invalid")
	env.setOrganizationMailbox(org, &mailbox)
	poolA := env.seedPool("pool-a")
	poolB := env.seedPool("pool-b")
	env.exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id) VALUES($1,$2),($3,$2)`, poolA, org, poolB)
	allocationA := env.seedAllocation(poolA, org)
	allocationB := env.seedAllocation(poolB, org)
	contactA := env.seedContact("cust-a", "Customer A", "a@example.invalid", "active")
	contactB := env.seedContact("cust-b", "Customer B", "b@example.invalid", "active")
	env.joinPool(poolA, contactA)
	env.allocate(allocationA, contactA, "active")
	env.joinPool(poolB, contactB)
	env.allocate(allocationB, contactB, "active")

	regularListID := int(env.id(`INSERT INTO customer_lists(name,type,status) VALUES('regular audience','private','active') RETURNING id`))
	campaignID := env.seedWritableCampaign(org, "branches", "branches", 1, mailbox)
	env.seedRegularCampaignList(campaignID, regularListID)
	env.seedAudience(campaignID, poolA, org, &allocationA, &mailbox)
	env.seedAudience(campaignID, poolB, org, &allocationB, &mailbox)
	env.exec(`INSERT INTO campaign_pool_org_orders(campaign_id,organization_id,dispatch_order) VALUES($1,$2,0)`, campaignID, org)

	regularRow := fmt.Sprintf("list|%d|-1|-1|regular audience", regularListID)
	poolARow := fmt.Sprintf("list|-1|%d|%d|pool-a", poolA, allocationA)
	poolBRow := fmt.Sprintf("list|-1|%d|%d|pool-b", poolB, allocationB)
	snapshotA := fmt.Sprintf("snapshot|%d|%d|%d|pending", contactA, poolA, allocationA)
	snapshotB := fmt.Sprintf("snapshot|%d|%d|%d|pending", contactB, poolB, allocationB)
	rotation := fmt.Sprintf("order|%d|0", org)

	// Build the starting snapshot for both selected audiences through the same
	// path the campaign API uses.
	env.commitTx("seed audience plan", func(tx *sqlx.Tx) error {
		return env.core.syncCampaignAudienceTx(tx, campaignID, access, CampaignAudiencePlan{
			Audiences: []CampaignAudience{
				{PoolID: poolA, AllocationID: &allocationA},
				{PoolID: poolB, AllocationID: &allocationB},
			},
		})
	})
	want := []string{poolARow, poolBRow, regularRow, rotation, snapshotA, snapshotB}
	slices.Sort(want)
	if got := env.campaignWriteState(campaignID); !slices.Equal(got, want) {
		t.Fatalf("seeded audience state = %v, want %v", got, want)
	}

	// Branch: a campaign with a recipient snapshot drops one of its pools. The
	// kept pool keeps its snapshot and the rotation rows are untouched.
	env.commitTx("prune audience plan", func(tx *sqlx.Tx) error {
		return env.core.syncCampaignAudienceTx(tx, campaignID, access, CampaignAudiencePlan{
			Audiences: []CampaignAudience{{PoolID: poolA, AllocationID: &allocationA}},
		})
	})
	want = []string{poolARow, regularRow, rotation, snapshotA}
	slices.Sort(want)
	if got := env.campaignWriteState(campaignID); !slices.Equal(got, want) {
		t.Errorf("pruned audience state = %v, want %v", got, want)
	}

	// Branch: a draft rebuild replaces the whole pool audience.
	env.commitTx("rebuild audience plan", func(tx *sqlx.Tx) error {
		return env.core.syncCampaignAudienceTx(tx, campaignID, access, CampaignAudiencePlan{
			Audiences:  []CampaignAudience{{PoolID: poolB, AllocationID: &allocationB}},
			RebuildAll: true,
		})
	})
	want = []string{poolBRow, regularRow, snapshotB}
	slices.Sort(want)
	if got := env.campaignWriteState(campaignID); !slices.Equal(got, want) {
		t.Errorf("rebuilt audience state = %v, want %v", got, want)
	}

	// Branch: a draft that selects no pool at all. Every pool association goes,
	// while the legacy customer_list relation stays.
	env.commitTx("empty audience plan", func(tx *sqlx.Tx) error {
		return env.core.syncCampaignAudienceTx(tx, campaignID, access, CampaignAudiencePlan{RebuildAll: true})
	})
	want = []string{regularRow}
	if got := env.campaignWriteState(campaignID); !slices.Equal(got, want) {
		t.Errorf("emptied audience state = %v, want %v", got, want)
	}

	// The campaign row itself is never touched by the audience sync.
	var status string
	if err := env.db.Get(&status, `SELECT status FROM campaigns WHERE id=$1`, campaignID); err != nil {
		t.Fatalf("read campaign: %v", err)
	}
	if status != "draft" {
		t.Errorf("campaign status = %q, want draft", status)
	}
}
