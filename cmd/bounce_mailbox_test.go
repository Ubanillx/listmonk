package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestBounceMailboxHTTPEnvelope(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan []string, 1)
	go func() {
		var commands []string
		defer func() { done <- commands }()
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		fmt.Fprint(c, "+OK ready\r\n")
		r := bufio.NewReader(c)
		for {
			line, e := r.ReadString('\n')
			if e != nil {
				return
			}
			line = strings.TrimSpace(line)
			commands = append(commands, line)
			if line == "STAT" {
				fmt.Fprint(c, "+OK 0 0\r\n")
			} else {
				fmt.Fprint(c, "+OK bye\r\n")
				return
			}
		}
	}()
	body := fmt.Sprintf(`{"host":"127.0.0.1","port":%d,"auth_protocol":"none","scan_interval":"15m"}`, ln.Addr().(*net.TCPAddr).Port)
	e := echo.New()
	r := httptest.NewRequest(http.MethodPost, "/api/settings/bounce/mailbox/test", strings.NewReader(body))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := (&App{}).TestBounceMailbox(e.NewContext(r, rec)); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data struct {
			Status string        `json:"status"`
			Steps  []interface{} `json:"steps"`
		} `json:"data"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Data.Status != "empty" || len(response.Data.Steps) != 4 {
		t.Fatalf("%s %v", rec.Body, err)
	}
	if commands := strings.Join(<-done, ","); commands != "STAT,QUIT" {
		t.Fatal(commands)
	}
}

func TestBounceMailboxRejectsInvalidConfig(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{"host":"localhost","port":0,"auth_protocol":"none"}`,
		`{"host":"localhost","port":110,"auth_protocol":"invalid"}`,
		`{"type":"imap","host":"localhost","port":110,"auth_protocol":"none"}`,
		`{"host":"localhost","port":110,"auth_protocol":"none","tls_enabled":true,"starttls":true}`,
	} {
		e := echo.New()
		r := httptest.NewRequest(http.MethodPost, "/api/settings/bounce/mailbox/test", strings.NewReader(body))
		r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c := e.NewContext(r, httptest.NewRecorder())
		err := (&App{}).TestBounceMailbox(c)
		h, ok := err.(*echo.HTTPError)
		if !ok || h.Code != 400 {
			t.Fatalf("%s: %v", body, err)
		}
	}
}

func TestBounceMailboxAcceptsSettingsForm(t *testing.T) {
	var req bounceMailboxTestRequest
	// scan_interval is a persisted duration string, not time.Duration JSON.
	err := json.Unmarshal([]byte(`{"host":"localhost","port":110,"auth_protocol":"none","scan_interval":"15m","starttls":true}`), &req)
	if err != nil || !req.StartTLS {
		t.Fatalf("%+v %v", req, err)
	}
}
