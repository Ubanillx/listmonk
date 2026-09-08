package mailbox

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const maxTestMessage = 5 * 1024 * 1024

type TestStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type TestResult struct {
	Status  string          `json:"status"`
	Count   int             `json:"count"`
	Steps   []TestStep      `json:"steps"`
	Message *MessagePreview `json:"message,omitempty"`
}

// Test reads only the highest POP message number in this session. It never
// deletes mail or enqueues a bounce event. POP does not provide arrival dates.
func Test(ctx context.Context, opt Opt) TestResult {
	out := TestResult{Status: "failed", Steps: []TestStep{{Name: "connect", Status: "skipped"}, {Name: "login", Status: "skipped"}, {Name: "read", Status: "skipped"}, {Name: "parse", Status: "skipped"}}}
	fail := func(step int, err error) TestResult {
		out.Steps[step].Status = "failed"
		out.Steps[step].Detail = err.Error()
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := openTestPOP(ctx, opt)
	if err != nil {
		return fail(0, err)
	}
	defer c.Close()
	out.Steps[0].Status = "success"
	if opt.AuthProtocol != "none" {
		if _, err = c.command("USER " + opt.Username); err != nil {
			return fail(1, err)
		}
		if _, err = c.command("PASS " + opt.Password); err != nil {
			return fail(1, err)
		}
	}
	out.Steps[1].Status = "success"
	s, err := c.command("STAT")
	if err != nil {
		return fail(2, err)
	}
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return fail(2, fmt.Errorf("invalid STAT response"))
	}
	out.Count, err = strconv.Atoi(fields[0])
	if err != nil || out.Count < 0 {
		return fail(2, fmt.Errorf("invalid mailbox count"))
	}
	if out.Count == 0 {
		out.Status = "empty"
		out.Steps[2].Status = "empty"
		_, _ = c.command("QUIT")
		return out
	}
	s, err = c.command(fmt.Sprintf("LIST %d", out.Count))
	if err != nil {
		return fail(2, err)
	}
	var id, size int
	if _, err = fmt.Sscanf(s, "%d %d", &id, &size); err != nil || id != out.Count || size < 0 {
		return fail(2, fmt.Errorf("invalid LIST response"))
	}
	if size > maxTestMessage {
		return fail(2, fmt.Errorf("message exceeds 5 MiB limit"))
	}
	if _, err = c.command(fmt.Sprintf("RETR %d", out.Count)); err != nil {
		return fail(2, err)
	}
	raw, err := io.ReadAll(io.LimitReader(c.text.DotReader(), maxTestMessage+1))
	if err != nil {
		return fail(2, err)
	}
	if len(raw) > maxTestMessage {
		return fail(2, fmt.Errorf("message exceeds 5 MiB limit"))
	}
	out.Steps[2].Status = "success"
	// Finish the mailbox session before parsing. No DELE command exists here.
	_, _ = c.command("QUIT")
	out.Message, err = parsePreview(raw)
	if err != nil {
		return fail(3, err)
	}
	out.Status = "success"
	out.Steps[3].Status = "success"
	if !out.Message.IsBounce {
		out.Status = "not_bounce"
		out.Steps[3].Status = "not_bounce"
	}
	return out
}

type testPOP struct {
	net.Conn
	text *textproto.Conn
	stop func() bool
}

func (c *testPOP) Close() error { c.stop(); return c.Conn.Close() }
func (c *testPOP) response() (string, error) {
	s, err := c.text.ReadLine()
	if err != nil {
		return "", err
	}
	if s != "+OK" && !strings.HasPrefix(s, "+OK ") {
		return "", fmt.Errorf("POP server: %s", s)
	}
	return strings.TrimSpace(strings.TrimPrefix(s, "+OK")), nil
}
func (c *testPOP) command(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n") {
		return "", fmt.Errorf("invalid POP command value")
	}
	if err := c.text.PrintfLine("%s", s); err != nil {
		return "", err
	}
	return c.response()
}

func openTestPOP(ctx context.Context, opt Opt) (*testPOP, error) {
	raw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(opt.Host, strconv.Itoa(opt.Port)))
	if err != nil {
		return nil, err
	}
	c := &testPOP{Conn: raw, stop: context.AfterFunc(ctx, func() { raw.Close() })}
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	upgrade := func() error {
		t := tls.Client(c.Conn, &tls.Config{ServerName: opt.Host, InsecureSkipVerify: opt.TLSSkipVerify})
		if err := t.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("TLS: %w", err)
		}
		c.Conn = t
		c.text = textproto.NewConn(t)
		return nil
	}
	c.text = textproto.NewConn(raw)
	if opt.TLSEnabled && !opt.StartTLS {
		err = upgrade()
	}
	if err == nil {
		_, err = c.response()
	}
	if err == nil && opt.StartTLS {
		_, err = c.command("STLS")
		if err == nil {
			err = upgrade()
		} else {
			err = fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// startTLSDialer adapts STLS to the existing background POP scanner. The
// library expects a greeting on a newly dialed connection; STLS consumed it.
type startTLSDialer struct{ opt Opt }
type greetedConn struct {
	net.Conn
	reader  io.Reader
	closeFn func() error
}

func (c *greetedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
func (c *greetedConn) Close() error               { return c.closeFn() }
func (d startTLSDialer) Dial(_, _ string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	c, err := openTestPOP(ctx, d.opt)
	if err != nil {
		cancel()
		return nil, err
	}
	return &greetedConn{Conn: c.Conn, reader: io.MultiReader(strings.NewReader("+OK\r\n"), bufio.NewReader(c.Conn)), closeFn: func() error {
		err := c.Close()
		cancel()
		return err
	}}, nil
}
