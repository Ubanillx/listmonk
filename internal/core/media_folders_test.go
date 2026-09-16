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
