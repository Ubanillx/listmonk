package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/bestedm"
	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
)

// stubBestEDM serves deterministic rows so the report contracts can be tested
// without a provider account or network access.
type stubBestEDM struct {
	failures []bestedm.Row
	err      error
	query    *url.Values
}

func (s stubBestEDM) Customer(context.Context, bestedm.Credentials) (bestedm.Row, error) {
	return bestedm.Row{"id": "1", "username": "stub", "company": "Stub"}, nil
}

func (s stubBestEDM) Service(context.Context, bestedm.Credentials) (bestedm.Row, error) {
	return bestedm.Row{"service_status": "enabled"}, nil
}

func (s stubBestEDM) Statistics(context.Context, bestedm.Credentials, url.Values) ([]bestedm.Row, error) {
	return []bestedm.Row{}, s.err
}

func (s stubBestEDM) Failures(_ context.Context, _ bestedm.Credentials, q url.Values) ([]bestedm.Row, error) {
	if s.query != nil {
		*s.query = q
	}
	return s.failures, s.err
}

func (s stubBestEDM) Logs(context.Context, bestedm.Credentials, url.Values) ([]map[string]any, error) {
	return []map[string]any{}, s.err
}

func stubFailures() []bestedm.Row {
	labels := []string{"邮箱不存在", "邮箱容量不足", "用户拒收", "垃圾内容", "拒绝投递", "收件人黑名单", "投诉地址", "无效地址", "地址格式错误", "连接异常"}
	rows := make([]bestedm.Row, 0, 27)
	for i := 1; i <= 27; i++ {
		typ := (i-1)%10 + 1
		rows = append(rows, bestedm.Row{
			"email":          fmt.Sprintf("recipient%02d@example.com", i),
			"error_type":     strconv.Itoa(typ),
			"error_type_dec": labels[typ-1],
		})
	}
	return rows
}

// providerTestApp wires the stubbed provider client and credentials so no
// request reaches the network.
func providerTestApp(t *testing.T, client bestEDMAPI) *App {
	t.Helper()
	cred := bestedm.Credentials{Username: "stub", Password: "stub"}
	return &App{i18n: exportTestLanguage(t, "en"), bestEDMClient: client, bestEDMCreds: &cred}
}

// providerContext builds a platform-administrator request. Route params are
// given as name/value pairs in Echo's handler registration order.
func providerContext(t *testing.T, target string, pairs ...string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	names := make([]string, 0, len(pairs)/2)
	values := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		names = append(names, pairs[i])
		values = append(values, pairs[i+1])
	}
	recorder := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, target, nil), recorder)
	c.Set(auth.UserHTTPCtxKey, auth.User{UserRoleID: auth.SuperAdminRoleID})
	c.SetParamNames(names...)
	c.SetParamValues(values...)
	return c, recorder
}

func TestDeliveryProviderQueryContracts(t *testing.T) {
	for _, tc := range []struct {
		kind, query string
		valid       bool
	}{
		{"statistics", "date=2026-10-20", true}, {"statistics", "ident=batch-only", true}, {"statistics", "id=123", true},
		{"statistics", "", false}, {"statistics", "date=2026-2-1", false}, {"statistics", "date=2026-10-20&date_end=2026-10-19", false},
		{"statistics", "date=2020-01-01&date_end=2026-01-01", false}, {"statistics", "date=2026-10-20&date=2026-10-21", false},
		{"statistics", "id=0", false}, {"statistics", "id=1&ident=a", false}, {"statistics", "ident=a&username=secret", false},
		{"statistics", "date=2026-10-20&mode=demo", false},
		{"failures", "date=2026-10-20&type=-1", true}, {"failures", "date=2026-10-20&type=8", true},
		{"failures", "date=2026-10-20&type=-1&search=recipient01", true},
		{"failures", "date=2026-10-20&type=-1&group_by=email", true},
		{"failures", "date=2026-10-20&type=-1&group_by=other", false},
		{"failures", "date=2026-10-20&type=-1&group_by=email&group_by=email", false},
		{"statistics", "date=2026-10-20&group_by=email", false},
		{"statistics", "date=2026-10-20&search=recipient01", false},
		{"failures", "date=2026-10-20&type=1", false}, {"failures", "task_ident=batch&type=1", true},
		{"failures", "date=2026-10-20&type=11", false}, {"failures", "date=2026-10-20&type=all", false},
		{"logs", "date=2026-10-20&mail_to=user%2Btag%40example.com", true},
		{"logs", "date=2026-10-20&mail_to=not-an-email", false}, {"logs", "mail_to=a%40example.com", false},
	} {
		t.Run(tc.kind+tc.query, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			err = validateBestEDMQuery(tc.kind, q)
			if (err == nil) != tc.valid {
				t.Fatalf("validation %v, wanted valid=%v", err, tc.valid)
			}
		})
	}
}

