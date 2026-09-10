package main

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

// The campaign clone endpoint keeps exactly one gate: canCopyWorkspaceCampaign,
// which mirrors Core.CanCopyCampaign. A legacy `campaigns:manage` fallback used to
// follow it, written as
// `!canCopyWorkspaceCampaign(...) && !workspaceCopyException(...)`. That branch
// could never run, because the gate above it already returns 403 whenever the
// campaign policy denies the copy, so the condition was always false.
//
// The tests below record why deleting it, rather than restoring it, is the
// conservative choice: the two layers do not agree, and the campaign policy is the
// stricter of the two, so the fallback could only ever have widened copying.

// TestCampaignCopyPolicyIsStricterThanPublishedResourceException documents the one
// case where the two layers disagree. A published organization campaign is
// copyable under the generic published-resource exception, but the campaign policy
// requires ownership or a manager/admin role for it. Because the endpoint gates on
// the campaign policy, this case is denied; the dead fallback would have allowed it.
//
// If the broader behaviour is wanted, it is a product decision that has to change
// canCopyWorkspaceCampaign (one layer), not resurrect a second gate.
func TestCampaignCopyPolicyIsStricterThanPublishedResourceException(t *testing.T) {
	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7},
		UserID:    30,
	}
	// Organization-shared campaign owned by a colleague in the same organization.
	scope := permissionTestScope(7, 40, models.ResourceVisibilityOrganization, false)

	if !workspaceCopyException(member, scope) {
		t.Fatal("expected the published-resource exception to allow copying an organization shared campaign")
	}
	if canCopyWorkspaceCampaign(member, scope) {
		t.Fatal("expected the campaign policy to deny a non-owner member, which is the gate the endpoint applies")
	}
}

// TestCampaignCopyPolicyAgreesWithExceptionWhereItShould covers the cases the two
// layers do agree on, plus the campaign-only manager widening.
func TestCampaignCopyPolicyAgreesWithExceptionWhereItShould(t *testing.T) {
	var (
		member = models.WorkspaceAccess{
			Workspace: models.Workspace{OrganizationID: 7},
			UserID:    30,
		}
		manager = models.WorkspaceAccess{
			Workspace: models.Workspace{OrganizationID: 7, Role: models.OrganizationMemberRoleManager},
			UserID:    40,
		}
	)

	for _, tc := range []struct {
		name   string
		access models.WorkspaceAccess
		scope  models.ResourceScope
	}{
		{"global campaign", member, permissionTestScope(0, 0, models.ResourceVisibilityGlobal, false)},
		{"global campaign owned elsewhere", member, permissionTestScope(0, 20, models.ResourceVisibilityGlobal, false)},
		{"org shared campaign owned by the caller", member, permissionTestScope(7, 30, models.ResourceVisibilityOrganization, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !workspaceCopyException(tc.access, tc.scope) {
				t.Fatal("expected the published-resource exception to allow copying")
			}
			if !canCopyWorkspaceCampaign(tc.access, tc.scope) {
				t.Fatal("expected the campaign policy to allow copying as well")
			}
		})
	}

	// The exception covers published resources only, so a caller's own private
	// campaign is allowed by the campaign policy's owner branch and by nothing else.
	ownPrivate := permissionTestScope(7, 30, models.ResourceVisibilityPrivate, false)
	if workspaceCopyException(member, ownPrivate) {
		t.Fatal("expected the published-resource exception not to cover a private campaign")
	}
	if !canCopyWorkspaceCampaign(member, ownPrivate) {
		t.Fatal("expected the campaign policy to allow an owner to copy their own private campaign")
	}

	// A manager's inspection path is a campaign-only widening: the exception denies
	// it, the campaign policy allows it, and the endpoint applies the latter.
	managerScope := permissionTestScope(7, 30, models.ResourceVisibilityPrivate, false)
	if workspaceCopyException(manager, managerScope) {
		t.Fatal("expected the published-resource exception to deny a manager copying another member's private campaign")
	}
	if !canCopyWorkspaceCampaign(manager, managerScope) {
		t.Fatal("expected the campaign policy to allow the manager path")
	}
}

// TestCanCopyWorkspaceCampaignBoundaries pins the cases the clone endpoint relies
// on, so a future edit to the policy cannot silently widen or narrow copying.
func TestCanCopyWorkspaceCampaignBoundaries(t *testing.T) {
	var (
		platformAdmin = models.WorkspaceAccess{
			Workspace: models.Workspace{Personal: true, PlatformAdmin: true},
			UserID:    10,
		}
		orgMember = models.WorkspaceAccess{
			Workspace: models.Workspace{OrganizationID: 7},
			UserID:    30,
		}
		orgManager = models.WorkspaceAccess{
			Workspace: models.Workspace{OrganizationID: 7, Role: models.OrganizationMemberRoleManager},
			UserID:    40,
		}
		otherOrgMember = models.WorkspaceAccess{
			Workspace: models.Workspace{OrganizationID: 9},
			UserID:    50,
		}
		personalOwner = models.WorkspaceAccess{
			Workspace: models.Workspace{Personal: true},
			UserID:    20,
		}
	)

	for _, tc := range []struct {
		name   string
		access models.WorkspaceAccess
		scope  models.ResourceScope
		want   bool
	}{
		{"platform admin copies an org private campaign", platformAdmin, permissionTestScope(7, 30, models.ResourceVisibilityPrivate, false), true},
		{"member copies their own private campaign", orgMember, permissionTestScope(7, 30, models.ResourceVisibilityPrivate, false), true},
		{"member copies a colleague's private campaign", orgMember, permissionTestScope(7, 40, models.ResourceVisibilityPrivate, false), false},
		{"member copies a colleague's org shared campaign", orgMember, permissionTestScope(7, 40, models.ResourceVisibilityOrganization, false), false},
		{"manager copies a member's private campaign", orgManager, permissionTestScope(7, 30, models.ResourceVisibilityPrivate, false), true},
		{"member copies their own org shared campaign", orgMember, permissionTestScope(7, 30, models.ResourceVisibilityOrganization, false), true},
		{"member copies a global campaign", orgMember, permissionTestScope(0, 0, models.ResourceVisibilityGlobal, false), true},
		{"member of another org copies an org campaign", otherOrgMember, permissionTestScope(7, 40, models.ResourceVisibilityOrganization, false), false},
		{"owner copies a personal private campaign", personalOwner, permissionTestScope(0, 20, models.ResourceVisibilityPrivate, false), true},
		{"other user copies a personal private campaign", personalOwner, permissionTestScope(0, 30, models.ResourceVisibilityPrivate, false), false},
		{"a campaign pending transfer is never copyable", orgManager, permissionTestScope(7, 30, models.ResourceVisibilityPrivate, true), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canCopyWorkspaceCampaign(tc.access, tc.scope); got != tc.want {
				t.Fatalf("canCopyWorkspaceCampaign = %v, want %v", got, tc.want)
			}
		})
	}
}
