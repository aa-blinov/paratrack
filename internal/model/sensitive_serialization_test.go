package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSensitiveModelFieldsAreExcludedFromJSON(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		secret string
	}{
		{"user password hash", User{PasswordHash: "user-hash-marker"}, "user-hash-marker"},
		{"auth session token", AuthSession{Token: "session-token-marker"}, "session-token-marker"},
		{"invite token", TeamInvite{Token: "invite-token-marker"}, "invite-token-marker"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) {
				t.Fatalf("JSON exposed sensitive value: %s", encoded)
			}
		})
	}
}
