package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestWorkspaceCustomerListOrganizationSharing(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
CREATE TYPE customer_list_type AS ENUM ('private', 'public', 'pool', 'org_pool_allocation');
CREATE TYPE customer_list_optin AS ENUM ('single', 'double');
CREATE TYPE customer_list_status AS ENUM ('active', 'archived');
ALTER TABLE customer_lists ALTER COLUMN type DROP DEFAULT, ALTER COLUMN status DROP DEFAULT;
ALTER TABLE customer_lists
    ALTER COLUMN type TYPE customer_list_type USING type::customer_list_type,
    ALTER COLUMN status TYPE customer_list_status USING status::customer_list_status;
ALTER TABLE customer_lists
    ALTER COLUMN type SET DEFAULT 'private',
    ALTER COLUMN status SET DEFAULT 'active';
ALTER TABLE customer_lists
    ADD COLUMN optin customer_list_optin NOT NULL DEFAULT 'single',
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private',
    ADD COLUMN transfer_pending_at TIMESTAMPTZ,
    ADD COLUMN tags VARCHAR(100)[],
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
CREATE TABLE mat_customer_list_customer_stats (
    customer_list_id BIGINT NOT NULL,
    status TEXT,
    customer_count INTEGER NOT NULL DEFAULT 0
);
`)
	env.core.consts.CacheSlowQueries = true
	env.exec(`INSERT INTO users(id) VALUES(2)`)
	env.exec(`INSERT INTO organizations(id,name) VALUES(7,'Current'),(8,'Other')`)
	sharedID := int(env.id(`INSERT INTO customer_lists(name,organization_id,owner_user_id,visibility)
		VALUES('Shared list',7,2,'organization') RETURNING id`))
	env.exec(`INSERT INTO customer_lists(name,organization_id,owner_user_id,visibility)
		VALUES('Private list',7,2,'private'),('Other organization',8,2,'organization')`)
	access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 7}, UserID: 1}

	lists, total, err := env.core.QueryWorkspaceLists(access, "", "", "", "active", nil, "name", SortAsc, 0, 20)
	if err != nil || total != 1 || len(lists) != 1 || lists[0].ID != sharedID {
		t.Fatalf("shared list query: total=%d lists=%v err=%v", total, lists, err)
	}
	list, err := env.core.GetWorkspaceList(access, sharedID)
	if err != nil || list.ID != sharedID {
		t.Fatalf("shared list detail: list=%v err=%v", list, err)
	}

	personal := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 1}
	lists, total, err = env.core.QueryWorkspaceLists(personal, "", "", "", "active", nil, "name", SortAsc, 0, 20)
	if err != nil || total != 0 || len(lists) != 0 {
		t.Fatalf("personal workspace leaked organization list: total=%d lists=%v err=%v", total, lists, err)
	}
}
