package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/media/providers/filesystem"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
	"github.com/labstack/echo/v4"
)

var resumeMediaNotifsOnce sync.Once

// Exercise a campaign that started before remote media delivery was enabled.
// Resume through the workspace status mutation and the real scheduler, store,
// worker and SMTP serializer, preserving the historical recipient snapshot.
func TestStartedCampaignResumesWithRemoteMedia(t *testing.T) {
	for _, source := range []string{"protected", "uploads", "cid", "live-pause"} {
		t.Run(source, func(t *testing.T) {
			a := newOrgPoolAllocationTestApp(t)
			seedOrgPoolAllocationFixtures(t, a.db)
			a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
			resumeMediaNotifsOnce.Do(func() {
				// No admin notifications are delivered by this integration fixture.
				notifs.Initialize(notifs.Opt{}, nil, nil, a.log)
			})
			provider, err := filesystem.New(filesystem.Opts{UploadPath: t.TempDir(), UploadURI: "/uploads"})
			if err != nil {
				t.Fatal(err)
			}
			a.media = provider
			files := []struct {
				name, contentType string
				content           []byte
			}{
				{"photo.png", "image/png", []byte("\x89PNG\r\n\x1a\nlegacy image")},
				{"catalog.pdf", "application/pdf", []byte("%PDF-1.4\nlegacy document")},
			}
			mediaIDs := make([]int, len(files))
			for i, file := range files {
				if _, err := provider.Put(file.name, file.contentType, bytes.NewReader(file.content)); err != nil {
					t.Fatal(err)
				}
				if err := a.db.Get(&mediaIDs[i], `INSERT INTO media(uuid,filename,content_type,thumb,owner_user_id)
					VALUES(gen_random_uuid(),$1,$2,'',1) RETURNING id`, file.name, file.contentType); err != nil {
					t.Fatal(err)
				}
			}
			imageSource := fmt.Sprintf("/api/media/file/%d/photo.png", mediaIDs[0])
			if source == "uploads" {
				imageSource = "/uploads/photo.png"
			} else if source == "cid" {
				imageSource = fmt.Sprintf("cid:media-%d@listmonk", mediaIDs[0])
			}
			wrapper, err := os.ReadFile("../static/email-templates/default.tpl")
			if err != nil {
				t.Fatal(err)
			}
			var templateID int
			if err := a.db.Get(&templateID, `INSERT INTO templates(name,type,subject,body,owner_user_id)
				VALUES('Default email','campaign','Resume media',$1,1) RETURNING id`, string(wrapper)); err != nil {
				t.Fatal(err)
			}
			originalBody := `<p>Existing campaign</p><img width="300" src="` + imageSource + `">`
			started := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
			var campaignID int
			if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,content_type,messenger,
				status,owner_user_id,started_at,to_send,sent,smtp_rate_limit,template_id) VALUES(gen_random_uuid(),'Historical campaign','Resume media',
				'sender@example.test',$1,'html','email','running',1,$2,3,1,1000000,$3) RETURNING id`, originalBody, started, templateID); err != nil {
				t.Fatal(err)
			}
			for i, id := range mediaIDs {
				if _, err := a.db.Exec(`INSERT INTO campaign_media(campaign_id,media_id,filename) VALUES($1,$2,$3)`,
					campaignID, id, files[i].name); err != nil {
					t.Fatal(err)
				}
			}
			var listID int
			if err := a.db.Get(&listID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id)
				VALUES(gen_random_uuid(),'Historical audience','private',1) RETURNING id`); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id) VALUES($1,$2)`, campaignID, listID); err != nil {
				t.Fatal(err)
			}
			for i, status := range []string{"sent", "pending", "queued"} {
				var customerID int
				if err := a.db.Get(&customerID, `INSERT INTO customers(uuid,email,name,owner_user_id)
					VALUES(gen_random_uuid(),$1,'Recipient',1) RETURNING id`, fmt.Sprintf("recipient%d@example.test", i)); err != nil {
					t.Fatal(err)
				}
				if _, err := a.db.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES($1,$2,'confirmed')`, customerID, listID); err != nil {
					t.Fatal(err)
				}
				if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,
					name_snapshot,attribs_snapshot,sent_at) VALUES($1,$2,$3::campaign_recipient_status,$4,'Recipient','{}',
					CASE WHEN $3='sent' THEN $5::timestamptz ELSE NULL END)`, campaignID, customerID, status,
					fmt.Sprintf("recipient%d@example.test", i), started); err != nil {
					t.Fatal(err)
				}
			}

			var holdAck chan struct{}
			if source == "live-pause" {
				holdAck = make(chan struct{})
				defer func() {
					select {
					case <-holdAck:
					default:
						close(holdAck)
					}
				}()
			}
			server, received := resumeMediaSMTP(t, holdAck)
			if _, err := a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
				VALUES(gen_random_uuid(),1,$1,$2,'none','none')`, server.Host, server.Port); err != nil {
				t.Fatal(err)
			}
			store := newManagerStore(a.queries, a.core, provider, a.db)
			access := models.WorkspaceAccess{UserID: 1, Workspace: models.Workspace{Personal: true}}
			if _, err := a.core.UpdateCampaignStatusInWorkspace(access, campaignID, models.CampaignStatusPaused); err != nil {
				t.Fatal(err)
			}
			if active, err := store.NextCampaigns(nil); err != nil || len(active) != 0 {
				t.Fatalf("paused campaign became sendable: %v %+v", err, active)
			}
			finished := make(chan struct{}, 1)
			m := manager.New(manager.Config{
				RootURL: "https://example.test", BatchSize: 1, Concurrency: 1, MessageRate: 100,
				ScanCampaigns: true, ScanInterval: 10 * time.Millisecond, DisableTracking: true, MaxSendErrors: 1,
				PersonalSMTP: func(userID int) (*email.Emailer, error) {
					if userID != 1 {
						return nil, fmt.Errorf("unexpected owner %d", userID)
					}
					return email.New(email.MessengerName, server)
				},
				AuditCampaign: func(action string, _ *models.Campaign, _ map[string]any) {
					if action == "campaign.finished" {
						finished <- struct{}{}
					}
				},
			}, store, a.i18n, a.log)
			a.manager = m
			requestStatus := func(status string) error {
				req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/campaigns/%d/status", campaignID),
					strings.NewReader(fmt.Sprintf(`{"status":%q}`, status)))
				req.Header.Set("Content-Type", "application/json")
				c := echo.New().NewContext(req, httptest.NewRecorder())
				c.Set("id", campaignID)
				c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID})
				return a.UpdateCampaignStatus(c)
			}
			changeStatus := func(status string) {
				t.Helper()
				if err := requestStatus(status); err != nil {
					t.Fatalf("%s through HTTP status handler: %v", status, err)
				}
			}
			runDone := make(chan struct{})
			go func() { m.Run(); close(runDone) }()
			t.Cleanup(func() { m.Close(); <-runDone })
			changeStatus(models.CampaignStatusRunning)
			e := echo.New()
			e.GET("/email-media/:token/:filename", a.ServeEmailMedia)
			seen := make(map[string]bool)
			for index := range 2 {
				select {
				case raw := <-received:
					msg, err := mail.ReadMessage(bytes.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
					to, err := mail.ParseAddress(msg.Header.Get("To"))
					if err != nil || (to.Address != "recipient1@example.test" && to.Address != "recipient2@example.test") || seen[to.Address] {
						t.Fatalf("resumed campaign repeated a recipient: %q %v", msg.Header.Get("To"), err)
					}
					seen[to.Address] = true
					body := resumeMediaHTML(t, textproto.MIMEHeader(msg.Header), msg.Body)
					footer := strings.Index(body, `<div class="footer"`)
					cover := strings.Index(body, `class="listmonk-attachment-cover"`)
					if cover < 0 || footer < cover || !strings.Contains(body[:footer], `>catalog.pdf</a>`) ||
						!strings.HasSuffix(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body[:footer]), "</div>")), "</table>") {
						t.Fatalf("SMTP message did not place the file cover inside the default wrapper: %s", body)
					}
					for _, file := range files {
						var link string
						if err := a.db.Get(&link, `SELECT '/email-media/'||token::text||'/'||filename FROM email_media_links WHERE filename=$1`, file.name); err != nil {
							t.Fatal(err)
						}
						attribute := `href="`
						if file.contentType == "image/png" {
							attribute = `src="`
						}
						if !strings.Contains(body, attribute+"https://example.test"+link+`"`) || strings.Contains(body, "cid:") {
							t.Fatalf("resumed message did not use remote media: %s", body)
						}
						r := httptest.NewRecorder()
						e.ServeHTTP(r, httptest.NewRequest(http.MethodGet, link, nil))
						if r.Code != http.StatusOK || r.Header().Get("Content-Type") != file.contentType || !bytes.Equal(r.Body.Bytes(), file.content) {
							t.Fatalf("recipient cannot retrieve %s without authentication: %d", file.name, r.Code)
						}
					}
					if holdAck != nil && index == 0 {
						// The SMTP server has the first message, but has not accepted
						// it yet. Pause and immediately resume while the old pipe and
						// its queued recipients are still alive.
						changeStatus(models.CampaignStatusPaused)
						select {
						case <-received:
							t.Fatal("queued message was sent during pause")
						default:
						}
						if _, err := a.db.Exec(`UPDATE user_smtp_servers SET enabled=FALSE WHERE user_id=1`); err != nil {
							t.Fatal(err)
						}
						var smtpErr *echo.HTTPError
						if err := requestStatus(models.CampaignStatusRunning); !errors.As(err, &smtpErr) || smtpErr.Code != http.StatusConflict {
							t.Fatalf("resume with disabled SMTP was not blocked: %v", err)
						}
						if _, err := a.db.Exec(`UPDATE user_smtp_servers SET enabled=TRUE WHERE user_id=1`); err != nil {
							t.Fatal(err)
						}
						changeStatus(models.CampaignStatusRunning)
						close(holdAck)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("resumed campaign did not send the remaining recipients")
				}
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("resumed campaign did not finish")
			}
			select {
			case <-received:
				t.Fatal("resumed campaign sent an extra message")
			default:
			}
			campaign, err := store.GetCampaign(campaignID)
			if err != nil || campaign.Sent != 3 || campaign.ToSend != 3 || !campaign.StartedAt.Time.Equal(started) || campaign.Body != originalBody {
				t.Fatalf("resume changed the saved content or historical progress: %+v %v", campaign, err)
			}
			var historicalSent time.Time
			if err := a.db.Get(&historicalSent, `SELECT sent_at FROM campaign_recipients WHERE campaign_id=$1 AND email_snapshot='recipient0@example.test'`, campaignID); err != nil || !historicalSent.Equal(started) {
				t.Fatalf("already sent recipient was changed: %v %v", historicalSent, err)
			}
		})
	}
}

