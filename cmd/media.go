package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

const (
	thumbPrefix   = "thumb_"
	thumbnailSize = 250
)

var (
	vectorExts = []string{"svg"}
	imageExts  = []string{"gif", "png", "jpg", "jpeg"}
)

// UploadMedia handles media file uploads.
func (a *App) UploadMedia(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("media.invalidFile", "error", err.Error()))
	}

	// Read the file from the HTTP form.
	src, err := file.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("media.errorReadingFile", "error", err.Error()))
	}
	defer src.Close()

	var (
		// Naive check for content type and extension.
		ext         = strings.TrimPrefix(strings.ToLower(filepath.Ext(file.Filename)), ".")
		contentType = file.Header.Get("Content-Type")
	)

	// Validate file extension.
	if !inArray("*", a.cfg.MediaUpload.Extensions) {
		if ok := inArray(ext, a.cfg.MediaUpload.Extensions); !ok {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("media.unsupportedFileType", "type", ext))
		}
	}

	// Sanitize the filename.
	fName := makeFilename(file.Filename)

	// If the filename already exists in the DB, make it unique by adding a random suffix.
	if _, err := a.core.GetMedia(0, "", fName, a.media); err == nil {
		suffix, err := generateRandomString(6)
		if err != nil {
			a.log.Printf("error generating random string: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
		}

		fName = appendSuffixToFilename(fName, suffix)
	}

	// Upload the file to the media store.
	fName, err = a.media.Put(fName, contentType, src)
	if err != nil {
		a.log.Printf("error uploading file: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("media.errorUploading", "error", err.Error()))
	}

	// This keeps track of whether the file has to be deleted from the DB and the store
	// if any of the subsequent steps fail.
	var (
		cleanUp    = false
		thumbfName = ""
	)
	defer func() {
		if cleanUp {
			a.media.Delete(fName)

			if thumbfName != "" {
				a.media.Delete(thumbfName)
			}
		}
	}()

	// Thumbnail width and height.
	var width, height int

	// Create thumbnail from file for non-vector formats.
	isImage := inArray(ext, imageExts)
	if isImage {
		thumbFile, wi, he, err := processImage(file)
		if err != nil {
			cleanUp = true
			a.log.Printf("error resizing image: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError,
				a.i18n.Ts("media.errorResizing", "error", err.Error()))
		}
		width = wi
		height = he

		// Upload thumbnail.
		tf, err := a.media.Put(thumbPrefix+fName, contentType, thumbFile)
		if err != nil {
			cleanUp = true
			a.log.Printf("error saving thumbnail: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError,
				a.i18n.Ts("media.errorSavingThumbnail", "error", err.Error()))
		}
		thumbfName = tf
	}
	if inArray(ext, vectorExts) {
		thumbfName = fName
	}

	// Images have metadata.
	meta := models.JSON{}
	if isImage {
		meta = models.JSON{
			"width":  width,
			"height": height,
		}
	}

	// Insert the media into the DB.
	visibility, err := normalizeResourceVisibility(access, resourceMedia, c.FormValue("visibility"))
	if err != nil {
		cleanUp = true
		return err
	}
	scope := core.ApplyWorkspaceScope(access, visibility)
	m, err := a.core.InsertMedia(fName, thumbfName, contentType, meta, a.cfg.MediaUpload.Provider, scope, a.media)
	if err != nil {
		cleanUp = true
		return err
	}
	// Keep the immediate upload response consistent with media list responses.
	// Editors should insert the protected application route, never a direct
	// filesystem or object-store URL.
	a.setWorkspaceMediaURLs(&m)

	return c.JSON(http.StatusOK, okResp{m})
}

// GetAllMedia handles retrieval of uploaded media.
func (a *App) GetAllMedia(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var (
		query = c.FormValue("query")

		pg = a.pg.NewFromURL(c.Request().URL.Query())
	)
	// Fetch the media items from the DB.
	res, total, err := a.core.QueryWorkspaceMedia(access, a.cfg.MediaUpload.Provider, a.media, query, pg.Offset, pg.Limit)
	if err != nil {
		return err
	}
	for i := range res {
		a.setWorkspaceMediaURLs(&res[i])
	}

	out := models.PageResults{
		Results: res,
		Total:   total,
		Page:    pg.Page,
		PerPage: pg.PerPage,
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetMedia handles retrieval of a media item by ID.
func (a *App) GetMedia(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	// Fetch the media item from the DB.
	id := getID(c)
	if _, err := a.core.RequireReadResource(access, "media", id); err != nil {
		return err
	}
	out, err := a.core.GetMedia(id, "", "", a.media)
	if err != nil {
		return err
	}
	a.setWorkspaceMediaURLs(&out)

	return c.JSON(http.StatusOK, okResp{out})
}

// DeleteMedia handles deletion of uploaded media.
func (a *App) DeleteMedia(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}

	// Delete the media record first. Cloned campaign media can share its
	// provider object, so DeleteMedia tells us whether each object is now safe
	// to remove from storage.
	id := getID(c)
	if _, err := a.core.RequireManageResource(access, "media", id); err != nil {
		return err
	}
	deleted, err := a.core.DeleteMedia(id)
	if err != nil {
		return err
	}

	// Delete the files from the media store.
	if deleted.DeleteFilename {
		a.media.Delete(deleted.Filename)
	}
	if deleted.DeleteThumb {
		a.media.Delete(deleted.Thumb)
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// ServeMediaFile serves a media object by its stored filename. Browser image
// requests cannot carry the workspace header, so the active workspace cookie
// is resolved through the same predicate as the JSON API before a blob is
// returned. A filename, rather than a media ID, keeps copied campaign/template
// bodies compatible with their copied media records, which deliberately retain
// the same binary filename.
func (a *App) ServeMediaFile(c echo.Context) error {
	return a.serveWorkspaceOrPublicMedia(c, c.Param("filename"))
}

// ServeLegacyMedia protects historical /uploads URLs. New editor inserts use
// /api/media/file/:filename, but existing templates and campaign bodies may still
// contain storage URLs. A session must be able to read a matching media row;
// the only unauthenticated exception is media explicitly rendered by a live
// public archive campaign.
func (a *App) ServeLegacyMedia(c echo.Context) error {
	return a.serveWorkspaceOrPublicMedia(c, c.Param("*"))
}

func (a *App) serveWorkspaceOrPublicMedia(c echo.Context, rawFilename string) error {
	filename := mediaFilename(rawFilename)
	if filename == "" || filename == "." || filename == "/" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing media file path")
	}

	if _, ok := c.Get(auth.UserHTTPCtxKey).(auth.User); ok {
		if access, err := a.workspaceAccess(c); err == nil {
			if _, err := a.core.GetWorkspaceMediaByFilename(access, filename); err == nil {
				return a.streamMediaBlob(c, filename)
			}
		}
	}

	// The archive query only determines whether a campaign is archive-eligible.
	// Do not use it as an anonymous media grant while the public archive feature
	// itself is disabled.
	if !a.cfg.EnablePublicArchive {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}

	public, err := a.core.IsPublicArchiveMedia(filename)
	if err != nil {
		return err
	}
	if !public {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	if _, err := a.core.GetMedia(0, "", filename, a.media); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	return a.streamMediaBlob(c, filename)
}

func mediaFilename(raw string) string {
	raw = strings.TrimSpace(raw)
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	return path.Base(raw)
}

func (a *App) streamMediaBlob(c echo.Context, filename string) error {
	b, err := a.media.GetBlob(filename)
	if err != nil {
		a.log.Printf("error fetching media %s: %v", filename, err)
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	c.Response().Header().Set("Cache-Control", "private, max-age=300")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, http.DetectContentType(b), bytes.NewReader(b))
}

func (a *App) setWorkspaceMediaURLs(m *media.Media) {
	if m == nil {
		return
	}
	m.URL = workspaceMediaFileURL(m.Filename)
	if m.Thumb != "" {
		m.ThumbURL.String = workspaceMediaFileURL(m.Thumb)
		m.ThumbURL.Valid = true
	}
}

func workspaceMediaFileURL(filename string) string {
	return "/api/media/file/" + url.PathEscape(filename)
}

// processImage reads the image file and returns thumbnail bytes and
// the original image's width, and height.
func processImage(file *multipart.FileHeader) (*bytes.Reader, int, int, error) {
	src, err := file.Open()
	if err != nil {
		return nil, 0, 0, err
	}
	defer src.Close()

	img, err := imaging.Decode(src)
	if err != nil {
		return nil, 0, 0, err
	}

	// Encode the image into a byte slice as PNG.
	var (
		thumb = imaging.Resize(img, thumbnailSize, 0, imaging.Lanczos)
		out   bytes.Buffer
	)
	if err := imaging.Encode(&out, thumb, imaging.PNG); err != nil {
		return nil, 0, 0, err
	}

	b := img.Bounds().Max
	return bytes.NewReader(out.Bytes()), b.X, b.Y, nil
}
