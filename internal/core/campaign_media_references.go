package core

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

type bodyMediaRow struct {
	models.ResourceScope
	ID       int    `db:"id"`
	Filename string `db:"filename"`
}

// reconcileBodyMediaTx makes authored media references part of the same locked
// mutation as the campaign body. Admin inspection rights never substitute for
// the campaign owner's send rights, and stale exact IDs never match a clone.
func (c *Core) reconcileBodyMediaTx(tx *sqlx.Tx, access models.WorkspaceAccess, scope models.ResourceScope, body string, selected []int, templateID int) ([]int, error) {
	owner := models.WorkspaceAccess{Workspace: models.Workspace{
		Personal: !scope.OrganizationID.Valid, OrganizationID: int(scope.OrganizationID.Int), Role: "member",
	}, UserID: int(scope.OwnerUserID.Int)}
	if !scope.OwnerUserID.Valid || owner.UserID < 1 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "campaign media requires a resource owner")
	}
	ids := uniqueMutationIDs(selected)
	known := map[int]bool{}
	for _, id := range ids {
		known[id] = true
	}
	loadTemplateMedia := func() (map[int]string, error) {
		out := map[int]string{}
		if templateID < 1 {
			return out, nil
		}
		var rows []struct {
			ID       int    `db:"id"`
			Filename string `db:"filename"`
		}
		stmt := `SELECT m.id,m.filename FROM template_media tm JOIN media m ON m.id=tm.media_id
			JOIN templates t ON t.id=tm.template_id WHERE t.id=$1 AND (` + workspaceTemplateMediaPredicate(owner, "t", "m") + `)`
		if err := tx.Select(&rows, stmt, templateID); err != nil {
			return nil, workspaceQueryError("reading template image dependencies", err)
		}
		for _, row := range rows {
			out[row.ID] = row.Filename
		}
		return out, nil
	}
	templateMedia, err := loadTemplateMedia()
	if err != nil {
		return nil, err
	}
	const columns = `m.id,m.filename,m.organization_id,m.owner_user_id,
		COALESCE((SELECT f.visibility FROM media_folders f WHERE f.id=m.folder_id),m.visibility) AS visibility,m.transfer_pending_at,
		m.folder_id AS media_folder_id, (m.organization_id IS NOT NULL AND NOT EXISTS
		(SELECT 1 FROM organizations o WHERE o.id=m.organization_id AND o.status='active')) AS organization_archived`
	var expected []struct {
		id       int
		filename string
	}
	for _, ref := range media.BodyReferences([]byte(body)) {
		if media.HasTemplateExpression(ref.Source) {
			continue // The SMTP guard checks the actual rendered customer value.
		}
		id, filename, protected, parseErr := media.ProtectedReference(ref.Source, c.consts.RootURL)
		if parseErr != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid body media reference")
		}
		if !protected {
			if ref.Image {
				u, err := url.Parse(ref.Source)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
					return nil, echo.NewHTTPError(http.StatusBadRequest, "body image requires an uploaded image or an absolute HTTP(S) URL")
				}
			}
			continue
		}
		var rows []bodyMediaRow
		if id > 0 {
			err = tx.Select(&rows, "SELECT "+columns+" FROM media m WHERE m.id=$1", id)
		} else {
			err = tx.Select(&rows, "SELECT "+columns+" FROM media m WHERE m.filename=$1 ORDER BY m.id", filename)
		}
		if err != nil {
			return nil, workspaceQueryError("resolving body image", err)
		}
		var usable, associated []bodyMediaRow
		for _, row := range rows {
			if (filename != "" && filename != row.Filename) || row.TransferPendingAt.Valid || row.OrganizationArchived {
				continue
			}
			if c.CanUseResource(owner, row.ResourceScope) || templateMedia[row.ID] == row.Filename {
				usable = append(usable, row)
				if known[row.ID] || templateMedia[row.ID] == row.Filename {
					associated = append(associated, row)
				}
			}
		}
		if len(associated) == 1 {
			usable = associated
		}
		if len(usable) != 1 {
			return nil, echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("body media %d is missing, ambiguous, or unavailable to the resource owner", id))
		}
		row := usable[0]
		expected = append(expected, struct {
			id       int
			filename string
		}{row.ID, row.Filename})
		if !known[row.ID] && templateMedia[row.ID] != row.Filename {
			ids = append(ids, row.ID)
			known[row.ID] = true
		}
	}
	ids = uniqueMutationIDs(ids)
	// Re-read after media locks: transfers/deletion/filename changes between
	// discovery and acquisition cannot authorize a different binary.
	templateMedia, err = loadTemplateMedia()
	if err != nil {
		return nil, err
	}
	var locked []bodyMediaRow
	checkIDs := append([]int(nil), ids...)
	for _, ref := range expected {
		checkIDs = append(checkIDs, ref.id)
	}
	checkIDs = uniqueMutationIDs(checkIDs)
	if len(checkIDs) > 0 {
		if err := tx.Select(&locked, "SELECT "+columns+" FROM media m WHERE m.id=ANY($1::INT[]) ORDER BY m.id FOR UPDATE", pq.Array(checkIDs)); err != nil {
			return nil, workspaceQueryError("checking body images under lock", err)
		}
	}
	if err := c.lockWorkspaceUsableResources(tx, access, resourceMedia, ids); err != nil {
		return nil, err
	}
	byID := map[int]bodyMediaRow{}
	var direct []int
	for _, row := range locked {
		if row.TransferPendingAt.Valid || row.OrganizationArchived || (!c.CanUseResource(owner, row.ResourceScope) && templateMedia[row.ID] != row.Filename) {
			return nil, echo.NewHTTPError(http.StatusForbidden, "body media is unavailable to the resource owner")
		}
		byID[row.ID] = row
		if templateMedia[row.ID] != row.Filename {
			direct = append(direct, row.ID)
		}
	}
	if len(byID) != len(checkIDs) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "body media no longer exists")
	}
	for _, ref := range expected {
		if byID[ref.id].Filename != ref.filename {
			return nil, echo.NewHTTPError(http.StatusConflict, "body media changed while saving")
		}
	}
	if err := c.lockMediaFolderAccess(tx, owner, direct); err != nil {
		return nil, err
	}
	return ids, nil
}