func TestCampaignTestDeliversAttachmentsToEveryRecipient(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		explicitTemplate, startNow, withoutMedia bool
	}{
		{"queued with a different template", true, false, false},
		{"queued with saved template", false, false, false},
		{"worker active during test request", false, true, false},
		{"worker active without attachments", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, received := newAttachmentDeliveryTestApp(t)
			f := seedCampaignPreviewMediaFixtures(t, a.db)
			if _, err := a.db.Exec(`UPDATE campaigns SET owner_user_id=1, attachment_lang='de' WHERE id=$1`, f.campaignID); err != nil {
				t.Fatal(err)
			}
			list := seedDeletionList(t, a, "Attachment QA audience", "private", 1)
			for _, name := range []string{"first", "second"} {
				if _, err := a.db.Exec(`INSERT INTO customers(uuid,email,name,owner_user_id)
					VALUES(gen_random_uuid(),$1,$2,1)`, name+"@example.test", name); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{"price-list.pdf": "application/pdf", "brochure.docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
			templateField, templateFile := "", "brochure.docx"
			mediaField := fmt.Sprint(f.pdfID)
			if tc.explicitTemplate {
				templateFile = "alternate.pdf"
				var templateID int
				if err := a.db.Get(&templateID, `INSERT INTO templates(name,type,subject,body,owner_user_id)
					VALUES('Alternate','campaign','Alternate',$1,1) RETURNING id`,
					`<div class="wrap">Alternate wrapper {{ template "content" . }}</div><div class="footer">Footer</div>`); err != nil {
					t.Fatal(err)
				}
				fileID := insertPrivatePreviewMedia(t, a.db, templateFile, 1)
				if _, err := a.db.Exec(`INSERT INTO template_media(template_id,media_id,filename) VALUES($1,$2,$3)`, templateID, fileID, templateFile); err != nil {
					t.Fatal(err)
				}
				files[templateFile] = "application/pdf"
				templateField = fmt.Sprintf(`,"template_id":%d`, templateID)
			}
			if tc.withoutMedia {
				if _, err := a.db.Exec(`DELETE FROM template_media WHERE template_id=$1`, f.templateID); err != nil {
					t.Fatal(err)
				}
				files, mediaField = nil, ""
			}
			for name, mediaType := range files {
				if _, err := a.media.Put(name, mediaType, strings.NewReader("fixture")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.startNow {
				go a.manager.Run()
			}
			body := fmt.Sprintf(`{"name":"Attachment test","subject":"Test {{ .Customer.Name }}","content_type":"html",
				"body":"<p>Hello {{ .Customer.Name }}</p>","customer_list_ids":[%d],"media":[%s],"smtp_rate_limit":60000,
				"customers":["first@example.test","second@example.test"]%s}`, list, mediaField, templateField)
			req := httptest.NewRequest(http.MethodPost, "/api/campaigns/1/test", strings.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			c.Set("id", f.campaignID)
			c.Set(auth.UserHTTPCtxKey, superAdminUser())
			if err := a.TestCampaign(c); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("test send: HTTP %d, %v", rec.Code, err)
			}

			// Drain the queued variants after both messages so stale compile markers are deterministic.
			if !tc.startNow {
				go a.manager.Run()
			}
			seen := make(map[string]bool)
			for range 2 {
				msg := readAttachmentDelivery(t, received)
				to, err := mail.ParseAddress(msg.Header.Get("To"))
				if err != nil {
					t.Fatal(err)
				}
				name, _, _ := strings.Cut(to.Address, "@")
				if seen[name] || (name != "first" && name != "second") {
					t.Fatalf("unexpected test recipient %q", to.Address)
				}
				seen[name] = true
				html := resumeMediaHTML(t, textproto.MIMEHeader(msg.Header), msg.Body)
				greeting := strings.Index(html, "<p>Hello "+name+"</p>")
				cover := strings.Index(html, `class="listmonk-attachment-cover"`)
				footer := strings.Index(html, `<div class="footer"`)
				if greeting < 0 || footer <= greeting || strings.Contains(html, "listmonk-attachment-tail-") ||
					msg.Header.Get("Subject") != "Test "+name || strings.Contains(html, "Alternate wrapper") != tc.explicitTemplate {
					t.Fatalf("test message for %s did not have the personalized subject and correct wrapper", name)
				}
				if tc.withoutMedia {
					if cover >= 0 || strings.Contains(html, "/email-media/") {
						t.Fatalf("test message for %s with no selected media unexpectedly has attachments", name)
					}
					continue
				}
				if cover <= greeting || footer <= cover ||
					!strings.HasSuffix(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(html[cover:footer]), "</div>")), "</table>") {
					t.Fatalf("test message for %s did not place the attachment section inside the body before the footer", name)
				}
				if !strings.Contains(html, ">Anhänge<") || !strings.Contains(html, ">price-list.pdf</a>") ||
					!strings.Contains(html, ">"+templateFile+"</a>") || strings.Contains(html, "catalog.png") ||
					(tc.explicitTemplate && strings.Contains(html, "brochure.docx")) {
					t.Fatalf("test attachments for %s did not match the selected files, effective template and saved language", name)
				}
			}
		})
	}
}

