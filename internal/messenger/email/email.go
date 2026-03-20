package email

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync/atomic"

	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
)

const (
	MessengerName = "email"

	hdrReturnPath = "Return-Path"
	hdrBcc        = "Bcc"
	hdrCc         = "Cc"
)

var ErrSMTPQuotaExceeded = errors.New("smtp daily quota exhausted")

type QuotaTracker interface {
	HasServerQuota(uuid string, limit int) (bool, error)
	ReserveServer(uuid string, limit int) (bool, error)
	CommitServer(uuid string) error
	ReleaseServer(uuid string)
}

// Server represents an SMTP server's credentials.
type Server struct {
	// Name is a unique identifier for the server.
	Name          string            `json:"name"`
	UUID          string            `json:"uuid"`
	IsPrimary     bool              `json:"is_primary"`
	FromEmail     string            `json:"from_email"`
	DailyLimit    int               `json:"daily_limit"`
	Username      string            `json:"username"`
	Password      string            `json:"password"`
	AuthProtocol  string            `json:"auth_protocol"`
	TLSType       string            `json:"tls_type"`
	TLSSkipVerify bool              `json:"tls_skip_verify"`
	EmailHeaders  map[string]string `json:"email_headers"`

	// Rest of the options are embedded directly from the smtppool lib.
	// The JSON tag is for config unmarshal to work.
	//lint:ignore SA5008 ,squash is needed by koanf/mapstructure config unmarshal.
	smtppool.Opt `json:",squash"`

	pool *smtppool.Pool
}

// Emailer is the SMTP e-mail messenger.
type Emailer struct {
	servers []*Server
	name    string
	next    atomic.Uint64
	tracker QuotaTracker
}

// New returns an SMTP e-mail Messenger backend with the given SMTP servers.
// Group indicates whether the messenger represents a group of SMTP servers (1 or more)
// that are used as a round-robin pool, or a single server.
func New(name string, servers ...Server) (*Emailer, error) {
	e := &Emailer{
		servers: make([]*Server, 0, len(servers)),
		name:    name,
	}

	for _, srv := range servers {
		s := srv

		var auth smtp.Auth
		switch s.AuthProtocol {
		case "cram":
			auth = smtp.CRAMMD5Auth(s.Username, s.Password)
		case "plain":
			auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
		case "login":
			auth = &smtppool.LoginAuth{Username: s.Username, Password: s.Password}
		case "", "none":
		default:
			return nil, fmt.Errorf("unknown SMTP auth type '%s'", s.AuthProtocol)
		}
		s.Opt.Auth = auth

		// TLS config.
		s.Opt.SSL = smtppool.SSLNone
		if s.TLSType != "none" {
			s.TLSConfig = &tls.Config{}
			if s.TLSSkipVerify {
				s.TLSConfig.InsecureSkipVerify = s.TLSSkipVerify
			} else {
				s.TLSConfig.ServerName = s.Host
			}

			// SSL/TLS, not STARTTLS.
			switch s.TLSType {
			case "TLS":
				s.Opt.SSL = smtppool.SSLTLS
			case "STARTTLS":
				s.Opt.SSL = smtppool.SSLSTARTTLS
			}
		}

		pool, err := smtppool.New(s.Opt)
		if err != nil {
			return nil, err
		}

		s.pool = pool
		e.servers = append(e.servers, &s)
	}

	return e, nil
}

// Name returns the messenger's name.
func (e *Emailer) Name() string {
	return e.name
}

func (e *Emailer) SetQuotaTracker(t QuotaTracker) {
	e.tracker = t
}

// DefaultFromEmail returns the sender configured on the first SMTP server.
func (e *Emailer) DefaultFromEmail() string {
	if len(e.servers) == 0 {
		return ""
	}

	return e.servers[0].FromEmail
}

func (e *Emailer) CanSend(m models.Message) error {
	_, _, err := e.pickServer(m, false)
	return err
}

