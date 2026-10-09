package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/smtppool/v2"
	"github.com/labstack/echo/v4"
)

func TestOwnedSMTPTestReportsTLSConnectionTimeout(t *testing.T) {
	// Accept TCP and read ClientHello, but never answer the TLS handshake.
	// This reproduces the exact bare deadline emitted by the installed pool.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		io.Copy(io.Discard, conn)
	}()

	previous := ko
	ko = koanf.New(".")
	t.Cleanup(func() { ko = previous })
	if err := ko.Set("smtp_delivery", smtpPlatformDefaults{
		MaxConns: 1, MaxMsgRetries: 1, IdleTimeout: "1s", WaitTimeout: "1s",
	}); err != nil {
		t.Fatal(err)
	}
	a := probeTestApp(t)
	a.importer = &subimporter.Importer{}
	port := listener.Addr().(*net.TCPAddr).Port
	body := fmt.Sprintf(`{"name":"Timeout","enabled":true,"host":"127.0.0.1","port":%d,
		"tls_type":"TLS","auth_protocol":"plain","username":"private-user","password":"private-password",
		"from_email":"sender@example.invalid","email":"recipient@example.invalid","wait_timeout":"1h"}`, port)
	e := echo.New()
	r := httptest.NewRequest(http.MethodPost, "/api/profile/smtp/test", strings.NewReader(body))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(r, rec)
	c.Set(auth.UserHTTPCtxKey, permissionTestUser(auth.PermMailboxesManage))
	err = a.TestPersonalSMTP(c)
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) || !errors.Is(httpErr.Internal, context.DeadlineExceeded) {
		t.Fatalf("expected actual TLS dial deadline, got %v", err)
	}
	e.HTTPErrorHandler(err, c)
	var response struct{ Message string }
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || !strings.Contains(response.Message, listener.Addr().String()) ||
		!strings.Contains(response.Message, "timeout per attempt: 1s") ||
		!strings.Contains(response.Message, "connection attempt failed before account authentication") {
		t.Fatalf("missing actionable timeout response: %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"private-user", "private-password", "recipient@example.invalid"} {
		if strings.Contains(response.Message, secret) {
			t.Fatalf("timeout response contains private input: %s", secret)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed-out TLS connection was not closed")
	}
}

func TestSMTPTestErrorPreservesOtherFailures(t *testing.T) {
	a := probeTestApp(t)
	for _, tc := range []struct {
		name, tlsType string
		err           error
	}{
		{"authentication", "TLS", &textproto.Error{Code: 535, Msg: "Authentication credentials invalid"}},
		{"certificate", "TLS", errors.New("tls: failed to verify certificate")},
		{"STARTTLS", "STARTTLS", context.DeadlineExceeded},
		{"plain", "none", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := a.smtpTestError(tc.err, email.Server{TLSType: tc.tlsType}).(*echo.HTTPError)
			if got.Code != http.StatusInternalServerError || got.Message != tc.err.Error() || got.Internal != tc.err {
				t.Fatalf("unrelated failure changed: %#v", got)
			}
		})
	}
}

func TestSMTPTestTimeoutTranslationsAndEffectiveDeadline(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN", "zh-TW"} {
		t.Run(lang, func(t *testing.T) {
			b, err := os.ReadFile("../i18n/" + lang + ".json")
			if err != nil {
				t.Fatal(err)
			}
			translator, err := i18n.New(b)
			if err != nil {
				t.Fatal(err)
			}
			a := &App{i18n: translator}
			for _, wait := range []time.Duration{0, 500 * time.Millisecond, 8 * time.Second} {
				server := email.Server{TLSType: "TLS", Opt: smtppool.Opt{Host: "::1", Port: 465, PoolWaitTimeout: wait}}
				got := a.smtpTestError(fmt.Errorf("dial: %w", context.DeadlineExceeded), server).(*echo.HTTPError)
				message := got.Message.(string)
				want := "2s"
				if wait >= time.Second {
					want = wait.String()
				}
				if !strings.Contains(message, "[::1]:465") || !strings.Contains(message, want) ||
					strings.Contains(message, "{address}") || strings.Contains(message, "{timeout}") ||
					strings.Contains(message, "settings.smtp.tlsConnectionTimeout") {
					t.Fatalf("unusable translated diagnostic: %s", message)
				}
			}
		})
	}
}
