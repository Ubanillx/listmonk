package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// Transactional message limits.
//
// /api/tx is reachable by any holder of tx:send, and neither its attachments
// nor its recipient lists were previously bounded: every multipart file was
// read into memory and retained for the life of the request, and every
// recipient became a queued message. These caps keep one request from
// exhausting the process heap or saturating the send queue. Sizes are checked
// against the declared multipart part size before the buffer is allocated, and
// re-checked in validateTxMessage, which also covers the JSON path where
// attachments arrive base64-encoded.
const (
	// maxTxAttachments caps the number of files per message.
	maxTxAttachments = 20
	// maxTxAttachmentSize caps one attachment. This is above what most
	// receiving mail servers accept.
	maxTxAttachmentSize = 10 << 20
	// maxTxAttachmentsTotal caps all attachments of one message together.
	maxTxAttachmentsTotal = 20 << 20
	// maxTxRecipients caps the combined customer_emails and customer_ids lists.
	// Transactional sends are per-recipient by nature; bulk sending belongs to
	// campaigns, which queue and throttle independently.
	maxTxRecipients = 5000
)

// SendTxMessage handles the sending of a transactional message.
func (a *App) SendTxMessage(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := requireWritableWorkspace(access); err != nil {
		return err
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermTxSend); err != nil {
		return err
	}
	var m models.TxMessage

	// If it's a multipart form, there may be file attachments.
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", err.Error()))
		}

		data, ok := form.Value["data"]
		if !ok || len(data) != 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "data"))
		}

		// Parse the JSON data.
		if err := json.Unmarshal([]byte(data[0]), &m); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("data: %s", err.Error())))
		}

		// Attach files. Over-sized and over-count uploads are rejected before
		// being read into memory: the declared part size is available without
		// buffering the file.
		files := form.File["file"]
		if len(files) > maxTxAttachments {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name",
					fmt.Sprintf("file (max %d attachments)", maxTxAttachments)))
		}

		var totalAttachments int64
		for _, f := range files {
			if f.Size > maxTxAttachmentSize {
				return tooLargeErr(a, fmt.Sprintf("file %s (max %d MB)", f.Filename, maxTxAttachmentSize>>20))
			}
			if totalAttachments += f.Size; totalAttachments > maxTxAttachmentsTotal {
				return tooLargeErr(a, fmt.Sprintf("attachments (max %d MB total)", maxTxAttachmentsTotal>>20))
			}

			b, err := a.readTxAttachment(f)
			if err != nil {
				return err
			}

			m.Attachments = append(m.Attachments, models.Attachment{
				Name:    f.Filename,
				Header:  manager.MakeAttachmentHeader(f.Filename, "base64", f.Header.Get("Content-Type")),
				Content: b,
			})
		}

	} else if err := c.Bind(&m); err != nil {
		return err
	}

	// Validate fields.
	if r, err := a.validateTxMessage(m); err != nil {
		return err
	} else {
		m = r
	}

	// Templates may be organization or globally shared, but manager inspection
	// rights must not let a transactional message use another member's private
	// template.
	if _, err := a.requireUsableWorkspaceResource(c, access, resourceTemplates, m.TemplateID, auth.PermTemplatesGet); err != nil {
		return err
	}

	// Get the cached tx template.
	tpl, err := a.manager.GetTpl(m.TemplateID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("template %d", m.TemplateID)))
	}

	// Template attachments are stored as media-library references and loaded
	// only when a message is sent. Do not trust the cached template's media ID
	// slice for authorization: a template can be edited or a media row can be
	// moved between workspaces after the cache was populated. Read the current
	// association set, validate every media row against the active workspace,
	// then load the binary blobs. Request-level multipart attachments are
	// appended below, so callers can add one-off files to a reusable template.
	var templateMediaIDs []int
	if err := a.db.Select(&templateMediaIDs, `
		SELECT media_id FROM template_media
		WHERE template_id = $1 AND media_id IS NOT NULL
		ORDER BY media_id`, m.TemplateID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.errorFetching", "name", err.Error()))
	}
	for _, mediaID := range templateMediaIDs {
		if _, err := a.requireUsableWorkspaceResource(c, access, resourceMedia, mediaID, auth.PermMediaGet); err != nil {
			// A shared/global template can deliberately carry a private image
			// owned by its author. Treat that association as part of the
			// template's published payload, but do not broaden access to an
			// unrelated private media ID.
			allowed, mediaErr := a.core.CanUseTemplateMedia(access, m.TemplateID, mediaID)
			if mediaErr != nil {
				return mediaErr
			}
			if !allowed {
				return err
			}
		}
	}
	mediaIDs := make([]int64, 0, len(templateMediaIDs))
	for _, mediaID := range templateMediaIDs {
		mediaIDs = append(mediaIDs, int64(mediaID))
	}
	// Resolve the binary through the database-backed store's workspace-aware
	// template path. The manager's historical ID-only loader is intentionally
	// not used here: a cached template can outlive a media transfer/deletion,
	// and a shared template may legitimately carry a private image owned by its
	// author. The store rechecks the exact template association and ownership
	// boundary before reading each blob.
	templateAttachments, err := newManagerStore(a.queries, a.core, a.media, a.db).
		GetTemplateAttachments(access, m.TemplateID, mediaIDs)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.errorFetching", "name", err.Error()))
	}

	var (
		num      = len(m.CustomerEmails)
		isEmails = true
	)
	if len(m.CustomerIDs) > 0 {
		num = len(m.CustomerIDs)
		isEmails = false
	}

	notFound := []string{}
	for n := range num {
		var sub models.Customer

		if m.CustomerMode == models.TxSubModeExternal {
			// `external`: Always create an ephemeral "customer" and don't
			// lookup in the DB.
			sub = models.Customer{
				Email: m.CustomerEmails[n],
			}
		} else {
			// Default/fallback mode: lookup customer in DB.
			var (
				subID    int
				subEmail string
			)

			if !isEmails {
				subID = m.CustomerIDs[n]
			} else {
				subEmail = m.CustomerEmails[n]
			}

			var err error
			if !isEmails {
				if _, err = a.requireManagedWorkspaceCustomer(c, access, subID); err == nil {
					// Resolve the row through the same workspace predicate that
					// authorized it. A member can be removed or a resource can be
					// transferred between these two operations; the legacy global
					// lookup would otherwise turn that check into a stale read.
					sub, err = a.core.GetWorkspaceCustomer(access, subID)
				}
			} else {
				var subs models.Customers
				subs, err = a.core.GetManagedWorkspaceCustomersByEmails(access, []string{subEmail})
				if err == nil {
					sub = subs[0]
				}
			}
			if err != nil {
				if m.CustomerMode == models.TxSubModeFallback {
					// `fallback` is only for an address that does not exist in the
					// caller's writable workspace. Do not turn a database or customer_list
					// loading failure into an untracked external send.
					if er, ok := err.(*echo.HTTPError); ok && er.Code == http.StatusBadRequest {
						sub = models.Customer{Email: subEmail}
					} else {
						return err
					}
				} else {
					// `default`: do not expose cross-workspace customer data.
					if er, ok := err.(*echo.HTTPError); ok {
						notFound = append(notFound, fmt.Sprintf("%v", er.Message))
						continue
					}
					return err
				}
			}
		}

		// Render a per-recipient copy. Render mutates subject/body/altbody, and
		// reusing the same instance would leak a rendered value to the next recipient.
		rendered := m
		if err := rendered.Render(sub, tpl, a.manager.GenericTemplateFuncs()); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.errorFetching", "name"))
		}

		// Prepare the final message.
		msg := models.Message{}
		msg.Customer = sub
		msg.To = []string{sub.Email}
		msg.From = rendered.FromEmail
		msg.Subject = rendered.Subject
		msg.ContentType = rendered.ContentType
		msg.Messenger = rendered.Messenger
		if email.IsMessengerName(msg.Messenger) {
			msg.Messenger = emailMsgr
			msg.OwnerUserID = access.UserID
			if err := a.requirePersonalSMTPAvailable(access.UserID); err != nil {
				return err
			}
		}
		msg.UseSMTPQuota = email.IsMessengerName(rendered.Messenger)
		msg.UseSMTPFrom = email.IsMessengerName(rendered.Messenger)
		msg.Body = rendered.Body
		msg.AltBody = []byte(rendered.AltBody)
		for _, a := range templateAttachments {
			msg.Attachments = append(msg.Attachments, models.Attachment{
				Name:      a.Name,
				Header:    a.Header,
				Content:   a.Content,
				MediaID:   a.MediaID,
				SourceURL: a.SourceURL,
			})
		}
		for _, a := range rendered.Attachments {
			msg.Attachments = append(msg.Attachments, models.Attachment{
				Name:    a.Name,
				Header:  a.Header,
				Content: a.Content,
			})
		}
		if msg.ContentType != models.CampaignContentTypePlain && email.IsMessengerName(msg.Messenger) {
			msg.Body, msg.Attachments = manager.InlineMediaImages(msg.Body, msg.Attachments)
		}

		// Optional headers.
		if len(rendered.Headers) != 0 {
			msg.Headers = make(textproto.MIMEHeader, len(rendered.Headers))
			for _, set := range rendered.Headers {
				for hdr, val := range set {
					msg.Headers.Add(hdr, val)
				}
			}
		}

		if err := a.manager.CanSendMessage(msg); err != nil {
			if errors.Is(err, email.ErrSMTPQuotaExceeded) {
				return echo.NewHTTPError(http.StatusTooManyRequests, a.i18n.T("tx.smtpQuotaExceeded"))
			}
			return err
		}

		if err := a.manager.PushMessage(msg); err != nil {
			a.log.Printf("error sending message (%s): %v", msg.Subject, err)
			return err
		}
	}

	if len(notFound) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, strings.Join(notFound, "; "))
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// tooLargeErr builds a 413 response for a payload that exceeds a size limit.
func tooLargeErr(a *App, name string) error {
	return echo.NewHTTPError(http.StatusRequestEntityTooLarge, a.i18n.Ts("globals.messages.tooLarge", "name", name))
}

