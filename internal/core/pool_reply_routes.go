package core

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/labstack/echo/v4"
)

// Reply routing addresses are optional bare email addresses. A contact may list
// several, separated by comma or semicolon, so a reply goes to every listed
// recipient. They contain no POP3 credentials and never grant mailbox
// management access.
//
// The value must stay pure printable ASCII: the SMTP layer encodes the whole
// header value as a single encoded-word as soon as one non-ASCII byte appears,
// which would invalidate every address in the list. The limit leaves room for
// the "Reply-To: " field name on one unfurled 998-byte header line.
const poolReplyToMaxLength = 988

var errInvalidPoolReplyTo = echo.NewHTTPError(http.StatusBadRequest, "invalid reply_to email address")

// normalizePoolReplyTo validates the address list and rewrites it to a single
// comma-space separated value. Empty input stays empty; duplicate addresses are
// dropped case-insensitively while the first spelling is kept.
func normalizePoolReplyTo(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > poolReplyToMaxLength || strings.ContainsAny(value, "\r\n") {
		return "", errInvalidPoolReplyTo
	}
	addresses := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '；'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := mail.ParseAddress(part)
		if err != nil || len(part) > 254 || !isPrintableASCII(part) ||
			!strings.EqualFold(parsed.Address, part) || !strings.Contains(parsed.Address, "@") {
			return "", errInvalidPoolReplyTo
		}
		key := strings.ToLower(parsed.Address)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		addresses = append(addresses, parsed.Address)
	}
	if len(addresses) == 0 {
		return "", errInvalidPoolReplyTo
	}
	normalized := strings.Join(addresses, ", ")
	if len(normalized) > poolReplyToMaxLength {
		return "", errInvalidPoolReplyTo
	}
	return normalized, nil
}

func isPrintableASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// Both snapshot writers resolve from the same two sources. A mailbox ID is
// associated only when one of the listed addresses belongs to an active mailbox
// in the target organization (receiving verification is required for AI mode,
// but not for address-only mode); an external imported Reply-To must never be
// attributed to the fallback mailbox.
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
			AND status='active' AND (NOT scan_enabled OR NOT ai_enabled OR verified_at IS NOT NULL)
			AND LOWER(email) IN (
				SELECT BTRIM(LOWER(split.part)) FROM regexp_split_to_table(route.email, '[,;，；]') AS split(part)
			)
		ORDER BY id LIMIT 1
	) route_mailbox ON TRUE`

// Used by validation and status reporting: every eligible recipient must have
// either its own reply address or the organization's available fallback.
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
