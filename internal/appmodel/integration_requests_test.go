package appmodel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIntegrationSyncCredentialsJSONOmitsPrivateState(t *testing.T) {
	encoded, err := json.Marshal(IntegrationSyncCredentials{
		ID: 12, TeamID: 34, Provider: "github", Secret: "provider-secret",
		Config: IntegrationConfig{Target: "owner/repo"}, Generation: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, privateValue := range []string{"12", "34", "github", "provider-secret", "owner/repo", "Generation"} {
		if strings.Contains(string(encoded), privateValue) {
			t.Fatalf("sync credentials JSON exposed private state: %s", encoded)
		}
	}
}
