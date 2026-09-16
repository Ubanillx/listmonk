package replyai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServer(t *testing.T, content string, status int, assert func(*http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assert != nil {
			assert(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chatReply(t *testing.T, content string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{
			"message": map[string]any{"role": "assistant", "content": content},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(Options{
		Enabled:       true,
		BaseURL:       srv.URL,
		APIKey:        "secret-key",
		Model:         "test-model",
		Timeout:       "5s",
		MinConfidence: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClassifyAcceptsBoundedDecisions(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantIntent string
		wantReason string
	}{
		{"explicit unsubscribe", `{"intent":"unsubscribe","confidence":0.99,"reason_code":"explicit_unsubscribe"}`, IntentUnsubscribe, ReasonExplicitUnsubscribe},
		{"explicit complaint", `{"intent":"complaint","confidence":0.98,"reason_code":"explicit_spam_or_abuse"}`, IntentComplaint, ReasonExplicitComplaint},
		{"product complaint", `{"intent":"product_complaint","confidence":0.98,"reason_code":"product_or_service_complaint"}`, IntentProductComplaint, ReasonProductServiceComplaint},
		{"other with none reason", `{"intent":"other","confidence":0.5,"reason_code":"none"}`, IntentOther, ReasonNone},
		{"code fence wrapped json", "```json\n{\"intent\":\"unsubscribe\",\"confidence\":0.95,\"reason_code\":\"explicit_unsubscribe\"}\n```", IntentUnsubscribe, ReasonExplicitUnsubscribe},
		{"prose around json", `Sure. {"intent":"complaint","confidence":0.97,"reason_code":"explicit_spam_or_abuse"} here.`, IntentComplaint, ReasonExplicitComplaint},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := testServer(t, chatReply(t, c.content), http.StatusOK, func(r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
					t.Fatalf("authorization = %q", got)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["model"] != "test-model" {
					t.Fatalf("model = %v", body["model"])
				}
				messages := body["messages"].([]any)
				if len(messages) != 2 {
					t.Fatalf("expected system+user messages, got %d", len(messages))
				}
			})
			decision, err := mustClient(t, srv).Classify(context.Background(), "please unsubscribe me")
			if err != nil {
				t.Fatal(err)
			}
			if decision.Intent != c.wantIntent || decision.ReasonCode != c.wantReason {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestClassifyRejectsAnythingBeyondTheSchema(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"unknown intent", `{"intent":"angry","confidence":0.9,"reason_code":"none"}`},
		{"unsubscribe without explicit reason", `{"intent":"unsubscribe","confidence":0.9,"reason_code":"none"}`},
		{"complaint with wrong reason", `{"intent":"complaint","confidence":0.9,"reason_code":"explicit_unsubscribe"}`},
		{"product complaint with wrong reason", `{"intent":"product_complaint","confidence":0.9,"reason_code":"none"}`},
		{"other with actionable reason", `{"intent":"other","confidence":0.9,"reason_code":"explicit_unsubscribe"}`},
		{"confidence out of range", `{"intent":"unsubscribe","confidence":2,"reason_code":"explicit_unsubscribe"}`},
		{"negative confidence", `{"intent":"other","confidence":-1,"reason_code":"none"}`},
		{"not json", "please block this sender"},
		{"empty choices", ``},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := testServer(t, chatReply(t, c.content), http.StatusOK, nil)
			if _, err := mustClient(t, srv).Classify(context.Background(), "body"); err == nil {
				t.Fatalf("expected error for %q", c.content)
			}
		})
	}
}

func TestClassifierPromptSeparatesProductComplaintsFromSpamComplaints(t *testing.T) {
	for _, want := range []string{"product_complaint", "产品质量有问题", "must never be treated as spam/abuse"} {
		if !strings.Contains(classifierPrompt, want) {
			t.Fatalf("classifier prompt does not contain %q", want)
		}
	}
}

func TestClassifyErrorPaths(t *testing.T) {
	t.Run("http error status", func(t *testing.T) {
		srv := testServer(t, `{"error":"boom"}`, http.StatusInternalServerError, nil)
		if _, err := mustClient(t, srv).Classify(context.Background(), "body"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("disabled client", func(t *testing.T) {
		c, err := New(Options{Enabled: false})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Classify(context.Background(), "body"); err != ErrDisabled {
			t.Fatalf("expected ErrDisabled, got %v", err)
		}
	})
}

func TestNewValidatesEnabledConfiguration(t *testing.T) {
	cases := []Options{
		{Enabled: true, BaseURL: "https://api.example.com/v1", APIKey: "k", Model: "m", MinConfidence: 0.9},
		{Enabled: true, BaseURL: "not-a-url", APIKey: "k", Model: "m", MinConfidence: 0.9},
		{Enabled: true, BaseURL: "https://api.example.com", APIKey: "", Model: "m", MinConfidence: 0.9},
		{Enabled: true, BaseURL: "https://api.example.com", APIKey: "k", Model: "", MinConfidence: 0.9},
		{Enabled: true, BaseURL: "https://api.example.com", APIKey: "k", Model: "m", MinConfidence: 0},
		{Enabled: true, BaseURL: "https://api.example.com", APIKey: "k", Model: "m", MinConfidence: 1.5},
		{Enabled: true, BaseURL: "https://api.example.com", APIKey: "k", Model: "m", Timeout: "not-a-duration", MinConfidence: 0.9},
		{Enabled: true, BaseURL: "https://api.example.com", APIKey: "k", Model: "m", Timeout: "500ms", MinConfidence: 0.9},
	}
	for i, opt := range cases {
		valid := opt.BaseURL == "https://api.example.com/v1" && opt.APIKey != "" && opt.Model != "" &&
			opt.MinConfidence > 0 && opt.MinConfidence <= 1 && opt.Timeout == ""
		_, err := New(opt)
		if valid && err != nil {
			t.Fatalf("case %d: expected valid config, got %v", i, err)
		}
		if !valid && err == nil {
			t.Fatalf("case %d: expected invalid config error", i)
		}
	}

	// Endpoint path suffix is appended for plain roots.
	c, err := New(Options{Enabled: true, BaseURL: "https://api.example.com", APIKey: "k", Model: "m", MinConfidence: 0.9})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(c.endpoint, "/chat/completions") {
		t.Fatalf("endpoint = %q", c.endpoint)
	}
}
