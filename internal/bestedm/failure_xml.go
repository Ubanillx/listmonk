package bestedm

import (
	"bytes"
	"regexp"
)

// Consume comments, CDATA, processing instructions and other tags unchanged so
// email-looking text inside them cannot be mistaken for an email element.
var failureXMLTokens = regexp.MustCompile(`(?s)<!--.*?-->|<!\[CDATA\[.*?\]\]>|<\?.*?\?>|<email>[^<]*</email>|<(?:[^<>"']|"[^"]*"|'[^']*')*>`)
var emailAmpersands = regexp.MustCompile(`&(?:amp|lt|gt|quot|apos|#[0-9]+|#x[0-9a-fA-F]+);|&`)

func escapeFailureEmailAmpersands(b []byte) []byte {
	return failureXMLTokens.ReplaceAllFunc(b, func(token []byte) []byte {
		if !bytes.HasPrefix(token, []byte("<email>")) {
			return token
		}
		return emailAmpersands.ReplaceAllFunc(token, func(entity []byte) []byte {
			if len(entity) == 1 {
				return []byte("&amp;")
			}
			return entity
		})
	})
}
