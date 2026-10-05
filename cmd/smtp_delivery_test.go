package main

import (
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

func TestPersonalSMTPUsesPlatformDeliverySettings(t *testing.T) {
	platform := models.SMTPDeliverySettings{
		MaxConns: 7, MaxMsgRetries: 3, IdleTimeout: "19s", WaitTimeout: "8s",
		EmailHeaders: models.Headers{{"X-Platform": "yes"}},
		SendDelayMin: 2000, SendDelayMax: 5000,
	}
	row := models.PersonalSMTPServer{SMTPServer: models.SMTPServer{
		Host: "smtp.example.test", Port: 587, AuthProtocol: "none",
		MaxConns: 99, MaxMsgRetries: 99, IdleTimeout: "1h", WaitTimeout: "1h",
		TLSType: "none", TLSSkipVerify: true,
		EmailHeaders: models.Headers{{"X-Account": "no"}},
	}}
	if err := validatePersonalSMTP(&row, platform, nil); err != nil {
		t.Fatal(err)
	}
	if row.MaxConns != 7 || row.MaxMsgRetries != 3 || row.TLSType != "none" || !row.TLSSkipVerify {
		t.Fatalf("account SMTP retained transport overrides: %+v", row.SMTPServer)
	}
	server, err := mapSMTPServerWithDelivery(models.PersonalSMTPServer{SMTPServer: models.SMTPServer{
		Host: "smtp.example.test", Port: 587, TLSType: "none", MaxConns: 99,
		IdleTimeout: "1h", WaitTimeout: "1h", EmailHeaders: models.Headers{{"X-Account": "no"}},
	}}, platform)
	if err != nil {
		t.Fatal(err)
	}
	if server.TLSType != "none" || server.Opt.MaxConns != 7 ||
		server.Opt.IdleTimeout != 19*time.Second || server.Opt.PoolWaitTimeout != 8*time.Second ||
		server.SendDelayMin != 2*time.Second || server.SendDelayMax != 5*time.Second ||
		server.EmailHeaders["X-Platform"] != "yes" || server.EmailHeaders["X-Account"] != "" {
		t.Fatalf("delivery did not use platform settings: %+v", server)
	}
}
