package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	auditlog "github.com/knadh/listmonk/internal/audit"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

type auditRouteSpec struct {
	action      string
	objectType  string
	objectParam string
}

const (
	auditContextAction         = "audit_action"
	auditContextActorType      = "audit_actor_type"
	auditContextActorUserID    = "audit_actor_user_id"
	auditContextActorTokenID   = "audit_actor_token_id"
	auditContextObjectID       = "audit_object_id"
	auditContextOrganizationID = "audit_organization_id"
	auditContextMetadata       = "audit_metadata"
	auditContextReasonCode     = "audit_reason_code"
	auditContextResult         = "audit_result"
)

const auditDetailTextLimit = 256

// auditRecorder keeps HTTP/background audit producers testable without
// coupling them to a concrete database writer. The production implementation
// is internal/audit.Writer.
type auditRecorder interface {
	Record(context.Context, auditlog.Event) error
}

// setAuditObjectDetails adds a small, human-readable snapshot to an audit
// event. The snapshot is deliberately separate from object_id: IDs are stable
// join keys, while these fields preserve enough context to understand an
// event after the referenced row has been renamed or deleted.
//
// Callers must only pass non-sensitive business labels here. In particular,
// this must never contain message bodies, credentials, attachments, or
// customer email addresses.
func setAuditObjectDetails(c echo.Context, details map[string]any) {
	clean := auditDetailMap(details)
	if len(clean) == 0 {
		return
	}
	setAuditMetadata(c, map[string]any{"object_details": clean})
}

func auditDetailMap(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}

	clean := make(map[string]any, len(details))
	for key, value := range details {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			clean[key] = auditDetailText(text)
			continue
		}
		clean[key] = value
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func auditDetailText(value string) string {
	runes := []rune(value)
	if len(runes) <= auditDetailTextLimit {
		return value
	}
	return string(runes[:auditDetailTextLimit]) + "…"
}

func auditTemplateDetails(t models.Template) map[string]any {
	details := map[string]any{
		"name": t.Name,
		"type": t.Type,
	}
	if t.Subject != "" {
		details["subject"] = t.Subject
	}
	return details
}

func auditCampaignDetails(campaign models.Campaign) map[string]any {
	details := map[string]any{
		"name":   campaign.Name,
		"type":   campaign.Type,
		"status": campaign.Status,
	}
	if campaign.Subject != "" {
		details["subject"] = campaign.Subject
	}
	return details
}

func auditCustomerDetails(customer models.Customer) map[string]any {
	details := map[string]any{
		"name":   customer.Name,
		"status": customer.Status,
	}
	if customer.CustomerCode != "" {
		details["customer_code"] = customer.CustomerCode
	}
	return details
}

// setAuditActorDetails preserves readable actor information in the event
// metadata. It is used for login requests as well, where the normal auth
// middleware has not established a session yet.
func setAuditActorDetails(c echo.Context, details map[string]any) {
	clean := auditDetailMap(details)
	if len(clean) == 0 {
		return
	}

	merged := make(map[string]any, len(clean))
	if current, ok := c.Get(auditContextMetadata).(map[string]any); ok {
		if existing, ok := current["actor_details"].(map[string]any); ok {
			for key, value := range existing {
				merged[key] = value
			}
		}
	}
	for key, value := range clean {
		merged[key] = value
	}
	setAuditMetadata(c, map[string]any{"actor_details": merged})
}

func auditUserDetails(user auth.User) map[string]any {
	return map[string]any{
		"name":     user.Name,
		"username": user.Username,
	}
}

// setAuditActorUser attaches the authenticated user to an event before a
// session exists, such as a successful password or OIDC login.
func setAuditActorUser(c echo.Context, user auth.User) {
	if user.ID > 0 {
		c.Set(auditContextActorType, "user")
		c.Set(auditContextActorUserID, user.ID)
	}
	setAuditActorDetails(c, auditUserDetails(user))
}

func setAuditAttemptedUsername(c echo.Context, username string) {
	if username = strings.TrimSpace(username); username != "" {
		setAuditActorDetails(c, map[string]any{"attempted_username": username})
	}
}

