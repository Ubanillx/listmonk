package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/internal/media/providers/filesystem"
	"github.com/knadh/listmonk/internal/migrations"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func TestEmailMediaRecipientAccessAndRevocation(t *testing.T) {
	db := newAdminTestDB(t)
	// Fresh-install and upgraded databases must produce the same table. Repeated
	// upgrades must preserve a recipient link already mailed to a customer.
	if _, err := db.Exec(`DROP TABLE email_media_links`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.V6_58_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	provider, err := filesystem.New(filesystem.Opts{UploadPath: t.TempDir(), UploadURI: "/uploads", RootURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	const filename = "photo.png"
	content := []byte("\x89PNG\r\n\x1a\nimage bytes")
	if _, err := provider.Put(filename, "image/png", bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := db.Get(&id, `INSERT INTO media(uuid,filename,content_type,thumb) VALUES(gen_random_uuid(),$1,'image/png','') RETURNING id`, filename); err != nil {
		t.Fatal(err)
	}
	s := newManagerStore(nil, nil, provider, db)
	var snapshot media.Media
	if err := db.Get(&snapshot, `SELECT id,uuid,filename,content_type,organization_id,owner_user_id FROM media WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	issued, err := s.linkedMediaAttachment(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.V6_58_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.linkedMediaAttachment(snapshot)
	if err != nil || issued.DeliveryURL != resumed.DeliveryURL {
		t.Fatalf("restart/upgrade changed mailed link: %v", err)
	}
	a := &App{db: db, media: provider, log: log.New(io.Discard, "", 0)}
	e := echo.New()
	e.GET("/email-media/:token/:filename", a.ServeEmailMedia)
	check := func(link string, want int) {
		t.Helper()
		r := httptest.NewRecorder()
		e.ServeHTTP(r, httptest.NewRequest(http.MethodGet, link, nil))
		if r.Code != want {
			t.Fatalf("GET %s: status=%d want=%d body=%s", link, r.Code, want, r.Body.String())
		}
		if want == http.StatusOK && (r.Header().Get("Content-Type") != "image/png" || !bytes.Equal(r.Body.Bytes(), content)) {
			t.Fatalf("mail client could not render remote image: %v %q", r.Header(), r.Body.Bytes())
		}
	}
	// No login, no archive setting, and no request workspace are needed.
	check(issued.DeliveryURL, http.StatusOK)
	check(strings.Replace(issued.DeliveryURL, filename, "other.png", 1), http.StatusNotFound)
	check("/email-media/11111111-2222-4333-8444-555555555555/photo.png", http.StatusNotFound)
	check("/email-media/invalid/photo.png", http.StatusNotFound)
	if _, err := db.Exec(`UPDATE media SET transfer_pending_at=NOW() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	check(issued.DeliveryURL, http.StatusNotFound)
	if _, err := db.Exec(`UPDATE media SET transfer_pending_at=NULL,uuid=gen_random_uuid() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	check(issued.DeliveryURL, http.StatusNotFound)
	if _, err := s.linkedMediaAttachment(snapshot); err == nil {
		t.Fatal("stale authorization issued a link to a replaced resource")
	}
	if err := db.Get(&snapshot, `SELECT id,uuid,filename,content_type,organization_id,owner_user_id FROM media WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	rotated, err := s.linkedMediaAttachment(snapshot)
	if err != nil || rotated.DeliveryURL == issued.DeliveryURL {
		t.Fatalf("changed resource did not rotate link: %v", err)
	}
	check(rotated.DeliveryURL, http.StatusOK)
	check(issued.DeliveryURL, http.StatusNotFound)
	// Grant issuance does not bypass campaign associations. A saved paused
	// campaign uses the same authorized media set when it resumes.
	var campaignID int
	if err := db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status)
		VALUES(gen_random_uuid(),'Remote media','Test','sender@example.com','<img src="/uploads/photo.png">','email','paused') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	campaign := &models.Campaign{}
	campaign.ID = campaignID
	if _, err := s.GetCampaignAttachments(campaign, []int64{int64(id)}); err == nil {
		t.Fatal("campaign issued a link to unassociated private media")
	}
	if _, err := db.Exec(`INSERT INTO campaign_media(campaign_id,media_id,filename) VALUES($1,$2,$3)`, campaignID, id, filename); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"paused", "running"} {
		if _, err := db.Exec(`UPDATE campaigns SET status=$2::campaign_status WHERE id=$1`, campaignID, status); err != nil {
			t.Fatal(err)
		}
		files, err := s.GetCampaignAttachments(campaign, []int64{int64(id)})
		if err != nil || len(files) != 1 || files[0].DeliveryURL != rotated.DeliveryURL {
			t.Fatalf("%s campaign did not retain remote link: %v %+v", status, err, files)
		}
	}
	// Organization lifecycle and ownership remain authoritative at read time.
	seedOrgPoolAllocationFixtures(t, db)
	if _, err := db.Exec(`UPDATE media SET organization_id=101,owner_user_id=2 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	check(rotated.DeliveryURL, http.StatusNotFound)
	if _, err := s.linkedMediaAttachment(snapshot); err == nil {
		t.Fatal("stale ownership snapshot issued a recipient grant")
	}
	if err := db.Get(&snapshot, `SELECT id,uuid,filename,content_type,organization_id,owner_user_id FROM media WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	organizationLink, err := s.linkedMediaAttachment(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	check(organizationLink.DeliveryURL, http.StatusOK)
	if _, err := db.Exec(`UPDATE organizations SET status='archived' WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	check(organizationLink.DeliveryURL, http.StatusNotFound)
	if _, err := db.Exec(`DELETE FROM media WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	check(rotated.DeliveryURL, http.StatusNotFound)
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM email_media_links`); err != nil || count != 0 {
		t.Fatalf("deletion left a recipient grant: %d %v", count, err)
	}
	if _, err := url.ParseRequestURI(issued.DeliveryURL); err != nil {
		t.Fatal(err)
	}
}
