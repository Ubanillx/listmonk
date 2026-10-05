package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	auditlog "github.com/knadh/listmonk/internal/audit"
)

// This file holds the metadata helpers shared by the audit producers. They exist
// so every "what changed" summary is built the same way and cannot accidentally
// grow a credential, a token, a message body or an attachment: callers pass
// small, already-safe values (statuses, roles, ids, permission names), and the
// helpers drop empty entries and truncate long text.

// auditStateChange summarizes one object's before/after state. Only fields whose
// value actually changed are reported, both sides are limited to the reported
// keys, and everything goes through auditDetailMap, which drops empty strings and
// truncates long text. Passing nil for either side is allowed: a creation has no
// "before", a deletion has no "after".
func auditStateChange(before, after map[string]any) map[string]any {
	before = auditDetailMap(before)
	after = auditDetailMap(after)

	changed := make([]string, 0, len(after)+len(before))
	for key := range after {
		if !sameAuditValue(before[key], after[key]) {
			changed = append(changed, key)
		}
	}
	for key := range before {
		if _, present := after[key]; !present {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	sort.Strings(changed)

	beforeOut := make(map[string]any, len(changed))
	afterOut := make(map[string]any, len(changed))
	for _, key := range changed {
		if value, ok := before[key]; ok {
			beforeOut[key] = value
		}
		if value, ok := after[key]; ok {
			afterOut[key] = value
		}
	}

	out := map[string]any{"changed_fields": changed}
	if len(beforeOut) > 0 {
		out["before"] = beforeOut
	}
	if len(afterOut) > 0 {
		out["after"] = afterOut
	}
	return out
}

// sameAuditValue compares two metadata values by their rendered form, which is
// enough for the scalar values these summaries carry (string, bool, int, null).
func sameAuditValue(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// auditPermissionDiff reports the grants a permission change added or removed.
// It is used for role and API key updates, where the permission list is the part
// of the change an operator has to be able to audit afterwards.
func auditPermissionDiff(before, after []string) map[string]any {
	beforeSet := make(map[string]struct{}, len(before))
	for _, permission := range before {
		if permission = strings.TrimSpace(permission); permission != "" {
			beforeSet[permission] = struct{}{}
		}
	}
	afterSet := make(map[string]struct{}, len(after))
	for _, permission := range after {
		if permission = strings.TrimSpace(permission); permission != "" {
			afterSet[permission] = struct{}{}
		}
	}

	added := make([]string, 0)
	for permission := range afterSet {
		if _, ok := beforeSet[permission]; !ok {
			added = append(added, permission)
		}
	}
	removed := make([]string, 0)
	for permission := range beforeSet {
		if _, ok := afterSet[permission]; !ok {
			removed = append(removed, permission)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil
	}
	sort.Strings(added)
	sort.Strings(removed)

	out := map[string]any{}
	if len(added) > 0 {
		out["added"] = added
	}
	if len(removed) > 0 {
		out["removed"] = removed
	}
	return out
}

// recordAuditEvent writes one background audit event that carries an actor
// identity. recordBackgroundAudit always records a system actor without a user,
// token or request identity, which is the wrong shape for a flow that completes
// after the request that started it (an import session, an aggregated bounce
// batch). Failures are logged and never change the business result.
func (a *App) recordAuditEvent(event auditlog.Event) {
	if a == nil || a.audit == nil {
		return
	}
	if strings.TrimSpace(event.ActorType) == "" {
		event.ActorType = "system"
	}
	if strings.TrimSpace(event.Result) == "" {
		event.Result = "success"
	}
	if err := a.audit.Record(context.Background(), event); err != nil {
		a.log.Printf("error recording audit event %s: %v", event.Action, err)
	}
}
