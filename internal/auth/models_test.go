package auth

import "testing"

func TestUserIsPlatformAdminSupportsRetrievedAndPendingUsers(t *testing.T) {
	retrievedPlatformAdmin := User{}
	retrievedPlatformAdmin.UserRole.ID = SuperAdminRoleID

	tests := []struct {
		name string
		user User
		want bool
	}{
		{
			name: "retrieved platform administrator",
			user: retrievedPlatformAdmin,
			want: true,
		},
		{
			name: "user pending persistence",
			user: User{UserRoleID: SuperAdminRoleID},
			want: true,
		},
		{
			name: "ordinary user",
			user: User{UserRoleID: SuperAdminRoleID + 1},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.user.IsPlatformAdmin(); got != test.want {
				t.Fatalf("IsPlatformAdmin() = %v, want %v", got, test.want)
			}
		})
	}
}
