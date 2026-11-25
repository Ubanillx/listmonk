package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/labstack/echo/v4"
)

// Campaign previews must show the delivered message, including the inline
// images and linked file cards the sender appends for attachments. These tests
// drive the real handlers over a throwaway database installed from schema.sql
// and skip without ADMIN_AUTH_TEST_DSN (or MIGRATION_TEST_DSN), following the
// package's database-backed test pattern.
func TestCampaignPreviewRendersAttachments(t *testing.T) {
	a := newCampaignPreviewAttachmentTestApp(t)
	f := seedCampaignPreviewMediaFixtures(t, a.db)

	t.Run("saved campaign uses the stored media set", func(t *testing.T) {
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodGet, "/api/campaigns/1/preview", f.campaignID, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d: %s", rec.Code, body)
		}
		if !strings.Contains(body, "Hello preview") {
			t.Fatalf("preview lost the campaign body: %s", body)
		}
		for _, want := range []string{
			"附件资料", "price-list.pdf", "brochure.docx",
			// Unreferenced image attachments render as images, not file cards.
			fmt.Sprintf(`<img src="/api/media/file/%d/catalog.png"`, f.imageID),
			// Preview links stay site-relative so a different app.root_url origin
			// can still inline them with the admin session.
			fmt.Sprintf(`href="/api/media/file/%d/price-list.pdf"`, f.pdfID),
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("saved preview missing %q: %s", want, body)
			}
		}
		if strings.Contains(body, "example.test") {
			t.Fatalf("saved preview leaked absolute app.root_url media URLs: %s", body)
		}
	})

	t.Run("GET preview ignores the media query parameter", func(t *testing.T) {
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodGet,
			"/api/campaigns/1/preview?media=[]", f.campaignID, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "price-list.pdf") {
			t.Fatalf("GET preview did not render the saved set: %d %s", rec.Code, body)
		}
	})

	t.Run("POST without media keeps the saved set", func(t *testing.T) {
		form := url.Values{
			"content_type": {"html"},
			"body":         {"<p>Legacy client</p>"},
		}
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview", f.campaignID, form)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "Legacy client") ||
			!strings.Contains(body, "price-list.pdf") || !strings.Contains(body, "brochure.docx") {
			t.Fatalf("POST without media lost the saved set: %d %s", rec.Code, body)
		}
	})

	t.Run("malformed and oversized media lists are rejected", func(t *testing.T) {
		for name, media := range map[string]string{
			"malformed": "not-json",
			"null":      "null",
			"oversized": "[" + strings.TrimSuffix(strings.Repeat("1,", maxPreviewMediaIDs+1), ",") + "]",
		} {
			form := url.Values{"content_type": {"html"}, "body": {"<p>x</p>"}, "media": {media}}
			_, err := callCampaignPreviewAs(a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview",
				f.campaignID, superAdminUser(), form)
			var httpErr *echo.HTTPError
			if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest {
				t.Fatalf("%s media list = %v, want HTTP 400", name, err)
			}
		}
	})

	t.Run("duplicate and unknown media IDs converge", func(t *testing.T) {
		form := url.Values{
			"content_type": {"html"},
			"body":         {"<p>Dedupe</p>"},
			"media":        {fmt.Sprintf(`[%d,%d,999999]`, f.pdfID, f.pdfID)},
		}
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview", f.campaignID, form)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || strings.Count(body, "附件资料") != 1 ||
			strings.Count(body, ">price-list.pdf</a>") != 1 || strings.Contains(body, "999999") {
			t.Fatalf("duplicate or unknown media was not converged: %d %s", rec.Code, body)
		}
	})

	t.Run("plain text preview appends file links", func(t *testing.T) {
		form := url.Values{
			"content_type": {"plain"},
			"body":         {"Plain body"},
			"media":        {fmt.Sprintf(`[%d]`, f.pdfID)},
		}
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview", f.campaignID, form)
		body := rec.Body.String()
		want := fmt.Sprintf("price-list.pdf: /api/media/file/%d/price-list.pdf", f.pdfID)
		if rec.Code != http.StatusOK || !strings.Contains(body, "Plain body") || !strings.Contains(body, want) {
			t.Fatalf("plain preview missing attachment link %q: %d %s", want, rec.Code, body)
		}
	})

	t.Run("draft request media overrides saved attachments", func(t *testing.T) {
		form := url.Values{
			"content_type": {"html"},
			"body":         {"<p>Edited body</p>"},
			"media":        {fmt.Sprintf(`[%d]`, f.pdfID)},
		}
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview", f.campaignID, form)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d: %s", rec.Code, body)
		}
		for _, want := range []string{"Edited body", "price-list.pdf", "brochure.docx"} {
			if !strings.Contains(body, want) {
				t.Fatalf("draft preview missing %q: %s", want, body)
			}
		}
		if strings.Contains(body, "catalog.png") {
			t.Fatalf("preview kept a removed attachment: %s", body)
		}
	})

	t.Run("empty request media hides campaign attachments", func(t *testing.T) {
		form := url.Values{
			"content_type": {"html"},
			"body":         {"<p>No attachments</p>"},
			"media":        {"[]"},
		}
		rec := callCampaignPreview(t, a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview", f.campaignID, form)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d: %s", rec.Code, body)
		}
		if !strings.Contains(body, "No attachments") || !strings.Contains(body, "brochure.docx") {
			t.Fatalf("preview missing body or template attachment: %s", body)
		}
		for _, unwanted := range []string{"price-list.pdf", "catalog.png"} {
			if strings.Contains(body, unwanted) {
				t.Fatalf("preview kept removed attachment %q: %s", unwanted, body)
			}
		}
	})

	t.Run("unsaved campaign preview renders the selected template media", func(t *testing.T) {
		form := url.Values{
			"content_type": {"html"},
			"body":         {"<p>Brand new</p>"},
			"template_id":  {fmt.Sprint(f.templateID)},
			"media":        {fmt.Sprintf(`[%d]`, f.pdfID)},
		}
		rec := callCampaignPreview(t, a.PreviewNewCampaign, http.MethodPost, "/api/campaigns/preview", 0, form)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d: %s", rec.Code, body)
		}
		for _, want := range []string{"Brand new", "price-list.pdf", "brochure.docx"} {
			if !strings.Contains(body, want) {
				t.Fatalf("unsaved preview missing %q: %s", want, body)
			}
		}
		if strings.Contains(body, "catalog.png") {
			t.Fatalf("unsaved preview rendered an unselected attachment: %s", body)
		}
	})

	t.Run("requested media is bounded by permissions and workspace", func(t *testing.T) {
		readerID := insertPreviewTestUser(t, a.db, "preview-reader")
		otherID := insertPreviewTestUser(t, a.db, "preview-other-owner")
		ownID := insertPrivatePreviewMedia(t, a.db, "own-attachment.pdf", readerID)
		foreignID := insertPrivatePreviewMedia(t, a.db, "foreign-secret.pdf", otherID)

		var campID int
		if err := a.db.Get(&campID, `INSERT INTO campaigns(uuid, name, subject, from_email, body, content_type, messenger, status, owner_user_id)
			VALUES(gen_random_uuid(), 'Reader campaign', 'Subject', 'reader@example.test', '<p>Reader preview</p>', 'html', 'email', 'paused', $1)
			RETURNING id`, readerID); err != nil {
			t.Fatalf("seeding reader campaign: %v", err)
		}
		if _, err := a.db.Exec(`INSERT INTO campaign_media(campaign_id, media_id, filename) VALUES($1, $2, $3)`,
			campID, ownID, "own-attachment.pdf"); err != nil {
			t.Fatalf("seeding reader campaign media: %v", err)
		}

		reader := auth.User{
			Base:           auth.Base{ID: readerID},
			UserRoleID:     auth.SuperAdminRoleID + 100,
			PermissionsMap: map[string]struct{}{"campaigns:get": {}, "workspaces:personal": {}},
		}
		post := func(media []int) (*httptest.ResponseRecorder, error) {
			return callCampaignPreviewAs(a.PreviewCampaign, http.MethodPost, "/api/campaigns/1/preview", campID,
				reader, url.Values{
					"content_type": {"html"},
					"body":         {"<p>Reader edit</p>"},
					"media":        {fmt.Sprintf(`[%d,%d]`, media[0], media[1])},
				})
		}

		// Without media:get an unassociated media ID must not be probeable,
		// while the campaign's own saved attachment still previews.
		rec, err := post([]int{ownID, foreignID})
		body := rec.Body.String()
		if err != nil || rec.Code != http.StatusOK {
			t.Fatalf("reader preview = %v %d %s", err, rec.Code, body)
		}
		if !strings.Contains(body, "own-attachment.pdf") || strings.Contains(body, "foreign-secret.pdf") {
			t.Fatalf("media permission boundary failed: %s", body)
		}

		// media:get still cannot cross the personal workspace boundary.
		reader.PermissionsMap["media:get"] = struct{}{}
		rec, err = post([]int{ownID, foreignID})
		body = rec.Body.String()
		if err != nil || rec.Code != http.StatusOK {
			t.Fatalf("reader preview with media:get = %v %d %s", err, rec.Code, body)
		}
		if !strings.Contains(body, "own-attachment.pdf") || strings.Contains(body, "foreign-secret.pdf") {
			t.Fatalf("workspace boundary failed: %s", body)
		}
	})
}