// reconcileStoredCampaignMediaTx repairs valid old references before a start
// transition. It leaves campaign status, recipients and sending history alone.
func (c *Core) reconcileStoredCampaignMediaTx(tx *sqlx.Tx, access models.WorkspaceAccess, id int) error {
	var camp models.Campaign
	if err := tx.Get(&camp, `SELECT * FROM campaigns WHERE id=$1`, id); err != nil {
		return workspaceQueryError("reading campaign images", err)
	}
	if camp.ContentType == models.CampaignContentTypePlain {
		return nil
	}
	if camp.TemplateID.Valid && camp.TemplateID.Int > 0 {
		if err := c.lockWorkspaceUsableResources(tx, access, resourceTemplates, []int{int(camp.TemplateID.Int)}); err != nil {
			return err
		}
		var templateBody string
		if err := tx.Get(&templateBody, `SELECT body FROM templates WHERE id=$1`, camp.TemplateID.Int); err != nil {
			return workspaceQueryError("reading campaign template images", err)
		}
		if camp.ContentType != models.CampaignContentTypeVisual {
			camp.Body += "\n" + templateBody
		}
	}
	var selected []int
	if err := tx.Select(&selected, `SELECT media_id FROM campaign_media WHERE campaign_id=$1 AND media_id IS NOT NULL`, id); err != nil {
		return workspaceQueryError("reading campaign media associations", err)
	}
	ids, err := c.reconcileBodyMediaTx(tx, access, camp.ResourceScope, camp.Body, selected, int(camp.TemplateID.Int))
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err := tx.Exec(`INSERT INTO campaign_media(campaign_id,media_id,filename)
			SELECT $1,id,filename FROM media WHERE id=ANY($2::INT[])
			ON CONFLICT(campaign_id,media_id) DO NOTHING`, id, pq.Array(ids)); err != nil {
			return workspaceQueryError("reconciling campaign image associations", err)
		}
	}
	return nil
}
