package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/replyai"
	"github.com/labstack/echo/v4"
)

// Sentinel validation errors. The HTTP handlers localize them, which keeps the
// validation itself testable without a translator.
var (
	errReplyAIBaseURLMissing = errors.New("reply AI base URL is required")
	errReplyAIExpectedIntent = errors.New("reply AI expected intent must be empty, unsubscribe, complaint, or other")
)

// replyAIProbeRequest is the unsaved reply-AI form as submitted by the settings
// page. A blank or fully masked api_key means "reuse the stored key", which is
// the same convention the settings update endpoint uses, so a probe result
// always matches what saving the form would produce.
type replyAIProbeRequest struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	Timeout        string `json:"timeout"`
	Sample         string `json:"sample_text"`
	ExpectedIntent string `json:"expected_intent"`
}

// normalizeReplyAIProbe trims the submitted form and applies the shared request
// timeout default. It never reads stored settings.
func normalizeReplyAIProbe(req *replyAIProbeRequest) error {
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)
	req.Timeout = strings.TrimSpace(req.Timeout)
	req.Sample = strings.TrimSpace(req.Sample)
	req.ExpectedIntent = strings.ToLower(strings.TrimSpace(req.ExpectedIntent))

	switch req.ExpectedIntent {
	case "", replyai.IntentUnsubscribe, replyai.IntentComplaint, replyai.IntentOther:
	default:
		return errReplyAIExpectedIntent
	}
	if req.BaseURL == "" {
		return errReplyAIBaseURLMissing
	}
	if req.Timeout == "" {
		req.Timeout = "15s"
	}
	return nil
}

// resolveReplyAIProbe normalizes the submitted form and fills the stored API key
// in when the field was left masked or blank. The key stays in memory for the
// outbound call only: it is never echoed back, stored, or logged.
func (a *App) resolveReplyAIProbe(req *replyAIProbeRequest) error {
	if err := normalizeReplyAIProbe(req); err != nil {
		switch {
		case errors.Is(err, errReplyAIBaseURLMissing):
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.inboundReplies.errorBaseURL"))
		case errors.Is(err, errReplyAIExpectedIntent):
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.T("settings.inboundReplies.errorExpectedIntent"))
		}
		return err
	}

	key := strings.TrimSpace(req.APIKey)
	if key == "" || strings.Trim(key, pwdMask) == "" {
		cur, err := a.core.GetSettings()
		if err != nil {
			return err
		}
		key = strings.TrimSpace(cur.ReplyAI.APIKey)
	}
	if key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.inboundReplies.errorAPIKey"))
	}
	req.APIKey = key
	return nil
}

// ListReplyAIModels proxies a model-discovery call to the configured
// OpenAI-compatible gateway (new-api, one-api, LiteLLM, ...) so an administrator
// can see and pick a model before saving the settings. The API key only ever
// travels in the outbound Authorization header: it is never echoed back, never
// stored, and never logged.
func (a *App) ListReplyAIModels(c echo.Context) error {
	var req replyAIProbeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidData"))
	}
	if err := a.resolveReplyAIProbe(&req); err != nil {
		return err
	}

	list, err := replyai.ListModels(c.Request().Context(), replyai.ProbeOptions{
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Timeout: req.Timeout,
	})
	if err != nil {
		a.log.Printf("reply AI model discovery failed for %s: %v", req.BaseURL, err)
		return a.replyAIProbeFailure(err)
	}
	return c.JSON(http.StatusOK, okResp{list})
}

// TestReplyAIModel pings the gateway and proves the selected model answers with
// a bounded classification. It always answers 200 with a per-step report so the
// settings page can show exactly which part of the configuration is wrong; only
// unusable input (missing URL, key, or model) is rejected with 400.
func (a *App) TestReplyAIModel(c echo.Context) error {
	var req replyAIProbeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("globals.messages.invalidData"))
	}
	if err := a.resolveReplyAIProbe(&req); err != nil {
		return err
	}
	if req.Model == "" {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("settings.inboundReplies.errorModelRequired"))
	}

	result := replyai.TestModel(c.Request().Context(), replyai.ProbeOptions{
		BaseURL:        req.BaseURL,
		APIKey:         req.APIKey,
		Model:          req.Model,
		Timeout:        req.Timeout,
		Sample:         req.Sample,
		ExpectedIntent: req.ExpectedIntent,
	})
	if result.Status == replyai.StatusFailed {
		a.log.Printf("reply AI model test failed for %s: %s", result.Model, lastStepDetail(result))
	}
	return c.JSON(http.StatusOK, okResp{result})
}

// replyAIProbeFailure maps a gateway failure to an administrator-facing status
// and a localized message. Gateway messages are already redacted and bounded.
func (a *App) replyAIProbeFailure(err error) error {
	var gw *replyai.GatewayError
	if errors.As(err, &gw) {
		switch gw.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("settings.inboundReplies.errorGatewayAuth", "status", strconv.Itoa(gw.StatusCode)))
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return echo.NewHTTPError(http.StatusBadGateway,
				a.i18n.T("settings.inboundReplies.errorGatewayNotFound"))
		}
		return echo.NewHTTPError(http.StatusBadGateway, gw.Message)
	}
	return echo.NewHTTPError(http.StatusBadGateway, err.Error())
}

func lastStepDetail(result replyai.TestResult) string {
	for _, s := range result.Steps {
		if s.Status == replyai.StatusFailed {
			return s.Reason + " " + s.Detail
		}
	}
	if len(result.Steps) == 0 {
		return ""
	}
	return result.Steps[len(result.Steps)-1].Detail
}
