package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
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

		// Attach files.
		for _, f := range form.File["file"] {
			file, err := f.Open()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError,
					a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
			}
			defer file.Close()

			b, err := io.ReadAll(file)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError,
					a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
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
		num      = len(m.SubscriberEmails)
		isEmails = true
	)
	if len(m.SubscriberIDs) > 0 {
		num = len(m.SubscriberIDs)
		isEmails = false
	}

	notFound := []string{}
	for n := range num {
		var sub models.Subscriber

		if m.SubscriberMode == models.TxSubModeExternal {
			// `external`: Always create an ephemeral "subscriber" and don't
			// lookup in the DB.
			sub = models.Subscriber{
				Email: m.SubscriberEmails[n],
			}
		} else {
			// Default/fallback mode: lookup subscriber in DB.
			var (
				subID    int
				subEmail string
			)

			if !isEmails {
				subID = m.SubscriberIDs[n]
			} else {
				subEmail = m.SubscriberEmails[n]
			}

			var err error
			if !isEmails {
				if _, err = a.requireManagedWorkspaceSubscriber(c, access, subID); err == nil {
					// Resolve the row through the same workspace predicate that
					// authorized it. A member can be removed or a resource can be
					// transferred between these two operations; the legacy global
					// lookup would otherwise turn that check into a stale read.
					sub, err = a.core.GetWorkspaceSubscriber(access, subID)
				}
			} else {
				var subs models.Subscribers
				subs, err = a.core.GetManagedWorkspaceSubscribersByEmails(access, []string{subEmail})
				if err == nil {
					sub = subs[0]
				}
			}
			if err != nil {
				if m.SubscriberMode == models.TxSubModeFallback {
					// `fallback` is only for an address that does not exist in the
					// caller's writable workspace. Do not turn a database or list
					// loading failure into an untracked external send.
					if er, ok := err.(*echo.HTTPError); ok && er.Code == http.StatusBadRequest {
						sub = models.Subscriber{Email: subEmail}
					} else {
						return err
					}
				} else {
					// `default`: do not expose cross-workspace subscriber data.
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
		msg.Subscriber = sub
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

// validateTxMessage validates the tx message fields.
func (a *App) validateTxMessage(m models.TxMessage) (models.TxMessage, error) {
	if len(m.SubscriberEmails) > 0 && m.SubscriberEmail != "" {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "do not send `subscriber_email`"))
	}
	if len(m.SubscriberIDs) > 0 && m.SubscriberID != 0 {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "do not send `subscriber_id`"))
	}

	if m.SubscriberEmail != "" {
		m.SubscriberEmails = append(m.SubscriberEmails, m.SubscriberEmail)
	}

	if m.SubscriberID != 0 {
		m.SubscriberIDs = append(m.SubscriberIDs, m.SubscriberID)
	}

	// Validate subscriber_mode.
	if m.SubscriberMode == "" {
		m.SubscriberMode = models.TxSubModeDefault
	}

	switch m.SubscriberMode {
	case models.TxSubModeDefault:
		// Need subscriber_emails OR subscriber_ids, but not both.
		if (len(m.SubscriberEmails) == 0 && len(m.SubscriberIDs) == 0) || (len(m.SubscriberEmails) > 0 && len(m.SubscriberIDs) > 0) {
			return m, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", "send subscriber_emails OR subscriber_ids"))
		}
	case models.TxSubModeFallback, models.TxSubModeExternal:
		// `fallback` and `external` can only use subscriber_emails.
		if len(m.SubscriberIDs) > 0 {
			return m, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", "subscriber_ids not allowed in fallback or external mode"))
		}
		if len(m.SubscriberEmails) == 0 {
			return m, echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", "subscriber_emails"))
		}
	default:
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "subscriber_mode"))
	}

	for n, email := range m.SubscriberEmails {
		if email != "" {
			em, err := a.importer.SanitizeEmail(email)
			if err != nil {
				return m, echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
			m.SubscriberEmails[n] = em
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
