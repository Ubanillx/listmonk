package main

import (
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"gopkg.in/volatiletech/null.v6"
)

func TestValidateBulkUserImportCreatesRegularUsers(t *testing.T) {
	users, issues := validateBulkUserImport(
		[]bulkUserImportRow{{
			Username: "editor1",
			Password: "password1",
			Email:    "editor1@example.com",
			UserRole: "Editors",
			ListRole: "3",
		}},
		[]auth.Role{{Base: auth.Base{ID: 2}, Name: null.NewString("Editors", true)}},
		[]auth.ListRole{{Base: auth.Base{ID: 3}, Name: null.NewString("Newsletter editors", true)}},
		nil,
	)

	if len(issues) != 0 {
		t.Fatalf("expected no validation issues, got %#v", issues)
	}
	if len(users) != 1 {
		t.Fatalf("expected one user, got %d", len(users))
	}

	user := users[0]
	if user.Name != "editor1" || user.Status != auth.UserStatusEnabled || !user.PasswordLogin {
		t.Fatalf("unexpected user defaults: %#v", user)
	}
	if user.UserRoleID != 2 || user.ListRoleID == nil || *user.ListRoleID != 3 {
		t.Fatalf("unexpected role assignment: %#v", user)
	}
}

func TestValidateBulkUserImportRejectsUnsafeAndDuplicateRows(t *testing.T) {
	_, issues := validateBulkUserImport(
		[]bulkUserImportRow{
			{Username: "editor1", Password: "password1", Email: "editor1@example.com", UserRole: "Editors"},
			{Username: "editor1", Password: "password2", Email: "editor2@example.com", UserRole: "Editors"},
			{Username: "admin2", Password: "password3", Email: "admin2@example.com", UserRole: "1"},
		},
		[]auth.Role{
			{Base: auth.Base{ID: auth.SuperAdminRoleID}, Name: null.NewString("Super Admin", true)},
			{Base: auth.Base{ID: 2}, Name: null.NewString("Editors", true)},
		},
		nil,
		nil,
	)

	got := make(map[int]string, len(issues))
	for _, issue := range issues {
		got[issue.Row] = issue.Code
	}
	if got[3] != "duplicate_username" {
		t.Fatalf("expected duplicate username issue on row 3, got %#v", issues)
	}
	if got[4] != "super_admin_role" {
		t.Fatalf("expected super admin issue on row 4, got %#v", issues)
	}
}