func newCampaignPreviewAttachmentTestApp(t *testing.T) *App {
	t.Helper()

	db := newAdminTestDB(t)
	queries := prepareQueries(readTestQueries(t), db, koanf.New("."))

	langB, err := os.ReadFile(filepath.Join("..", "i18n", "en.json"))
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	translator, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}
	logger := log.New(io.Discard, "", 0)

	return &App{
		db:      db,
		queries: queries,
		core:    core.New(&core.Opt{DB: db, Queries: queries, I18n: translator, Log: logger}, nil),
		manager: manager.New(manager.Config{
			RootURL:         "https://example.test",
			UnsubURL:        "https://example.test/unsub/%s/%s",
			ViewTrackURL:    "https://example.test/view/%s/%s",
			LinkTrackURL:    "https://example.test/link/%s/%s",
			OptinURL:        "https://example.test/optin/%s?%s",
			DisableTracking: true,
		}, nil, translator, logger),
		urlCfg: &UrlConfig{RootURL: "https://example.test"},
		i18n:   translator,
		log:    logger,
	}
}

func callCampaignPreview(t *testing.T, handler func(echo.Context) error, method, path string, campID int, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	rec, err := callCampaignPreviewAs(handler, method, path, campID, superAdminUser(), form)
	if err != nil {
		t.Fatalf("campaign preview handler: %v", err)
	}
	return rec
}

