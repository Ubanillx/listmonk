package replyai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Gateway probing. The classifier is normally pointed at an OpenAI-compatible
// aggregator gateway (new-api, one-api, LiteLLM, ...) where the model catalogue
// and the credentials are decided by the gateway instead of by a single vendor.
// Administrators therefore have to discover which models exist, and whether the
// selected one actually answers, before automation is enabled. Everything in
// this file is read-only: probes never persist settings, and the API key only
// ever travels in the Authorization header.

// Probe step names reported to the settings UI.
const (
	StepConfig     = "config"
	StepGateway    = "gateway"
	StepModel      = "model"
	StepCompletion = "completion"
)

// Probe step statuses. Success means the step proved what it checks, warning
// means the probe continued with a caveat, and failed means a step that must
// hold for classification to work did not.
const (
	StatusSuccess = "success"
	StatusWarning = "warning"
	StatusFailed  = "failed"
)

// Machine-readable reasons. The UI localizes the interpretation while Detail
// keeps the raw, language-neutral technical evidence.
const (
	ReasonInvalidBaseURL   = "invalid_base_url"
	ReasonInvalidTimeout   = "invalid_timeout"
	ReasonMissingAPIKey    = "missing_api_key"
	ReasonMissingModel     = "missing_model"
	ReasonUnreachable      = "unreachable"
	ReasonTimeout          = "timeout"
	ReasonAuthRejected     = "auth_rejected"
	ReasonNoModelList      = "no_model_list"
	ReasonInvalidList      = "invalid_model_list"
	ReasonListUnavailable  = "model_list_unavailable"
	ReasonNotAdvertised    = "model_not_advertised"
	ReasonHTTPError        = "http_error"
	ReasonReasoningOnly    = "reasoning_only"
	ReasonEmptyResponse    = "empty_response"
	ReasonInvalidJSON      = "invalid_classification"
	ReasonUnexpectedIntent = "unexpected_intent"
)

const (
	maxProbeBodyBytes = 1 << 20
	maxModelCount     = 3000
	maxModelIDRunes   = 200
	maxOwnedByRunes   = 80
	maxDetailRunes    = 400
	maxResponseRunes  = 1200
	// probeMinConfidence is irrelevant to probing; decisions are inspected
	// directly instead of being gated, so a valid but low-confidence answer
	// still proves the model replied.
	probeMinConfidence = 0.5
)

// DefaultSampleReply is the built-in probe body. It is an unambiguous
// unsubscribe request, so it doubles as a quality check: a model that cannot
// return "unsubscribe" for it must not be trusted to automate destructive
// subscription changes.
const DefaultSampleReply = "Please stop sending me your newsletter and remove me from your mailing list immediately."

// ProbeOptions is an unsaved settings form as submitted by the settings page.
type ProbeOptions struct {
	BaseURL        string
	APIKey         string
	Model          string
	Timeout        string
	Sample         string
	ExpectedIntent string
}

// ModelInfo is one model advertised by the gateway.
type ModelInfo struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
	// ChatHint is an advisory hint only: gateways list embeddings, speech and
	// image models alongside chat models, and those can never classify a reply.
	ChatHint bool `json:"chat_hint"`
}

// ModelList is the result of a gateway model discovery call.
type ModelList struct {
	// APIRoot is the root that answered; it differs from the configured base
	// URL when the gateway had to be reached through its /v1 mount.
	APIRoot          string      `json:"api_root"`
	SuggestedBaseURL string      `json:"suggested_base_url,omitempty"`
	Models           []ModelInfo `json:"models"`
	ChatCandidates   int         `json:"chat_candidates"`
}

