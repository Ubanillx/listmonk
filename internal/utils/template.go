package utils

import (
	"regexp"
	"strings"
)

var templateTitle = regexp.MustCompile(`(?s)<title\s*data-i18n\s*>(.+?)</title>`)

// GetTplSubject extracts a custom subject rendered in a notification template.
// If no data-i18n title is present, the incoming subject and body are returned.
func GetTplSubject(subject string, body []byte) (string, []byte) {
	m := templateTitle.FindSubmatch(body)
	if len(m) != 2 {
		return subject, body
	}
	return strings.TrimSpace(string(m[1])), templateTitle.ReplaceAll(body, []byte(""))
}
