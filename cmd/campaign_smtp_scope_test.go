package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// Audience organizations select recipients and reply routes, never SMTP owners.
func TestCampaignSMTPDoesNotFollowPublicAudienceOrganizations(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	seedOrgPoolAllocationFixtures(t, a.db)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO user_smtp_servers(uuid,user_id,name,from_email) VALUES
		(gen_random_uuid(),1,'owner','owner@example.invalid'),
		(gen_random_uuid(),2,'member','member@example.invalid')`)
	var senderPool int
	if err := a.db.Get(&senderPool, `INSERT INTO organization_smtp_pools(organization_id,name) VALUES(101,'Selected pool') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO user_smtp_servers(uuid,organization_id,smtp_pool_id,name,from_email)
		VALUES(gen_random_uuid(),101,$1,'organization','organization@example.invalid')`, senderPool)
	pool := seedOrgPoolAllocationPool(t, a.db, "Recipient audience")
	var list int
	if err := a.db.Get(&list, `INSERT INTO customer_lists(uuid,name,type,organization_id)
		VALUES(gen_random_uuid(),'Allocation','org_pool_allocation',101) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,101)`, list, pool)
	actor := auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}
	for _, tc := range []struct {
		name, source, want string
		org                int
	}{
		{"personal workspace uses owner", "personal", "owner", 0},
		{"organization workspace uses owner", "personal", "owner", 101},
		{"organization source uses selected pool", "organization", "organization", 101},
		{"personal workspace hides organization senders", "organization", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/campaigns/smtp-overview?pool_scope=all_organizations&source=" + tc.source +
				"&customer_list_ids=" + strconv.Itoa(pool) + "&smtp_pool_id=" + strconv.Itoa(senderPool)
			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.Header.Set(workspaceHeader, strconv.Itoa(tc.org))
			rec := httptest.NewRecorder()
			ctx := echo.New().NewContext(req, rec)
			ctx.Set(auth.UserHTTPCtxKey, actor)
			if err := a.GetCampaignSMTPOverview(ctx); err != nil {
				t.Fatal(err)
			}
			var out struct {
				Data []campaignSMTPOverview `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(out.Data) != 0 {
					t.Fatalf("personal workspace exposed organization senders: %+v", out.Data)
				}
			} else if len(out.Data) != 1 || out.Data[0].Name != tc.want {
				t.Fatalf("senders=%+v, want only %s", out.Data, tc.want)
			}
		})
	}
	t.Run("all-organization audience accepts workspace sender pool", func(t *testing.T) {
		camp := models.Campaign{SMTPSource: "organization", SMTPPoolID: null.IntFrom(senderPool), PoolScope: models.CampaignPoolScopeAllOrganizations}
		if err := a.validateCampaignSMTPPool(&camp, 101); err != nil {
			t.Fatal(err)
		}
		if err := a.validateCampaignSMTPPool(&camp, 102); err == nil {
			t.Fatal("accepted another organization's sender pool")
		}
	})
}
