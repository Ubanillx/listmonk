package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

func privilegeTestContext(t *testing.T, caller auth.User) echo.Context {
	t.Helper()

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, caller)

	return c
}

func httpStatusOf(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return 0
	}
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected an *echo.HTTPError, got %T: %v", err, err)
	}

	return he.Code
}

// TestRequireRoleAssignmentAllowed locks the guard that stops a user holding
// only `users:manage` from granting itself (or anybody) the Super Admin role.
func TestRequireRoleAssignmentAllowed(t *testing.T) {
	regular := auth.User{Base: auth.Base{ID: 5}, UserRoleID: 3}
	platformAdmin := auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}

	for _, tc := range []struct {
		name   string
		caller auth.User
		roleID int
		want   int
	}{
		{"platform admin assigns super admin", platformAdmin, auth.SuperAdminRoleID, 0},
		{"platform admin assigns regular role", platformAdmin, 2, 0},
		{"users:manage user assigns super admin", regular, auth.SuperAdminRoleID, http.StatusForbidden},
		{"users:manage user assigns regular role", regular, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := privilegeTestContext(t, tc.caller)
			if got := httpStatusOf(t, requireRoleAssignmentAllowed(c, tc.roleID)); got != tc.want {
				t.Fatalf("got status %d, want %d", got, tc.want)
			}
		})
	}
}

// TestRequireSuperAdminManageable locks the guard that stops a user holding only
// `users:manage` from modifying or deleting a platform administrator.
func TestRequireSuperAdminManageable(t *testing.T) {
	regular := auth.User{Base: auth.Base{ID: 5}, UserRoleID: 3}
	platformAdmin := auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}

	for _, tc := range []struct {
		name   string
		caller auth.User
		target auth.User
		want   int
	}{
		{"platform admin manages platform admin", platformAdmin, auth.User{Base: auth.Base{ID: 9}, UserRoleID: auth.SuperAdminRoleID}, 0},
		{"platform admin manages regular user", platformAdmin, auth.User{Base: auth.Base{ID: 9}, UserRoleID: 3}, 0},
		{"users:manage user manages platform admin", regular, auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}, http.StatusForbidden},
		{"users:manage user manages regular user", regular, auth.User{Base: auth.Base{ID: 9}, UserRoleID: 3}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := privilegeTestContext(t, tc.caller)
			if got := httpStatusOf(t, requireSuperAdminManageable(c, tc.target)); got != tc.want {
				t.Fatalf("got status %d, want %d", got, tc.want)
			}
		})
	}
}