// auditRoutes deliberately names business operations instead of persisting
// raw HTTP paths. The list is kept close to the route registration so adding a
// new business mutation requires an explicit audit decision.
var auditRoutes = map[string]auditRouteSpec{
	"GET /api/audit-events/export":           {"audit.exported", "audit_event", ""},
	"PUT /api/settings":                      {"settings.updated", "settings", ""},
	"PUT /api/settings/:key":                 {"settings.updated", "settings", "key"},
	"POST /api/settings/smtp/test":           {"smtp.tested", "smtp", ""},
	"POST /api/settings/bounce/mailbox/test": {"bounce_mailbox.tested", "bounce_mailbox", ""},
	"POST /api/settings/reply-ai/models":     {"reply_ai.models_tested", "reply_ai", ""},
	"POST /api/settings/reply-ai/test":       {"reply_ai.tested", "reply_ai", ""},
	"POST /api/admin/reload":                 {"system.reloaded", "system", ""},

	"POST /api/customers":                     {"customer.created", "customer", ""},
	"PUT /api/customers/:id":                  {"customer.updated", "customer", "id"},
	"GET /api/customers/:id/export":           {"customer.data_exported", "customer", "id"},
	"POST /api/customers/:id/optin":           {"subscription.optin_sent", "customer", "id"},
	"PUT /api/customers/blocklist":            {"customer.blocklist_changed", "customer", ""},
	"PUT /api/customers/:id/blocklist":        {"customer.blocklist_changed", "customer", "id"},
	"PUT /api/customers/customer-lists/:id":   {"customer.memberships_changed", "customer", "id"},
	"PUT /api/customers/customer-lists":       {"customer.memberships_changed", "customer", ""},
	"DELETE /api/customers/:id":               {"customer.deleted", "customer", "id"},
	"DELETE /api/customers":                   {"customer.bulk_deleted", "customer", ""},
	"DELETE /api/customers/:id/bounces":       {"bounce.history_deleted", "customer", "id"},
	"POST /api/customers/query/delete":        {"customer.bulk_deleted", "customer_query", ""},
	"PUT /api/customers/query/blocklist":      {"customer.blocklist_changed", "customer_query", ""},
	"PUT /api/customers/query/customer-lists": {"customer.memberships_changed", "customer_query", ""},

	"POST /api/import/customers":                                     {"customer.import_started", "customer_import", ""},
	"DELETE /api/import/customers":                                   {"customer.import_stopped", "customer_import", ""},
	"POST /api/customer-lists":                                       {"customer_list.created", "customer_list", ""},
	"PUT /api/customer-lists/:id":                                    {"customer_list.updated", "customer_list", "id"},
	"DELETE /api/customer-lists/:id":                                 {"customer_list.deleted", "customer_list", "id"},
	"DELETE /api/customer-lists":                                     {"customer_list.bulk_deleted", "customer_list", ""},
	"POST /api/pools/import":                                         {"pool_contact.imported", "pool", ""},
	"POST /api/pools/permissions":                                    {"pool.organization_grant_changed", "customer_list", ""},
	"DELETE /api/pools/permissions":                                  {"pool.organization_grant_changed", "customer_list", ""},
	"POST /api/pool-segments":                                        {"pool_segment.created", "pool_segment", ""},
	"PUT /api/pool-segments/:id/reply-mailbox":                       {"pool_segment.reply_mailbox_changed", "pool_segment", "id"},
	"POST /api/pool-segments/members":                                {"pool_segment.members_changed", "pool_segment", ""},
	"POST /api/pool-segments/:id/import-members":                     {"pool_segment.members_imported", "pool_segment", "id"},
	"DELETE /api/pool-segments/members":                              {"pool_segment.members_changed", "pool_segment", ""},
	"PUT /api/pool-segments/members":                                 {"pool_segment.members_changed", "pool_segment", ""},
	"POST /api/pools/segments":                                       {"pool_segment.created", "pool_segment", ""},
	"PUT /api/pools/segments/:id/reply-mailbox":                      {"pool_segment.reply_mailbox_changed", "pool_segment", "id"},
	"POST /api/pools/segments/members":                               {"pool_segment.members_changed", "pool_segment", ""},
	"POST /api/pools/segments/:id/import-members":                    {"pool_segment.members_imported", "pool_segment", "id"},
	"DELETE /api/pools/segments/members":                             {"pool_segment.members_changed", "pool_segment", ""},
	"PUT /api/pools/segments/members":                                {"pool_segment.members_changed", "pool_segment", ""},
	"POST /api/customer-lists/:id/pool-contacts":                     {"pool_contact.created", "pool", "id"},
	"DELETE /api/customer-lists/:id/pool-contacts/:contact_id/email": {"pool_contact.email_cleared", "pool_contact", "contact_id"},
	"POST /api/pools/:id/contacts":                                   {"pool_contact.created", "pool", "id"},
	"DELETE /api/pools/:id/contacts/:contact_id/email":               {"pool_contact.email_cleared", "pool_contact", "contact_id"},

	"POST /api/media":                 {"media.uploaded", "media", ""},
	"POST /api/media/folders":         {"media_folder.created", "media_folder", ""},
	"PUT /api/media/folders/:id":      {"media_folder.renamed", "media_folder", "id"},
	"PUT /api/media/folders/:id/move": {"media_folder.moved", "media_folder", "id"},
	"DELETE /api/media/folders/:id":   {"media_folder.deleted", "media_folder", "id"},
	"PUT /api/media/:id/folder":       {"media.folder_changed", "media", "id"},
	"DELETE /api/media/:id":           {"media.deleted", "media", "id"},
	"POST /api/custom-fields":         {"custom_field.created", "custom_field", ""},
	"PUT /api/custom-fields/:key":     {"custom_field.updated", "custom_field", "key"},
	"DELETE /api/custom-fields/:key":  {"custom_field.deactivated", "custom_field", "key"},

	"POST /api/campaigns":            {"campaign.created", "campaign", ""},
	"POST /api/campaigns/:id/clone":  {"campaign.cloned", "campaign", "id"},
	"PUT /api/campaigns/:id":         {"campaign.updated", "campaign", "id"},
	"PUT /api/campaigns/:id/status":  {"campaign.status_changed", "campaign", "id"},
	"PUT /api/campaigns/:id/archive": {"campaign.archive_changed", "campaign", "id"},
	"POST /api/campaigns/:id/test":   {"campaign.test_sent", "campaign", "id"},
	"POST /api/campaigns/:id/pools":  {"campaign.audience_changed", "campaign", "id"},
	"DELETE /api/campaigns/:id":      {"campaign.deleted", "campaign", "id"},
	"DELETE /api/campaigns":          {"campaign.bulk_deleted", "campaign", ""},
	"POST /api/tx":                   {"transactional_email.accepted", "transactional_email", ""},

	"POST /api/templates":            {"template.created", "template", ""},
	"POST /api/templates/:id/clone":  {"template.cloned", "template", "id"},
	"PUT /api/templates/:id":         {"template.updated", "template", "id"},
	"PUT /api/templates/:id/default": {"template.default_changed", "template", "id"},
	"DELETE /api/templates/:id":      {"template.deleted", "template", "id"},

	"PUT /api/profile/smtp":                              {"smtp.personal_updated", "smtp", ""},
	"DELETE /api/profile/smtp/:id":                       {"smtp.personal_deleted", "smtp", "id"},
	"POST /api/profile/smtp/test":                        {"smtp.personal_tested", "smtp", ""},
	"POST /api/profile/reply-mailboxes":                  {"reply_mailbox.created", "reply_mailbox", ""},
	"PUT /api/profile/reply-mailboxes/:id":               {"reply_mailbox.updated", "reply_mailbox", "id"},
	"DELETE /api/profile/reply-mailboxes/:id":            {"reply_mailbox.disabled", "reply_mailbox", "id"},
	"PUT /api/profile/reply-mailboxes/:id/enable":        {"reply_mailbox.enabled", "reply_mailbox", "id"},
	"POST /api/profile/reply-mailboxes/test":             {"reply_mailbox.tested", "reply_mailbox", ""},
	"PUT /api/organizations/reply-forwarding/:id":        {"reply_forward_rule.updated", "reply_forward_rule", "id"},
	"DELETE /api/organizations/reply-forwarding/:id":     {"reply_forward_rule.deleted", "reply_forward_rule", "id"},
	"PUT /api/profile":                                   {"user.profile_updated", "user", ""},
	"POST /api/profile/api-keys":                         {"api_key.created", "api_key", ""},
	"PUT /api/profile/api-keys/:id":                      {"api_key.updated", "api_key", "id"},
	"POST /api/profile/api-keys/:id/rotate":              {"api_key.rotated", "api_key", "id"},
	"DELETE /api/profile/api-keys/:id":                   {"api_key.revoked", "api_key", "id"},
	"POST /api/users/:id/integration-tokens":             {"api_key.created", "api_key", "id"},
	"DELETE /api/users/:id/integration-tokens/:token_id": {"api_key.revoked", "api_key", "token_id"},

	"POST /api/users":               {"user.created", "user", ""},
	"POST /api/users/bulk":          {"user.bulk_created", "user", ""},
	"PUT /api/users/:id":            {"user.updated", "user", "id"},
	"DELETE /api/users/:id":         {"user.deleted", "user", "id"},
	"DELETE /api/users":             {"user.bulk_deleted", "user", ""},
	"GET /api/users/:id/twofa/totp": {"auth.two_factor_setup_started", "user", "id"},
	"PUT /api/users/:id/twofa":      {"auth.two_factor_enabled", "user", "id"},
	"DELETE /api/users/:id/twofa":   {"auth.two_factor_disabled", "user", "id"},

	"POST /api/roles/users":             {"role.user_created", "role", ""},
	"POST /api/roles/customer-lists":    {"role.customer_list_created", "role", ""},
	"PUT /api/roles/users/:id":          {"role.updated", "role", "id"},
	"PUT /api/roles/customer-lists/:id": {"role.updated", "role", "id"},
	"DELETE /api/roles/:id":             {"role.deleted", "role", "id"},

	"POST /api/organizations/requests":                         {"organization.request_created", "organization_request", ""},
	"DELETE /api/organizations/requests/:id":                   {"organization.request_withdrawn", "organization_request", "id"},
	"PUT /api/organizations/requests/:id":                      {"organization.request_reviewed", "organization_request", "id"},
	"POST /api/organizations/join":                             {"organization.joined", "organization", ""},
	"POST /api/organizations/leave":                            {"organization.left", "organization", ""},
	"POST /api/organizations/members":                          {"organization.member_added", "organization_member", ""},
	"POST /api/organizations":                                  {"organization.created", "organization", ""},
	"POST /api/organizations/:id/members/bulk":                 {"organization.members_bulk_added", "organization", "id"},
	"PUT /api/organizations/members/:user_id":                  {"organization.member_updated", "organization_member", "user_id"},
	"DELETE /api/organizations/members/:user_id":               {"organization.member_removed", "organization_member", "user_id"},
	"POST /api/organizations/resources/migrate":                {"organization.resources_migrated", "organization", ""},
	"POST /api/organizations/resources/customer-lists/migrate": {"organization.resources_migrated", "organization", ""},
	"POST /api/organizations/resources/transfer":               {"organization.resources_transferred", "organization", ""},
	"POST /api/organizations/:id/resources/transfer":           {"organization.resources_transferred", "organization", "id"},
	"POST /api/organizations/templates/:id/transfer":           {"organization.template_transferred", "template", "id"},
	"POST /api/organizations/templates/:id/unpublish":          {"organization.template_unpublished", "template", "id"},
	"POST /api/organizations/invites":                          {"organization.invite_created", "organization_invite", ""},
	"DELETE /api/organizations/invites/:id":                    {"organization.invite_revoked", "organization_invite", "id"},
	"POST /api/organizations/:id/archive":                      {"organization.archived", "organization", "id"},
	"DELETE /api/organizations/:id":                            {"organization.deleted", "organization", "id"},

	"DELETE /api/bounces":        {"bounce.deleted", "bounce", ""},
	"DELETE /api/bounces/:id":    {"bounce.deleted", "bounce", "id"},
	"PUT /api/bounces/blocklist": {"bounce.blocklist_applied", "bounce", ""},

	"DELETE /api/maintenance/customers/:type":           {"maintenance.customers_cleaned", "maintenance", "type"},
	"DELETE /api/maintenance/analytics/:type":           {"maintenance.analytics_cleaned", "maintenance", "type"},
	"DELETE /api/maintenance/subscriptions/unconfirmed": {"maintenance.subscriptions_cleaned", "maintenance", ""},

	"POST /subscription/form":               {"subscription.created", "subscription", ""},
	"POST /api/public/subscription":         {"subscription.created", "subscription", ""},
	"POST /subscription/:campUUID/:subUUID": {"subscription.unsubscribed", "customer", "subUUID"},
	"POST /subscription/optin/:subUUID":     {"subscription.optin_confirmed", "customer", "subUUID"},
	"POST /subscription/export/:subUUID":    {"customer.data_exported", "customer", "subUUID"},
	"POST /subscription/wipe/:subUUID":      {"customer.data_erased", "customer", "subUUID"},

	"POST /admin/login":       {"auth.login", "user", ""},
	"POST /admin/login/twofa": {"auth.two_factor_verified", "user", ""},
	"POST /admin/forgot":      {"auth.password_reset_requested", "user", ""},
	"POST /admin/reset":       {"auth.password_reset", "user", ""},
	"POST /api/logout":        {"auth.logout", "user", ""},
	"POST /auth/oidc":         {"auth.oidc_started", "user", ""},
	"GET /auth/oidc":          {"auth.oidc_completed", "user", ""},
}

