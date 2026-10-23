package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

// A webhook's signing secret can be replaced by a manager. Each rotation answers a new
// secret once, with no-store, and only managers may rotate (ADM-13).
func TestWebhookSecretIsRotatedByManagersOnly(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("webhook-rotate@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("webhook-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"webhook-member@x.test": member}, "webhook-member@x.test")

	created := owner.do("POST", "/api/webhooks", url.Values{
		"url": {"https://example.com/hook"}, "secret": {"first-secret-value"}, "events": {"*"},
	}, nil)
	readBody(t, created)
	var id string
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM webhooks LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := "/api/webhooks/" + id + "/secret"

	first := rotateWebhookSecret(t, owner, path)
	second := rotateWebhookSecret(t, owner, path)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first) {
		t.Fatalf("the rotated secret is not 32 random bytes in hex: %q", first)
	}
	if first == second || first == "first-secret-value" {
		t.Fatalf("a rotation kept the old secret: first %q, second %q", first, second)
	}

	refused := member.do("POST", path, url.Values{}, map[string]string{"Accept": "application/json"})
	readBody(t, refused)
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("a member rotated the secret: status %d, want 403", refused.StatusCode)
	}

	missing := owner.do("POST", "/api/webhooks/999999/secret", url.Values{}, map[string]string{"Accept": "application/json"})
	readBody(t, missing)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown webhook answered %d, want 404", missing.StatusCode)
	}
}

func rotateWebhookSecret(t *testing.T, e *apiEnv, path string) string {
	t.Helper()
	resp := e.do("POST", path, url.Values{}, map[string]string{"Accept": "application/json"})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate the secret: status %d, body %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("the new secret may be cached: %q", resp.Header.Get("Cache-Control"))
	}
	var answer struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %v, body %s", err, body)
	}
	return answer.Secret
}