// Step is one probe checkpoint.
type Step struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Detail    string `json:"detail,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

// TestResult is a full gateway + model + classification round trip.
type TestResult struct {
	Status           string    `json:"status"`
	Steps            []Step    `json:"steps"`
	APIRoot          string    `json:"api_root"`
	SuggestedBaseURL string    `json:"suggested_base_url,omitempty"`
	Model            string    `json:"model"`
	Sample           string    `json:"sample"`
	ExpectedIntent   string    `json:"expected_intent,omitempty"`
	Matched          *bool     `json:"matched,omitempty"`
	Decision         *Decision `json:"decision,omitempty"`
	Response         string    `json:"response,omitempty"`
	ModelCount       int       `json:"model_count"`
	LatencyMS        int64     `json:"latency_ms"`
}

// GatewayError is a non-2xx gateway response. Message is truncated and has the
// API key redacted, so it is safe to show to an administrator and to log.
type GatewayError struct {
	StatusCode int
	Message    string
}

func (e *GatewayError) Error() string {
	return e.Message
}

// prober performs the read-only HTTP calls shared by model discovery and the
// model test.
type prober struct {
	root   string
	apiKey string
	client *http.Client
}

func newProber(baseURL, apiKey, timeout string) (*prober, error) {
	root, err := apiRoot(baseURL)
	if err != nil {
		return nil, err
	}
	d, err := resolveTimeout(timeout)
	if err != nil {
		return nil, err
	}
	return &prober{root: root, apiKey: strings.TrimSpace(apiKey), client: newHTTPClient(d)}, nil
}

// roots lists the candidate API roots for model discovery. Gateways are
// commonly mounted under /v1, so when the configured root does not already
// carry it, that mount is tried as a fallback and reported back to the operator
// as a suggested base URL.
func (p *prober) roots() []string {
	out := []string{p.root}
	if !strings.HasSuffix(p.root, "/v1") {
		out = append(out, p.root+"/v1")
	}
	return out
}

func (p *prober) do(ctx context.Context, method, target string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBodyBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// listModels queries GET /models on the configured root and, when that path is
// absent, on the /v1 mount. Authentication failures are returned immediately
// because they apply to every candidate.
func (p *prober) listModels(ctx context.Context) (string, []byte, error) {
	var last *GatewayError
	for _, root := range p.roots() {
		status, body, err := p.do(ctx, http.MethodGet, root+"/models", nil)
		if err != nil {
			return "", nil, err
		}
		if status >= http.StatusOK && status < http.StatusMultipleChoices {
			return root, body, nil
		}
		last = gatewayError(status, body, p.apiKey)
		if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			return "", nil, last
		}
	}
	return "", nil, last
}

// ListModels discovers the models an OpenAI-compatible gateway advertises.
func ListModels(ctx context.Context, opt ProbeOptions) (ModelList, error) {
	p, err := newProber(opt.BaseURL, opt.APIKey, opt.Timeout)
	if err != nil {
		return ModelList{}, err
	}
	root, body, err := p.listModels(ctx)
	if err != nil {
		return ModelList{}, err
	}
	models, err := decodeModelList(body)
	if err != nil {
		return ModelList{}, err
	}

	out := ModelList{APIRoot: root, Models: models}
	if root != p.root {
		out.SuggestedBaseURL = root
	}
	for _, m := range models {
		if m.ChatHint {
			out.ChatCandidates++
		}
	}
	return out, nil
}

// TestModel runs the full configuration → gateway → model → classification
// round trip used by the settings page. It always returns a result: a failure
// is reported as a failed step instead of an error, so the UI can show exactly
// which part of the configuration did not work.
func TestModel(ctx context.Context, opt ProbeOptions) TestResult {
	started := time.Now()
	secret := strings.TrimSpace(opt.APIKey)
	res := TestResult{Model: strings.TrimSpace(opt.Model)}

	add := func(name, status, reason, detail string, latency time.Duration) {
		res.Steps = append(res.Steps, Step{
			Name:      name,
			Status:    status,
			Reason:    reason,
			Detail:    truncate(redact(detail, secret), maxDetailRunes),
			LatencyMS: latency.Milliseconds(),
		})
	}

	// Step 1: the configuration itself.
	root, rootErr := apiRoot(opt.BaseURL)
	_, timeoutErr := resolveTimeout(opt.Timeout)
	switch {
	case rootErr != nil:
		add(StepConfig, StatusFailed, ReasonInvalidBaseURL, rootErr.Error(), 0)
	case timeoutErr != nil:
		add(StepConfig, StatusFailed, ReasonInvalidTimeout, timeoutErr.Error(), 0)
	case secret == "":
		add(StepConfig, StatusFailed, ReasonMissingAPIKey, "enter the gateway API key", 0)
	case res.Model == "":
		add(StepConfig, StatusFailed, ReasonMissingModel, "select or type a model before testing", 0)
	default:
		timeout, _ := resolveTimeout(opt.Timeout)
		res.APIRoot = root
		add(StepConfig, StatusSuccess, "", fmt.Sprintf("POST %s · timeout %s", root+"/chat/completions", timeout), 0)
	}
	if res.Steps[0].Status == StatusFailed {
		return res.finish(started)
	}

	// Step 2: reachability and credentials, proved by the model catalogue.
	p, _ := newProber(root, secret, opt.Timeout)
	gatewayStarted := time.Now()
	modelsRoot, body, err := p.listModels(ctx)
	var (
		models     []ModelInfo
		listLoaded bool
	)
	if err != nil {
		reason, detail := probeFailure(err, secret, stageModelList)
		add(StepGateway, StatusWarning, reason, detail, time.Since(gatewayStarted))
	} else if parsed, decodeErr := decodeModelList(body); decodeErr != nil {
		add(StepGateway, StatusWarning, ReasonInvalidList, decodeErr.Error(), time.Since(gatewayStarted))
	} else {
		models, listLoaded = parsed, true
		res.ModelCount = len(parsed)
		if modelsRoot != root {
			res.SuggestedBaseURL = modelsRoot
		}
		add(StepGateway, StatusSuccess, "",
			fmt.Sprintf("GET %s/models · HTTP 200 · %d models", modelsRoot, len(parsed)),
			time.Since(gatewayStarted))
	}

	// Step 3: is the selected model part of the catalogue?
	advertised := false
	for _, m := range models {
		if strings.EqualFold(m.ID, res.Model) {
			advertised = true
			break
		}
	}
	switch {
	case !listLoaded:
		add(StepModel, StatusWarning, ReasonListUnavailable,
			"the gateway did not return a model list; the selected model was used as typed", 0)
	case advertised:
		add(StepModel, StatusSuccess, "", fmt.Sprintf("%s is advertised by the gateway", res.Model), 0)
	default:
		add(StepModel, StatusWarning, ReasonNotAdvertised,
			fmt.Sprintf("the gateway did not list %s; a completion call was attempted anyway", res.Model), 0)
	}

	// Step 4: can the model actually answer with a bounded classification?
	sample := strings.TrimSpace(opt.Sample)
	expected := strings.ToLower(strings.TrimSpace(opt.ExpectedIntent))
	if sample == "" {
		sample = DefaultSampleReply
		if expected == "" {
			expected = IntentUnsubscribe
		}
	}
	res.Sample, res.ExpectedIntent = sample, expected

	client, clientErr := New(Options{
		Enabled:       true,
		BaseURL:       root,
		APIKey:        secret,
		Model:         res.Model,
		Timeout:       opt.Timeout,
		MinConfidence: probeMinConfidence,
	})
	if clientErr != nil {
		add(StepCompletion, StatusFailed, ReasonInvalidBaseURL, clientErr.Error(), 0)
		return res.finish(started)
	}

	completionStarted := time.Now()
	out, err := client.chat(ctx, "Untrusted reply body follows:\n---\n"+sample+"\n---")
	latency := time.Since(completionStarted)
	switch {
	case err != nil:
		reason, detail := probeFailure(err, secret, stageCompletion)
		add(StepCompletion, StatusFailed, reason, detail, latency)
	case out.Content == "":
		reason, detail := ReasonEmptyResponse, "the model returned an empty message"
		if out.Reasoning != "" {
			reason = ReasonReasoningOnly
			detail = "the model returned reasoning content only; pick a plain chat model"
		}
		add(StepCompletion, StatusFailed, reason, detail, latency)
	default:
		decision, decodeErr := decodeDecision(out.Content)
		if decodeErr != nil {
			add(StepCompletion, StatusFailed, ReasonInvalidJSON,
				fmt.Sprintf("%s · %s", decodeErr.Error(), excerpt(out.Content)), latency)
			break
		}
		res.Decision = &decision
		res.Response = excerpt(out.Content)
		matched := expected == "" || decision.Intent == expected
		res.Matched = &matched
		detail := fmt.Sprintf("intent=%s · confidence=%.2f · reason_code=%s",
			decision.Intent, decision.Confidence, decision.ReasonCode)
		if matched {
			add(StepCompletion, StatusSuccess, "", detail, latency)
		} else {
			add(StepCompletion, StatusWarning, ReasonUnexpectedIntent,
				fmt.Sprintf("%s · expected intent=%s", detail, expected), latency)
		}
	}

	return res.finish(started)
}

// finish derives the overall status from the individual steps.
func (r TestResult) finish(started time.Time) TestResult {
	status := StatusSuccess
	for _, s := range r.Steps {
		switch s.Status {
		case StatusFailed:
			r.Status = StatusFailed
			r.LatencyMS = time.Since(started).Milliseconds()
			return r
		case StatusWarning:
			status = StatusWarning
		}
	}
	r.Status = status
	r.LatencyMS = time.Since(started).Milliseconds()
	return r
}

// modelEntry tolerates both the OpenAI object form and bare model-id strings.
type modelEntry struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

func (m *modelEntry) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		m.ID = s
		return nil
	}
	type plain modelEntry
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*m = modelEntry(p)
	return nil
}

// decodeModelList parses an OpenAI-compatible model catalogue, dropping
// duplicates and bounding every field before it is shown to an administrator.
func decodeModelList(raw []byte) ([]ModelInfo, error) {
	var payload struct {
		Data   []modelEntry `json:"data"`
		Models []modelEntry `json:"models"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, &GatewayError{
			StatusCode: http.StatusOK,
			Message:    "the gateway returned an unreadable model list: " + excerpt(string(raw)),
		}
	}

	entries := payload.Data
	if len(entries) == 0 {
		entries = payload.Models
	}

	seen := make(map[string]bool, len(entries))
	out := make([]ModelInfo, 0, len(entries))
	for _, e := range entries {
		id := strings.TrimSpace(e.ID)
		// Oversized ids are catalogue noise: truncating one would hand the
		// operator an unusable model name, so it is dropped instead.
		if id == "" || len([]rune(id)) > maxModelIDRunes {
			continue
		}
		key := strings.ToLower(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ModelInfo{
			ID:       id,
			OwnedBy:  truncate(strings.TrimSpace(e.OwnedBy), maxOwnedByRunes),
			ChatHint: isChatModel(id),
		})
		if len(out) >= maxModelCount {
			break
		}
	}

	// Chat-capable models first, then alphabetical, so the models an operator
	// can actually use are at the top of a long aggregator catalogue.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ChatHint != out[j].ChatHint {
			return out[i].ChatHint
		}
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out, nil
}