type auditEventRow struct {
	ID             int64     `db:"id" json:"id"`
	OccurredAt     time.Time `db:"occurred_at" json:"occurred_at"`
	OrganizationID int64     `db:"organization_id" json:"organization_id"`
	ActorType      string    `db:"actor_type" json:"actor_type"`
	ActorUserID    int       `db:"actor_user_id" json:"actor_user_id"`
	ActorTokenID   int       `db:"actor_token_id" json:"actor_token_id"`
	ActorUsername  string    `db:"actor_username" json:"actor_username"`
	ActorName      string    `db:"actor_name" json:"actor_name"`
	Action         string    `db:"action" json:"action"`
	ObjectType     string    `db:"object_type" json:"object_type"`
	ObjectID       string    `db:"object_id" json:"object_id"`
	Result         string    `db:"result" json:"result"`
	ReasonCode     string    `db:"reason_code" json:"reason_code"`
	RequestID      string    `db:"request_id" json:"request_id"`
	Metadata       auditJSON `db:"metadata" json:"metadata"`
	IP             string    `db:"ip" json:"ip"`
	UserAgent      string    `db:"user_agent" json:"user_agent"`
}

type auditJSON json.RawMessage

func (j *auditJSON) Scan(src any) error {
	if src == nil {
		*j = auditJSON(`{}`)
		return nil
	}
	switch value := src.(type) {
	case []byte:
		*j = append((*j)[:0], value...)
		return nil
	case string:
		*j = auditJSON(value)
		return nil
	default:
		return fmt.Errorf("decode audit metadata from %T", src)
	}
}