func callCampaignPreviewAs(handler func(echo.Context) error, method, path string, campID int, user auth.User, form url.Values) (*httptest.ResponseRecorder, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetPath(path)
	c.Set("id", campID)
	c.Set(auth.UserHTTPCtxKey, user)
	return rec, handler(c)
}

func superAdminUser() auth.User {
	return auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}
}

func insertPreviewTestUser(t *testing.T, db *sqlx.DB, username string) int {
	t.Helper()

	if _, err := db.Exec(`INSERT INTO roles (id, name, permissions) VALUES (1, 'Preview test role', '{}')
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("seeding test role: %v", err)
	}
	var id int
	if err := db.Get(&id, `INSERT INTO users(username, email, name, type, user_role_id, status)
		VALUES($1, $2, $1, 'user', 1, 'enabled') RETURNING id`, username, username+"@example.test"); err != nil {
		t.Fatalf("seeding user %q: %v", username, err)
	}
	return id
}

func insertPrivatePreviewMedia(t *testing.T, db *sqlx.DB, filename string, ownerID int) int {
	t.Helper()

	var id int
	if err := db.Get(&id, `INSERT INTO media(uuid, filename, content_type, thumb, owner_user_id, visibility)
		VALUES(gen_random_uuid(), $1, 'application/pdf', '', $2, 'private') RETURNING id`, filename, ownerID); err != nil {
		t.Fatalf("seeding private media %q: %v", filename, err)
	}
	return id
}

type campaignPreviewFixtures struct {
	campaignID int
	templateID int
	pdfID      int
	imageID    int
}

// seedCampaignPreviewMediaFixtures creates a campaign whose own attachments
// are a PDF and an image, wrapped by a template that carries a DOCX of its own.
func seedCampaignPreviewMediaFixtures(t *testing.T, db *sqlx.DB) campaignPreviewFixtures {
	t.Helper()

	insertMedia := func(filename, contentType string) int {
		t.Helper()
		var id int
		if err := db.Get(&id, `INSERT INTO media(uuid, filename, content_type, thumb, visibility)
			VALUES(gen_random_uuid(), $1, $2, '', 'global') RETURNING id`, filename, contentType); err != nil {
			t.Fatalf("seeding media %q: %v", filename, err)
		}
		return id
	}
	f := campaignPreviewFixtures{
		pdfID:   insertMedia("price-list.pdf", "application/pdf"),
		imageID: insertMedia("catalog.png", "image/png"),
	}
	docID := insertMedia("brochure.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")

	if err := db.Get(&f.templateID, `INSERT INTO templates(name, type, subject, body)
		VALUES('Preview wrapper', 'campaign', 'Subject', '<html><body>{{ template "content" . }}</body></html>')
		RETURNING id`); err != nil {
		t.Fatalf("seeding template: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO template_media(template_id, media_id, filename) VALUES($1, $2, $3)`,
		f.templateID, docID, "brochure.docx"); err != nil {
		t.Fatalf("seeding template media: %v", err)
	}

	if err := db.Get(&f.campaignID, `INSERT INTO campaigns(uuid, name, subject, from_email, body, content_type, messenger, status, template_id)
		VALUES(gen_random_uuid(), 'Preview attachments', 'Subject', 'sender@example.test', '<p>Hello preview</p>', 'html', 'email', 'paused', $1)
		RETURNING id`, f.templateID); err != nil {
		t.Fatalf("seeding campaign: %v", err)
	}
	for _, m := range []struct {
		id   int
		name string
	}{{f.pdfID, "price-list.pdf"}, {f.imageID, "catalog.png"}} {
		if _, err := db.Exec(`INSERT INTO campaign_media(campaign_id, media_id, filename) VALUES($1, $2, $3)`,
			f.campaignID, m.id, m.name); err != nil {
			t.Fatalf("seeding campaign media %q: %v", m.name, err)
		}
	}
	return f
}
