package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/paginator"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

func newSendErrorReportFixture(t *testing.T) (*App, *store, int, []int) {
	t.Helper()
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.pg = paginator.New(paginator.Opt{DefaultPerPage: 20, MaxPerPage: 50, PageParam: "page", PerPageParam: "per_page", AllowAll: true})
	s := newManagerStore(nil, a.core, nil, a.db)
	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,owner_user_id,organization_id,visibility,send_errors)
		VALUES(gen_random_uuid(),'Error report','Subject','sender@example.test','Body','email','running',3,101,'organization',2) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	ids := []int{}
	for i := 0; i < 23; i++ {
		var id int
		if err := a.db.Get(&id, `INSERT INTO customers(uuid,email,name,customer_code,owner_user_id,organization_id) VALUES(gen_random_uuid(),$1,$2,$3,3,101) RETURNING id`, fmt.Sprintf("recipient%d@example.test", i), fmt.Sprintf("Customer %d", i), fmt.Sprintf("C-%02d", i)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		f := models.CampaignSendFailure{CampaignID: campaignID, CampaignOwnerUserID: null.Int{Int: 3, Valid: true}, CampaignOrganizationID: null.Int{Int: 101, Valid: true},
			RecipientType: "private", RecipientID: int64(id), RecipientOrganizationID: 101, CustomerCode: fmt.Sprintf("C-%02d", i), Name: fmt.Sprintf("Customer %d", i), Email: fmt.Sprintf("recipient%d@example.test", i), Stage: "send", Category: "network", Error: "connection refused"}
		if i == 0 {
			f.Name = "=SUM(1,1)"
			f.Category = "smtp_rejected"
			f.SMTPCode = 550
			f.Error = "550 5.1.1 rejected recipient0@example.test"
		}
		if err := s.RecordCampaignSendFailure(f); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := s.RecordCampaignSendFailure(f); err != nil {
				t.Fatal(err)
			}
		}
	}
	f := models.CampaignSendFailure{CampaignID: campaignID, CampaignOwnerUserID: null.Int{Int: 3, Valid: true}, CampaignOrganizationID: null.Int{Int: 101, Valid: true},
		RecipientType: "pool", RecipientID: 41, RecipientOrganizationID: 101, CustomerCode: "POOL-41", Name: "Pool customer", Email: "pool-secret@example.test", Stage: "send", Category: "smtp_auth", SMTPCode: 535, Error: "535 account rejected pool-secret@example.test"}
	for i := 0; i < 2; i++ {
		if err := s.RecordCampaignSendFailure(f); err != nil {
			t.Fatal(err)
		}
	}
	return a, s, campaignID, ids
}

func sendErrorReportUser(perms ...string) auth.User {
	base := []string{auth.PermCampaignsGetAnalytics, auth.PermCampaignsRecipients, auth.PermCustomersGet, auth.PermPoolsGet}
	u := permissionTestUser(append(base, perms...)...)
	u.ID = 3
	return u
}

func sendErrorReportContext(user auth.User, id int, query string, export bool) (echo.Context, *httptest.ResponseRecorder) {
	path := "/api/campaigns/:id/send-errors"
	if export {
		path += "/export"
	}
	req := httptest.NewRequest(http.MethodGet, strings.Replace(path, ":id", strconv.Itoa(id), 1)+query, nil)
	req.Header.Set(workspaceHeader, "101")
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetPath(path)
	c.SetParamNames("id")
	c.SetParamValues(strconv.Itoa(id))
	c.Set("id", id)
	c.Set(auth.UserHTTPCtxKey, user)
	return c, rec
}

func readSendErrorReport(t *testing.T, a *App, u auth.User, id int, q string) models.CampaignSendErrorReport {
	t.Helper()
	c, rec := sendErrorReportContext(u, id, q, false)
	if err := a.GetCampaignSendErrors(c); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data models.CampaignSendErrorReport `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}

// Inspect workbook cell XML without depending on Excel's display formatting.
func readSendErrorWorkbook(t *testing.T, data []byte) ([][]string, [][]string) {
	t.Helper()
	book, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var details, summary [][]string
	for _, f := range book.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		x, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		var sheet struct {
			Rows []struct {
				Cells []struct {
					Value  string `xml:"v"`
					Inline struct {
						Text string `xml:"t"`
					} `xml:"is"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err := xml.Unmarshal(x, &sheet); err != nil {
			t.Fatal(err)
		}
		for _, row := range sheet.Rows {
			cells := []string{}
			for _, cell := range row.Cells {
				value := cell.Value
				if cell.Inline.Text != "" {
					value = cell.Inline.Text
				}
				cells = append(cells, value)
			}
			if len(cells) == 15 {
				details = append(details, cells)
			} else if len(cells) == 2 {
				summary = append(summary, cells)
			}
		}
	}
	return details, summary
}

