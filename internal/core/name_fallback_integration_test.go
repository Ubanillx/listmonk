package core

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

func TestTemplateNameFallbackPersistence(t *testing.T) {
	dsn := os.Getenv("NAME_FALLBACK_TEST_DSN")
	if dsn == "" {
		t.Skip("NAME_FALLBACK_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db = db.Unsafe()
	db.SetMaxOpenConns(1)
	// The application's fresh-install script contains DROP statements. Restrict
	// search_path to a new, test-owned schema (no public fallback).
	schema := fmt.Sprintf("name_fallback_test_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
	if _, err := db.Exec("SET search_path TO " + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(b)); err != nil {
		t.Fatal(err)
	}
	queries := goyesql.Queries{}
	for _, file := range []string{"templates.sql", "campaigns.sql"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "queries", file))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := goyesql.ParseBytes(b)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range parsed {
			queries[k] = v
		}
	}
	prepare := func(name string) *sqlx.Stmt {
		s, err := db.Preparex(queries[name].Query)
		if err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}
		deferClose := s
		t.Cleanup(func() { deferClose.Close() })
		return s
	}
	q := &models.Queries{GetTemplates: prepare("get-templates"), CreateTemplate: prepare("create-template"),
		UpdateTemplate: prepare("update-template"), GetCampaign: prepare("get-campaign"),
		CreateCampaign: prepare("create-campaign"), UpdateCampaign: prepare("update-campaign")}
	translator, err := i18n.New([]byte(`{"_.code":"en","_.name":"English"}`))
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{db: db, q: q, i18n: translator, log: log.New(io.Discard, "", 0)}
	var userID int
	if _, err := db.Exec(`INSERT INTO roles (id,type,name) VALUES (1,'user','Super Admin') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&userID, `INSERT INTO users (username,email,name,type,user_role_id,status)
		VALUES ('fallback-test','fallback@example.invalid','Test','user',1,'enabled') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: userID}
	scope := ApplyWorkspaceScope(access, models.ResourceVisibilityPrivate)
	rule := models.NameFallback{Enabled: true, Value: "Sir or Madam", InvalidValues: []string{"N/A"}}
	tpl, err := c.CreateTemplateInWorkspace(access, "fallback", "campaign", "", []byte(`{{ template "content" . }}`), null.String{}, nil, scope, &rule)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.NameFallback.Resolve("n/a") != rule.Value {
		t.Fatal("create lost rule")
	}
	if _, err := c.UpdateTemplateInWorkspace(access, tpl.ID, "edited", "", []byte(tpl.Body), null.String{}, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	clone, err := c.CloneTemplateForWorkspace(tpl.ID, access, "copy", "")
	if err != nil || clone.NameFallback.Resolve("") != rule.Value {
		t.Fatalf("clone lost rule: %+v %v", clone.NameFallback, err)
	}
	other := models.WorkspaceAccess{UserID: userID + 100}
	if _, err := c.UpdateTemplateInWorkspace(other, tpl.ID, "forbidden", "", []byte(tpl.Body), null.String{}, nil, "", &rule); err == nil {
		t.Fatal("non-owner changed rule")
	}
	visual, err := c.CreateTemplateInWorkspace(access, "visual", "campaign_visual", "", []byte(`Dear {{ .Customer.Name }}`), null.String{}, nil, scope, &rule)
	if err != nil {
		t.Fatal(err)
	}
	camp, err := c.CreateCampaignInWorkspace(access, models.Campaign{Type: "regular", Name: "visual import", Subject: "Hello", FromEmail: "test@example.invalid",
		ContentType: "visual", Messenger: "email", DailySendLimit: 300, DailyResumeTime: "09:00", Headers: models.Headers{}, Attribs: models.JSON{},
		ArchiveMeta: json.RawMessage(`{}`), TemplateID: null.IntFrom(visual.ID)}, nil, nil, scope)
	if err != nil {
		t.Fatal(err)
	}
	if camp.TemplateID.Valid || camp.NameFallback.Resolve("") != rule.Value {
		t.Fatalf("visual rule snapshot missing: %+v", camp.NameFallback)
	}
	// Linked template reads use the same scope as the wrapper body.
	if _, err := db.Exec(`UPDATE campaigns SET content_type='html', template_id=$2 WHERE id=$1`, camp.ID, tpl.ID); err != nil {
		t.Fatal(err)
	}
	linked, err := c.GetWorkspaceCampaignForPreview(access, camp.ID, tpl.ID)
	if err != nil || linked.TemplateNameFallback.Resolve("") != rule.Value {
		t.Fatalf("preview rule missing: %v", err)
	}
	if err := q.GetCampaign.Get(&linked, camp.ID, nil, nil, "default"); err != nil {
		t.Fatal(err)
	}
	if linked.TemplateNameFallback.Resolve("") != rule.Value {
		t.Fatal("send query lost rule")
	}
	if _, err := db.Exec(`UPDATE campaigns SET content_type='visual', template_id=NULL WHERE id=$1`, camp.ID); err != nil {
		t.Fatal(err)
	}
	changed := models.NameFallback{Enabled: true, Value: "New Team"}
	if _, err := c.UpdateTemplateInWorkspace(access, visual.ID, "visual", "", []byte(visual.Body), null.String{}, nil, "", &changed); err != nil {
		t.Fatal(err)
	}
	copied, err := c.CloneCampaignForWorkspace(camp.ID, access, "copied visual")
	if err != nil || copied.NameFallback.Resolve("") != rule.Value {
		t.Fatalf("independent visual copy: %+v %v", copied.NameFallback, err)
	}
	camp.TemplateID = null.IntFrom(visual.ID)
	updated, err := c.UpdateCampaignInWorkspace(access, camp.ID, camp, nil, nil, "")
	if err != nil || updated.NameFallback.Resolve("") != changed.Value {
		t.Fatalf("visual reimport lost rule: %+v %v", updated.NameFallback, err)
	}
	disabled := models.NameFallback{}
	updatedTpl, err := c.UpdateTemplateInWorkspace(access, tpl.ID, tpl.Name, "", []byte(tpl.Body), null.String{}, nil, "", &disabled)
	if err != nil || updatedTpl.NameFallback.Resolve("") != "" {
		t.Fatal("disable failed", err)
	}
}
