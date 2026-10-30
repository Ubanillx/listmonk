package core

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// Apply the same allowlisted status predicate to pages, exports and bulk IDs.
// Unsubscribe is membership state, independent of the customer's blocklist.
func customerStatusPredicate(access models.WorkspaceAccess, alias, listsArg string, filters []string) (string, error) {
	status := ""
	if len(filters) > 0 {
		status = strings.TrimSpace(filters[0])
	}
	if status == "" || status == "all" {
		return "TRUE", nil
	}
	if status == "blocklisted" || status == "disabled" {
		return alias + ".status='" + status + "'", nil
	}
	if status != "enabled" && status != "unsubscribed" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid customer contact status")
	}
	listState := "ul.transfer_pending_at IS NULL"
	if access.PlatformAdmin || access.IsOrganizationManager() {
		listState = "TRUE"
	}
	unsubscribed := fmt.Sprintf(`EXISTS(SELECT 1 FROM customer_list_memberships us
		JOIN customer_lists ul ON ul.id=us.customer_list_id
		WHERE us.customer_id=%s.id AND us.status='unsubscribed'
		AND ul.owner_user_id IS NOT DISTINCT FROM %s.owner_user_id
		AND ul.organization_id IS NOT DISTINCT FROM %s.organization_id AND (%s)
		AND (CARDINALITY(%s::INT[])=0 OR ul.id=ANY(%s::INT[])))`, alias, alias, alias, listState, listsArg, listsArg)
	if status == "unsubscribed" {
		return unsubscribed, nil
	}
	return alias + ".status='enabled' AND NOT " + unsubscribed, nil
}

func poolUnsubscribedPredicate(organizationID int64) string {
	where := ""
	if organizationID > 0 {
		where = fmt.Sprintf(" AND ue.organization_id=%d", organizationID)
	}
	return `EXISTS(SELECT 1 FROM org_pool_allocation_exclusions ue
		WHERE ue.pool_id=pm.pool_id AND ue.contact_id=pc.id AND ue.restored_at IS NULL
		AND ue.source='unsubscribe'` + where + `)`
}

func poolContactStatusPredicate(status, unsubscribed string) (string, error) {
	switch status {
	case "", "all", "removed":
		return "TRUE", nil
	case "active":
		return "pc.status='active' AND NOT " + unsubscribed, nil
	case "blocklisted", "archived":
		return "pc.status='" + status + "'", nil
	case "unsubscribed":
		return unsubscribed, nil
	default:
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid pool contact status")
	}
}
