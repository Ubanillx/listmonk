package core

import (
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"gopkg.in/volatiletech/null.v6"
)

func TestNormalizeMediaFolderName(t *testing.T) {
	if got, err := normalizeMediaFolderName("  Product images  "); err != nil || got != "Product images" {
		t.Fatalf("normalized folder name = %q, error = %v; want %q", got, err, "Product images")
	}

	for _, name := range []string{"", ".", "..", `product/images`, "bad\tname", strings.Repeat("x", maxMediaFolderNameLength+1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeMediaFolderName(name); err == nil {
				t.Fatalf("normalizeMediaFolderName(%q) accepted an invalid name", name)
			}
		})
	}
}

func TestMediaFolderWorkspaceBoundaries(t *testing.T) {
	orgAccess := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7},
		UserID:    10,
	}
	personalAccess := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    10,
	}
	orgFolder := media.MediaFolder{OrganizationID: null.IntFrom(7)}
	otherOrgFolder := media.MediaFolder{OrganizationID: null.IntFrom(8)}
	personalFolder := media.MediaFolder{OwnerUserID: null.IntFrom(10)}
	otherPersonalFolder := media.MediaFolder{OwnerUserID: null.IntFrom(11)}

	if !mediaFolderMatchesSelectedWorkspace(orgAccess, orgFolder) || mediaFolderMatchesSelectedWorkspace(orgAccess, otherOrgFolder) {
		t.Fatal("organization folder matching crossed the organization boundary")
	}
	if !mediaFolderMatchesSelectedWorkspace(personalAccess, personalFolder) || mediaFolderMatchesSelectedWorkspace(personalAccess, otherPersonalFolder) {
		t.Fatal("personal folder matching crossed the owner boundary")
	}
	if !sameMediaFolderWorkspace(orgFolder, orgFolder) || sameMediaFolderWorkspace(orgFolder, personalFolder) {
		t.Fatal("folder workspace comparison returned an unexpected result")
	}

	orgScope := models.ResourceScope{OrganizationID: null.IntFrom(7)}
	personalScope := models.ResourceScope{OwnerUserID: null.IntFrom(10)}
	if !mediaFolderMatchesResourceScope(orgScope, orgFolder) || mediaFolderMatchesResourceScope(orgScope, personalFolder) {
		t.Fatal("organization resource scope matching returned an unexpected result")
	}
	if !mediaFolderMatchesResourceScope(personalScope, personalFolder) || mediaFolderMatchesResourceScope(personalScope, otherPersonalFolder) {
		t.Fatal("personal resource scope matching returned an unexpected result")
	}
}

func TestMediaFolderWorkspacePredicateScopesActivePlatformAdmin(t *testing.T) {
	organizationAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, PlatformAdmin: true},
		UserID:    10,
	}
	organizationPredicate, organizationArgs := mediaFolderWorkspacePredicate(organizationAdmin, "f", 3)
	if len(organizationArgs) != 1 || organizationArgs[0] != 7 || organizationPredicate != "f.organization_id = $3" {
		t.Fatalf("organization folder predicate = %q %#v, want selected organization scope", organizationPredicate, organizationArgs)
	}

	personalAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true, PlatformAdmin: true},
		UserID:    10,
	}
	personalPredicate, personalArgs := mediaFolderWorkspacePredicate(personalAdmin, "f", 2)
	if len(personalArgs) != 1 || personalArgs[0] != 10 ||
		personalPredicate != "f.organization_id IS NULL AND f.owner_user_id = $2" {
		t.Fatalf("personal folder predicate = %q %#v, want caller-owned personal scope", personalPredicate, personalArgs)
	}

	archivedAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, PlatformAdmin: true, Archived: true},
		UserID:    10,
	}
	archivedPredicate, archivedArgs := mediaFolderWorkspacePredicate(archivedAdmin, "f", 1)
	if archivedPredicate != "TRUE" || len(archivedArgs) != 0 {
		t.Fatalf("archived admin folder predicate = %q %#v, want broad cleanup visibility", archivedPredicate, archivedArgs)
	}
}

func TestWorkspaceMediaReadPredicateScopesActivePlatformAdmin(t *testing.T) {
	organizationAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, PlatformAdmin: true},
		UserID:    10,
	}
	organizationPredicate, organizationArgs := workspaceMediaReadPredicate(organizationAdmin, "m", 3)
	if len(organizationArgs) != 1 || organizationArgs[0] != 7 {
		t.Fatalf("organization predicate args = %#v, want [7]", organizationArgs)
	}
	if !strings.Contains(organizationPredicate, "m.organization_id = $3") ||
		strings.Contains(organizationPredicate, "m.organization_id IS NULL AND") {
		t.Fatalf("organization predicate = %q, want selected organization scope", organizationPredicate)
	}

	personalAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true, PlatformAdmin: true},
		UserID:    10,
	}
	personalPredicate, personalArgs := workspaceMediaReadPredicate(personalAdmin, "m", 2)
	if len(personalArgs) != 1 || personalArgs[0] != 10 {
		t.Fatalf("personal predicate args = %#v, want [10]", personalArgs)
	}
	if !strings.Contains(personalPredicate, "m.organization_id IS NULL") ||
		!strings.Contains(personalPredicate, "m.owner_user_id = $2") {
		t.Fatalf("personal predicate = %q, want caller-owned personal scope", personalPredicate)
	}

	archivedAdmin := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: 7, PlatformAdmin: true, Archived: true},
		UserID:    10,
	}
	archivedPredicate, archivedArgs := workspaceMediaReadPredicate(archivedAdmin, "m", 1)
	if archivedPredicate != "TRUE" || len(archivedArgs) != 0 {
		t.Fatalf("archived admin predicate = %q %#v, want broad cleanup visibility", archivedPredicate, archivedArgs)
	}
}