func TestDeliveryProviderGroupedFailuresMatchExcel(t *testing.T) {
	var query url.Values
	app := providerTestApp(t, stubBestEDM{query: &query, failures: []bestedm.Row{
		{"email": " User@example.com ", "error_type": "1", "error_type_dec": "Missing"},
		{"email": "user@EXAMPLE.COM", "error_type": "1", "error_type_dec": "Missing"},
		{"email": "user@example.com", "error_type": "10", "error_type_dec": "Connection"},
		{"email": "user@example.com", "error_type": "10", "error_type_dec": "Timeout"},
		{"email": "other@example.com", "error_type": "2", "error_type_dec": "Full"},
	}})
	for _, format := range []string{"", "export"} {
		c, rec := providerContext(t, "/api/delivery-provider/failures?date=2026-10-20&type=-1&group_by=email&search=USER",
			"kind", "failures", "format", format)
		if err := app.QueryDeliveryProvider(c); err != nil {
			t.Fatal(err)
		}
		if query.Has("group_by") || query.Has("search") {
			t.Fatalf("local query options forwarded upstream: %v", query)
		}
		if format == "" {
			var result struct {
				Data struct {
					Results []providerFailureSummary `json:"results"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			rows := result.Data.Results
			if len(rows) != 1 || rows[0].Email != "User@example.com" || rows[0].Count != 4 || len(rows[0].Errors) != 3 || rows[0].Errors[0].Count != 2 {
				t.Fatalf("incorrect grouped result: %s", rec.Body.String())
			}
			continue
		}
		file, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := file.GetRows(file.GetSheetName(0))
		if err != nil || len(rows) != 2 || rows[1][0] != "User@example.com" || rows[1][1] != "4" || rows[1][2] != "1 (2)\n10 (1)\n10 (1)" || !strings.Contains(rows[1][3], "Timeout (1)") {
			t.Fatalf("grouped export differs: %v, %v", rows, err)
		}
		if typ, _ := file.GetCellType(file.GetSheetName(0), "B2"); typ != excelize.CellTypeNumber && typ != excelize.CellTypeUnset {
			t.Fatalf("count is not numeric: %v", typ)
		}
		file.Close()
	}
	c, rec := providerContext(t, "/api/delivery-provider/failures?date=2026-10-20&type=-1", "kind", "failures")
	if err := app.QueryDeliveryProvider(c); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Data struct {
			Results []bestedm.Row `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Data.Results) != 5 {
		t.Fatalf("raw API must preserve duplicate records: %s (%v)", rec.Body.String(), err)
	}
}

func TestDeliveryProviderFailureGroupingPreservesCountsAndOrder(t *testing.T) {
	if result := summarizeProviderFailures(nil); result == nil || len(result) != 0 {
		t.Fatalf("empty results must be an array: %#v", result)
	}
	rows := []bestedm.Row{
		{"email": "first@example.com", "error_type": "1"},
		{"email": "second@example.com", "error_type": "2"},
		{"email": "FIRST@example.com", "error_type": "1"},
		{"error_type": "1"}, {"error_type": "2"},
	}
	got := summarizeProviderFailures(rows)
	if len(got) != 4 || got[0].Email != "first@example.com" || got[0].Count != 2 || got[1].Email != "second@example.com" {
		t.Fatalf("unexpected groups: %#v", got)
	}
	total := 0
	for _, group := range got {
		causeTotal := 0
		for _, cause := range group.Errors {
			causeTotal += cause.Count
		}
		if causeTotal != group.Count {
			t.Fatalf("lost error classifications: %#v", group)
		}
		total += group.Count
	}
	if total != len(rows) {
		t.Fatalf("lost occurrences: %d != %d", total, len(rows))
	}
}

func TestDeliveryProviderFailureSearchMatchesExcel(t *testing.T) {
	app := providerTestApp(t, stubBestEDM{failures: stubFailures()})
	for _, format := range []string{"", "export"} {
		c, rec := providerContext(t,
			"/api/delivery-provider/failures?date=2026-10-20&type=-1&search=%20RECIPIENT01%20",
			"kind", "failures", "format", format)
		if err := app.QueryDeliveryProvider(c); err != nil {
			t.Fatal(err)
		}
		if format == "" {
			var result struct {
				Data struct {
					Results []map[string]string `json:"results"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Data.Results) != 1 || result.Data.Results[0]["email"] != "recipient01@example.com" {
				t.Fatalf("search returned other addresses: %s", rec.Body.String())
			}
			continue
		}
		file, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := file.GetRows(file.GetSheetName(0))
		file.Close()
		if err != nil || len(rows) != 2 || rows[1][0] != "recipient01@example.com" {
			t.Fatalf("export differs from searched results: %v %v", rows, err)
		}
	}
}

func TestDeliveryProviderRejectsNonAdmins(t *testing.T) {
	app := providerTestApp(t, stubBestEDM{})
	for _, route := range []struct {
		name   string
		handle func(echo.Context) error
		params []string
	}{
		{"statistics", app.QueryDeliveryProvider, []string{"kind", "statistics"}},
		{"account", app.GetDeliveryProviderAccount, nil},
		{"config", app.GetDeliveryProviderConfig, nil},
	} {
		t.Run(route.name, func(t *testing.T) {
			c, _ := providerContext(t, "/api/delivery-provider/"+route.name, route.params...)
			c.Set(auth.UserHTTPCtxKey, auth.User{UserRoleID: 5})
			if err := route.handle(c); err == nil {
				t.Fatal("non-platform user could read platform data")
			}
		})
	}
}

// The console has a single data source. A provider failure must surface as an
// error rather than as empty or zeroed reports.
func TestDeliveryProviderSurfacesProviderFailure(t *testing.T) {
	app := providerTestApp(t, stubBestEDM{err: &bestedm.Error{Code: "invalid_xml", Message: "服务商返回的 XML 无法解析"}})
	c, rec := providerContext(t, "/api/delivery-provider/statistics?date=2026-10-20", "kind", "statistics")
	if err := app.QueryDeliveryProvider(c); err == nil {
		t.Fatal("provider failure was swallowed")
	}
	if strings.Contains(rec.Body.String(), "results") {
		t.Fatalf("failure returned report data: %s", rec.Body.String())
	}
}

func TestProviderStatusNeverReturnsPassword(t *testing.T) {
	result := bestEDMStatus(bestEDMConfig{Username: "user", Password: "do-not-return", Verified: true})
	if _, ok := result["password"]; ok || result["has_password"] != true {
		t.Fatalf("password leaked or status lost: %v", result)
	}
}

func TestGenericSettingsCannotWriteProviderCredentials(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPut, "/api/settings/"+bestEDMSettingsKey, strings.NewReader(`{}`)), httptest.NewRecorder())
	c.SetParamNames("key")
	c.SetParamValues(bestEDMSettingsKey)
	err := (&App{}).UpdateSettingsByKey(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusForbidden {
		t.Fatalf("generic write permitted: %v", err)
	}
}