func TestCampaignSendErrorReportGroupsFiltersAndExportsAllPages(t *testing.T) {
	a, s, id, ids := newSendErrorReportFixture(t)
	u := sendErrorReportUser(auth.PermCustomersSensitiveRead, auth.PermCustomersExport, auth.PermPoolsExport)
	r := readSendErrorReport(t, a, u, id, "")
	if r.Total != 24 || len(r.Results) != 20 || r.RecordedErrors != 26 || r.HistoricalErrors != 2 || !r.CanExport {
		t.Fatalf("wrong grouped totals: %+v", r)
	}
	for _, row := range r.Results {
		if row.RecipientType == "pool" && (row.Error != "" || strings.Contains(row.Email, "pool-secret")) {
			t.Fatalf("pool identity leaked: %+v", row)
		}
	}
	r = readSendErrorReport(t, a, u, id, "?category=smtp_rejected&per_page=1")
	if r.Total != 1 || r.RecordedErrors != 2 || len(r.Results) != 1 || r.Results[0].Count != 2 || r.Results[0].SMTPCode != 550 || r.Results[0].CustomerCode != "C-00" {
		t.Fatalf("incorrect reason grouping: %+v", r)
	}
	r = readSendErrorReport(t, a, u, id, "?search=C-01")
	if r.Total != 1 || r.Results[0].CustomerCode != "C-01" {
		t.Fatalf("customer filter failed: %+v", r)
	}
	r = readSendErrorReport(t, a, u, id, "?page=9")
	if r.Total != 24 || len(r.Results) != 0 {
		t.Fatalf("empty page lost totals: %+v", r)
	}
	c, rec := sendErrorReportContext(u, id, "?per_page=1", true)
	if err := a.ExportCampaignSendErrors(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Header().Get(echo.HeaderContentDisposition), ".xlsx") {
		t.Fatal("export is not an Excel workbook")
	}
	rows, summary := readSendErrorWorkbook(t, rec.Body.Bytes())
	if len(rows) != 25 {
		t.Fatalf("export stopped at page boundary: %d", len(rows))
	}
	for _, row := range rows[1:] {
		if row[0] == "C-00" && (row[1] != "=SUM(1,1)" || row[6] != "550 5.1.1 rejected recipient0@example.test" || row[7] != "2") {
			t.Fatalf("Excel lost customer, error or literal text: %v", row)
		}
		if row[0] == "POOL-41" && strings.Contains(row[2]+row[6], "pool-secret@example.test") {
			t.Fatal("Excel leaked raw pool identity")
		}
	}
	if len(summary) != 5 {
		t.Fatalf("reason summary missing: %+v", summary)
	}
	// Failed history and counts survive a later successful retry's state change.
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES($1,$2,'sent')`, id, ids[0]); err != nil {
		t.Fatal(err)
	}
	r = readSendErrorReport(t, a, u, id, "?category=smtp_rejected")
	if r.RecordedErrors != 2 {
		t.Fatal("successful retry erased error history")
	}
	// Rejecting a diagnostic insert must also roll back the cumulative count.
	bad := models.CampaignSendFailure{CampaignID: id, RecipientID: 1, RecipientType: "invalid", Stage: "send"}
	if err := s.RecordCampaignSendFailure(bad); err == nil {
		t.Fatal("invalid diagnostic accepted")
	}
	var count int
	if err := a.db.Get(&count, `SELECT send_errors FROM campaigns WHERE id=$1`, id); err != nil || count != 28 {
		t.Fatalf("failed detail write changed count: %d %v", count, err)
	}
}

func TestCampaignSendErrorReportEnforcesOwnershipMaskingAndExportGrants(t *testing.T) {
	a, _, id, ids := newSendErrorReportFixture(t)
	u := sendErrorReportUser()
	r := readSendErrorReport(t, a, u, id, "?category=smtp_rejected")
	if r.CanExport || len(r.Results) != 1 || r.Results[0].Error != "" || strings.Contains(r.Results[0].Email, "recipient0@example.test") {
		t.Fatalf("masked view exposed diagnostics: %+v", r)
	}
	if r := readSendErrorReport(t, a, u, id, "?search=recipient0@example.test"); r.Total != 0 {
		t.Fatal("search exposed masked recipient")
	}
	c, _ := sendErrorReportContext(u, id, "", true)
	requireOrgPoolAllocationRejection(t, a.ExportCampaignSendErrors(c), http.StatusForbidden)
	u = sendErrorReportUser(auth.PermCustomersExport)
	c, _ = sendErrorReportContext(u, id, "", true)
	requireOrgPoolAllocationRejection(t, a.ExportCampaignSendErrors(c), http.StatusForbidden)
	c, _ = sendErrorReportContext(u, id, "?category=smtp_rejected", true)
	if err := a.ExportCampaignSendErrors(c); err != nil {
		t.Fatalf("private-only filtered export rejected: %v", err)
	}
	// An organization manager may inspect a shared campaign but not its identities.
	u.ID = 2
	c, _ = sendErrorReportContext(u, id, "", false)
	if err := a.GetCampaignSendErrors(c); err == nil {
		t.Fatal("manager inspected another owner's failure identities")
	}
	u = sendErrorReportUser(auth.PermCustomersSensitiveRead)
	c, _ = sendErrorReportContext(u, id, "", false)
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal, WorkspaceOrganizationID: null.Int{Int: 101, Valid: true}, Scopes: pq.StringArray{apiKeyScopeCampaignsRecipients}})
	requireOrgPoolAllocationRejection(t, a.GetCampaignSendErrors(c), http.StatusForbidden)
	if _, err := a.db.Exec(`UPDATE customers SET owner_user_id=2 WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if r := readSendErrorReport(t, a, u, id, "?category=smtp_rejected"); r.Total != 0 {
		t.Fatal("moved customer's historical identity remained visible")
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET owner_user_id=2 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	u.ID = 2
	if r := readSendErrorReport(t, a, u, id, ""); r.Total != 0 {
		t.Fatal("campaign transfer exposed previous owner's snapshots")
	}
}
