package main

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// providerFailureTarget is the per-row match shown next to a failed address.
// CanBlock is false when the caller may see the match but not act on it.
type providerFailureTarget struct {
	core.ProviderFailureMatch
	CanBlock bool `json:"can_block"`
}

type providerFailureBlocklistRequest struct {
	Email  string   `json:"email"`
	Emails []string `json:"emails"`
	Action string   `json:"action"`
	// Scope is "all", "private" or "pool". It is the explicit second step of
	// the dialog: one failed address can resolve to both kinds of target and
	// the operator chooses which blocklist to change.
	Scope string `json:"scope"`
}

type providerFailureApplied struct {
	Email        string `json:"email"`
	Kind         string `json:"kind"`
	ID           int64  `json:"id"`
	PoolID       int64  `json:"pool_id,omitempty"`
	PoolName     string `json:"pool_name,omitempty"`
	CustomerCode string `json:"customer_code"`
	Name         string `json:"name"`
}

type providerFailureBlocklistResult struct {
	Applied   []providerFailureApplied `json:"applied"`
	Skipped   []providerFailureSkip    `json:"skipped"`
	Unmatched []string                 `json:"unmatched"`
}

type providerFailureSkip struct {
	Email  string `json:"email"`
	Kind   string `json:"kind"`
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// requireProviderBlocklistAction keeps the platform-wide delivery provider
// report readable for viewers while giving explicitly granted accounts a
// separate write capability.
func requireProviderBlocklistAction(c echo.Context) error {
	u := auth.GetUser(c)
	if !u.HasPerm(auth.PermDeliveryProviderGet) || !u.HasPerm(auth.PermDeliveryProviderBlocklist) {
		return auth.ErrPermDenied
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return nil
}

// providerFailureTargets marks every match with the capability that its own
// blocklist action would need, so the report can disable an icon without
// guessing from a global permission.
func providerFailureTargets(u auth.User, matches []core.ProviderFailureMatch) []providerFailureTarget {
	out := make([]providerFailureTarget, 0, len(matches))
	for _, match := range matches {
		canBlock := false
		switch match.Kind {
		case "private":
			canBlock = u.HasPerm(auth.PermCustomersBlocklist)
		case "pool":
			canBlock = u.HasPerm(auth.PermPoolsMasterManage)
		}
		out = append(out, providerFailureTarget{ProviderFailureMatch: match, CanBlock: canBlock})
	}
	return out
}

func normalizeBlocklistScope(scope string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "all":
		return "all", true
	case "private":
		return "private", true
	case "pool":
		return "pool", true
	}
	return "", false
}

// BlocklistProviderFailure moves every selected failed address into the target
// blocklist(s), or removes it again. The scope is explicit: the dialog first
// asks whether private customers, public-pool contacts, or both should change.
func (a *App) BlocklistProviderFailure(c echo.Context) error {
	if err := requireProviderBlocklistAction(c); err != nil {
		return err
	}
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var req providerFailureBlocklistRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	scope, ok := normalizeBlocklistScope(req.Scope)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid scope")
	}
	var blocked bool
	switch req.Action {
	case "block":
		blocked = true
	case "unblock":
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "invalid action")
	}

	rawEmails := req.Emails
	if len(rawEmails) == 0 && strings.TrimSpace(req.Email) != "" {
		rawEmails = []string{req.Email}
	}
	emails := make([]string, 0, len(rawEmails))
	seen := make(map[string]struct{}, len(rawEmails))
	for _, raw := range rawEmails {
		email := strings.TrimSpace(raw)
		parsed, parseErr := mail.ParseAddress(email)
		if parseErr != nil || !strings.EqualFold(parsed.Address, email) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid email address")
		}
		key := strings.ToLower(email)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		emails = append(emails, email)
	}
	if len(emails) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "emails is required")
	}
	if len(emails) > 500 {
		return echo.NewHTTPError(http.StatusBadRequest, "at most 500 addresses per request")
	}

	matchesByEmail, err := a.core.MatchProviderFailureEmails(access, emails)
	if err != nil {
		return err
	}

	user := auth.GetUser(c)
	result := providerFailureBlocklistResult{
		Applied: []providerFailureApplied{}, Skipped: []providerFailureSkip{}, Unmatched: []string{},
	}
	type candidate struct {
		email string
		match core.ProviderFailureMatch
	}
	var (
		privateCandidates []candidate
		poolCandidates    []candidate
	)
	for _, email := range emails {
		key := strings.ToLower(email)
		matches := matchesByEmail[key]
		if len(matches) == 0 {
			result.Unmatched = append(result.Unmatched, email)
			continue
		}
		for _, match := range matches {
			if scope != "all" && match.Kind != scope {
				continue
			}
			switch match.Kind {
			case "private":
				if !user.HasPerm(auth.PermCustomersBlocklist) {
					result.Skipped = append(result.Skipped, providerFailureSkip{Email: email, Kind: match.Kind, ID: match.ID, Reason: "no_permission"})
					continue
				}
				privateCandidates = append(privateCandidates, candidate{email: email, match: match})
			case "pool":
				if !user.HasPerm(auth.PermPoolsMasterManage) {
					result.Skipped = append(result.Skipped, providerFailureSkip{Email: email, Kind: match.Kind, ID: match.ID, Reason: "no_permission"})
					continue
				}
				poolCandidates = append(poolCandidates, candidate{email: email, match: match})
			}
		}
	}

	// One ownership query for the whole batch; the per-customer helper would
	// otherwise issue one statement per address.
	managedSet := map[int]struct{}{}
	if len(privateCandidates) > 0 {
		candidateIDs := make([]int, 0, len(privateCandidates))
		for _, item := range privateCandidates {
			candidateIDs = append(candidateIDs, int(item.match.ID))
		}
		managedIDs, err := a.core.ManagedWorkspaceCustomerIDs(access, candidateIDs)
		if err != nil {
			return err
		}
		// List-role restrictions are checked in one membership query as well.
		if len(managedIDs) > 0 && !user.IsPlatformAdmin() && !user.HasPerm(auth.PermListManageAll) {
			if len(user.ManageCustomerListIDs) == 0 {
				managedIDs = nil
			} else {
				permitted, err := a.core.HasCustomerListMembershipsInWorkspace(access, managedIDs, user.ManageCustomerListIDs)
				if err != nil {
					return err
				}
				filtered := managedIDs[:0]
				for _, id := range managedIDs {
					if permitted[id] {
						filtered = append(filtered, id)
					}
				}
				managedIDs = filtered
			}
		}
		for _, id := range managedIDs {
			managedSet[id] = struct{}{}
		}
	}

	customerIDs := make([]int, 0, len(privateCandidates))
	for _, item := range privateCandidates {
		if _, ok := managedSet[int(item.match.ID)]; !ok {
			result.Skipped = append(result.Skipped, providerFailureSkip{Email: item.email, Kind: item.match.Kind, ID: item.match.ID, Reason: "no_permission"})
			continue
		}
		customerIDs = append(customerIDs, int(item.match.ID))
		result.Applied = append(result.Applied, providerFailureApplied{
			Email: item.email, Kind: item.match.Kind, ID: item.match.ID, PoolID: item.match.PoolID,
			PoolName: item.match.PoolName, CustomerCode: item.match.CustomerCode, Name: item.match.Name,
		})
	}
	contactIDs := make([]int64, 0, len(poolCandidates))
	for _, item := range poolCandidates {
		contactIDs = append(contactIDs, item.match.ID)
		result.Applied = append(result.Applied, providerFailureApplied{
			Email: item.email, Kind: item.match.Kind, ID: item.match.ID, PoolID: item.match.PoolID,
			PoolName: item.match.PoolName, CustomerCode: item.match.CustomerCode, Name: item.match.Name,
		})
	}

	if len(customerIDs) > 0 {
		if err := a.core.SetCustomersBlocklistInWorkspace(access, customerIDs, blocked); err != nil {
			return err
		}
	}
	if len(contactIDs) > 0 {
		if _, err := a.core.SetPoolContactsBlocklist(contactIDs, blocked); err != nil {
			return err
		}
	}

	setAuditObjectDetails(c, map[string]any{
		"emails":    len(emails),
		"scope":     scope,
		"action":    req.Action,
		"applied":   len(result.Applied),
		"skipped":   len(result.Skipped),
		"unmatched": len(result.Unmatched),
		"workspace": access.OrganizationID,
	})
	return c.JSON(http.StatusOK, okResp{result})
}