func (j auditJSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte(`{}`), nil
	}
	return []byte(j), nil
}

func (j *auditJSON) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid audit metadata JSON")
	}
	*j = append((*j)[:0], data...)
	return nil
}

func (a *App) auditMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		err := next(c)
		spec, ok := auditRouteSpecForContext(c)
		if !ok || a.audit == nil {
			return err
		}
		if action, ok := c.Get(auditContextAction).(string); ok && action != "" {
			spec.action = action
		}

		status := c.Response().Status
		if err != nil {
			status = http.StatusInternalServerError
			if httpErr, ok := err.(*echo.HTTPError); ok {
				status = httpErr.Code
			}
		} else if status == 0 {
			status = http.StatusOK
		}
		result := "success"
		reason := ""
		if override, ok := c.Get(auditContextResult).(string); ok && (override == "success" || override == "failed" || override == "denied") {
			result = override
			if value, ok := c.Get(auditContextReasonCode).(string); ok {
				reason = value
			}
		} else if status >= http.StatusBadRequest {
			result = "failed"
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				result = "denied"
			}
			reason = fmt.Sprintf("http_%d", status)
		}

		actorType, actorUserID, actorTokenID := auditActor(c)
		metadata := map[string]any{
			"http_method": c.Request().Method,
			"http_status": status,
			"route":       c.Path(),
		}
		objectID := ""
		if spec.objectParam != "" {
			objectID = c.Param(spec.objectParam)
		}
		if override, ok := c.Get(auditContextObjectID).(string); ok && override != "" {
			objectID = override
		}
		if extra, ok := c.Get(auditContextMetadata).(map[string]any); ok {
			for key, value := range extra {
				metadata[key] = value
			}
		}
		if user, ok := c.Get(auth.UserHTTPCtxKey).(auth.User); ok && user.ID > 0 {
			actorDetails := map[string]any{}
			if name := strings.TrimSpace(user.Name); name != "" {
				actorDetails["name"] = auditDetailText(name)
			}
			if username := strings.TrimSpace(user.Username); username != "" {
				actorDetails["username"] = auditDetailText(username)
			}
			if token, ok := auth.GetIntegrationTokenContext(c); ok {
				if tokenName := strings.TrimSpace(token.Name); tokenName != "" {
					actorDetails["token_name"] = auditDetailText(tokenName)
				}
			}
			if len(actorDetails) > 0 {
				metadata["actor_details"] = actorDetails
			}
		}
		event := auditlog.Event{
			OrganizationID: a.auditOrganizationID(c),
			ActorType:      actorType,
			ActorUserID:    actorUserID,
			ActorTokenID:   actorTokenID,
			Action:         spec.action,
			ObjectType:     spec.objectType,
			ObjectID:       objectID,
			Result:         result,
			ReasonCode:     reason,
			RequestID:      auditRequestID(c),
			Metadata:       metadata,
			IP:             c.RealIP(),
			UserAgent:      c.Request().UserAgent(),
		}
		if recordErr := a.audit.Record(context.Background(), event); recordErr != nil {
			a.log.Printf("error recording audit event %s: %v", spec.action, recordErr)
		}
		return err
	}
}

