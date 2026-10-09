package main

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// linkedMediaAttachment must only be called after campaign/template/workspace
// authorization. The opaque link delegates access to this file to recipients;
// it does not grant access to any other file or the media-library API.
func (s *store) linkedMediaAttachment(m media.Media) (models.Attachment, error) {
	id, filename, contentType := m.ID, m.Filename, m.ContentType
	a := models.Attachment{
		Name: filename, MediaID: id,
		Header:    manager.MakeAttachmentHeader(filename, "base64", contentType),
		SourceURL: personalMediaSourceURL(id, filename),
	}
	// Keep the generic messenger contract for non-email integrations and check
	// provider availability before queueing a message. Email preparation removes
	// these bytes from every outgoing MIME part.
	blob, err := s.media.GetBlob(s.media.GetURL(filename))
	if err != nil {
		return a, err
	}
	a.Content = blob
	if s.db == nil {
		return a, nil
	}
	token, err := uuid.NewV4()
	if err != nil {
		return a, err
	}
	var issued string
	err = s.db.Get(&issued, `
		INSERT INTO email_media_links AS old (media_id, token, filename, media_uuid, organization_id, owner_user_id)
		SELECT id, $3::uuid, filename, uuid, organization_id, owner_user_id FROM media
		WHERE id=$1 AND filename=$2 AND transfer_pending_at IS NULL
			AND uuid=$4::uuid AND organization_id IS NOT DISTINCT FROM $5::integer
			AND owner_user_id IS NOT DISTINCT FROM $6::integer
		ON CONFLICT (media_id) DO UPDATE SET
			token=CASE WHEN old.filename=EXCLUDED.filename AND old.media_uuid=EXCLUDED.media_uuid
				AND old.organization_id IS NOT DISTINCT FROM EXCLUDED.organization_id
				AND old.owner_user_id IS NOT DISTINCT FROM EXCLUDED.owner_user_id
				THEN old.token ELSE EXCLUDED.token END,
			filename=EXCLUDED.filename, media_uuid=EXCLUDED.media_uuid,
			organization_id=EXCLUDED.organization_id, owner_user_id=EXCLUDED.owner_user_id
		RETURNING token`, id, filename, token.String(), m.UUID, m.OrganizationID, m.OwnerUserID)
	if err != nil {
		return a, fmt.Errorf("error issuing media link %d: %w", id, err)
	}
	a.DeliveryURL = "/email-media/" + issued + "/" + url.PathEscape(filename)
	return a, nil
}

// ServeEmailMedia allows mail clients (including image proxies) to load exactly
// one delegated file without a session, even when public archives are disabled.
func (a *App) ServeEmailMedia(c echo.Context) error {
	token, err := uuid.FromString(c.Param("token"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	var filename string
	err = a.db.Get(&filename, `
		SELECT m.filename FROM email_media_links l JOIN media m ON m.id=l.media_id
		LEFT JOIN organizations o ON o.id=m.organization_id
		WHERE l.token=$1 AND l.filename=$2 AND m.filename=l.filename AND m.uuid=l.media_uuid
			AND m.organization_id IS NOT DISTINCT FROM l.organization_id
			AND m.owner_user_id IS NOT DISTINCT FROM l.owner_user_id
			AND m.transfer_pending_at IS NULL
			AND (m.organization_id IS NULL OR o.status='active')`, token.String(), c.Param("filename"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	return a.streamMediaBlob(c, filename)
}
