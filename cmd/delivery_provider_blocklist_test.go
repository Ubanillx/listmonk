package main

import (
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
)

func permissionSet(permissions ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		out[permission] = struct{}{}
	}
	return out
}

// TestProviderFailureTargetsUseTheTargetPermission keeps the row icon honest:
// viewing the report never implies the right to blocklist a private customer or
// a shared pool contact.
func TestProviderFailureTargetsUseTheTargetPermission(t *testing.T) {
	matches := []core.ProviderFailureMatch{
		{Kind: "private", ID: 1},
		{Kind: "pool", ID: 2},
	}

	viewer := auth.User{PermissionsMap: permissionSet(auth.PermDeliveryProviderGet)}
	targets := providerFailureTargets(viewer, matches)
	if len(targets) != 2 || targets[0].CanBlock || targets[1].CanBlock {
		t.Fatalf("viewer targets = %#v, want both not blockable", targets)
	}

	privateOnly := auth.User{PermissionsMap: permissionSet(auth.PermDeliveryProviderBlocklist, auth.PermCustomersBlocklist)}
	targets = providerFailureTargets(privateOnly, matches)
	if !targets[0].CanBlock || targets[1].CanBlock {
		t.Fatalf("private-only targets = %#v, want only the private target blockable", targets)
	}

	poolOnly := auth.User{PermissionsMap: permissionSet(auth.PermPoolsMasterManage)}
	targets = providerFailureTargets(poolOnly, matches)
	if targets[0].CanBlock || !targets[1].CanBlock {
		t.Fatalf("pool-only targets = %#v, want only the pool target blockable", targets)
	}

	admin := auth.User{UserRoleID: auth.SuperAdminRoleID}
	targets = providerFailureTargets(admin, matches)
	if !targets[0].CanBlock || !targets[1].CanBlock {
		t.Fatalf("platform admin targets = %#v, want both blockable", targets)
	}
}

// TestRequireProviderBlocklistAction pins the separate write permission on top
// of the existing report read permission.
func TestRequireProviderBlocklistAction(t *testing.T) {
	c, rec := providerContext(t, "/api/delivery-provider/failures/blocklist")

	c.Set(auth.UserHTTPCtxKey, auth.User{PermissionsMap: permissionSet(auth.PermDeliveryProviderGet)})
	if err := requireProviderBlocklistAction(c); err != auth.ErrPermDenied {
		t.Fatalf("read-only caller error = %v, want permission denied", err)
	}

	c.Set(auth.UserHTTPCtxKey, auth.User{PermissionsMap: permissionSet(auth.PermDeliveryProviderGet, auth.PermDeliveryProviderBlocklist)})
	if err := requireProviderBlocklistAction(c); err != nil {
		t.Fatalf("granted caller error = %v, want nil", err)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