func auditRequestID(c echo.Context) string {
	if requestID := strings.TrimSpace(c.Request().Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	if requestID, ok := c.Get("audit_request_id").(string); ok && requestID != "" {
		return requestID
	}
	requestID, err := uuid.NewV4()
	if err != nil {
		return ""
	}
	value := requestID.String()
	c.Set("audit_request_id", value)
	c.Response().Header().Set("X-Request-ID", value)
	return value
}

func auditRouteSpecForContext(c echo.Context) (auditRouteSpec, bool) {
	path := c.Path()
	if path == "" {
		path = c.Request().URL.Path
	}
	spec, ok := auditRoutes[c.Request().Method+" "+path]
	return spec, ok
}

func auditActor(c echo.Context) (string, *int, *int) {
	if actorType, ok := c.Get(auditContextActorType).(string); ok && actorType != "" {
		var actorUserID, actorTokenID *int
		if value, ok := auditContextInt(c, auditContextActorUserID); ok && value > 0 {
			actorUserID = &value
		}
		if value, ok := auditContextInt(c, auditContextActorTokenID); ok && value > 0 {
			actorTokenID = &value
		}
		return actorType, actorUserID, actorTokenID
	}
	user, authenticated := c.Get(auth.UserHTTPCtxKey).(auth.User)
	if !authenticated {
		return "anonymous", nil, nil
	}
	userID := user.ID
	if token, ok := auth.GetIntegrationTokenContext(c); ok {
		tokenID := token.ID
		return "api_key", &userID, &tokenID
	}
	return "user", &userID, nil
}

func (a *App) auditOrganizationID(c echo.Context) *int64 {
	if value, ok := auditContextInt64(c, auditContextOrganizationID); ok {
		return &value
	}
	if raw := strings.TrimSpace(c.Request().Header.Get(workspaceHeader)); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			return &id
		}
	}
	if token, ok := auth.GetIntegrationTokenContext(c); ok && token.WorkspaceOrganizationID.Valid && token.WorkspaceOrganizationID.Int > 0 {
		id := int64(token.WorkspaceOrganizationID.Int)
		return &id
	}
	if _, authenticated := c.Get(auth.UserHTTPCtxKey).(auth.User); authenticated {
		if access, err := a.workspaceAccess(c); err == nil && access.OrganizationID > 0 {
			id := int64(access.OrganizationID)
			return &id
		}
	}
	return nil
}

