package core

import (
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func TestNormalizeOrganizationMemberAssignments(t *testing.T) {
	assignments, err := normalizeOrganizationMemberAssignments([]models.OrganizationMemberAssignment{
		{UserID: 4, Role: models.OrganizationMemberRoleManager},
		{UserID: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 2 || assignments[1].Role != models.OrganizationMemberRoleMember {
		t.Fatalf("normalized assignments = %+v", assignments)
	}
	if !hasOrganizationManager(assignments) {
		t.Fatal("expected a manager assignment")
	}
}

func TestNormalizeOrganizationMemberAssignmentsRejectsDuplicatesAndRoles(t *testing.T) {
	tests := []struct {
		name        string
		assignments []models.OrganizationMemberAssignment
	}{
		{name: "duplicate", assignments: []models.OrganizationMemberAssignment{{UserID: 4}, {UserID: 4}}},
		{name: "invalid role", assignments: []models.OrganizationMemberAssignment{{UserID: 4, Role: "owner"}}},
		{name: "missing user", assignments: []models.OrganizationMemberAssignment{{Role: models.OrganizationMemberRoleManager}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeOrganizationMemberAssignments(tt.assignments)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if _, ok := err.(*echo.HTTPError); !ok {
				t.Fatalf("error = %T, want *echo.HTTPError", err)
			}
		})
	}
}

func TestHasOrganizationManager(t *testing.T) {
	if hasOrganizationManager([]models.OrganizationMemberAssignment{{UserID: 4, Role: models.OrganizationMemberRoleMember}}) {
		t.Fatal("member-only assignments must not satisfy manager requirement")
	}
}
