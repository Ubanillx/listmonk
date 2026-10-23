package media

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
)

// Reference is a URL used by email HTML. Image distinguishes renderable sources
// (including backgrounds and srcset) from ordinary document hyperlinks.
type Reference struct {
	Source string
	Image  bool
}

var referenceTags = regexp.MustCompile(`(?is)<[a-z][^>]*>`)
var referenceAttributes = regexp.MustCompile("(?is)(\\s+([^\\s=/>]+)\\s*=\\s*)(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")
var referenceCSS = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^\s)]+))\s*\)`)

// TransformReferences preserves authored HTML and replaces only URL values.
// Save reconciliation and the SMTP boundary share this scanner so a source
// cannot be recognized by one path and silently missed by the other.
func TransformReferences(body []byte, transform func(Reference) (string, error)) ([]byte, error) {
	var firstErr error
	apply := func(source string, image bool) string {
		if firstErr != nil {
			return source
		}
		result, err := transform(Reference{Source: html.UnescapeString(strings.TrimSpace(source)), Image: image})
		if err != nil {
			firstErr = err
			return source
		}
		return result
	}
	transformCSS := func(css []byte) []byte {
		return referenceCSS.ReplaceAllFunc(css, func(value []byte) []byte {
			groups := referenceCSS.FindSubmatchIndex(value)
			for i := 2; i+1 < len(groups); i += 2 {
				if groups[i] >= 0 {
					source := string(value[groups[i]:groups[i+1]])
					updated := apply(source, true)
					if updated == html.UnescapeString(strings.TrimSpace(source)) {
						return value
					}
					// A quoted CSS string is safe in both a stylesheet and a
					// style attribute (escaped as HTML by transformTag below).
					updated = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\a `, "\r", `\d `).Replace(updated)
					return []byte(`url("` + updated + `")`)
				}
			}
			return value
		})
	}
	transformTag := func(tag []byte) []byte {
		return referenceAttributes.ReplaceAllFunc(tag, func(attribute []byte) []byte {
			match := referenceAttributes.FindSubmatch(attribute)
			name := strings.ToLower(string(match[2]))
			if name == "style" {
				style := html.UnescapeString(firstReferenceValue(match[3:]))
				updated := string(transformCSS([]byte(style)))
				if updated == style {
					return attribute
				}
				return []byte(string(match[1]) + `"` + html.EscapeString(updated) + `"`)
			}
			if name != "src" && name != "href" && name != "background" && name != "srcset" {
				return attribute
			}
			source := firstReferenceValue(match[3:])
			if name == "srcset" {
				// Uploaded data images are handled by the editor before saving.
				// Keep a data URL intact so the send validator can reject it.
				if strings.HasPrefix(strings.ToLower(source), "data:") {
					source = apply(source, true)
				} else {
					candidates := strings.Split(source, ",")
					for i, candidate := range candidates {
						fields := strings.Fields(candidate)
						if len(fields) > 0 {
							fields[0] = apply(fields[0], true)
							candidates[i] = strings.Join(fields, " ")
						}
					}
					source = strings.Join(candidates, ", ")
				}
			} else {
				source = apply(source, name != "href")
			}
			return []byte(string(match[1]) + `"` + html.EscapeString(source) + `"`)
		})
	}
	var document bytes.Buffer
	inStyle := false
	tokenizer := nethtml.NewTokenizer(bytes.NewReader(body))
	for {
		kind := tokenizer.Next()
		if kind == nethtml.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return nil, tokenizer.Err()
			}
			break
		}
		raw := tokenizer.Raw()
		switch kind {
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			inStyle = bytes.EqualFold(name, []byte("style"))
			raw = transformTag(raw)
		case nethtml.EndTagToken:
			inStyle = false
		case nethtml.TextToken:
			if inStyle {
				raw = transformCSS(raw)
			}
		case nethtml.CommentToken:
			// Outlook's conditional comments contain real image/VML sources.
			if bytes.Contains(bytes.ToLower(raw), []byte("[if")) {
				raw = referenceTags.ReplaceAllFunc(raw, transformTag)
			}
		}
		document.Write(raw)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return document.Bytes(), nil
}

func firstReferenceValue(values [][]byte) string {
	for _, value := range values {
		if len(value) > 0 {
			return string(value)
		}
	}
	return ""
}

// BodyReferences returns distinct references without changing the body.
func BodyReferences(body []byte) []Reference {
	var refs []Reference
	seen := map[Reference]bool{}
	_, _ = TransformReferences(body, func(ref Reference) (string, error) {
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
		return ref.Source, nil
	})
	return refs
}

// ProtectedReference recognizes only this application's media URLs. Exact IDs
// never fall back to a filename clone. A zero ID denotes a legacy filename URL.
func ProtectedReference(source, rootURL string) (id int, filename string, protected bool, err error) {
	if strings.HasPrefix(source, "cid:media-") && strings.HasSuffix(source, "@listmonk") {
		id, err = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(source, "cid:media-"), "@listmonk"))
		if err == nil && id < 1 {
			err = fmt.Errorf("invalid protected media content ID")
		}
		return id, "", true, err
	}
	u, parseErr := url.Parse(source)
	if parseErr != nil {
		return 0, "", false, nil
	}
	root, _ := url.Parse(rootURL)
	if u.Host != "" && (root == nil || !strings.EqualFold(u.Host, root.Host)) && !localReferenceHost(u.Hostname()) {
		return 0, "", false, nil
	}
	var rest string
	switch {
	case strings.HasPrefix(u.Path, "/api/media/file/"):
		rest = strings.TrimPrefix(u.Path, "/api/media/file/")
	case strings.HasPrefix(u.Path, "/uploads/"):
		rest = strings.TrimPrefix(u.Path, "/uploads/")
	default:
		return 0, "", false, nil
	}
	parts := strings.Split(rest, "/")
	if strings.HasPrefix(u.Path, "/api/media/file/") && len(parts) == 2 {
		id, err = strconv.Atoi(parts[0])
		filename = parts[1]
		if err != nil || id < 1 || filename == "" {
			return 0, "", true, fmt.Errorf("invalid protected media reference")
		}
	} else if len(parts) == 1 && parts[0] != "" {
		filename = parts[0]
	} else {
		return 0, "", true, fmt.Errorf("invalid protected media reference")
	}
	return id, filename, true, nil
}

func localReferenceHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

// HasTemplateExpression is used only before rendering. Actual SMTP validation
// always checks the fully rendered customer-specific value.
func HasTemplateExpression(source string) bool {
	return bytes.Contains([]byte(source), []byte("{{"))
}