func setAuditContext(c echo.Context, actorType string, organizationID *int64, objectID string, metadata map[string]any) {
	if actorType != "" {
		c.Set(auditContextActorType, actorType)
	}
	if organizationID != nil {
		c.Set(auditContextOrganizationID, *organizationID)
	}
	if objectID != "" {
		c.Set(auditContextObjectID, objectID)
	}
	setAuditMetadata(c, metadata)
}

func setAuditCustomerContext(c echo.Context, organizationID int, objectID string, metadata map[string]any) {
	var organization *int64
	if organizationID > 0 {
		value := int64(organizationID)
		organization = &value
	}
	setAuditContext(c, "customer", organization, objectID, metadata)
}

func setAuditAction(c echo.Context, action string) {
	if action != "" {
		c.Set(auditContextAction, action)
	}
}

func setAuditObjectID(c echo.Context, objectID string) {
	if objectID != "" {
		c.Set(auditContextObjectID, objectID)
	}
}

func setAuditOrganizationID(c echo.Context, organizationID int) {
	if organizationID < 0 {
		return
	}
	c.Set(auditContextOrganizationID, int64(organizationID))
}

func setAuditOutcome(c echo.Context, result, reasonCode string) {
	if result != "" {
		c.Set(auditContextResult, result)
	}
	if reasonCode != "" {
		c.Set(auditContextReasonCode, reasonCode)
	}
}

