package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
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

// Media upload limits.
//
// processImage decodes and resamples the whole image, so the cost of an upload
// is driven by the image's declared dimensions rather than by its file size: a
// few KB of highly compressed PNG can declare tens of thousands of pixels per
// side. Dimensions are read from the header and validated before any pixels are
// decoded, which is what bounds the memory and CPU a single upload can demand.
const (
	// maxMediaUploadSize caps one uploaded media file.
	maxMediaUploadSize = 32 << 20
	// maxMediaImageDimension caps either side of an uploaded image.
	maxMediaImageDimension = 20000
	// maxMediaImagePixels caps the width multiplied by the height of an uploaded
	// image.
	maxMediaImagePixels = 50 * 1000 * 1000
)

// errMediaTooLarge reports an upload or image that exceeds the media limits.
// Callers map it to a 413 instead of a generic failure.
var errMediaTooLarge = errors.New("uploaded media exceeds the allowed size")

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
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermMediaManage); err != nil {
		return err
	}
	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("media.invalidFile", "error", err.Error()))
	}
	folderID, err := parseMediaFolderID(c.FormValue("folder_id"))
	if err != nil {
		return err
	}
	if folderID > 0 {
		if err := a.core.RequireReadableMediaFolder(access, folderID); err != nil {
			return err
		}
	}

	// A multipart upload larger than the in-memory threshold is spilled by
	// net/http to a temporary file it does not remove on its own.
	if mf := c.Request().MultipartForm; mf != nil {
		defer func() {
			if err := mf.RemoveAll(); err != nil {
				a.log.Printf("error removing multipart temporary files: %v", err)
			}
		}()
	}

	if file.Size > maxMediaUploadSize {
		return tooLargeErr(a, fmt.Sprintf("file (max %d MB)", maxMediaUploadSize>>20))
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

	// The extension and the declared Content-Type are both supplied by the
	// client, so inspect the actual bytes as well. Active documents (HTML and
	// XHTML) must never enter the media library: files are served from the
	// application origin, where such a document would execute scripts with the
	// viewer's session.
	sample := make([]byte, 512)
	n, err := io.ReadFull(src, sample)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("media.errorReadingFile", "error", err.Error()))
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("media.errorReadingFile", "error", err.Error()))
	}
	if isActiveDocumentType(http.DetectContentType(sample[:n])) {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("media.unsupportedFileType", "type", ext))
	}

	// Validate file extension.
	if !inArray("*", a.cfg.MediaUpload.Extensions) {
		if ok := inArray(ext, a.cfg.MediaUpload.Extensions); !ok {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("media.unsupportedFileType", "type", ext))
		}
	}

	// Sanitize the filename.
	fName := makeFilename(file.Filename)

	// Provider objects are shared by all workspaces.  Check names through the
	// existence-only query rather than the legacy GetMedia lookup, which would
	// read a row from another workspace just to detect a collision.  Check both
	// the original object and the thumbnail namespace, and retry in the very
	// unlikely event that a generated suffix is already present.
	for attempt := 0; attempt < 5; attempt++ {
		filenameExists, err := a.core.MediaFilenameExists(a.cfg.MediaUpload.Provider, fName)
		if err != nil {
			return err
		}
		thumbExists := false
		if !filenameExists {
			thumbExists, err = a.core.MediaFilenameExists(a.cfg.MediaUpload.Provider, thumbPrefix+fName)
			if err != nil {
				return err
			}
		}
		if !filenameExists && !thumbExists {
			break
		}
		suffix, err := generateRandomString(6)
		if err != nil {
			a.log.Printf("error generating random string: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.T("globals.messages.internalError"))
		}
		fName = appendSuffixToFilename(fName, suffix)
		if attempt == 4 {
			return echo.NewHTTPError(http.StatusConflict, "media filename is already in use")
		}
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
			if errors.Is(err, errMediaTooLarge) {
				a.log.Printf("rejecting media upload: %v", err)
				return tooLargeErr(a, err.Error())
			}
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
	m, err := a.core.InsertMediaInWorkspace(access, fName, thumbfName, contentType, meta, a.cfg.MediaUpload.Provider, folderID, scope, a.media)
	if err != nil {
		cleanUp = true
		return err
	}
	setAuditObjectID(c, strconv.Itoa(m.ID))
	setAuditObjectDetails(c, map[string]any{
		"filename":     m.Filename,
		"content_type": contentType,
		"extension":    ext,
	})
	setAuditMetadata(c, map[string]any{
		"content_type":  contentType,
		"extension":     ext,
		"has_thumbnail": thumbfName != "",
	})
	// Keep the immediate upload response consistent with media customer_list responses.
	// Editors should insert the protected application route, never a direct
	// filesystem or object-store URL.
	a.setWorkspaceMediaURLs(&m)

	return c.JSON(http.StatusOK, okResp{m})
}

// GetAllMedia handles retrieval of uploaded media.
func (a *App) GetAllMedia(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if !access.IsOrganizationManager() && !user.IsPlatformAdmin() {
		if err := requireLegacyPermission(user, auth.PermMediaGet); err != nil {
			return err
		}
	}
	var (
		query = c.FormValue("query")

		pg = a.pg.NewFromURL(c.Request().URL.Query())
	)
	folderID, err := parseMediaFolderFilter(c.QueryParam("folder_id"))
	if err != nil {
		return err
	}
	if folderID != nil && *folderID > 0 {
		if err := a.core.RequireReadableMediaFolder(access, *folderID); err != nil {
			return err
		}
	}
	// Fetch the media items from the DB.
	res, total, err := a.core.QueryWorkspaceMedia(access, a.cfg.MediaUpload.Provider, a.media, query, folderID, pg.Offset, pg.Limit)
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
	if _, err := a.requireReadableWorkspaceResource(c, access, resourceMedia, id, auth.PermMediaGet); err != nil {
		return err
	}
	out, err := a.core.GetWorkspaceMediaByID(access, id)
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
	if _, err := a.requireManagedWorkspaceResource(c, access, resourceMedia, id, auth.PermMediaManage); err != nil {
		return err
	}
	if out, err := a.core.GetWorkspaceMediaByID(access, id); err == nil {
		setAuditObjectDetails(c, map[string]any{
			"filename":     out.Filename,
			"content_type": out.ContentType,
		})
	}
	deleted, err := a.core.DeleteMediaInWorkspace(access, id)
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

// ServeMediaFileByID serves a media object through its exact database ID. New
// editor URLs use this route so cloned records that share a provider filename
// cannot resolve to the wrong binary. The filename segment is retained as a
// human-readable and tamper-evident check; the stored row remains authoritative.
func (a *App) ServeMediaFileByID(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid media ID")
	}
	return a.serveWorkspaceOrPublicMediaByID(c, id, c.Param("filename"))
}

// ServeMediaFile serves a media object by its stored filename. It is kept for
// backwards compatibility with historical editor content. New content should
// use ServeMediaFileByID because filenames may be shared by cloned records.
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

	if _, err := a.core.GetPublicArchiveMediaByFilename(filename, nil); err != nil {
		if err == core.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "media file not found")
		}
		return err
	}
	// The row lookup above is authorization and existence in one statement.
	// Stream the requested provider object only after that exact graph check.
	return a.streamMediaBlob(c, filename)
}

