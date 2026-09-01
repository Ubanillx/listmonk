package manager

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/knadh/listmonk/models"
)

var reAutoTrackHREF = regexp.MustCompile(`(?i)(href\s*=\s*)(['"])(https?://[^"'<>]+)(['"])`)

const trackingLinkMarker = "__listmonk_link_uuid__"

// NewCampaignMessage creates and returns a CampaignMessage that is made available
// to message templates while they're compiled. It represents a message from
// a campaign that's bound to a single Subscriber.
func (m *Manager) NewCampaignMessage(c *models.Campaign, s models.Subscriber) (CampaignMessage, error) {
	// Account custom fields are scoped to the campaign owner. Merge them into
	// the subscriber view exposed to templates, with account values taking
	// precedence if a legacy subscriber attribute uses the same key.
	if len(c.OwnerUserAttribs) > 0 {
		merged := make(models.JSON, len(s.Attribs)+len(c.OwnerUserAttribs))
		for key, value := range s.Attribs {
			merged[key] = value
		}
		for key, value := range c.OwnerUserAttribs {
			merged[key] = value
		}
		s.Attribs = merged
	}
	msg := CampaignMessage{
		Campaign:   c,
		Subscriber: s,

		subject:  c.Subject,
		from:     c.FromEmail,
		to:       s.Email,
		unsubURL: fmt.Sprintf(m.cfg.UnsubURL, c.UUID, s.UUID),
	}

	if err := msg.render(); err != nil {
		return msg, err
	}
	m.autoTrackMessageLinks(&msg)

	return msg, nil
}

// autoTrackMessageLinks rewrites links after all campaign and subscriber
// template fields have been rendered. This includes links introduced by a
// campaign template or a custom subscriber attribute, which are unavailable
// while the campaign body is compiled.
func (m *Manager) autoTrackMessageLinks(msg *CampaignMessage) {
	if msg == nil || msg.Campaign == nil || !msg.Campaign.AutoTrackLinks ||
		msg.Campaign.ContentType == models.CampaignContentTypePlain || m.cfg.DisableTracking {
		return
	}

	body := string(msg.body)
	matches := reAutoTrackHREF.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return
	}

	subUUID := m.trackingSubscriberUUID(msg.Subscriber.UUID)
	var (
		out  strings.Builder
		last int
	)
	out.Grow(len(body))
	for _, match := range matches {
		start, end := match[0], match[1]
		urlStart, urlEnd := match[6], match[7]
		url := body[urlStart:urlEnd]

		out.WriteString(body[last:start])
		if m.isCampaignTrackingURL(url, msg.Campaign.UUID, subUUID) {
			out.WriteString(body[start:end])
		} else {
			out.WriteString(body[start:urlStart])
			out.WriteString(m.trackLink(url, msg.Campaign.UUID, subUUID))
			out.WriteString(body[urlEnd:end])
		}
		last = end
	}
	out.WriteString(body[last:])
	msg.body = []byte(out.String())
}

func (m *Manager) trackingSubscriberUUID(subUUID string) string {
	if !m.cfg.IndividualTracking {
		return dummyUUID
	}
	return subUUID
}

// isCampaignTrackingURL avoids registering an URL for a second redirect when
// the body already contains a link produced by the explicit @TrackLink syntax.
func (m *Manager) isCampaignTrackingURL(url, campUUID, subUUID string) bool {
	tracked := fmt.Sprintf(m.cfg.LinkTrackURL, trackingLinkMarker, campUUID, subUUID)
	prefix, suffix, found := strings.Cut(tracked, trackingLinkMarker)
	if !found || !strings.HasPrefix(url, prefix) || !strings.HasSuffix(url, suffix) {
		return false
	}

	linkUUID := strings.TrimSuffix(strings.TrimPrefix(url, prefix), suffix)
	return linkUUID != ""
}

// render takes a Message, executes its pre-compiled Campaign.Tpl
// and applies the resultant bytes to Message.body to be used in messages.
func (m *CampaignMessage) render() error {
	out := bytes.Buffer{}

	// Render the subject if it's a template.
	if m.Campaign.SubjectTpl != nil {
		if err := m.Campaign.SubjectTpl.ExecuteTemplate(&out, models.ContentTpl, m); err != nil {
			return err
		}
		m.subject = out.String()
		out.Reset()
	}

	// Compile the main template.
	if err := m.Campaign.Tpl.ExecuteTemplate(&out, models.BaseTpl, m); err != nil {
		return err
	}
	m.body = out.Bytes()

	// Is there an alt body?
	if m.Campaign.ContentType != models.CampaignContentTypePlain && m.Campaign.AltBody.Valid {
		if m.Campaign.AltBodyTpl != nil {
			b := bytes.Buffer{}
			if err := m.Campaign.AltBodyTpl.ExecuteTemplate(&b, models.ContentTpl, m); err != nil {
				return err
			}
			m.altBody = b.Bytes()
		} else {
			m.altBody = []byte(m.Campaign.AltBody.String)
		}
	}

	return nil
}

// Subject returns a copy of the message subject
func (m *CampaignMessage) Subject() string {
	return m.subject
}

// Body returns a copy of the message body.
func (m *CampaignMessage) Body() []byte {
	out := make([]byte, len(m.body))
	copy(out, m.body)
	return out
}

// AltBody returns a copy of the message's alt body.
func (m *CampaignMessage) AltBody() []byte {
	out := make([]byte, len(m.altBody))
	copy(out, m.altBody)
	return out
}
