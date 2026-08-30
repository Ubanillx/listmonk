package email

import "testing"

func TestIsMessengerName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "email", want: true},
		{name: "email-primary", want: true},
		{name: "email-", want: true},
		{name: "emailwebhook", want: false},
		{name: "email_webhook", want: false},
		{name: "postback", want: false},
		{name: "", want: false},
	}

	for _, tt := range tests {
		if got := IsMessengerName(tt.name); got != tt.want {
			t.Errorf("IsMessengerName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
