package web

import (
	"github.com/aa-blinov/paratrack/internal/i18n"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestPushKeysAndSubscribe(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.SQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)

	pub, priv, err := d.EnsureVAPIDKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pub == "" || priv == "" {
		t.Fatal("empty vapid keys")
	}
	// idempotent
	pub2, _, _ := d.EnsureVAPIDKeys(ctx)
	if pub2 != pub {
		t.Fatal("keys not stable")
	}
	// pub is base64url uncompressed P-256 (65 bytes)
	b, err := base64.RawURLEncoding.DecodeString(pub)
	if err != nil || len(b) != 65 {
		t.Fatalf("pub key len=%d err=%v", len(b), err)
	}

	if err := d.UpsertPushSubscription(ctx, 1, 1, "https://push.example/1", "p256dh", "auth"); err != nil {
		t.Fatal(err)
	}
	// upsert same endpoint refreshes
	if err := d.UpsertPushSubscription(ctx, 1, 1, "https://push.example/1", "p256dh2", "auth2"); err != nil {
		t.Fatal(err)
	}
	subs, _ := d.ListPushSubscriptions(ctx, 1)
	if len(subs) != 1 || subs[0].P256DH != "p256dh2" {
		t.Fatalf("subs=%+v", subs)
	}
	if err := d.DeletePushSubscription(ctx, "https://push.example/1"); err != nil {
		t.Fatal(err)
	}
	subs, _ = d.ListPushSubscriptions(ctx, 1)
	if len(subs) != 0 {
		t.Fatalf("not deleted: %+v", subs)
	}
}

func TestPushAPI(t *testing.T) {
	e := newAPIEnv(t)
	e.register("push@x.test")
	// public key endpoint
	resp := e.do("GET", "/api/push/key", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("key: %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "publicKey") {
		t.Fatalf("body=%s", body)
	}
	// subscribe
	resp = e.do("POST", "/api/push/subscribe", url.Values{
		"endpoint": {"https://push.example/x"},
		"p256dh":   {"AAA"},
		"auth":     {"BBB"},
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("subscribe: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()
	// notifications page
	resp = e.do("GET", "/settings/notifications", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("notify page: %d %s", resp.StatusCode, readBody(t, resp))
	}
	page := readBody(t, resp)
	if !strings.Contains(page, "push-enable") || !strings.Contains(page, string(i18n.T(i18n.Default, "push.devices"))) {
		t.Fatalf("page missing controls: %s", page[200:500])
	}
	// unsubscribe
	resp = e.do("POST", "/api/push/unsubscribe", url.Values{
		"endpoint": {"https://push.example/x"},
	}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("unsub: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
