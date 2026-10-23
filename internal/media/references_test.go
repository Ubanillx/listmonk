package media

import (
	"strings"
	"testing"
)

func TestBodyReferencesIncludesEveryImageSource(t *testing.T) {
	body := []byte(`<img src='/api/media/file/95/signature.jpg' srcset="/uploads/small.png 1x, /uploads/large.png 2x"><td background=/uploads/bg.png style="background-image:url('/uploads/css.png')"><a href="https://example.test/?a=1&amp;b=2">View</a>`)
	refs := BodyReferences(body)
	if len(refs) != 6 {
		t.Fatalf("references = %+v", refs)
	}
	for _, ref := range refs {
		if ref.Source == "https://example.test/?a=1&b=2" && ref.Image {
			t.Fatal("ordinary hyperlink classified as an image")
		}
	}
	updated, err := TransformReferences(body, func(ref Reference) (string, error) {
		return strings.ReplaceAll(ref.Source, "/uploads/", "https://public.test/"), nil
	})
	if err != nil || strings.Contains(string(updated), "/uploads/") {
		t.Fatalf("not all source forms transformed: %s %v", updated, err)
	}
}

func TestProtectedReferenceUsesExactIdentityAndOrigin(t *testing.T) {
	for _, source := range []string{"/api/media/file/95/E-mail%E2%80%94Outdoor.jpg", "https://mail.test/api/media/file/95/E-mail%E2%80%94Outdoor.jpg"} {
		id, name, ok, err := ProtectedReference(source, "https://mail.test")
		if err != nil || !ok || id != 95 || name != "E-mail—Outdoor.jpg" {
			t.Fatalf("parse %s = %d %s %t %v", source, id, name, ok, err)
		}
	}
	if _, _, ok, _ := ProtectedReference("https://external.test/api/media/file/95/secret.jpg", "https://mail.test"); ok {
		t.Fatal("foreign host gained a local media dependency")
	}
}

func TestCSSReferencesPreserveHTMLAndStylesheetEscaping(t *testing.T) {
	body := []byte(`<style>.photo{background:url('/uploads/a.png')}</style><td style="background:url(&quot;/uploads/a.png&quot;)"><p>Example url('/uploads/just-text.png')</p>`)
	refs := BodyReferences(body)
	if len(refs) != 1 || refs[0].Source != "/uploads/a.png" {
		t.Fatalf("encoded style or plain text was parsed incorrectly: %+v", refs)
	}
	out, err := TransformReferences(body, func(ref Reference) (string, error) { return "https://public.test/a.png?a=1&b=2", nil })
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, `url("https://public.test/a.png?a=1&b=2")`) ||
		!strings.Contains(text, `url(&#34;https://public.test/a.png?a=1&amp;b=2&#34;)`) ||
		!strings.Contains(text, `Example url('/uploads/just-text.png')`) {
		t.Fatalf("CSS URL changed markup or escaping: %s", out)
	}
}