// readTxAttachment opens one multipart attachment and reads it whole, bounding
// the read so that a part declaring a small size cannot be buffered beyond the
// limit. The handle is closed before returning instead of accumulating defers
// for every file in the request.
func (a *App) readTxAttachment(f *multipart.FileHeader) ([]byte, error) {
	file, err := f.Open()
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
	}
	defer file.Close()

	b, err := io.ReadAll(io.LimitReader(file, maxTxAttachmentSize+1))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
	}
	if int64(len(b)) > maxTxAttachmentSize {
		return nil, tooLargeErr(a, fmt.Sprintf("file %s (max %d MB)", f.Filename, maxTxAttachmentSize>>20))
	}

	return b, nil
}

// validateTxMessage validates the tx message fields.
func (a *App) validateTxMessage(m models.TxMessage) (models.TxMessage, error) {
	if len(m.CustomerEmails) > 0 && m.CustomerEmail != "" {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "do not send `customer_email`"))
	}
	if len(m.CustomerIDs) > 0 && m.CustomerID != 0 {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "do not send `customer_id`"))
	}

	if m.CustomerEmail != "" {
		m.CustomerEmails = append(m.CustomerEmails, m.CustomerEmail)
	}

	if m.CustomerID != 0 {
		m.CustomerIDs = append(m.CustomerIDs, m.CustomerID)
	}

	// Bound recipients and attachments for both request encodings. The multipart
	// path rejects over-sized parts before buffering them; this is the
	// authoritative check because the JSON path arrives already base64-decoded.
	if n := len(m.CustomerEmails) + len(m.CustomerIDs); n > maxTxRecipients {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("recipients (max %d)", maxTxRecipients)))
	}

	if len(m.Attachments) > maxTxAttachments {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("attachments (max %d)", maxTxAttachments)))
	}
	var totalAttachments int
	for _, at := range m.Attachments {
		if len(at.Content) > maxTxAttachmentSize {
			return m, tooLargeErr(a, fmt.Sprintf("attachment %s (max %d MB)", at.Name, maxTxAttachmentSize>>20))
		}
		if totalAttachments += len(at.Content); totalAttachments > maxTxAttachmentsTotal {
			return m, tooLargeErr(a, fmt.Sprintf("attachments (max %d MB total)", maxTxAttachmentsTotal>>20))
		}
	}

	// Validate customer_mode.
	if m.CustomerMode == "" {
		m.CustomerMode = models.TxSubModeDefault
	}

	switch m.CustomerMode {
	case models.TxSubModeDefault:
		// Need customer_emails OR customer_ids, but not both.
		if (len(m.CustomerEmails) == 0 && len(m.CustomerIDs) == 0) || (len(m.CustomerEmails) > 0 && len(m.CustomerIDs) > 0) {
			return m, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", "send customer_emails OR customer_ids"))
		}
	case models.TxSubModeFallback, models.TxSubModeExternal:
		// `fallback` and `external` can only use customer_emails.
		if len(m.CustomerIDs) > 0 {
			return m, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", "customer_ids not allowed in fallback or external mode"))
		}
		if len(m.CustomerEmails) == 0 {
			return m, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", "customer_emails"))
		}
	default:
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "customer_mode"))
	}

	for n, email := range m.CustomerEmails {
		if email != "" {
			em, err := a.importer.SanitizeEmail(email)
			if err != nil {
				return m, echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
			m.CustomerEmails[n] = em
		}
	}

	if m.FromEmail == "" {
		m.FromEmail = a.cfg.FromEmail
	}

	if m.Messenger == "" {
		m.Messenger = emailMsgr
	} else if email.IsMessengerName(m.Messenger) {
		// Account-owned SMTP is always a single logical messenger backed by the
		// caller's complete enabled pool. Never allow a transaction request to
		// select a platform SMTP (including legacy email-* names).
		m.Messenger = emailMsgr
	} else if !a.manager.HasMessenger(m.Messenger) {
		return m, echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("campaigns.fieldInvalidMessenger", "name", m.Messenger))
	}

	return m, nil
}
