package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// txTestApp returns an App sufficient for validateTxMessage's limit checks: a
// real translator for the error messages, and a config so the FromEmail default
// resolves. The checks under test return before the importer or the messenger
// manager are consulted.
func txTestApp(t *testing.T) *App {
	t.Helper()
	a := probeTestApp(t)
	a.cfg = &Config{}
	return a
}

func httpCodeOf(t *testing.T, err error) int {
	t.Helper()
	var he *echo.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected an echo HTTP error, got %v", err)
	}
	return he.Code
}

func attachment(name string, size int) models.Attachment {
	return models.Attachment{Name: name, Content: bytes.Repeat([]byte("a"), size)}
}

// TestValidateTxMessageAttachmentLimits locks the caps that keep one /api/tx
// request from buffering an unbounded amount of attachment data. The JSON path
// arrives already base64-decoded, so this is the authoritative check for it;
// the multipart path additionally rejects over-sized parts before reading them.
func TestValidateTxMessageAttachmentLimits(t *testing.T) {
	tooMany := make([]models.Attachment, 0, maxTxAttachments+1)
	for i := 0; i <= maxTxAttachments; i++ {
		tooMany = append(tooMany, attachment(fmt.Sprintf("f%d.bin", i), 1))
	}

	overTotal := make([]models.Attachment, 0, 4)
	for i := 0; i < 4; i++ {
		overTotal = append(overTotal, attachment(fmt.Sprintf("f%d.bin", i), (maxTxAttachmentsTotal/4)+1))
	}

	for _, tc := range []struct {
		name        string
		attachments []models.Attachment
		wantCode    int
	}{
		{"too many attachments", tooMany, http.StatusBadRequest},
		{"one attachment over the per-file limit", []models.Attachment{attachment("big.bin", maxTxAttachmentSize+1)}, http.StatusRequestEntityTooLarge},
		{"attachments over the total limit", overTotal, http.StatusRequestEntityTooLarge},
		{"one attachment at the per-file limit", []models.Attachment{attachment("ok.bin", maxTxAttachmentSize)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := txTestApp(t)
			_, err := a.validateTxMessage(models.TxMessage{
				CustomerIDs: []int{1},
				Attachments: tc.attachments,
			})

			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("expected the message to be accepted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the message to be rejected")
			}
			if got := httpCodeOf(t, err); got != tc.wantCode {
				t.Fatalf("expected HTTP %d, got %d (%v)", tc.wantCode, got, err)
			}
		})
	}
}

// TestValidateTxMessageRecipientLimit bounds the number of queued messages one
// request can create.
func TestValidateTxMessageRecipientLimit(t *testing.T) {
	a := txTestApp(t)

	emails := make([]string, maxTxRecipients+1)
	for i := range emails {
		emails[i] = fmt.Sprintf("customer%d@example.com", i)
	}
	if _, err := a.validateTxMessage(models.TxMessage{CustomerEmails: emails}); err == nil {
		t.Fatal("expected too many recipients to be rejected")
	} else if got := httpCodeOf(t, err); got != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400, got %d (%v)", got, err)
	}

	// IDs and emails count against the same budget, so a request that is under
	// the cap in each list separately is still rejected when combined.
	ids := make([]int, maxTxRecipients/2+1)
	for i := range ids {
		ids[i] = i + 1
	}
	half := make([]string, maxTxRecipients/2+1)
	for i := range half {
		half[i] = fmt.Sprintf("customer%d@example.com", i)
	}
	if _, err := a.validateTxMessage(models.TxMessage{CustomerIDs: ids, CustomerEmails: half}); err == nil {
		t.Fatal("expected the combined recipient count to be rejected")
	}
}