func setAuditMetadata(c echo.Context, metadata map[string]any) {
	if len(metadata) == 0 {
		return
	}
	current, _ := c.Get(auditContextMetadata).(map[string]any)
	if current == nil {
		current = make(map[string]any, len(metadata))
		c.Set(auditContextMetadata, current)
	}
	for key, value := range metadata {
		current[key] = value
	}
}

func auditContextInt(c echo.Context, key string) (int, bool) {
	value, ok := auditContextInt64(c, key)
	return int(value), ok
}

func auditContextInt64(c echo.Context, key string) (int64, bool) {
	switch value := c.Get(key).(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case *int64:
		if value != nil {
			return *value, true
		}
	}
	return 0, false
}

func (a *App) recordBackgroundAudit(actorType, action, objectType, objectID string, organizationID *int64, metadata map[string]any) {
	a.recordBackgroundAuditResult(actorType, action, objectType, objectID, organizationID, "success", "", metadata)
}

func (a *App) recordBackgroundAuditResult(actorType, action, objectType, objectID string, organizationID *int64, result, reasonCode string, metadata map[string]any) {
	if a == nil || a.audit == nil {
		return
	}
	if actorType == "" {
		actorType = "system"
	}
	if err := a.audit.Record(context.Background(), auditlog.Event{
		OrganizationID: organizationID,
		ActorType:      actorType,
		Action:         action,
		ObjectType:     objectType,
		ObjectID:       objectID,
		Result:         result,
		ReasonCode:     reasonCode,
		Metadata:       metadata,
	}); err != nil {
		a.log.Printf("error recording background audit event %s: %v", action, err)
	}
}

