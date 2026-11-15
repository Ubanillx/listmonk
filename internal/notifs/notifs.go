// package notifs is a special singleton, stateful globally accessible package
// that handles sending out arbitrary notifications to the admin and users.
// It's initialized once in the main package and is accessed globally across
// other packages.
package notifs

import (
	"bytes"
	"errors"
	"html/template"
	"log"
	"net/textproto"
	"strings"

	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/utils"
	"github.com/knadh/listmonk/models"
)

const (
	TplImport         = "import-status"
	TplCampaignStatus = "campaign-status"
	TplSendErrorAlert = "send-error-alert"
	TplCustomerOptin  = "customer-optin"
	TplCustomerData   = "customer-data"
	TplForgotPassword = "forgot-password"
)

var ErrNotInitialized = errors.New("notifs: not initialized")

type emailPusher interface {
	Push(models.Message) error
}

type Opt struct {
	FromEmail    string
	SystemEmails []string
	ContentType  string
}

type Notifs struct {
	em emailPusher
	lo *log.Logger

	opt Opt
}

var (
	Tpls *template.Template
	no   *Notifs
)

// Initialize returns a new Notifs instance.
func Initialize(opt Opt, tpls *template.Template, em *email.Emailer, lo *log.Logger) {
	if no != nil {
		lo.Fatal("notifs already initialized")
	}

	Tpls = tpls
	var pusher emailPusher
	if em != nil {
		pusher = em
	}
	no = &Notifs{
		opt: opt,
		em:  pusher,
		lo:  lo,
	}
}

// SystemEmails returns the configured admin notification addresses.
func SystemEmails() []string {
	if no == nil {
		return nil
	}
	return append([]string{}, no.opt.SystemEmails...)
}

// NotifySystem sends out an e-mail notification to the admin emails.
func NotifySystem(subject, tplName string, data any, hdr textproto.MIMEHeader) error {
	if no == nil {
		return ErrNotInitialized
	}
	return notify(no.opt.SystemEmails, subject, tplName, data, hdr)
}

// NotifySystemAnd sends the notification to the admin emails and then to each
// extra address that is not already part of the admin list. Admin and extra
// recipients are deliberately sent separate messages so that neither audience
// sees the other's addresses. Delivery failures for individual recipients are
// logged; an error is returned only when rendering fails or no recipient
// received the message.
func NotifySystemAnd(extra []string, subject, tplName string, data any, hdr textproto.MIMEHeader) error {
	if no == nil {
		return ErrNotInitialized
	}

	system := no.opt.SystemEmails
	extra = unlistedEmails(system, extra)
	if len(system) == 0 && len(extra) == 0 {
		return nil
	}

	subj, body, err := no.render(subject, tplName, data)
	if err != nil {
		return err
	}

	sent := 0
	var lastErr error
	groups := make([][]string, 0, len(extra)+1)
	if len(system) > 0 {
		groups = append(groups, system)
	}
	for _, recipient := range extra {
		groups = append(groups, []string{recipient})
	}
	for _, recipients := range groups {
		if err := no.send(recipients, subj, body, hdr); err != nil {
			lastErr = err
			no.logf("error sending notification (%s) to %s: %s", subj,
				strings.Join(maskEmails(recipients), ", "), redactEmails(err.Error(), recipients))
			continue
		}
		sent += len(recipients)
	}
	if sent == 0 {
		return lastErr
	}
	return nil
}

// Notify sends out an e-mail notification.
func Notify(toEmails []string, subject, tplName string, data any, hdr textproto.MIMEHeader) error {
	return notify(toEmails, subject, tplName, data, hdr)
}

func notify(toEmails []string, subject, tplName string, data any, hdr textproto.MIMEHeader) error {
	if no == nil {
		return ErrNotInitialized
	}
	if len(toEmails) == 0 {
		return nil
	}

	subj, body, err := no.render(subject, tplName, data)
	if err != nil {
		return err
	}
	return no.send(toEmails, subj, body, hdr)
}

func (n *Notifs) render(subject, tplName string, data any) (string, []byte, error) {
	if Tpls == nil || n.em == nil {
		return "", nil, ErrNotInitialized
	}

	var buf bytes.Buffer
	if err := Tpls.ExecuteTemplate(&buf, tplName, data); err != nil {
		n.logf("error compiling notification template '%s': %v", tplName, err)
		return "", nil, err
	}
	body := buf.Bytes()

	subject, body = utils.GetTplSubject(subject, body)
	return subject, body, nil
}

func (n *Notifs) send(toEmails []string, subject string, body []byte, hdr textproto.MIMEHeader) error {
	m := models.Message{
		Messenger:   "email",
		ContentType: n.opt.ContentType,
		From:        n.opt.FromEmail,
		To:          toEmails,
		Subject:     subject,
		Body:        body,
		Headers:     hdr,
	}

	// Send the message.
	if err := n.em.Push(m); err != nil {
		n.logf("error sending admin notification (%s) to %s: %s", subject,
			strings.Join(maskEmails(toEmails), ", "), redactEmails(err.Error(), toEmails))
		return err
	}

	return nil
}

func (n *Notifs) logf(format string, args ...any) {
	if n.lo != nil {
		n.lo.Printf(format, args...)
	}
}

// unlistedEmails returns the unique, trimmed extra addresses that are not in
// the system list. Comparison is case-insensitive.
func unlistedEmails(system, extra []string) []string {
	seen := make(map[string]struct{}, len(system)+len(extra))
	for _, e := range system {
		seen[normalizeEmail(e)] = struct{}{}
	}

	out := make([]string, 0, len(extra))
	for _, e := range extra {
		key := normalizeEmail(e)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(e))
	}
	return out
}

func normalizeEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}

// maskEmails keeps the domain for diagnostics while hiding the local part.
func maskEmails(emails []string) []string {
	out := make([]string, 0, len(emails))
	for _, e := range emails {
		out = append(out, maskEmail(e))
	}
	return out
}

func maskEmail(e string) string {
	e = strings.TrimSpace(e)
	at := strings.LastIndexByte(e, '@')
	if at <= 0 {
		return "***"
	}
	return "***" + e[at:]
}

// redactEmails removes recipient addresses that an SMTP error may echo back.
func redactEmails(text string, emails []string) string {
	for _, e := range emails {
		if e = strings.TrimSpace(e); e != "" {
			text = strings.ReplaceAll(text, e, maskEmail(e))
		}
	}
	return text
}
