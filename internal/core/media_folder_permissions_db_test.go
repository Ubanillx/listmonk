package core

import (
	"io"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestMediaFolderPermissionBoundaries(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
CREATE TABLE media_folders (
 id SERIAL PRIMARY KEY, name TEXT NOT NULL, parent_id INT REFERENCES media_folders(id),
 organization_id BIGINT REFERENCES organizations(id), owner_user_id INT REFERENCES users(id),
 created_by_user_id INT, visibility TEXT NOT NULL DEFAULT 'private',
 created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE TABLE media (
 id SERIAL PRIMARY KEY, uuid TEXT DEFAULT '', filename TEXT DEFAULT '', thumb TEXT DEFAULT '',
 folder_id INT REFERENCES media_folders(id), content_type TEXT DEFAULT '', provider TEXT DEFAULT 'filesystem',
 meta JSONB DEFAULT '{}', organization_id BIGINT, owner_user_id INT, original_owner_user_id INT,
 visibility TEXT DEFAULT 'private', transfer_pending_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE TABLE templates (id INT, organization_id BIGINT, owner_user_id INT, visibility TEXT, transfer_pending_at TIMESTAMPTZ);
CREATE TABLE template_media (template_id INT, media_id INT);
CREATE TABLE campaign_media (campaign_id INT, media_id INT);
ALTER TABLE campaigns ADD COLUMN owner_user_id INT, ADD COLUMN visibility TEXT, ADD COLUMN transfer_pending_at TIMESTAMPTZ;
`)
	env.exec(`INSERT INTO users(id) VALUES(2),(3);
INSERT INTO organizations(id,name) VALUES(7,'Current'),(8,'Other');
INSERT INTO organization_members(organization_id,user_id) VALUES(7,1),(7,2),(8,3)`)
	owner := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 7}, UserID: 1}
	member := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 7}, UserID: 2}
	other := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 8}, UserID: 3}
	personal := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 3}
	for _, visibility := range []string{"private", "organization", "global"} {
		t.Run(visibility, func(t *testing.T) {
			folder, err := env.core.CreateMediaFolderInWorkspace(owner, visibility, 0, visibility)
			if err != nil {
				t.Fatal(err)
			}
			fileID := int(env.id(`INSERT INTO media(filename,folder_id,organization_id,owner_user_id)
			 VALUES($1,$2,7,1) RETURNING id`, visibility+".png", folder.ID))
			for _, test := range []struct {
				access models.WorkspaceAccess
				want   bool
			}{
				{owner, true}, {member, visibility != "private"}, {other, visibility == "global"}, {personal, visibility == "global"},
			} {
				if err := env.core.RequireReadableMediaFolder(test.access, folder.ID); (err == nil) != test.want {
					t.Fatalf("folder access for %v: err=%v want=%v", test.access, err, test.want)
				}
				if _, err := env.core.RequireUseResource(test.access, resourceMedia, fileID); (err == nil) != test.want {
					t.Fatalf("media use for %v: err=%v want=%v", test.access, err, test.want)
				}
				if _, err := env.core.GetWorkspaceMediaByID(test.access, fileID); (err == nil) != test.want {
					t.Fatalf("media URL for %v: err=%v want=%v", test.access, err, test.want)
				}
				if _, err := env.core.GetWorkspaceMediaByFilename(test.access, visibility+".png"); (err == nil) != test.want {
					t.Fatalf("legacy URL for %v: err=%v want=%v", test.access, err, test.want)
				}
				items, total, err := env.core.QueryWorkspaceMedia(test.access, "filesystem", folderPermissionTestStore{}, "", &folder.ID, 0, 20)
				if err != nil || (total > 0) != test.want {
					t.Fatalf("media listing: total=%d err=%v want=%v", total, err, test.want)
				}
				if test.want && (len(items) != 1 || items[0].Visibility != visibility) {
					t.Fatalf("folder audience missing from media DTO: %v", items)
				}
				tx, err := env.db.Beginx()
				if err != nil {
					t.Fatal(err)
				}
				err = env.core.lockWorkspaceUsableResources(tx, test.access, resourceMedia, []int{fileID})
				tx.Rollback()
				if (err == nil) != test.want {
					t.Fatalf("locked use: err=%v want=%v", err, test.want)
				}
			}
			if _, err := env.core.RenameMediaFolderInWorkspace(member, folder.ID, "stolen", "global"); err == nil {
				t.Fatal("member changed another owner's permissions")
			}
			if visibility == "private" {
				child, err := env.core.CreateMediaFolderInWorkspace(owner, "public child", folder.ID, "global")
				if err != nil {
					t.Fatal(err)
				}
				if err := env.core.RequireReadableMediaFolder(other, child.ID); err == nil {
					t.Fatal("child bypassed private parent")
				}
			}
			folders, err := env.core.QueryWorkspaceMediaFolders(member)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range folders {
				if f.ID == folder.ID && f.Manageable {
					t.Fatal("non-owner folder is manageable")
				}
			}
			if _, err := env.core.RenameMediaFolderInWorkspace(owner, folder.ID, visibility, "private"); err != nil {
				t.Fatal(err)
			}
			if _, err := env.core.GetWorkspaceMediaByID(member, fileID); err == nil {
				t.Fatal("permission revocation did not hide media")
			}
			// Revocation also restricts media uploaded by a different member:
			// media ownership cannot bypass a private directory, while its creator
			// can still browse and use the directory's contents.
			env.exec(`UPDATE media SET owner_user_id=2 WHERE id=$1`, fileID)
			if _, err := env.core.RequireUseResource(owner, resourceMedia, fileID); err != nil {
				t.Fatalf("creator cannot use folder contents: %v", err)
			}
			if _, err := env.core.RequireUseResource(member, resourceMedia, fileID); err == nil {
				t.Fatal("media owner bypassed private directory")
			}
		})
	}
	if _, err := env.core.CreateMediaFolderInWorkspace(personal, "invalid", 0, "organization"); err == nil {
		t.Fatal("personal workspace accepted organization audience")
	}
	global, err := env.core.CreateMediaFolderInWorkspace(owner, "archived-global", 0, "global")
	if err != nil {
		t.Fatal(err)
	}
	env.exec(`UPDATE organizations SET status='archived' WHERE id=7`)
	if err := env.core.RequireReadableMediaFolder(other, global.ID); err == nil {
		t.Fatal("archived organization exposed global folder")
	}
}

// Keep the store interface deterministic while exercising production SQL.
type folderPermissionTestStore struct{}

func (folderPermissionTestStore) GetURL(name string) string { return "/uploads/" + name }
func (folderPermissionTestStore) Put(name, _ string, _ io.ReadSeeker) (string, error) {
	return name, nil
}
func (folderPermissionTestStore) Delete(string) error            { return nil }
func (folderPermissionTestStore) GetBlob(string) ([]byte, error) { return nil, nil }
