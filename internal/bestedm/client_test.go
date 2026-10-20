package bestedm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client := New()
	client.baseURL = server.URL + "/"
	client.http.Transport = server.Client().Transport
	return client
}
func TestAuthenticatedStatisticsAndQueryEncoding(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "运营账号" || password != "pw:secret" {
			t.Error("missing Basic authentication")
		}
		if r.URL.Path != "/stask.php" || r.URL.Query().Get("ident") != "batch/+&=" || r.URL.Query().Has("date") {
			t.Error("query changed or date was added to full task query")
		}
		fmt.Fprint(w, `<data><task><task_id>12</task_id><total>5</total><actual>4</actual></task><task><task_id>13</task_id><total>3</total></task></data>`)
	})
	rows, err := client.Statistics(context.Background(), Credentials{"运营账号", "pw:secret"}, url.Values{"ident": {"batch/+&="}})
	if err != nil || len(rows) != 2 || rows[0]["actual"] != "4" || rows[1]["actual"] != "" {
		t.Fatalf("field semantics changed: %#v %v", rows, err)
	}
}
func TestProviderErrorsAreNotSuccessfulEmptyLists(t *testing.T) {
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"business error", `<result><status>param_error</status><data>password=secret</data></result>`, "param_error", 200},
		{"wrong password", `secret`, "authentication_failed", 401},
		{"invalid XML", `<data><task><rebate>0</rebtae></task></data>`, "invalid_xml", 200},
		{"HTML login page", `<html><body>Login</body></html>`, "unexpected_response", 200},
		{"empty body", "", "empty_response", 200},
		{"two roots", `<data/><data/>`, "invalid_xml", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			_, err := client.Statistics(context.Background(), Credentials{"user", "secret"}, url.Values{"date": {"2026-10-20"}})
			var pe *Error
			if !errors.As(err, &pe) || pe.Code != tc.code || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe or incorrect error: %v", err)
			}
		})
	}
}
func TestFailuresKeepClassificationAndDoNotModifyCallerQuery(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("out_type") != "xml" {
			t.Error("expected XML classification")
		}
		fmt.Fprint(w, `<fail_recipient_list><recipient><email>a@example.com</email><error_type>1</error_type><error_type_dec>不存在</error_type_dec></recipient><recipient><email>a@example.com</email><error_type>2</error_type></recipient></fail_recipient_list>`)
	})
	query := url.Values{"type": {"-1"}}
	rows, err := client.Failures(context.Background(), Credentials{}, query)
	if err != nil || len(rows) != 2 || rows[0]["error_type_dec"] != "不存在" || query.Has("out_type") {
		t.Fatalf("failure details lost: %v %v", rows, err)
	}
}
func TestLogsHandleXMLBusinessErrorJSONAndInvalidExamples(t *testing.T) {
	for _, tc := range []struct {
		body string
		good bool
	}{
		{`{"lists":[{"code":250,"reason/suggestion":"accepted","send_time":"2026-10-20 08:00:00"}]}`, true},
		{`{"lists":[]}`, true}, {`{"lists":null}`, true},
		// "No rows" arrives as err_msg, not as an empty lists array.
		{`{"err_msg":"未找到相应结果，请确认查询条件重试！"}`, true},
		{`{"err_msg":"该接口今日调用次数已用完"}`, false},
		{`{"status":"rate_limit"}`, false}, {`{}`, false},
		{`{"lists":[{"size":"1" "code":250,}]}`, false}, {`<result><status>param_error</status></result>`, false},
	} {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
		_, err := client.Logs(context.Background(), Credentials{}, nil)
		if (err == nil) != tc.good {
			t.Fatalf("unexpected parse result for %s: %v", tc.body, err)
		}
	}
}

// The provider reports an empty result set with err_msg. The console must show an
// empty table for it, while a genuine provider error still surfaces.
func TestLogsDistinguishEmptyResultsFromProviderErrors(t *testing.T) {
	empty := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"err_msg": "未找到相应结果，请确认查询条件重试！"}`)
	})
	rows, err := empty.Logs(context.Background(), Credentials{}, url.Values{"date": {"2026-10-14"}})
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("empty result treated as an error: %v %v", rows, err)
	}

	quota := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"err_msg": "该接口今日调用次数已用完"}`)
	})
	var pe *Error
	if _, err := quota.Logs(context.Background(), Credentials{}, url.Values{}); !errors.As(err, &pe) {
		t.Fatalf("provider error was swallowed: %v", err)
	}
}

// Both report endpoints answer an empty result set with an XML <result> envelope
// carrying status no_data. That is a successful empty answer, not an error.
func TestStatisticsAndFailuresTreatNoDataAsEmpty(t *testing.T) {
	const noData = `<?xml version="1.0" encoding="UTF-8"?><result><status>no_data</status><data><![CDATA[没有数据]]></data></result>`
	stats := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, noData) })
	rows, err := stats.Statistics(context.Background(), Credentials{}, url.Values{"date": {"2020-01-01"}})
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("statistics no_data treated as an error: %v %v", rows, err)
	}

	failures := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, noData) })
	rows, err = failures.Failures(context.Background(), Credentials{}, url.Values{"type": {"-1"}})
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("failures no_data treated as an error: %v %v", rows, err)
	}

	// A real business failure must still surface.
	broken := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<result><status>user_verify_error</status></result>`)
	})
	var pe *Error
	if _, err := broken.Statistics(context.Background(), Credentials{}, url.Values{}); !errors.As(err, &pe) || pe.Code != "user_verify_error" {
		t.Fatalf("provider error was swallowed: %v", err)
	}
}
func TestDoNotFollowCredentialRedirectsAndBoundResponse(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://example.com/", 302) })
	_, err := client.Customer(context.Background(), Credentials{"user", "secret"})
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "upstream_http_error" {
		t.Fatalf("redirect followed: %v", err)
	}
	client = testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", maxResponse+1)) })
	_, err = client.Customer(context.Background(), Credentials{})
	if !errors.As(err, &pe) || pe.Code != "response_too_large" {
		t.Fatalf("unbounded response: %v", err)
	}
}
