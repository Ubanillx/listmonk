package manager

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/knadh/listmonk/models"
)

var (
	smtpFailureCode       = regexp.MustCompile(`(?:^|:\s*)([45][0-9]{2})(?:\s|$)`)
	failureSecrets        = regexp.MustCompile(`(?i)\b(password|passwd|authorization|api[_-]?key|access[_-]?token|secret)\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)`)
	failureBearer         = regexp.MustCompile(`(?i)\bBearer\s+\S+`)
	failureURLCredentials = regexp.MustCompile(`(://)[^/\s@]+@`)
)

// sanitizeSendFailure excludes accidental credential material and invalid DB
// text while preserving SMTP diagnostics. Recipient redaction happens on read.
func sanitizeSendFailure(err error) string {
	s := failureBearer.ReplaceAllString(err.Error(), "Bearer [redacted]")
	s = failureSecrets.ReplaceAllString(s, "$1=[redacted]")
	s = failureURLCredentials.ReplaceAllString(s, "$1[redacted]@")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	r := []rune(s)
	if len(r) > 4000 {
		s = string(r[:4000]) + "…"
	}
	return s
}

func classifySendFailure(stage string, err error) (string, int) {
	if stage == "render" {
		return "render", 0
	}
	if errors.Is(err, ErrPersonalSMTPUnavailable) || errors.Is(err, ErrPoolSMTPUnavailable) {
		return "smtp_unavailable", 0
	}
	if errors.Is(err, ErrReplyMailboxUnavailable) {
		return "reply_unavailable", 0
	}
	var smtpErr *textproto.Error
	code := 0
	if errors.As(err, &smtpErr) {
		code = smtpErr.Code
	} else if m := smtpFailureCode.FindStringSubmatch(err.Error()); len(m) > 1 {
		code, _ = strconv.Atoi(m[1])
	}
	if code == 530 || code == 534 || code == 535 || code == 538 {
		return "smtp_auth", code
	}
	if code >= 400 && code < 500 {
		return "smtp_temporary", code
	}
	if code >= 500 && code < 600 {
		return "smtp_rejected", code
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "timeout", 0
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
		return "network", 0
	}
	return "other", 0
}

func makeCampaignSendFailure(c *models.Campaign, customer models.Customer, poolID int64, organizationID int, stage string, err error) models.CampaignSendFailure {
	category, code := classifySendFailure(stage, err)
	f := models.CampaignSendFailure{CampaignID: c.ID, CampaignOwnerUserID: c.OwnerUserID, CampaignOrganizationID: c.OrganizationID, RecipientType: "private", RecipientID: int64(customer.ID),
		RecipientOrganizationID: c.OrganizationID.Int, CustomerCode: customer.CustomerCode, Name: customer.Name, Email: customer.Email,
		Stage: stage, Category: category, SMTPCode: code, Error: sanitizeSendFailure(err)}
	if poolID > 0 {
		f.RecipientType = "pool"
		f.RecipientID = poolID
		if organizationID > 0 {
			f.RecipientOrganizationID = organizationID
		}
	}
	return f
}