// nonChatModelMarkers flags catalogue entries that can never classify a reply.
// The list is deliberately narrow: it only covers families that are certain to
// be non-conversational, and the hint never blocks a selection.
var nonChatModelMarkers = []string{
	"embedding", "embed-", "-embed", "bge-", "rerank", "moderation",
	"whisper", "transcribe", "tts", "dall-e", "stable-diffusion", "sdxl", "flux", "midjourney",
}

func isChatModel(id string) bool {
	lower := strings.ToLower(id)
	for _, marker := range nonChatModelMarkers {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

// gatewayError builds a redacted, bounded error for a non-2xx gateway response.
func gatewayError(status int, body []byte, secret string) *GatewayError {
	detail := redact(errorMessage(body), secret)
	msg := fmt.Sprintf("HTTP %d", status)
	if detail != "" {
		msg += ": " + detail
	}
	return &GatewayError{StatusCode: status, Message: truncate(msg, maxDetailRunes)}
}

// errorMessage prefers the gateway's own error message over the raw body.
func errorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return strings.TrimSpace(string(body))
}

// probeStage distinguishes the two calls that share the failure mapping.
type probeStage int

const (
	stageModelList probeStage = iota
	stageCompletion
)

// probeFailure maps a transport or HTTP error to a reason code plus a redacted
// technical detail.
func probeFailure(err error, secret string, stage probeStage) (string, string) {
	var gw *GatewayError
	if errors.As(err, &gw) {
		switch gw.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ReasonAuthRejected, gw.Message
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			// A missing /models path means the gateway simply does not publish
			// a catalogue; a missing chat endpoint means the base URL is wrong.
			if stage == stageModelList {
				return ReasonNoModelList, gw.Message
			}
			return ReasonHTTPError, gw.Message
		}
		return ReasonHTTPError, gw.Message
	}
	if isTimeout(err) {
		return ReasonTimeout, redact(err.Error(), secret)
	}
	return ReasonUnreachable, redact(err.Error(), secret)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var nerr net.Error
	return errors.As(err, &nerr) && nerr.Timeout()
}

// redact removes the API key from any text that may reach a UI or a log.
func redact(s, secret string) string {
	if secret == "" || s == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}

// excerpt bounds a raw model or gateway payload for display.
func excerpt(s string) string {
	return truncate(collapseSpace(s), maxResponseRunes)
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