// SetPoolContactsBlocklist changes the contact-level blocklist for the selected
// pool contacts. Archived contacts are untouched by the core update. The route
// serves both the aggregate pool page and a single pool list.
func (a *App) SetPoolContactsBlocklist(c echo.Context) error {
	if _, err := a.requirePoolPermission(c, auth.PermPoolsMasterManage); err != nil {
		return err
	}
	var req struct {
		ContactIDs []int64 `json:"contact_ids"`
		Blocked    bool    `json:"blocked"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if len(req.ContactIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "contact_ids is required")
	}
	changed, err := a.core.SetPoolContactsBlocklist(req.ContactIDs, req.Blocked)
	if err != nil {
		return err
	}
	setAuditMetadata(c, map[string]any{"contact_ids": req.ContactIDs, "blocked": req.Blocked, "changed": changed})
	return c.JSON(http.StatusOK, okResp{map[string]any{"changed": changed}})
}

// providerFailureMatches enriches grouped failure rows with the workspace
// targets each address resolves to. It is best-effort: a matching failure must
// not hide the provider report itself.
func (a *App) providerFailureMatches(c echo.Context, access models.WorkspaceAccess, summaries []providerFailureSummary) {
	if a.core == nil || len(summaries) == 0 {
		return
	}
	emails := make([]string, 0, len(summaries))
	for _, row := range summaries {
		emails = append(emails, row.Email)
	}
	matches, err := a.core.MatchProviderFailureEmails(access, emails)
	if err != nil {
		return
	}
	user := auth.GetUser(c)
	for i := range summaries {
		key := strings.ToLower(strings.TrimSpace(summaries[i].Email))
		summaries[i].Matches = providerFailureTargets(user, matches[key])
	}
}
