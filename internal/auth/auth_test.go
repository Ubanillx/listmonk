package auth

import "testing"

func TestHashIntegrationTokenDeterministic(t *testing.T) {
	tok := "lmit_example_secret"

	a := HashIntegrationToken(tok)
	b := HashIntegrationToken(tok)

	if a == "" {
		t.Fatal("expected non-empty token hash")
	}
	if a != b {
		t.Fatalf("expected deterministic hash, got %q and %q", a, b)
	}
}

func TestGetIntegrationToken(t *testing.T) {
	user := User{Username: "svc-openclaw"}
	tok := "lmit_example_secret"

	a := &Auth{
		integrationTokens: map[string]IntegrationToken{
			HashIntegrationToken(tok): {
				Base: Base{ID: 9},
				User: user,
			},
		},
	}

	gotUser, gotID, ok := a.GetIntegrationToken(tok)
	if !ok {
		t.Fatal("expected token lookup to succeed")
	}
	if gotID != 9 {
		t.Fatalf("expected token id 9, got %d", gotID)
	}
	if gotUser.Username != user.Username {
		t.Fatalf("expected username %q, got %q", user.Username, gotUser.Username)
	}

	if _, _, ok := a.GetIntegrationToken("wrong"); ok {
		t.Fatal("expected invalid token lookup to fail")
	}
}
