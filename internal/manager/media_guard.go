package manager

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
)

var ErrUnresolvedMedia = errors.New("email contains an unresolved image or media reference")

// validateCampaignMedia checks static dependencies before any customer is
// queued. Customer-specific expressions are checked again after rendering.
func validateCampaignMedia(c *models.Campaign, rootURL string) error {
	if c.ContentType == models.CampaignContentTypePlain {
		return nil
	}
	root, _ := url.Parse(rootURL)
	for _, ref := range media.BodyReferences([]byte(c.Body + "\n" + c.TemplateBody)) {
		if media.HasTemplateExpression(ref.Source) {
			continue
		}
		if root != nil && linkedMediaIndex(ref.Source, c.Attachments, root) >= 0 {
			continue
		}
		if err := validateMediaReference(ref, c.Attachments, rootURL); err != nil {
			return err
		}
	}
	return nil
}

// validateRecipientMedia runs after rendering and link substitution, immediately
// before handing the message to SMTP. Never fetch arbitrary authored URLs here.
func validateRecipientMedia(body []byte, attachments []models.Attachment, rootURL string) error {
	for _, ref := range media.BodyReferences(body) {
		if err := validateMediaReference(ref, attachments, rootURL); err != nil {
			return err
		}
	}
	return nil
}

func validateMediaReference(ref media.Reference, attachments []models.Attachment, rootURL string) error {
	id, _, protected, err := media.ProtectedReference(ref.Source, rootURL)
	if err != nil || protected {
		return fmt.Errorf("%w: media %d is not associated or cannot be resolved", ErrUnresolvedMedia, id)
	}
	if !ref.Image {
		return nil
	}
	u, err := url.Parse(ref.Source)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil {
		return nil
	}
	if err == nil && u.Scheme == "cid" {
		found := false
		for _, a := range attachments {
			if a.Inline && strings.Trim(a.Header.Get("Content-ID"), "<>") == strings.TrimPrefix(ref.Source, "cid:") {
				found = true
				break
			}
		}
		if found {
			return nil
		}
	}
	// Do not print the URL: it may contain an opaque token or customer data.
	return fmt.Errorf("%w: image requires an absolute HTTP(S) URL or an attached content ID", ErrUnresolvedMedia)
}
