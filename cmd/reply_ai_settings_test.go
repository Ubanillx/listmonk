package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/replyai"
	"github.com/labstack/echo/v4"
)

// probeTestApp builds an App with a real translator and a discarding logger, so
// handler tests also guard the i18n keys the settings page relies on.
func probeTestApp(t *testing.T) *App {
	t.Helper()
	langB, err := os.ReadFile("../i18n/en.json")
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	translator, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}
	return &App{i18n: translator, log: log.New(io.Discard, "", 0)}
}

func probeContext(t *testing.T, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	return e.NewContext(r, rec), rec
}

// probeGateway is a small OpenAI-compatible gateway for handler tests.
func probeGateway(t *testing.T, models []string, chatContent string, chatStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			entries := make([]map[string]any, 0, len(models))
			for _, id := range models {
				entries = append(entries, map[string]any{"id": id, "owned_by": "gateway"})
			}
			body, _ := json.Marshal(map[string]any{"object": "list", "data": entries})
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(chatStatus)
		_, _ = w.Write([]byte(chatContent))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNormalizeReplyAIProbe(t *testing.T) {
	req := replyAIProbeRequest{
		BaseURL: "  https://gateway.example.com/v1  ",
		Model:   " gpt-4o-mini ",
		Sample:  "  please stop  ",
	}
	if err := normalizeReplyAIProbe(&req); err != nil {
		t.Fatal(err)
	}
	if req.BaseURL != "https://gateway.example.com/v1" || req.Model != "gpt-4o-mini" || req.Sample != "please stop" {
		t.Fatalf("req = %+v", req)
	}
	if req.Timeout != "15s" {
		t.Fatalf("timeout = %q, want the shared 15s default", req.Timeout)
	}

	for _, intent := range []string{"", "unsubscribe", "complaint", "other"} {
		req := replyAIProbeRequest{BaseURL: "https://gateway.example.com/v1", ExpectedIntent: strings.ToUpper(intent)}
		if err := normalizeReplyAIProbe(&req); err != nil {
			t.Fatalf("intent %q: %v", intent, err)
		}
		if req.ExpectedIntent != intent {
			t.Fatalf("intent = %q, want %q", req.ExpectedIntent, intent)
		}
	}

	if err := normalizeReplyAIProbe(&replyAIProbeRequest{BaseURL: "https://gateway.example.com/v1", ExpectedIntent: "angry"}); err != errReplyAIExpectedIntent {
		t.Fatalf("err = %v", err)
	}
	if err := normalizeReplyAIProbe(&replyAIProbeRequest{BaseURL: "   "}); err != errReplyAIBaseURLMissing {
		t.Fatalf("err = %v", err)
	}
}

func TestListReplyAIModelsHTTPEnvelope(t *testing.T) {
	gateway := probeGateway(t, []string{"gpt-4o-mini", "text-embedding-3-small"}, "", http.StatusOK)
	app := probeTestApp(t)
	c, rec := probeContext(t, "/api/settings/reply-ai/models",
		`{"base_url":"`+gateway.URL+`/v1","api_key":"sk-live-key","timeout":"5s"}`)

	if err := app.ListReplyAIModels(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var response struct {
		Data replyai.ModelList `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	if len(response.Data.Models) != 2 || response.Data.Models[0].ID != "gpt-4o-mini" {
		t.Fatalf("models = %+v", response.Data.Models)
	}
	// The key must never be echoed back to the browser.
	if strings.Contains(rec.Body.String(), "sk-live-key") {
		t.Fatalf("response leaked the API key: %s", rec.Body)
	}
}

func TestListReplyAIModelsRejectsUnusableInput(t *testing.T) {
	app := probeTestApp(t)
	// A blank or masked key resolves against stored settings, which needs a
	// database; that path is covered by the dev gateway-probe verification.
	cases := []struct {
		name string
		body string
		key  string
		want int
	}{
		{"unparseable body", `{`, "globals.messages.invalidData", http.StatusBadRequest},
		{"missing base url", `{"api_key":"sk-live-key"}`, "settings.inboundReplies.errorBaseURL", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := probeContext(t, "/api/settings/reply-ai/models", tc.body)
			err := app.ListReplyAIModels(c)
			httpErr, ok := err.(*echo.HTTPError)
			if !ok || httpErr.Code != tc.want {
				t.Fatalf("err = %v, want %d", err, tc.want)
			}
			// An untranslated key comes back verbatim, so a translated message
			// proves the key exists in i18n/en.json.
			msg, _ := httpErr.Message.(string)
			if msg == "" || msg == tc.key {
				t.Fatalf("message %q is not translated", msg)
			}
		})
	}
}

func TestListReplyAIModelsMapsGatewayStatuses(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token sk-live-key"}}`))
	}))
	defer gateway.Close()

	app := probeTestApp(t)
	c, _ := probeContext(t, "/api/settings/reply-ai/models",
		`{"base_url":"`+gateway.URL+`/v1","api_key":"sk-live-key"}`)
	err := app.ListReplyAIModels(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusBadRequest {
		t.Fatalf("err = %v", err)
	}
	msg, _ := httpErr.Message.(string)
	if !strings.Contains(msg, "401") || strings.Contains(msg, "sk-live-key") {
		t.Fatalf("message = %q", msg)
	}
}

