package core

import (
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestApplyReplyAIActionRejectsProductComplaint(t *testing.T) {
	err := (&Core{}).ApplyReplyAIAction(models.WorkspaceAccess{}, ReplyAIAction{
		EventID:    1,
		CustomerID: 1,
		Intent:     models.ReplyAIIntentProductComplaint,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported intent") {
		t.Fatalf("error = %v, want unsupported intent", err)
	}
}
