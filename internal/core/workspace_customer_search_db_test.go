package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

// Keep the three customer search consumers in sync without changing the shared
// development database. The harness creates a disposable PostgreSQL schema.
func TestWorkspaceCustomerCodeSearch(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
CREATE TYPE customer_status AS ENUM ('enabled', 'disabled', 'blocklisted');
CREATE TYPE subscription_status AS ENUM ('unconfirmed', 'confirmed', 'unsubscribed');
ALTER TABLE customer_lists ADD COLUMN transfer_pending_at TIMESTAMPTZ;
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    uuid UUID NOT NULL DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    attribs JSONB NOT NULL DEFAULT '{}',
    status customer_status NOT NULL DEFAULT 'enabled',
    customer_code TEXT NOT NULL DEFAULT '',
    organization_id BIGINT,
    owner_user_id INTEGER,
    original_owner_user_id INTEGER,
    visibility TEXT NOT NULL DEFAULT 'personal',
    transfer_pending_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE customer_list_memberships (
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    customer_list_id BIGINT NOT NULL REFERENCES customer_lists(id),
    status subscription_status NOT NULL DEFAULT 'unconfirmed',
    meta JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`)
	visibleID := int(env.id(`INSERT INTO customers(email,name,customer_code,owner_user_id)
		VALUES('visible@example.com','Visible','CUST-EDIT-42',1) RETURNING id`))
	env.exec(`INSERT INTO users(id) VALUES(2)`)
	hiddenID := int(env.id(`INSERT INTO customers(email,name,customer_code,owner_user_id)
		VALUES('hidden@example.com','Hidden','CUST-EDIT-42',2) RETURNING id`))
	access := models.WorkspaceAccess{UserID: 1}

	rows, total, err := env.core.QueryWorkspaceCustomers(access, "edit-42", nil, "", "ASC", "customer_code", 0, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != visibleID {
		t.Fatalf("customer code list search: total=%d rows=%v err=%v", total, rows, err)
	}
	ids, err := env.core.GetWorkspaceCustomerIDs(access, "edit-42", nil, "")
	if err != nil || len(ids) != 1 || ids[0] != visibleID {
		t.Fatalf("customer code bulk targets: ids=%v err=%v", ids, err)
	}
	next, err := env.core.ExportWorkspaceCustomers(access, "edit-42", nil, []int{visibleID, hiddenID}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := next()
	if err != nil || len(exported) != 1 || exported[0].ID != visibleID || exported[0].CustomerCode != "CUST-EDIT-42" {
		t.Fatalf("customer code export: rows=%v err=%v", exported, err)
	}

	// Existing name/e-mail searches retain their previous behavior.
	rows, total, err = env.core.QueryWorkspaceCustomers(access, "Visible", nil, "", "ASC", "name", 0, 20)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("name search regression: total=%d rows=%v err=%v", total, rows, err)
	}
}