func (a *App) GetAuditEvents(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return echo.NewHTTPError(http.StatusBadRequest, "page is too large")
	}
	limit, _ := strconv.Atoi(c.QueryParam("per_page"))
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	whereSQL, args := auditEventWhere(c, access.OrganizationID)
	var total int
	if err := a.db.Get(&total, "SELECT COUNT(*) FROM audit_events WHERE "+whereSQL, args...); err != nil {
		return err
	}
	args = append(args, limit, offset)
	query := `SELECT audit_events.id, audit_events.occurred_at,
		COALESCE(audit_events.organization_id, 0) AS organization_id,
		audit_events.actor_type, COALESCE(audit_events.actor_user_id, 0) AS actor_user_id,
		COALESCE(audit_events.actor_token_id, 0) AS actor_token_id,
		COALESCE(audit_actor.username, '') AS actor_username,
		COALESCE(audit_actor.name, '') AS actor_name, audit_events.action,
		audit_events.object_type, audit_events.object_id, audit_events.result,
		audit_events.reason_code, audit_events.request_id, audit_events.metadata,
		COALESCE(audit_events.ip::TEXT, '') AS ip, audit_events.user_agent
		FROM audit_events
		LEFT JOIN users audit_actor ON audit_actor.id = audit_events.actor_user_id
		WHERE ` + whereSQL + ` ORDER BY audit_events.occurred_at DESC, audit_events.id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows := []auditEventRow{}
	if err := a.db.Select(&rows, query, args...); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{models.PageResults{
		Results: rows,
		Total:   total,
		Page:    page,
		PerPage: limit,
	}})
}

// ExportAuditEvents streams either the selected audit rows or all rows matching
// the current filters. It deliberately does not apply the list endpoint's
// page/per_page window, so a full export remains complete while the browser
// query remains bounded by server-side pagination.
func (a *App) ExportAuditEvents(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}

	scope := strings.ToLower(strings.TrimSpace(c.QueryParam("scope")))
	if scope == "" {
		if len(c.QueryParams()["ids"]) > 0 {
			scope = "selected"
		} else {
			scope = "all"
		}
	}
	if scope != "selected" && scope != "all" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid audit export scope")
	}
	ids, err := auditExportIDs(c)
	if err != nil {
		return err
	}
	if scope == "selected" && len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no audit events selected")
	}
	if scope == "all" && len(ids) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "selected IDs require selected export scope")
	}

	whereSQL, args := auditEventWhere(c, access.OrganizationID)
	if scope == "selected" {
		args = append(args, pq.Array(ids))
		whereSQL += fmt.Sprintf(" AND id = ANY($%d)", len(args))
	}
	query := `SELECT id, occurred_at, COALESCE(organization_id, 0) AS organization_id,
		actor_type, COALESCE(actor_user_id, 0) AS actor_user_id,
		COALESCE(actor_token_id, 0) AS actor_token_id, action, object_type,
		object_id, result, reason_code, request_id, metadata,
		COALESCE(ip::TEXT, '') AS ip, user_agent
		FROM audit_events WHERE ` + whereSQL + ` ORDER BY occurred_at DESC, id DESC`
	rows, err := a.db.QueryxContext(c.Request().Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	filename := fmt.Sprintf("audit-events-%s-%s.csv", scope, time.Now().UTC().Format("20060102-150405"))
	hdr := c.Response().Header()
	hdr.Set(echo.HeaderContentType, "text/csv; charset=utf-8")
	hdr.Set(echo.HeaderContentDisposition, mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")

	writer := csv.NewWriter(c.Response())
	if err := writer.Write(auditCSVHeader()); err != nil {
		return err
	}
	for rows.Next() {
		var row auditEventRow
		if err := rows.StructScan(&row); err != nil {
			return fmt.Errorf("scan audit export row: %w", err)
		}
		if err := writer.Write(auditCSVRecord(row)); err != nil {
			return fmt.Errorf("write audit export row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read audit export rows: %w", err)
	}
	writer.Flush()
	return writer.Error()
}

func auditEventWhere(c echo.Context, organizationID int) (string, []any) {
	where := []string{"COALESCE(organization_id, 0) = $1"}
	args := []any{organizationID}
	addFilter := func(value, clause string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, strings.TrimSpace(value))
			where = append(where, fmt.Sprintf(clause, len(args)))
		}
	}
	addFilter(c.QueryParam("action"), "action = $%d")
	addFilter(c.QueryParam("result"), "result = $%d")
	addFilter(c.QueryParam("object_type"), "object_type = $%d")
	addFilter(c.QueryParam("object_id"), "object_id = $%d")
	return strings.Join(where, " AND "), args
}

func auditExportIDs(c echo.Context) ([]int64, error) {
	const maxSelectedAuditEvents = 1000
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, raw := range c.QueryParams()["ids"] {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id < 1 {
				return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid audit event id")
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			if len(ids) > maxSelectedAuditEvents {
				return nil, echo.NewHTTPError(http.StatusBadRequest, "too many audit events selected")
			}
		}
	}
	return ids, nil
}

func auditCSVHeader() []string {
	return []string{"id", "occurred_at", "organization_id", "actor_type", "actor_user_id", "actor_token_id", "action", "object_type", "object_id", "result", "reason_code", "request_id", "metadata", "ip", "user_agent"}
}

func auditCSVRecord(row auditEventRow) []string {
	metadata := string(row.Metadata)
	if metadata == "" {
		metadata = "{}"
	}
	return []string{
		strconv.FormatInt(row.ID, 10),
		row.OccurredAt.UTC().Format(time.RFC3339Nano),
		strconv.FormatInt(row.OrganizationID, 10),
		row.ActorType,
		auditCSVInt(row.ActorUserID),
		auditCSVInt(row.ActorTokenID),
		row.Action,
		row.ObjectType,
		row.ObjectID,
		row.Result,
		row.ReasonCode,
		row.RequestID,
		metadata,
		row.IP,
		row.UserAgent,
	}
}

func auditCSVInt(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func (a *App) GetAuditEvent(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid audit event id")
	}
	var row auditEventRow
	err = a.db.Get(&row, `SELECT audit_events.id, audit_events.occurred_at,
		COALESCE(audit_events.organization_id, 0) AS organization_id,
		audit_events.actor_type, COALESCE(audit_events.actor_user_id, 0) AS actor_user_id,
		COALESCE(audit_events.actor_token_id, 0) AS actor_token_id,
		COALESCE(audit_actor.username, '') AS actor_username,
		COALESCE(audit_actor.name, '') AS actor_name, audit_events.action,
		audit_events.object_type, audit_events.object_id, audit_events.result,
		audit_events.reason_code, audit_events.request_id, audit_events.metadata,
		COALESCE(audit_events.ip::TEXT, '') AS ip, audit_events.user_agent
		FROM audit_events
		LEFT JOIN users audit_actor ON audit_actor.id = audit_events.actor_user_id
		WHERE audit_events.id=$1 AND COALESCE(audit_events.organization_id, 0)=$2`, id, access.OrganizationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "audit event not found")
		}
		return err
	}
	return c.JSON(http.StatusOK, okResp{row})
}