func TestTestReplyAIModelHTTPEnvelope(t *testing.T) {
	gateway := probeGateway(t, []string{"gpt-4o-mini"},
		`{"choices":[{"message":{"content":"{\"intent\":\"unsubscribe\",\"confidence\":0.99,\"reason_code\":\"explicit_unsubscribe\"}"}}]}`,
		http.StatusOK)
	app := probeTestApp(t)
	c, rec := probeContext(t, "/api/settings/reply-ai/test",
		`{"base_url":"`+gateway.URL+`/v1","api_key":"sk-live-key","model":"gpt-4o-mini","timeout":"5s"}`)

	if err := app.TestReplyAIModel(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var response struct {
		Data replyai.TestResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	if response.Data.Status != replyai.StatusSuccess {
		t.Fatalf("result = %+v", response.Data)
	}
	if response.Data.Decision == nil || response.Data.Decision.Intent != replyai.IntentUnsubscribe {
		t.Fatalf("decision = %+v", response.Data.Decision)
	}
	if len(response.Data.Steps) != 4 {
		t.Fatalf("steps = %+v", response.Data.Steps)
	}
}

// A gateway failure is reported as a step, not as an HTTP error, so the settings
// page can always render what went wrong.
func TestTestReplyAIModelReportsFailuresAsSteps(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token"}}`))
	}))
	defer gateway.Close()

	app := probeTestApp(t)
	c, rec := probeContext(t, "/api/settings/reply-ai/test",
		`{"base_url":"`+gateway.URL+`/v1","api_key":"sk-live-key","model":"gpt-4o-mini","timeout":"5s"}`)
	if err := app.TestReplyAIModel(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var response struct {
		Data replyai.TestResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Status != replyai.StatusFailed {
		t.Fatalf("status = %q", response.Data.Status)
	}
	completion := response.Data.Steps[len(response.Data.Steps)-1]
	if completion.Name != replyai.StepCompletion || completion.Reason != replyai.ReasonAuthRejected {
		t.Fatalf("completion = %+v", completion)
	}
}

func TestTestReplyAIModelRequiresAModel(t *testing.T) {
	app := probeTestApp(t)
	c, _ := probeContext(t, "/api/settings/reply-ai/test",
		`{"base_url":"https://gateway.example.com/v1","api_key":"sk-live-key"}`)
	err := app.TestReplyAIModel(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusBadRequest {
		t.Fatalf("err = %v", err)
	}
	if msg, _ := httpErr.Message.(string); msg == "" || msg == "settings.inboundReplies.errorModelRequired" {
		t.Fatalf("message %q is not translated", msg)
	}
}

func TestLastStepDetail(t *testing.T) {
	if got := lastStepDetail(replyai.TestResult{}); got != "" {
		t.Fatalf("empty result = %q", got)
	}
	result := replyai.TestResult{Steps: []replyai.Step{
		{Name: replyai.StepConfig, Status: replyai.StatusSuccess, Detail: "config ok"},
		{Name: replyai.StepGateway, Status: replyai.StatusFailed, Reason: replyai.ReasonAuthRejected, Detail: "HTTP 401"},
		{Name: replyai.StepCompletion, Status: replyai.StatusSuccess, Detail: "intent=other"},
	}}
	if got := lastStepDetail(result); got != "auth_rejected HTTP 401" {
		t.Fatalf("detail = %q", got)
	}
}