func (a *App) serveWorkspaceOrPublicMediaByID(c echo.Context, id int, rawFilename string) error {
	requested := mediaFilename(rawFilename)
	if requested == "" || requested == "." || requested == "/" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing media file path")
	}

	// Authenticated browser/API requests are checked against the exact media
	// row. The active workspace cookie/header is still required for private and
	// organization resources; the ID never grants access on its own.
	if _, ok := c.Get(auth.UserHTTPCtxKey).(auth.User); ok {
		access, err := a.workspaceAccess(c)
		if err != nil {
			return err
		}
		if out, err := a.core.GetWorkspaceMediaByID(access, id); err == nil {
			if requested != out.Filename && requested != out.Thumb {
				return echo.NewHTTPError(http.StatusNotFound, "media file not found")
			}
			return a.streamMediaBlob(c, requested)
		}
	}

	// Public archive pages are the only unauthenticated exception. Check the
	// exact media row so a public filename cannot expose a different clone.
	if !a.cfg.EnablePublicArchive {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	out, err := a.core.GetPublicArchiveMediaByID(id, a.media)
	if err == core.ErrNotFound {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	if err != nil || (requested != out.Filename && requested != out.Thumb) {
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}
	return a.streamMediaBlob(c, requested)
}

func mediaFilename(raw string) string {
	raw = strings.TrimSpace(raw)
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	return path.Base(raw)
}

// isActiveDocumentType reports whether a sniffed MIME type is a document that
// browsers may execute (scripts, event handlers) when rendered. Such content is
// refused on upload and never served inline from the application origin.
func isActiveDocumentType(sniffed string) bool {
	return strings.HasPrefix(sniffed, "text/html") ||
		strings.HasPrefix(sniffed, "application/xhtml+xml")
}

// mediaServingPolicy maps a sniffed MIME type to the headers a stored media
// blob is served with. Anything browsers may treat as an active document is
// neutralised: active types are turned into an opaque download, XML (which
// includes SVG) is sandboxed so a directly opened document cannot run scripts.
// The returned contentType is the header value to serve, attach forces a
// Content-Disposition: attachment, and csp, when non-empty, is the
// Content-Security-Policy to set.
func mediaServingPolicy(sniffed string) (contentType string, attach bool, csp string) {
	switch {
	case isActiveDocumentType(sniffed):
		return "application/octet-stream", true, "default-src 'none'; sandbox"
	case strings.HasPrefix(sniffed, "text/xml"), strings.HasPrefix(sniffed, "application/xml"):
		return sniffed, false, "default-src 'none'; style-src 'unsafe-inline'; sandbox"
	default:
		return sniffed, false, ""
	}
}

func (a *App) streamMediaBlob(c echo.Context, filename string) error {
	b, err := a.media.GetBlob(filename)
	if err != nil {
		a.log.Printf("error fetching media %s: %v", filename, err)
		return echo.NewHTTPError(http.StatusNotFound, "media file not found")
	}

	ctype, attach, csp := mediaServingPolicy(http.DetectContentType(b))
	c.Response().Header().Set("Cache-Control", "private, max-age=300")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	if attach {
		c.Response().Header().Set("Content-Disposition", "attachment")
	}
	if csp != "" {
		c.Response().Header().Set("Content-Security-Policy", csp)
	}

	return c.Stream(http.StatusOK, ctype, bytes.NewReader(b))
}

func (a *App) setWorkspaceMediaURLs(m *media.Media) {
	if m == nil {
		return
	}
	m.URL = workspaceMediaIDFileURL(m.ID, m.Filename)
	if m.Thumb != "" {
		m.ThumbURL.String = workspaceMediaIDFileURL(m.ID, m.Thumb)
		m.ThumbURL.Valid = true
	}
}

// workspaceMediaIDFileURL is the canonical URL for newly returned media.
// Including the ID disambiguates cloned rows that intentionally share the
// same provider filename.
func workspaceMediaIDFileURL(id int, filename string) string {
	if id > 0 {
		return "/api/media/file/" + strconv.Itoa(id) + "/" + url.PathEscape(filename)
	}
	return workspaceMediaFileURL(filename)
}

// workspaceMediaFileURL is retained for callers/tests that need the legacy
// filename-only route.
func workspaceMediaFileURL(filename string) string {
	return "/api/media/file/" + url.PathEscape(filename)
}

// processImage reads the image file and returns thumbnail bytes and
// the original image's width, and height.
//
// The image is validated from its header before the pixels are decoded: a
// decode allocates the full bitmap, so an image that declares an enormous
// canvas has to be refused while only its header has been read. Returns an
// error wrapping errMediaTooLarge for uploads and images over the limits.
func processImage(file *multipart.FileHeader) (*bytes.Reader, int, int, error) {
	if file.Size > maxMediaUploadSize {
		return nil, 0, 0, fmt.Errorf("%w: file is larger than %d bytes", errMediaTooLarge, maxMediaUploadSize)
	}

	src, err := file.Open()
	if err != nil {
		return nil, 0, 0, err
	}
	defer src.Close()

	// Read the upload once, bounded: the declared size of a multipart part is
	// not authoritative, and both the header check and the decode below need the
	// bytes.
	raw, err := io.ReadAll(io.LimitReader(src, maxMediaUploadSize+1))
	if err != nil {
		return nil, 0, 0, err
	}
	if len(raw) > maxMediaUploadSize {
		return nil, 0, 0, fmt.Errorf("%w: file is larger than %d bytes", errMediaTooLarge, maxMediaUploadSize)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width > maxMediaImageDimension || cfg.Height > maxMediaImageDimension {
		return nil, 0, 0, fmt.Errorf("%w: image is larger than %d pixels per side (%d x %d)",
			errMediaTooLarge, maxMediaImageDimension, cfg.Width, cfg.Height)
	}
	if cfg.Width*cfg.Height > maxMediaImagePixels {
		return nil, 0, 0, fmt.Errorf("%w: image is larger than %d pixels (%d x %d)",
			errMediaTooLarge, maxMediaImagePixels, cfg.Width, cfg.Height)
	}

	img, err := imaging.Decode(bytes.NewReader(raw))
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
