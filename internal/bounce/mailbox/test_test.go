package mailbox

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The server records every command, so deletion or scanning another message
// fails the test independently of the client implementation.
func fakePOP(t *testing.T, mode, scenario string) (Opt, <-chan []string) {
	t.Helper()
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	opt := Opt{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Username: "user", Password: "pass", AuthProtocol: "userpass", TLSEnabled: mode == "tls", StartTLS: mode == "starttls", TLSSkipVerify: true}
	done := make(chan []string, 1)
	go func() {
		var commands []string
		defer func() { done <- commands }()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		upgrade := func() { c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}}) }
		if mode == "tls" {
			upgrade()
		}
		fmt.Fprint(c, "+OK ready\r\n")
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			commands = append(commands, line)
			switch {
			case line == "NOOP":
				fmt.Fprint(c, "+OK\r\n")
			case line == "STLS":
				if scenario == "stls-fail" {
					fmt.Fprint(c, "-ERR unsupported\r\n")
					continue
				}
				fmt.Fprint(c, "+OK upgrade\r\n")
				upgrade()
				r = bufio.NewReader(c)
			case strings.HasPrefix(line, "USER "):
				fmt.Fprint(c, "+OK user\r\n")
			case strings.HasPrefix(line, "PASS "):
				if scenario == "auth-fail" {
					fmt.Fprint(c, "-ERR invalid login\r\n")
				} else {
					fmt.Fprint(c, "+OK logged in\r\n")
				}
			case line == "STAT":
				if scenario == "empty" {
					fmt.Fprint(c, "+OK 0 0\r\n")
				} else {
					fmt.Fprint(c, "+OK 3 4096\r\n")
				}
			case line == "LIST 3":
				size := len(dsnFixture)
				if scenario == "large" {
					size = maxTestMessage + 1
				}
				fmt.Fprintf(c, "+OK 3 %d\r\n", size)
			case line == "RETR 3":
				if scenario == "read-fail" {
					fmt.Fprint(c, "-ERR busy\r\n")
				} else {
					raw := dsnFixture
					if scenario == "ordinary" {
						raw = "From: person@example.com\r\nSubject: Hi\r\n\r\nHello\r\n"
					}
					if scenario == "malformed" {
						raw = "bad header\r\n\r\nHi\r\n"
					}
					fmt.Fprint(c, "+OK message\r\n"+raw+".\r\n")
				}
			case line == "QUIT":
				fmt.Fprint(c, "+OK bye\r\n")
				return
			default:
				fmt.Fprint(c, "-ERR unexpected command\r\n")
			}
		}
	}()
	return opt, done
}

func TestPOPReadOnlyModes(t *testing.T) {
	for _, mode := range []string{"plain", "tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			opt, done := fakePOP(t, mode, "")
			out := Test(context.Background(), opt)
			if out.Status != "success" || out.Count != 3 || out.Message.Subject != "退信" {
				t.Fatalf("%+v", out)
			}
			commands := <-done
			want := "USER user,PASS pass,STAT,LIST 3,RETR 3,QUIT"
			if mode == "starttls" {
				want = "STLS," + want
			}
			if strings.Join(commands, ",") != want {
				t.Fatalf("unexpected commands: %v", commands)
			}
		})
	}
}

func TestPOPFailures(t *testing.T) {
	for _, tc := range []struct{ scenario, status, step string }{
		{"auth-fail", "failed", "login"}, {"read-fail", "failed", "read"}, {"empty", "empty", ""}, {"large", "failed", "read"}, {"ordinary", "not_bounce", ""}, {"malformed", "failed", "parse"}, {"stls-fail", "failed", "connect"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			opt, done := fakePOP(t, "starttls", tc.scenario)
			out := Test(context.Background(), opt)
			if out.Status != tc.status {
				t.Fatalf("%+v", out)
			}
			if tc.step != "" {
				found := false
				for _, s := range out.Steps {
					if s.Name == tc.step && s.Status == "failed" {
						found = true
					}
				}
				if !found {
					t.Fatalf("%+v", out.Steps)
				}
			}
			for _, cmd := range <-done {
				if strings.HasPrefix(cmd, "DELE") {
					t.Fatal("deleted mail")
				}
			}
		})
	}
}

func TestPOPCancellation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(io.Discard, c)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	out := Test(ctx, Opt{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port})
	if out.Steps[0].Status != "failed" {
		t.Fatal(out)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection leaked after timeout")
	}
}

func TestScannerSTARTTLSCompatibility(t *testing.T) {
	opt, done := fakePOP(t, "starttls", "")
	c, err := NewPOP(opt, log.Default()).client.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Auth(opt.Username, opt.Password); err != nil {
		t.Fatal(err)
	}
	if err = c.Quit(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(<-done, ","); got != "STLS,USER user,PASS pass,NOOP,QUIT" {
		t.Fatal(got)
	}
}