// Push pushes a message to the server.
func (e *Emailer) Push(m models.Message) error {
	srv, reserved, err := e.pickServer(m, true)
	if err != nil {
		return err
	}
	if reserved {
		defer func() {
			if err != nil {
				e.tracker.ReleaseServer(srv.UUID)
				return
			}
			if cerr := e.tracker.CommitServer(srv.UUID); cerr != nil {
				err = cerr
			}
		}()
	}

	// Are there attachments?
	var files []smtppool.Attachment
	if m.Attachments != nil {
		files = make([]smtppool.Attachment, 0, len(m.Attachments))
		for _, f := range m.Attachments {
			a := smtppool.Attachment{
				Filename: f.Name,
				Header:   f.Header,
				Content:  make([]byte, len(f.Content)),
			}
			copy(a.Content, f.Content)
			files = append(files, a)
		}
	}

	// Create the email.
	em := smtppool.Email{
		From:        m.From,
		To:          m.To,
		Subject:     m.Subject,
		Attachments: files,
	}
	if m.UseSMTPFrom && srv.FromEmail != "" {
		em.From = srv.FromEmail
	}

	em.Headers = textproto.MIMEHeader{}

	// Attach SMTP level headers.
	for k, v := range srv.EmailHeaders {
		em.Headers.Set(k, v)
	}

	// Attach e-mail level headers.
	for k, v := range m.Headers {
		em.Headers.Set(k, v[0])
	}

	// If the `Return-Path` header is set, it should be set as the
	// the SMTP envelope sender (via the Sender field of the email struct).
	if sender := em.Headers.Get(hdrReturnPath); sender != "" {
		em.Sender = sender
		em.Headers.Del(hdrReturnPath)
	}

	// If the `Bcc` header is set, it should be set on the Envelope
	if bcc := em.Headers.Get(hdrBcc); bcc != "" {
		for _, part := range strings.Split(bcc, ",") {
			em.Bcc = append(em.Bcc, strings.TrimSpace(part))
		}
		em.Headers.Del(hdrBcc)
	}

	// If the `Cc` header is set, it should be set on the Envelope
	if cc := em.Headers.Get(hdrCc); cc != "" {
		for _, part := range strings.Split(cc, ",") {
			em.Cc = append(em.Cc, strings.TrimSpace(part))
		}
		em.Headers.Del(hdrCc)
	}

	switch m.ContentType {
	case "plain":
		em.Text = []byte(m.Body)
	default:
		em.HTML = m.Body
		if len(m.AltBody) > 0 {
			em.Text = m.AltBody
		}
	}

	err = srv.pool.Send(em)
	return err
}

// Flush flushes the message queue to the server.
func (e *Emailer) Flush() error {
	return nil
}

// Close closes the SMTP pools.
func (e *Emailer) Close() error {
	for _, s := range e.servers {
		s.pool.Close()
	}
	return nil
}

func (e *Emailer) pickServer(m models.Message, reserve bool) (*Server, bool, error) {
	ln := len(e.servers)
	if ln == 0 {
		return nil, false, fmt.Errorf("no SMTP servers configured")
	}

	start := e.next.Load()
	if reserve {
		start = e.next.Add(1) - 1
	}

	for i := range ln {
		srv := e.servers[(start+uint64(i))%uint64(ln)]
		if !m.UseSMTPQuota || e.tracker == nil || srv.UUID == "" || srv.DailyLimit <= 0 {
			return srv, false, nil
		}

		var (
			ok  bool
			err error
		)
		if reserve {
			ok, err = e.tracker.ReserveServer(srv.UUID, srv.DailyLimit)
		} else {
			ok, err = e.tracker.HasServerQuota(srv.UUID, srv.DailyLimit)
		}
		if err != nil {
			return nil, false, err
		}
		if ok {
			return srv, reserve, nil
		}
	}

	return nil, false, ErrSMTPQuotaExceeded
}
