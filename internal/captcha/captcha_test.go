package captcha

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestHCaptcha(t *testing.T, handler http.HandlerFunc) *Captcha {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	orig := hCaptchaURL
	hCaptchaURL = srv.URL
	t.Cleanup(func() { hCaptchaURL = orig })

	o := Opt{}
	o.HCaptcha.Enabled = true
	o.HCaptcha.Secret = "test-secret"

	return New(o)
}

// TestHCaptchaFailsClosedOnInvalidResponse locks the fail-closed behavior: a
// 200 response that is not the expected JSON (an upstream error page or an
// intermediary injecting HTML) must not waive the CAPTCHA.
func TestHCaptchaFailsClosedOnInvalidResponse(t *testing.T) {
	c := newTestHCaptcha(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>proxy error</html>"))
	})

	err, ok := c.Verify("token")
	if ok {
		t.Fatal("an unparseable hCaptcha response must not verify")
	}
	if err == nil {
		t.Fatal("expected an error for an unparseable hCaptcha response")
	}
}

// TestHCaptchaHonorsSuccessFlag verifies the regular success/failure paths are
// unchanged.
func TestHCaptchaHonorsSuccessFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"success", `{"success": true}`, true},
		{"failure", `{"success": false, "error_codes": ["invalid-input-response"]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestHCaptcha(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			})

			_, ok := c.Verify("token")
			if ok != tc.want {
				t.Fatalf("Verify returned ok=%v, want %v", ok, tc.want)
			}
		})
	}
}
