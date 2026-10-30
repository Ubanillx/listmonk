package email

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
)

func TestCampaignSMTPFailureYieldsWithoutInlineRetry(t *testing.T) {
	for _, kind := range []string{"regular", "optin", "transactional"} {
		t.Run(kind, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var attempts atomic.Int32
			go func() {
				for {
					connection, err := listener.Accept()
					if err != nil {
						return
					}
					attempts.Add(1)
					connection.Close() // EOF is a retryable SMTP connection failure.
				}
			}()
			sender, err := New(MessengerName, Server{UUID: t.Name(), AuthProtocol: "none", TLSType: "none", Opt: smtppool.Opt{
				Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, MaxConns: 1, MaxMessageRetries: 3, PoolWaitTimeout: time.Second,
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			message := models.Message{From: "sender@example.test", To: []string{"customer@example.test"}, ContentType: "plain", Body: []byte("test")}
			if kind != "transactional" {
				message.Campaign = &models.Campaign{Type: kind}
			}
			if err := sender.Push(message); err == nil {
				t.Fatal("SMTP failure disappeared")
			}
			want := int32(3)
			if kind == "regular" {
				want = 1
			}
			if got := attempts.Load(); got != want {
				t.Fatalf("%s used %d inline attempts, want %d", kind, got, want)
			}
		})
	}
}
