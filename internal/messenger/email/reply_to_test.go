package email

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
)

// The receiving mail client only sees the wire format, so assert the exact
// header line the SMTP transaction carries rather than the Message struct.
func TestSMTPDataCarriesFullReplyToList(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	data := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				fmt.Fprint(conn, "220 localhost SMTP\r\n")
				reader := bufio.NewReader(conn)
				var body strings.Builder
				inData := false
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					trimmed := strings.TrimRight(line, "\r\n")
					if inData {
						if trimmed == "." {
							inData = false
							data <- body.String()
							fmt.Fprint(conn, "250 accepted\r\n")
							continue
						}
						body.WriteString(trimmed + "\r\n")
						continue
					}
					switch {
					case trimmed == "DATA":
						inData = true
						fmt.Fprint(conn, "354 send data\r\n")
					case trimmed == "QUIT":
						fmt.Fprint(conn, "221 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "250 localhost\r\n")
					}
				}
			}()
		}
	}()

	server := Server{UUID: t.Name(), AuthProtocol: "none", TLSType: "none", Opt: smtppool.Opt{
		Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
		MaxConns: 1, IdleTimeout: time.Second, PoolWaitTimeout: time.Second,
	}}
	mailer, err := New("reply-to", server)
	if err != nil {
		t.Fatal(err)
	}
	defer mailer.Close()
	if err := mailer.Push(models.Message{
		From:        "sender@example.test",
		To:          []string{"customer@example.test"},
		Subject:     "reply list",
		ContentType: "plain",
		Body:        []byte("hello"),
		Headers:     textproto.MIMEHeader{"Reply-To": []string{"first@example.com, second@example.com"}},
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case raw := <-data:
		if !strings.Contains(raw, "Reply-To: first@example.com, second@example.com\r\n") {
			t.Fatalf("SMTP DATA does not carry the full Reply-To list:\n%s", raw)
		}
		if strings.Contains(raw, "=?utf-8?") {
			t.Fatalf("Reply-To was encoded instead of written as a plain list:\n%s", raw)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no SMTP data received")
	}
}
