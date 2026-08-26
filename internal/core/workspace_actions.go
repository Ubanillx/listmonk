package core

import (
	"context"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// CloneCampaignForWorkspace creates an independent draft in target. The
// caller is responsible for checking read access to source and membership in
// target; keeping the database work here makes the empty-recipient guarantee
// independent of every frontend client.
func (c *Core) CloneCampaignForWorkspace(sourceID int, target models.WorkspaceAccess, name string) (models.Campaign, error) {
	source, err := c.GetCampaign(sourceID, "", "")
	if err != nil {
		return models.Campaign{}, err
	}
	if strings.TrimSpace(name) != "" {
		source.Name = strings.TrimSpace(name)
	}

	// A campaign clone always gets an independent template snapshot. Besides
	// satisfying the private-template rule, this keeps a cloned campaign's
	// layout and inline-media relationships valid after a shared source template
	// is edited or deleted.
	newTemplateID := null.Int{}

	newUUID, err := uuid.NewV4()
	if err != nil {
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	targetScope := ApplyWorkspaceScope(target, models.ResourceVisibilityPrivate)
	headers := cloneCampaignHeaders(source.Headers)

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.Campaign{}, workspaceQueryError("starting campaign clone", err)
	}
	defer tx.Rollback()

	mediaCopies, err := c.cloneCampaignMedia(tx, sourceID, source.TemplateID, targetScope)
	if err != nil {
		return models.Campaign{}, err
	}

	if source.TemplateID.Valid {
		var templateID int
		if err := tx.Get(&templateID, `
			INSERT INTO templates (
				name, type, subject, body, body_source, is_default,
				organization_id, owner_user_id, original_owner_user_id, visibility
			)
			SELECT name || ' (copy)', type, subject, body, body_source, FALSE,
				$2, $3, $4, 'private'
			FROM templates WHERE id = $1
			RETURNING id`, int(source.TemplateID.Int), targetScope.OrganizationID,
			targetScope.OwnerUserID, targetScope.OriginalOwnerUserID); err != nil {
			return models.Campaign{}, workspaceQueryError("copying campaign template", err)
		}
		var sourceMedia []mediaAssociation
		if err := tx.Select(&sourceMedia, `
			SELECT media_id, filename FROM template_media WHERE template_id = $1`, int(source.TemplateID.Int)); err != nil {
			return models.Campaign{}, workspaceQueryError("reading template media", err)
		}
		for _, association := range sourceMedia {
			if err := insertMediaAssociation(tx, "template_media", "template_id", templateID, association, mediaCopies); err != nil {
				return models.Campaign{}, err
			}
		}
		newTemplateID = null.Int{Int: templateID, Valid: true}
	}

	var newID int
	if err := tx.Get(&newID, `
		INSERT INTO campaigns (
			uuid, type, name, subject, from_email, body, body_source, altbody,
			content_type, send_at, headers, attribs, status, daily_send_limit,
			daily_resume_time, tags, messenger, template_id, to_send, sent,
			max_subscriber_id, last_subscriber_id, archive, archive_slug,
			archive_template_id, archive_meta, auto_track_links,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES (
			$1, $2, $3, $4, '', $5, $6, $7,
			$8, NULL, $9, $10, 'draft', $11,
			$12, $13, $14, $15, 0, 0,
			0, 0, FALSE, NULL,
			NULL, '{}'::JSONB, $16,
			$17, $18, $19, 'private'
		) RETURNING id`,
		newUUID, source.Type, source.Name, source.Subject, source.Body,
		source.BodySource, source.AltBody, source.ContentType, headers,
		source.Attribs, source.DailySendLimit, source.DailyResumeTime,
		pq.StringArray(normalizeTags(source.Tags)), source.Messenger, newTemplateID,
		source.AutoTrackLinks, targetScope.OrganizationID, targetScope.OwnerUserID,
		targetScope.OriginalOwnerUserID); err != nil {
		return models.Campaign{}, workspaceQueryError("creating campaign clone", err)
	}

	// Campaign media records carry the original filenames used by CID/MIME
	// attachment assembly. Include template media as a snapshot too, so a
	// subsequent source-template deletion cannot remove inline image parts.
	var sourceMedia []mediaAssociation
	if err := tx.Select(&sourceMedia, `
		SELECT media_id, filename FROM campaign_media WHERE campaign_id = $1
		UNION
		SELECT media_id, filename FROM template_media WHERE template_id = $2`, sourceID, source.TemplateID); err != nil {
		return models.Campaign{}, workspaceQueryError("reading campaign media", err)
	}
	for _, association := range sourceMedia {
		if err := insertMediaAssociation(tx, "campaign_media", "campaign_id", newID, association, mediaCopies); err != nil {
			return models.Campaign{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.Campaign{}, workspaceQueryError("committing campaign clone", err)
	}
	return c.GetCampaign(newID, "", "")
}

type mediaAssociation struct {
	MediaID  null.Int `db:"media_id"`
	Filename string   `db:"filename"`
}

// CloneTemplateForWorkspace copies a readable template into the caller's
// selected workspace. Its media records are copied as well, so a later source
// media deletion cannot turn the copied template's images into broken URLs.
func (c *Core) CloneTemplateForWorkspace(sourceID int, target models.WorkspaceAccess, name, subject string) (models.Template, error) {
	source, err := c.GetTemplate(sourceID, false)
	if err != nil {
		return models.Template{}, err
	}
	clone := source.Clone(name, subject)
	targetScope := ApplyWorkspaceScope(target, models.ResourceVisibilityPrivate)

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return models.Template{}, workspaceQueryError("starting template clone", err)
	}
	defer tx.Rollback()

	var newID int
	if err := tx.Get(&newID, `
		INSERT INTO templates (
			name, type, subject, body, body_source, is_default,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES ($1, $2, $3, $4, $5, FALSE, $6, $7, $8, 'private')
		RETURNING id`, clone.Name, clone.Type, clone.Subject, clone.Body, clone.BodySource,
		targetScope.OrganizationID, targetScope.OwnerUserID, targetScope.OriginalOwnerUserID); err != nil {
		return models.Template{}, workspaceQueryError("creating template clone", err)
	}

	var sourceMedia []mediaAssociation
	if err := tx.Select(&sourceMedia, `SELECT media_id, filename FROM template_media WHERE template_id = $1`, sourceID); err != nil {
		return models.Template{}, workspaceQueryError("reading template media", err)
	}
	mediaCopies := make(map[int]int, len(sourceMedia))
	for _, association := range sourceMedia {
		if !association.MediaID.Valid {
			continue
		}
		sourceMediaID := int(association.MediaID.Int)
		if _, exists := mediaCopies[sourceMediaID]; exists {
			continue
		}
		copyID, err := cloneMediaRecord(tx, sourceMediaID, targetScope)
		if err != nil {
			return models.Template{}, err
		}
		mediaCopies[sourceMediaID] = copyID
	}
	for _, association := range sourceMedia {
		if err := insertMediaAssociation(tx, "template_media", "template_id", newID, association, mediaCopies); err != nil {
			return models.Template{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.Template{}, workspaceQueryError("committing template clone", err)
	}
	return c.GetTemplate(newID, false)
}

// cloneCampaignMedia snapshots every still-present media row associated with
// the source campaign or its template. A single physical file may safely back
// several records; media deletion checks remaining filename references before
// deleting the store object.
func (c *Core) cloneCampaignMedia(tx *sqlx.Tx, campaignID int, templateID null.Int, target models.ResourceScope) (map[int]int, error) {
	var sourceIDs []int
	if err := tx.Select(&sourceIDs, `
		SELECT DISTINCT media_id FROM (
			SELECT media_id FROM campaign_media WHERE campaign_id = $1 AND media_id IS NOT NULL
			UNION
			SELECT media_id FROM template_media WHERE template_id = $2 AND media_id IS NOT NULL
		) source_media`, campaignID, templateID); err != nil {
		return nil, workspaceQueryError("reading campaign media", err)
	}
	clones := make(map[int]int, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		newID, err := cloneMediaRecord(tx, sourceID, target)
		if err != nil {
			return nil, err
		}
		clones[sourceID] = newID
	}
	return clones, nil
}

func cloneMediaRecord(tx *sqlx.Tx, sourceID int, target models.ResourceScope) (int, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		return 0, err
	}
	var newID int
	if err := tx.Get(&newID, `
		INSERT INTO media (
			uuid, provider, filename, content_type, thumb, meta,
			organization_id, owner_user_id, original_owner_user_id, visibility
		)
		SELECT $2, provider, filename, content_type, thumb, meta,
			$3, $4, $5, 'private'
		FROM media WHERE id = $1
		RETURNING id`, sourceID, uu, target.OrganizationID, target.OwnerUserID, target.OriginalOwnerUserID); err != nil {
		return 0, workspaceQueryError("copying media", err)
	}
	return newID, nil
}

func insertMediaAssociation(tx *sqlx.Tx, table, parentColumn string, parentID int, source mediaAssociation, copies map[int]int) error {
	if table != "campaign_media" && table != "template_media" {
		return echo.NewHTTPError(http.StatusInternalServerError, "unknown media association")
	}
	if parentColumn != "campaign_id" && parentColumn != "template_id" {
		return echo.NewHTTPError(http.StatusInternalServerError, "unknown media association parent")
	}
	var mediaID any
	if source.MediaID.Valid {
		copiedID, ok := copies[int(source.MediaID.Int)]
		if !ok {
			// The original binary disappeared before cloning. Keep the historical
			// filename row, which matches the behavior of existing associations.
			mediaID = nil
		} else {
			mediaID = copiedID
		}
	}
	stmt := "INSERT INTO " + table + " (" + parentColumn + ", media_id, filename) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING"
	if _, err := tx.Exec(stmt, parentID, mediaID, source.Filename); err != nil {
		return workspaceQueryError("copying media association", err)
	}
	return nil
}

func cloneCampaignHeaders(headers models.Headers) models.Headers {
	out := make(models.Headers, 0, len(headers))
	for _, header := range headers {
		copyHeader := make(map[string]string, len(header))
		for key, value := range header {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "from", "reply-to", "sender", "return-path":
				continue
			}
			copyHeader[key] = value
		}
		if len(copyHeader) > 0 {
			out = append(out, copyHeader)
		}
	}
	return out
}
