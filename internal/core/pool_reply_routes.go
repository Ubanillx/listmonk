package core

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/labstack/echo/v4"
)

// Imported reply routing addresses are optional, single bare email addresses.
// They contain no POP3 credentials and never grant mailbox management access.
func validatePoolReplyTo(address string) error {
	if address == "" {
		return nil
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || len(address) > 254 || strings.ContainsAny(address, "\r\n") ||
		!strings.EqualFold(parsed.Address, address) || !strings.Contains(parsed.Address, "@") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid reply_to email address")
	}
	return nil
}

// Both snapshot writers resolve from the same two sources. Mailbox IDs are
// associated only with an active, verified mailbox in the target organization;
// an external imported Reply-To must never be attributed to the fallback mailbox.
const poolReplySnapshotRouteSQL = `
	CROSS JOIN LATERAL (
		SELECT CASE WHEN chosen.priority='organization_first'
			THEN COALESCE(NULLIF(chosen.organization_reply_to,''),chosen.contact_reply_to)
			ELSE COALESCE(NULLIF(chosen.contact_reply_to,''),chosen.organization_reply_to) END AS email,
		CASE WHEN chosen.priority='organization_first' AND chosen.organization_reply_to<>'' THEN 'organization'
			WHEN chosen.contact_reply_to<>'' THEN 'contact'
			WHEN chosen.organization_reply_to<>'' THEN 'organization' ELSE '' END AS source
	) route
	LEFT JOIN LATERAL (
		SELECT id FROM reply_mailboxes WHERE organization_id=chosen.organization_id
			AND LOWER(email)=LOWER(route.email) AND status='active' AND verified_at IS NOT NULL
		ORDER BY id LIMIT 1
	) route_mailbox ON TRUE`

// Used by validation and status reporting: every eligible recipient must have
// either its own reply address or the organization's verified fallback.
const poolReplyCandidateSQL = `
	SELECT 1 FROM org_pool_allocation_members am
	JOIN pool_members pm ON pm.contact_id=am.contact_id AND pm.pool_id=s.pool_id
	JOIN pool_contacts pc ON pc.id=am.contact_id
	LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=s.pool_id
		AND ex.organization_id=s.organization_id AND ex.contact_id=pc.id AND ex.restored_at IS NULL
	WHERE am.allocation_id=s.id AND am.status='active' AND pc.status='active'
		AND ex.contact_id IS NULL`

const poolMissingContactReplySQL = `(NOT EXISTS (` + poolReplyCandidateSQL + `)
	OR EXISTS (` + poolReplyCandidateSQL + ` AND BTRIM(pc.reply_to)=''))`
