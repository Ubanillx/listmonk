package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

func replyMailboxConfigContext(userID, orgID, mailboxID int, body string) (echo.Context, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(http.MethodPost, "/api/profile/reply-mailboxes/test", strings.NewReader(body))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if orgID > 0 {
		r.Header.Set(workspaceHeader, strconv.Itoa(orgID))
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(r, rec)
	c.Set("id", mailboxID)
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: userID}, UserRoleID: auth.SuperAdminRoleID,
		PermissionsMap: map[string]struct{}{auth.PermMailboxesManage: {}}})
	return c, rec
}

func createReplyMailboxForTest(t *testing.T, a *App, orgID, port int) int {
	t.Helper()
	c, rec := replyMailboxConfigContext(1, orgID, 0, fmt.Sprintf(`{
		"email":"reply@example.com","username":"reply-user","password":"stored-test-password",
		"imap_host":"127.0.0.1","imap_port":%d,"imap_tls":false,"ai_enabled":true}`, port))
	if err := a.CreateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data models.ReplyMailbox `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.Manageable || response.Data.Status != "pending" ||
		strings.Contains(rec.Body.String(), "stored-test-password") || strings.Contains(rec.Body.String(), `"password"`) {
		t.Fatalf("unexpected save response: %s", rec.Body)
	}
	return response.Data.ID
}

func TestReplyMailboxSavedPasswordConnection(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	original := mailboxHostPolicyCheck
	mailboxHostPolicyCheck = func(net.IP) error { return nil }
	t.Cleanup(func() { mailboxHostPolicyCheck = original })

	port, done := fakeReplyIMAP(t, nil)
	id := createReplyMailboxForTest(t, a, 0, port)
	// Saving another field with an empty password preserves the stored secret.
	c, rec := replyMailboxConfigContext(1, 0, id, fmt.Sprintf(`{
		"email":"reply@example.com","name":"Renamed","username":"reply-user","password":"",
		"imap_host":"127.0.0.1","imap_port":%d,"imap_tls":false,"ai_enabled":true}`, port))
	if err := a.UpdateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "stored-test-password") {
		t.Fatal("update response leaked password")
	}
	// Extra draft fields must never replace the saved parameters or credentials.
	c, rec = replyMailboxConfigContext(1, 0, id, fmt.Sprintf(`{
		"id":%d,"password":"wrong-draft-password","imap_host":"169.254.169.254"}`, id))
	if err := a.TestReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "{\"data\":true}\n" {
		t.Fatalf("unexpected test response: %s", rec.Body)
	}
	select {
	case commands := <-done:
		joined := strings.Join(commands, "\n")
		if !strings.Contains(joined, "LOGIN reply-user stored-test-password") || !strings.Contains(joined, "EXAMINE INBOX") {
			t.Fatalf("did not authenticate with saved credentials: %v", commands)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connection test did not finish")
	}
	var row struct {
		Status, Password string
		Verified         bool
	}
	if err := a.db.Get(&row, `SELECT status,password,verified_at IS NOT NULL AS verified FROM reply_mailboxes WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if row.Status != "active" || !row.Verified || row.Password != "stored-test-password" {
		t.Fatal("saved credentials were lost or mailbox was not verified")
	}
	// A replacement password invalidates verification and is used on the next test.
	port, done = fakeReplyIMAP(t, nil)
	c, _ = replyMailboxConfigContext(1, 0, id, fmt.Sprintf(`{
		"email":"reply@example.com","username":"reply-user","password":"replacement-test-password",
		"imap_host":"127.0.0.1","imap_port":%d,"imap_tls":false,"ai_enabled":true}`, port))
	if err := a.UpdateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&row, `SELECT status,password,verified_at IS NOT NULL AS verified FROM reply_mailboxes WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending" || row.Verified {
		t.Fatal("changed credentials retained verification")
	}
	c, _ = replyMailboxConfigContext(1, 0, id, fmt.Sprintf(`{"id":%d}`, id))
	if err := a.TestReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	select {
	case commands := <-done:
		if !strings.Contains(strings.Join(commands, "\n"), "LOGIN reply-user replacement-test-password") {
			t.Fatal("test did not reuse the replacement password")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("replacement connection test did not finish")
	}
}

func TestReplyMailboxConnectionScopeAndSaveRequired(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	id := createReplyMailboxForTest(t, a, orgPoolAllocationTestHomeOrgID, 995)
	for _, tc := range []struct {
		name                string
		user, org, id, want int
	}{
		{"save first", 1, orgPoolAllocationTestHomeOrgID, 0, http.StatusBadRequest},
		{"another owner", 2, orgPoolAllocationTestHomeOrgID, id, http.StatusNotFound},
		{"another workspace", 1, orgPoolAllocationTestOtherOrgID, id, http.StatusNotFound},
		{"personal workspace", 1, 0, id, http.StatusNotFound},
		{"missing mailbox", 1, orgPoolAllocationTestHomeOrgID, id + 1000, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := replyMailboxConfigContext(tc.user, tc.org, tc.id, fmt.Sprintf(`{"id":%d}`, tc.id))
			if got := replyMailboxDeleteStatus(t, a.TestReplyMailbox(c)); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReplyMailboxConnectionConcurrentChange(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	port, _ := fakeReplyIMAP(t, nil)
	id := createReplyMailboxForTest(t, a, 0, port)
	originalCheck, originalDial := mailboxHostPolicyCheck, mailboxDialFunc
	mailboxHostPolicyCheck = func(net.IP) error { return nil }
	mailboxDialFunc = func(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
		if _, err := a.db.Exec(`UPDATE reply_mailboxes SET password='concurrent-password',status='pending',verified_at=NULL WHERE id=$1`, id); err != nil {
			return nil, err
		}
		return originalDial(ctx, network, addr, timeout)
	}
	t.Cleanup(func() { mailboxHostPolicyCheck, mailboxDialFunc = originalCheck, originalDial })
	c, _ := replyMailboxConfigContext(1, 0, id, fmt.Sprintf(`{"id":%d}`, id))
	if got := replyMailboxDeleteStatus(t, a.TestReplyMailbox(c)); got != http.StatusConflict {
		t.Fatalf("old configuration was verified after a concurrent edit: status=%d", got)
	}
	var verified bool
	if err := a.db.Get(&verified, `SELECT verified_at IS NOT NULL FROM reply_mailboxes WHERE id=$1`, id); err != nil || verified {
		t.Fatalf("concurrent configuration was marked verified: %v", err)
	}
}

func TestReplyMailboxConnectionDoesNotEnableDisabledMailbox(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	allowLoopbackMailboxHosts(t)
	port, _ := fakeReplyIMAP(t, nil)
	id := createReplyMailboxForTest(t, a, 0, port)
	c, _ := replyMailboxConfigContext(1, 0, id, "")
	if err := a.DisableReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	c, _ = replyMailboxConfigContext(1, 0, id, fmt.Sprintf(`{"id":%d}`, id))
	if err := a.TestReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	var row struct {
		Status   string
		Verified bool
	}
	if err := a.db.Get(&row, `SELECT status,verified_at IS NOT NULL AS verified FROM reply_mailboxes WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if row.Status != "disabled" || !row.Verified {
		t.Fatalf("connection test changed lifecycle: status=%s verified=%t", row.Status, row.Verified)
	}
	c, _ = replyMailboxConfigContext(1, 0, id, "")
	if err := a.EnableReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&row.Status, `SELECT status FROM reply_mailboxes WHERE id=$1`, id); err != nil || row.Status != "active" {
		t.Fatalf("explicit enable did not reuse verification: status=%s err=%v", row.Status, err)
	}
}

