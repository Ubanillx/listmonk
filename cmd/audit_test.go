package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	auditlog "github.com/knadh/listmonk/internal/audit"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

type recordingAudit struct {
	events []auditlog.Event
	err    error
}

func (r *recordingAudit) Record(_ context.Context, event auditlog.Event) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

func TestAuditRouteSpecUsesStableBusinessAction(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest("PUT", "/api/campaigns/42/status", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/api/campaigns/:id/status")
	c.SetParamNames("id")
	c.SetParamValues("42")

	spec, ok := auditRouteSpecForContext(c)
	if !ok {
		t.Fatal("campaign status route was not mapped")
	}
	if spec.action != "campaign.status_changed" || spec.objectType != "campaign" || c.Param(spec.objectParam) != "42" {
		t.Fatalf("unexpected audit route spec: %+v", spec)
	}
}

func TestAuditJSONScanAndMarshal(t *testing.T) {
	var value auditJSON
	if err := value.Scan([]byte(`{"count":2}`)); err != nil {
		t.Fatalf("scan metadata: %v", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if string(encoded) != `{"count":2}` {
		t.Fatalf("metadata = %s", encoded)
	}
}

func TestAuditMiddlewareRecordsUserSuccess(t *testing.T) {
	sink := &recordingAudit{}
	a := &App{audit: sink, log: log.New(io.Discard, "", 0)}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/campaigns/42/status", nil)
	req.Header.Set(workspaceHeader, "7")
	req.Header.Set("X-Request-ID", "request-123")
	req.Header.Set("User-Agent", "audit-test")
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/api/campaigns/:id/status")
	c.SetParamNames("id")
	c.SetParamValues("42")
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: 11}})

	err := a.auditMiddleware(func(c echo.Context) error {
		return c.JSON(http.StatusAccepted, map[string]bool{"ok": true})
	})(c)
	if err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.Action != "campaign.status_changed" || event.ObjectType != "campaign" || event.ObjectID != "42" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if event.ActorType != "user" || event.ActorUserID == nil || *event.ActorUserID != 11 {
		t.Fatalf("unexpected user actor: %+v", event)
	}
	if event.OrganizationID == nil || *event.OrganizationID != 7 {
		t.Fatalf("organization_id = %v, want 7", event.OrganizationID)
	}
	if event.Result != "success" || event.ReasonCode != "" || event.RequestID != "request-123" {
		t.Fatalf("unexpected result envelope: %+v", event)
	}
	metadata, ok := event.Metadata.(map[string]any)
	if !ok || metadata["http_status"] != http.StatusAccepted || metadata["route"] != "/api/campaigns/:id/status" {
		t.Fatalf("unexpected metadata: %#v", event.Metadata)
	}
}

func TestAuditMiddlewareRecordsAPIKeyDeniedAndGeneratesRequestID(t *testing.T) {
	sink := &recordingAudit{}
	a := &App{audit: sink, log: log.New(io.Discard, "", 0)}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/customers/42", nil)
	req.Header.Set(workspaceHeader, "9")
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/api/customers/:id")
	c.SetParamNames("id")
	c.SetParamValues("42")
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: 13}})
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{
		Base: auth.Base{ID: 77},
		Kind: auth.IntegrationTokenKindPersonal,
	})

	err := a.auditMiddleware(func(echo.Context) error {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied")
	})(c)
	if err == nil {
		t.Fatal("middleware swallowed the handler error")
	}
	if len(sink.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.ActorType != "api_key" || event.ActorUserID == nil || *event.ActorUserID != 13 || event.ActorTokenID == nil || *event.ActorTokenID != 77 {
		t.Fatalf("unexpected API key actor: %+v", event)
	}
	if event.Result != "denied" || event.ReasonCode != "http_403" || event.RequestID == "" {
		t.Fatalf("unexpected denied event: %+v", event)
	}
	if got := c.Response().Header().Get("X-Request-ID"); got != event.RequestID {
		t.Fatalf("response request id = %q, event request id = %q", got, event.RequestID)
	}
}

