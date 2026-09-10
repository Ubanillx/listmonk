package replyai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// gatewayStub is a minimal OpenAI-compatible aggregator gateway used to test
// probing without any network dependency.
type gatewayStub struct {
	// modelsStatus/modelsPath control model discovery. A path of "" serves the
	// catalogue from every path; otherwise only that path answers 200.
	modelsStatus int
	modelsPath   string
	models       []map[string]any
	modelsBody   string

	chatStatus  int
	chatPath    string
	chatContent string
	chatRaw     string

	mu       sync.Mutex
	paths    []string
	chatReqs []map[string]any
	authSeen []string
}

func newGateway(t *testing.T, stub *gatewayStub) *httptest.Server {
	t.Helper()
	if stub.modelsStatus == 0 {
		stub.modelsStatus = http.StatusOK
	}
	if stub.chatStatus == 0 {
		stub.chatStatus = http.StatusOK
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.paths = append(stub.paths, r.URL.Path)
		stub.authSeen = append(stub.authSeen, r.Header.Get("Authorization"))
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			if stub.modelsPath != "" && r.URL.Path != stub.modelsPath {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
				return
			}
			w.WriteHeader(stub.modelsStatus)
			if stub.modelsBody != "" {
				_, _ = w.Write([]byte(stub.modelsBody))
				return
			}
			if stub.modelsStatus != http.StatusOK {
				_, _ = w.Write([]byte(`{"error":{"message":"denied"}}`))
				return
			}
			body, _ := json.Marshal(map[string]any{"object": "list", "data": stub.models})
			_, _ = w.Write(body)
		default:
			if stub.chatPath != "" && r.URL.Path != stub.chatPath {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"unknown endpoint"}}`))
				return
			}
			var decoded map[string]any
			_ = json.NewDecoder(r.Body).Decode(&decoded)
			stub.mu.Lock()
			stub.chatReqs = append(stub.chatReqs, decoded)
			stub.mu.Unlock()

			w.WriteHeader(stub.chatStatus)
			if stub.chatRaw != "" {
				_, _ = w.Write([]byte(stub.chatRaw))
				return
			}
			_, _ = w.Write([]byte(chatReply(t, stub.chatContent)))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *gatewayStub) requestPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func (s *gatewayStub) chatRequest(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.chatReqs) == 0 {
		t.Fatal("no chat completion request was sent")
	}
	return s.chatReqs[len(s.chatReqs)-1]
}

func catalogue(ids ...string) []map[string]any {
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"id": id, "object": "model", "owned_by": "gateway"})
	}
	return out
}

func TestListModelsFallsBackToV1Mount(t *testing.T) {
	stub := &gatewayStub{
		modelsPath: "/v1/models",
		models:     catalogue("zz-last-chat", "text-embedding-3-small", "gpt-4o-mini", "gpt-4o-mini", "Whisper-1"),
	}
	srv := newGateway(t, stub)

	list, err := ListModels(context.Background(), ProbeOptions{
		BaseURL: srv.URL, APIKey: "sk-test", Timeout: "5s",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The /v1 mount answered, so the operator is told which root works.
	if list.APIRoot != srv.URL+"/v1" || list.SuggestedBaseURL != srv.URL+"/v1" {
		t.Fatalf("api root = %q, suggested = %q", list.APIRoot, list.SuggestedBaseURL)
	}
	// The key travels only in the Authorization header.
	stub.mu.Lock()
	for _, auth := range stub.authSeen {
		if auth != "Bearer sk-test" {
			t.Fatalf("authorization = %q", auth)
		}
	}
	stub.mu.Unlock()

	// Chat-capable models come first, duplicates are dropped, embeddings last.
	ids := make([]string, 0, len(list.Models))
	for _, m := range list.Models {
		ids = append(ids, m.ID)
	}
	want := []string{"gpt-4o-mini", "zz-last-chat", "text-embedding-3-small", "Whisper-1"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("models = %v, want %v", ids, want)
	}
	if list.Models[0].OwnedBy != "gateway" {
		t.Fatalf("owned_by = %q", list.Models[0].OwnedBy)
	}
	for _, m := range list.Models {
		if m.ID == "text-embedding-3-small" && m.ChatHint {
			t.Fatal("embeddings must not be hinted as chat models")
		}
		if m.ID == "gpt-4o-mini" && !m.ChatHint {
			t.Fatal("chat models must carry the chat hint")
		}
	}
	if list.ChatCandidates != 2 {
		t.Fatalf("chat candidates = %d, want 2", list.ChatCandidates)
	}
}

func TestListModelsReportsGatewayFailures(t *testing.T) {
	t.Run("rejected credentials stop the fallback", func(t *testing.T) {
		stub := &gatewayStub{modelsStatus: http.StatusUnauthorized}
		srv := newGateway(t, stub)

		_, err := ListModels(context.Background(), ProbeOptions{BaseURL: srv.URL, APIKey: "sk-bad", Timeout: "5s"})
		var gw *GatewayError
		if !errors.As(err, &gw) || gw.StatusCode != http.StatusUnauthorized {
			t.Fatalf("err = %v", err)
		}
		if paths := stub.requestPaths(); len(paths) != 1 {
			t.Fatalf("a rejected key must not be retried on the /v1 mount, paths = %v", paths)
		}
	})

	t.Run("no models endpoint", func(t *testing.T) {
		stub := &gatewayStub{modelsPath: "/nowhere/models"}
		srv := newGateway(t, stub)

		_, err := ListModels(context.Background(), ProbeOptions{BaseURL: srv.URL, APIKey: "sk-test", Timeout: "5s"})
		var gw *GatewayError
		if !errors.As(err, &gw) || gw.StatusCode != http.StatusNotFound {
			t.Fatalf("err = %v", err)
		}
		if paths := stub.requestPaths(); len(paths) != 2 {
			t.Fatalf("both candidate roots must be tried, paths = %v", paths)
		}
	})

	t.Run("unreachable gateway", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close()
		if _, err := ListModels(context.Background(), ProbeOptions{BaseURL: srv.URL, APIKey: "sk-test", Timeout: "2s"}); err == nil {
			t.Fatal("expected an error for an unreachable gateway")
		}
	})

	t.Run("invalid base url", func(t *testing.T) {
		for _, base := range []string{"", "not-a-url", "ftp://gateway.example.com"} {
			if _, err := ListModels(context.Background(), ProbeOptions{BaseURL: base, APIKey: "sk-test"}); err == nil {
				t.Fatalf("expected an error for %q", base)
			}
		}
	})

	t.Run("api key never appears in the error", func(t *testing.T) {
		stub := &gatewayStub{modelsStatus: http.StatusInternalServerError, modelsBody: `{"error":{"message":"token sk-leaky is invalid"}}`}
		srv := newGateway(t, stub)
		_, err := ListModels(context.Background(), ProbeOptions{BaseURL: srv.URL, APIKey: "sk-leaky", Timeout: "5s"})
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), "sk-leaky") {
			t.Fatalf("error leaked the API key: %v", err)
		}
		if !strings.Contains(err.Error(), "[redacted]") {
			t.Fatalf("expected a redacted message, got %v", err)
		}
	})
}

func TestTestModelRoundTrip(t *testing.T) {
	stub := &gatewayStub{
		modelsPath:  "/v1/models",
		models:      catalogue("gpt-4o-mini", "text-embedding-3-small"),
		chatContent: `{"intent":"unsubscribe","confidence":0.99,"reason_code":"explicit_unsubscribe"}`,
	}
	srv := newGateway(t, stub)

	res := TestModel(context.Background(), ProbeOptions{
		BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "gpt-4o-mini", Timeout: "5s",
	})

	if res.Status != StatusSuccess {
		t.Fatalf("status = %q, steps = %+v", res.Status, res.Steps)
	}
	names := make([]string, 0, len(res.Steps))
	for _, s := range res.Steps {
		names = append(names, s.Name)
		if s.Status != StatusSuccess {
			t.Fatalf("step %s = %q (%s)", s.Name, s.Status, s.Detail)
		}
	}
	if strings.Join(names, ",") != "config,gateway,model,completion" {
		t.Fatalf("steps = %v", names)
	}
	if res.APIRoot != srv.URL+"/v1" {
		t.Fatalf("api root = %q", res.APIRoot)
	}
	if res.SuggestedBaseURL != "" {
		t.Fatalf("no suggestion expected when the configured root answered, got %q", res.SuggestedBaseURL)
	}
	if res.ModelCount != 2 {
		t.Fatalf("model count = %d", res.ModelCount)
	}
	if res.Decision == nil || res.Decision.Intent != IntentUnsubscribe || res.Decision.ReasonCode != ReasonExplicitUnsubscribe {
		t.Fatalf("decision = %+v", res.Decision)
	}
	if res.Matched == nil || !*res.Matched {
		t.Fatalf("matched = %v", res.Matched)
	}
	if res.ExpectedIntent != IntentUnsubscribe || !strings.Contains(res.Sample, "remove me") {
		t.Fatalf("sample/expected = %q/%q", res.Sample, res.ExpectedIntent)
	}
	if !strings.Contains(res.Response, "unsubscribe") {
		t.Fatalf("response = %q", res.Response)
	}

	// The probe must exercise the production prompt, not a reduced one.
	req := stub.chatRequest(t)
	if req["model"] != "gpt-4o-mini" {
		t.Fatalf("model = %v", req["model"])
	}
	messages, _ := req["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v", messages)
	}
	system, _ := messages[0].(map[string]any)
	if content, _ := system["content"].(string); !strings.Contains(content, "Classify exactly one inbound customer e-mail reply") {
		t.Fatalf("system prompt = %v", system["content"])
	}
	user, _ := messages[1].(map[string]any)
	if content, _ := user["content"].(string); !strings.Contains(content, "Untrusted reply body follows") {
		t.Fatalf("user prompt = %v", user["content"])
	}
}

// A base URL that is missing the gateway's mount must fail honestly: the
// completion call still uses the configured root, and the operator gets the
// root that answered as a suggestion instead of a false pass.
func TestTestModelSuggestsTheRootThatAnswered(t *testing.T) {
	stub := &gatewayStub{
		modelsPath:  "/v1/models",
		chatPath:    "/v1/chat/completions",
		models:      catalogue("gpt-4o-mini"),
		chatContent: `{"intent":"unsubscribe","confidence":0.99,"reason_code":"explicit_unsubscribe"}`,
	}
	srv := newGateway(t, stub)

	res := TestModel(context.Background(), ProbeOptions{
		BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o-mini", Timeout: "5s",
	})

	if res.Status != StatusFailed {
		t.Fatalf("status = %q, steps = %+v", res.Status, res.Steps)
	}
	if res.SuggestedBaseURL != srv.URL+"/v1" {
		t.Fatalf("suggested = %q", res.SuggestedBaseURL)
	}
	completion := stepNamed(t, res, StepCompletion)
	if completion.Status != StatusFailed || completion.Reason != ReasonHTTPError {
		t.Fatalf("completion = %+v", completion)
	}
	if !strings.Contains(completion.Detail, "404") {
		t.Fatalf("completion detail = %q", completion.Detail)
	}
}

func stepNamed(t *testing.T, res TestResult, name string) Step {
	t.Helper()
	for _, s := range res.Steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("step %s missing from %+v", name, res.Steps)
	return Step{}
}

func TestTestModelReportsEveryFailure(t *testing.T) {
	const (
		ok       = StatusSuccess
		warn     = StatusWarning
		failed   = StatusFailed
		notFound = http.StatusNotFound
	)
	cases := []struct {
		name           string
		opt            func(srvURL string) ProbeOptions
		stub           *gatewayStub
		wantStatus     string
		wantStep       string
		wantStepStatus string
		wantReason     string
		wantMatched    bool
	}{
		{
			name:       "invalid base url",
			opt:        func(string) ProbeOptions { return ProbeOptions{BaseURL: "http://", APIKey: "k", Model: "m"} },
			stub:       &gatewayStub{},
			wantStatus: failed, wantStep: StepConfig, wantStepStatus: failed, wantReason: ReasonInvalidBaseURL,
		},
		{
			name: "invalid timeout",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u, APIKey: "k", Model: "m", Timeout: "500ms"}
			},
			stub:       &gatewayStub{},
			wantStatus: failed, wantStep: StepConfig, wantStepStatus: failed, wantReason: ReasonInvalidTimeout,
		},
		{
			name:       "missing api key",
			opt:        func(u string) ProbeOptions { return ProbeOptions{BaseURL: u, Model: "m"} },
			stub:       &gatewayStub{},
			wantStatus: failed, wantStep: StepConfig, wantStepStatus: failed, wantReason: ReasonMissingAPIKey,
		},
		{
			name:       "missing model",
			opt:        func(u string) ProbeOptions { return ProbeOptions{BaseURL: u, APIKey: "k"} },
			stub:       &gatewayStub{},
			wantStatus: failed, wantStep: StepConfig, wantStepStatus: failed, wantReason: ReasonMissingModel,
		},
		{
			name: "rejected key fails the gateway and the completion",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u, APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub:       &gatewayStub{modelsStatus: http.StatusUnauthorized, chatStatus: http.StatusUnauthorized},
			wantStatus: failed, wantStep: StepCompletion, wantStepStatus: failed, wantReason: ReasonAuthRejected,
		},
		{
			name: "gateway without a model list still classifies",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u, APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath:  "/nowhere/models",
				chatContent: `{"intent":"unsubscribe","confidence":0.97,"reason_code":"explicit_unsubscribe"}`,
			},
			wantStatus: warn, wantStep: StepModel, wantStepStatus: warn, wantReason: ReasonListUnavailable, wantMatched: true,
		},
		{
			name: "model missing from the catalogue still classifies",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u + "/v1", APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath:  "/v1/models",
				models:      catalogue("other-model"),
				chatContent: `{"intent":"unsubscribe","confidence":0.97,"reason_code":"explicit_unsubscribe"}`,
			},
			wantStatus: warn, wantStep: StepModel, wantStepStatus: warn, wantReason: ReasonNotAdvertised, wantMatched: true,
		},
		{
			name: "model violates the schema",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u + "/v1", APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath:  "/v1/models",
				models:      catalogue("m"),
				chatContent: "I am sorry, I cannot help with that.",
			},
			wantStatus: failed, wantStep: StepCompletion, wantStepStatus: failed, wantReason: ReasonInvalidJSON,
		},
		{
			name: "reasoning-only answer",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u + "/v1", APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath: "/v1/models",
				models:     catalogue("m"),
				chatRaw:    `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`,
			},
			wantStatus: failed, wantStep: StepCompletion, wantStepStatus: failed, wantReason: ReasonReasoningOnly,
		},
		{
			name: "unexpected intent for the built-in sample",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u + "/v1", APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath:  "/v1/models",
				models:      catalogue("m"),
				chatContent: `{"intent":"other","confidence":0.4,"reason_code":"none"}`,
			},
			wantStatus: warn, wantStep: StepCompletion, wantStepStatus: warn, wantReason: ReasonUnexpectedIntent,
		},
		{
			name: "gateway http error on completion",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u + "/v1", APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath: "/v1/models",
				models:     catalogue("m"),
				chatStatus: http.StatusBadRequest,
				chatRaw:    `{"error":{"message":"model m is not available"}}`,
			},
			wantStatus: failed, wantStep: StepCompletion, wantStepStatus: failed, wantReason: ReasonHTTPError,
		},
		{
			name: "unreadable model list",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u + "/v1", APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub: &gatewayStub{
				modelsPath:  "/v1/models",
				modelsBody:  "<html>gateway error</html>",
				chatContent: `{"intent":"unsubscribe","confidence":0.9,"reason_code":"explicit_unsubscribe"}`,
			},
			wantStatus: warn, wantStep: StepGateway, wantStepStatus: warn, wantReason: ReasonInvalidList, wantMatched: true,
		},
		{
			name: "missing chat endpoint",
			opt: func(u string) ProbeOptions {
				return ProbeOptions{BaseURL: u, APIKey: "k", Model: "m", Timeout: "5s"}
			},
			stub:       &gatewayStub{modelsStatus: notFound, chatStatus: notFound},
			wantStatus: failed, wantStep: StepCompletion, wantStepStatus: failed, wantReason: ReasonHTTPError,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newGateway(t, c.stub)
			res := TestModel(context.Background(), c.opt(srv.URL))
			if res.Status != c.wantStatus {
				t.Fatalf("status = %q, want %q (steps %+v)", res.Status, c.wantStatus, res.Steps)
			}
			step := stepNamed(t, res, c.wantStep)
			if step.Status != c.wantStepStatus || step.Reason != c.wantReason {
				t.Fatalf("step = %+v, want %s/%s (steps %+v)", step, c.wantStepStatus, c.wantReason, res.Steps)
			}
			if res.Matched != nil && *res.Matched != c.wantMatched {
				t.Fatalf("matched = %v, want %v", *res.Matched, c.wantMatched)
			}
		})
	}

	_ = ok
}

func TestTestModelUnreachableGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	res := TestModel(context.Background(), ProbeOptions{
		BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: "2s",
	})
	if res.Status != StatusFailed {
		t.Fatalf("status = %q, steps = %+v", res.Status, res.Steps)
	}
	if gateway := stepNamed(t, res, StepGateway); gateway.Reason != ReasonUnreachable {
		t.Fatalf("gateway step = %+v", gateway)
	}
	if completion := stepNamed(t, res, StepCompletion); completion.Status != StatusFailed || completion.Reason != ReasonUnreachable {
		t.Fatalf("completion step = %+v", completion)
	}
}

func TestTestModelHonoursCustomSampleAndExpectation(t *testing.T) {
	stub := &gatewayStub{
		modelsPath:  "/v1/models",
		models:      catalogue("classifier-v1"),
		chatContent: `{"intent":"complaint","confidence":0.95,"reason_code":"explicit_spam_or_abuse"}`,
	}
	srv := newGateway(t, stub)

	res := TestModel(context.Background(), ProbeOptions{
		BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "classifier-v1", Timeout: "5s",
		Sample: "I will report this spam to the authorities.", ExpectedIntent: IntentComplaint,
	})
	if res.Status != StatusSuccess {
		t.Fatalf("status = %q, steps = %+v", res.Status, res.Steps)
	}
	if res.Sample != "I will report this spam to the authorities." || res.ExpectedIntent != IntentComplaint {
		t.Fatalf("sample/expected = %q/%q", res.Sample, res.ExpectedIntent)
	}
	if res.Decision == nil || res.Decision.Intent != IntentComplaint {
		t.Fatalf("decision = %+v", res.Decision)
	}

	// The sample body reaches the model verbatim.
	req := stub.chatRequest(t)
	messages, _ := req["messages"].([]any)
	user, _ := messages[1].(map[string]any)
	if content, _ := user["content"].(string); !strings.Contains(content, "report this spam") {
		t.Fatalf("user prompt = %v", user["content"])
	}
}

func TestDecodeModelListAcceptsGatewayShapes(t *testing.T) {
	t.Run("models key and bare strings", func(t *testing.T) {
		models, err := decodeModelList([]byte(`{"models":["a-model",{"id":"b-model","owned_by":"acme"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 2 || models[0].ID != "a-model" || models[1].OwnedBy != "acme" {
			t.Fatalf("models = %+v", models)
		}
	})

	t.Run("empty catalogue is valid", func(t *testing.T) {
		models, err := decodeModelList([]byte(`{"object":"list","data":[]}`))
		if err != nil || len(models) != 0 {
			t.Fatalf("models = %+v, err = %v", models, err)
		}
	})

	t.Run("unreadable body", func(t *testing.T) {
		_, err := decodeModelList([]byte("<html>"))
		var gw *GatewayError
		if !errors.As(err, &gw) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("blank and oversized ids are dropped", func(t *testing.T) {
		models, err := decodeModelList([]byte(`{"data":[{"id":"  "},{"id":"` + strings.Repeat("x", 500) + `"},{"id":"good-model"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 1 || models[0].ID != "good-model" {
			t.Fatalf("models = %+v", models)
		}
	})
}

func TestResolveTimeoutAndAPIKeyRedaction(t *testing.T) {
	if d, err := resolveTimeout(""); err != nil || d != 15*time.Second {
		t.Fatalf("default timeout = %v, err = %v", d, err)
	}
	if _, err := resolveTimeout("0s"); err == nil {
		t.Fatal("expected an error for a 0s timeout")
	}
	if _, err := resolveTimeout("3m"); err == nil {
		t.Fatal("expected an error for a 3m timeout")
	}

	if got := redact("token sk-a rejected", "sk-a"); strings.Contains(got, "sk-a") {
		t.Fatalf("redact = %q", got)
	}
	if got := redact("nothing to hide", ""); got != "nothing to hide" {
		t.Fatalf("redact = %q", got)
	}
}

func TestIsTimeoutDetectsDeadlines(t *testing.T) {
	if !isTimeout(&url.Error{Op: "Post", Err: context.DeadlineExceeded}) {
		t.Fatal("wrapped deadline must be a timeout")
	}
	if isTimeout(errors.New("connection refused")) {
		t.Fatal("a plain error must not be a timeout")
	}
}