func TestReplyMailboxAddressOnlyAndAIOptIn(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	c, rec := replyMailboxConfigContext(1, 0, 0, `{"email":"address-only@example.com"}`)
	if err := a.CreateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data models.ReplyMailbox `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	row := response.Data
	if row.Status != "active" || row.AIEnabled || row.HasPassword || row.VerifiedAt != nil || !row.Manageable {
		t.Fatalf("address-only save did not produce a usable address: %+v", row)
	}
	access := models.WorkspaceAccess{UserID: 1}
	campaign := models.Campaign{ReplyMailboxID: null.NewInt(row.ID, true)}
	if err := a.validateCampaignReplyMailbox(access, &campaign); err != nil {
		t.Fatalf("address-only mailbox not selectable: %v", err)
	}
	c, _ = replyMailboxConfigContext(1, 0, row.ID, fmt.Sprintf(`{"id":%d}`, row.ID))
	if got := replyMailboxDeleteStatus(t, a.TestReplyMailbox(c)); got != http.StatusBadRequest {
		t.Fatalf("connection test accepted an address-only mailbox: %d", got)
	}
	// Opting in without an existing or replacement password is rejected.
	c, _ = replyMailboxConfigContext(1, 0, row.ID, `{"email":"address-only@example.com","ai_enabled":true}`)
	if got := replyMailboxDeleteStatus(t, a.UpdateReplyMailbox(c)); got != http.StatusBadRequest {
		t.Fatalf("AI opt-in without receiving credentials was accepted: %d", got)
	}
	c, rec = replyMailboxConfigContext(1, 0, row.ID, `{
		"email":"address-only@example.com","ai_enabled":true,"password":"new-test-password"}`)
	if err := a.UpdateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Status != "pending" || !response.Data.HasPassword || response.Data.VerifiedAt != nil {
		t.Fatal("unverified AI connection did not remain pending")
	}
	if err := a.validateCampaignReplyMailbox(access, &campaign); err == nil {
		t.Fatal("pending AI mailbox was accepted for a campaign")
	}
	// Turning AI off omits receiving fields and preserves the stored password.
	c, rec = replyMailboxConfigContext(1, 0, row.ID, `{"email":"address-only@example.com","ai_enabled":false}`)
	if err := a.UpdateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	var password string
	if err := a.db.Get(&password, `SELECT password FROM reply_mailboxes WHERE id=$1`, row.ID); err != nil || password != "new-test-password" {
		t.Fatalf("turning AI off lost receiving credentials: %v", err)
	}
	if err := a.validateCampaignReplyMailbox(access, &campaign); err != nil {
		t.Fatalf("turning AI off did not restore the reply address: %v", err)
	}
}