func newAttachmentDeliveryTestApp(t *testing.T) (*App, <-chan []byte) {
	t.Helper()
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	a.cfg = &Config{FromEmail: "sender@example.test"}
	a.importer = subimporter.New(subimporter.Options{}, a.db.DB, a.i18n)
	provider, err := filesystem.New(filesystem.Opts{UploadPath: t.TempDir(), UploadURI: "/uploads"})
	if err != nil {
		t.Fatal(err)
	}
	a.media = provider
	server, received := resumeMediaSMTP(t, nil)
	if _, err := a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
		VALUES(gen_random_uuid(),1,$1,$2,'none','none')`, server.Host, server.Port); err != nil {
		t.Fatal(err)
	}
	a.manager = manager.New(manager.Config{
		RootURL: "https://example.test", UnsubURL: "https://example.test/unsub/%s/%s",
		Concurrency: 1, MessageRate: 100, DisableTracking: true,
		PersonalSMTP: func(userID int) (*email.Emailer, error) {
			if userID != 1 {
				return nil, fmt.Errorf("unexpected owner %d", userID)
			}
			return email.New(email.MessengerName, server)
		},
	}, newManagerStore(a.queries, a.core, provider, a.db), a.i18n, a.log)
	t.Cleanup(a.manager.Close)
	return a, received
}

func readAttachmentDelivery(t *testing.T, received <-chan []byte) *mail.Message {
	t.Helper()
	select {
	case raw := <-received:
		msg, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("local SMTP received no message")
		return nil
	}
}

// Reject every binary/inline MIME part, decoding HTML as a mail client would.
func resumeMediaHTML(t *testing.T, header textproto.MIMEHeader, body io.Reader) string {
	t.Helper()
	contentType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if header.Get("Content-Disposition") != "" || header.Get("Content-ID") != "" || params["name"] != "" {
		t.Fatalf("media attachment survived in SMTP MIME: %v", header)
	}
	if strings.HasPrefix(contentType, "multipart/") {
		r := multipart.NewReader(body, params["boundary"])
		var html string
		for {
			part, err := r.NextPart()
			if err == io.EOF {
				return html
			}
			if err != nil {
				t.Fatal(err)
			}
			html += resumeMediaHTML(t, part.Header, part)
		}
	}
	if contentType != "text/html" && contentType != "text/plain" {
		t.Fatalf("binary MIME part sent: %s", contentType)
	}
	if header.Get("Content-Transfer-Encoding") == "quoted-printable" {
		body = quotedprintable.NewReader(body)
	}
	content, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType == "text/html" {
		return string(content)
	}
	return ""
}

func resumeMediaSMTP(t *testing.T, holdAck <-chan struct{}) (email.Server, <-chan []byte) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan []byte, 10)
	var firstMessage sync.Once
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				fmt.Fprint(conn, "220 localhost SMTP\r\n")
				r := textproto.NewReader(bufio.NewReader(conn))
				for {
					line, err := r.ReadLine()
					if err != nil {
						return
					}
					switch line {
					case "DATA":
						fmt.Fprint(conn, "354 send data\r\n")
						data, err := r.ReadDotBytes()
						if err != nil {
							return
						}
						received <- data
						firstMessage.Do(func() {
							if holdAck != nil {
								<-holdAck
							}
						})
						fmt.Fprint(conn, "250 accepted\r\n")
					case "QUIT":
						fmt.Fprint(conn, "221 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "250 localhost\r\n")
					}
				}
			}()
		}
	}()
	return email.Server{UUID: t.Name(), FromEmail: "sender@example.test", AuthProtocol: "none", TLSType: "none",
		Opt: smtppool.Opt{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
			MaxConns: 1, IdleTimeout: time.Second, PoolWaitTimeout: time.Second}}, received
}
