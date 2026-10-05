package main

import (
	"encoding/json"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func redactCustomerExportProfile(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	redact := func(profile map[string]any) {
		delete(profile, "email")
		delete(profile, "uuid")
		delete(profile, "attribs")
		delete(profile, "attributes")
	}
	switch profile := value.(type) {
	case map[string]any:
		redact(profile)
	case []any:
		for _, row := range profile {
			if record, ok := row.(map[string]any); ok {
				redact(record)
			}
		}
	}
	return json.Marshal(value)
}

// Sharing is an action permission, independent of resource ownership and
// maintenance. Unchanged visibility can be saved without a sharing grant.
func requireAssetSharing(user auth.User, previous, next string) error {
	if next == "" || next == previous {
		return nil
	}
	if previous == models.ResourceVisibilityOrganization || previous == models.ResourceVisibilityGlobal ||
		next == models.ResourceVisibilityOrganization || next == models.ResourceVisibilityGlobal {
		return requireLegacyPermission(user, auth.PermAssetsShare)
	}
	return nil
}

func requireMailboxPermission(c echo.Context, permissions ...string) error {
	return requireLegacyPermission(auth.GetUser(c), permissions...)
}

func canManagePoolMaster(user auth.User) bool {
	return user.HasPerm(auth.PermPoolsMasterManage)
}

func canManagePoolDelivery(user auth.User) bool {
	return user.HasPerm(auth.PermPoolsDeliveryManage)
}

// Pool master data and ordinary lists use different action permissions.
// The Core ownership check still runs separately, including in the write tx.
func requireCustomerListAction(user auth.User, listType string, deleting bool) error {
	if listType == models.CustomerListTypePool {
		return requireLegacyPermission(user, auth.PermPoolsMasterManage)
	}
	if deleting {
		return requireLegacyPermission(user, auth.PermListDelete)
	}
	return nil
}

func requireCampaignMailboxSelection(user auth.User, previous, next models.Campaign) error {
	if (next.SMTPSource != "" && next.SMTPSource != previous.SMTPSource) ||
		next.SMTPPoolID != previous.SMTPPoolID || next.ReplyMailboxID != previous.ReplyMailboxID {
		return requireLegacyPermission(user, auth.PermMailboxesUse)
	}
	return nil
}

// A folder move can change the audience inherited by its contents. Private
// folder moves remain maintenance; entering or leaving shared folders also
// needs the sharing capability.
func (a *App) requireMediaPlacementSharing(c echo.Context, sourceID, targetID int) error {
	if sourceID == targetID {
		return nil
	}
	var shared bool
	if err := a.db.Get(&shared, `WITH RECURSIVE ancestors AS (
		SELECT id,parent_id,visibility FROM media_folders WHERE id IN ($1,$2)
		UNION SELECT f.id,f.parent_id,f.visibility FROM media_folders f JOIN ancestors a ON f.id=a.parent_id
	) SELECT EXISTS(SELECT 1 FROM ancestors WHERE visibility IN ('organization','global'))`, sourceID, targetID); err != nil {
		return err
	}
	if shared {
		return requireLegacyPermission(auth.GetUser(c), auth.PermAssetsShare)
	}
	return nil
}