func TestAuditMiddlewareSupportsCustomerSelfServiceContext(t *testing.T) {
	sink := &recordingAudit{}
	a := &App{audit: sink, log: log.New(io.Discard, "", 0)}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/subscription/campaign/customer", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/subscription/:campUUID/:subUUID")
	c.SetParamNames("campUUID", "subUUID")
	c.SetParamValues("campaign", "customer")

	err := a.auditMiddleware(func(c echo.Context) error {
		setAuditCustomerContext(c, 42, "customer", map[string]any{
			"channel":    "public_self_service",
			"list_count": 2,
		})
		setAuditAction(c, "subscription.unsubscribed")
		return c.NoContent(http.StatusNoContent)
	})(c)
	if err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.ActorType != "customer" || event.OrganizationID == nil || *event.OrganizationID != 42 || event.Action != "subscription.unsubscribed" || event.ObjectID != "customer" {
		t.Fatalf("unexpected customer event: %+v", event)
	}
	metadata, ok := event.Metadata.(map[string]any)
	if !ok || metadata["channel"] != "public_self_service" || metadata["list_count"] != 2 {
		t.Fatalf("unexpected customer metadata: %#v", event.Metadata)
	}
}

func TestAuditSecondPhaseRouteTaxonomy(t *testing.T) {
	want := map[string]string{
		"POST /api/public/subscription":              "subscription.created",
		"POST /subscription/:campUUID/:subUUID":      "subscription.unsubscribed",
		"POST /subscription/optin/:subUUID":          "subscription.optin_confirmed",
		"POST /subscription/export/:subUUID":         "customer.data_exported",
		"POST /subscription/wipe/:subUUID":           "customer.data_erased",
		"POST /api/customer-lists/:id/pool-contacts": "pool_contact.created",
		"POST /api/media":                            "media.uploaded",
		"POST /api/custom-fields":                    "custom_field.created",
		"POST /api/profile/api-keys":                 "api_key.created",
		"POST /api/users":                            "user.created",
		"POST /api/roles/users":                      "role.user_created",
		"POST /api/organizations/members":            "organization.member_added",
		"POST /admin/login":                          "auth.login",
		"POST /admin/login/twofa":                    "auth.two_factor_verified",
		"POST /api/logout":                           "auth.logout",
	}
	for route, action := range want {
		spec, ok := auditRoutes[route]
		if !ok {
			t.Errorf("missing second-phase audit route %q", route)
			continue
		}
		if spec.action != action {
			t.Errorf("audit route %q action = %q, want %q", route, spec.action, action)
		}
	}
}

func TestRecordBackgroundAuditKeepsFailureReason(t *testing.T) {
	sink := &recordingAudit{}
	a := &App{audit: sink, log: log.New(io.Discard, "", 0)}
	a.recordBackgroundAuditResult("system", "reply.forward_failed", "reply_forward_message", "88", nil, "failed", "push_message_failed", map[string]any{"attempt": 3})
	if len(sink.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.ActorType != "system" || event.Action != "reply.forward_failed" || event.ObjectID != "88" || event.Result != "failed" || event.ReasonCode != "push_message_failed" {
		t.Fatalf("unexpected background event: %+v", event)
	}
}

func TestAuditRoutesAreRegistered(t *testing.T) {
	e := echo.New()
	a := &App{
		auth:   &auth.Auth{},
		cfg:    &Config{},
		urlCfg: &UrlConfig{},
		log:    log.New(io.Discard, "", 0),
	}
	a.cfg.Security.OIDC.Enabled = true
	initHTTPHandlers(e, a)

	registered := make(map[string]struct{}, len(e.Routes()))
	for _, route := range e.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for key := range auditRoutes {
		if _, ok := registered[key]; !ok {
			t.Errorf("audit route %q is not registered in the HTTP router", key)
		}
	}
}
