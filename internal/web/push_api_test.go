package web

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestPushKeysAndSubscribe(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), 1)
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)

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

	if err := d.UpsertPushSubscription(ctx, appmodel.PushSubscribeRequest{TeamID: 1, UserID: 1, CallerID: 1, Endpoint: "https://push.example/1", PublicKey: "p256dh", AuthSecret: "auth"}); err != nil {
		t.Fatal(err)
	}
	// upsert same endpoint refreshes
	if err := d.UpsertPushSubscription(ctx, appmodel.PushSubscribeRequest{TeamID: 1, UserID: 1, CallerID: 1, Endpoint: "https://push.example/1", PublicKey: "p256dh2", AuthSecret: "auth2"}); err != nil {
		t.Fatal(err)
	}
	subs, _ := d.ListPushSubscriptions(ctx, 1)
	if len(subs) != 1 || subs[0].P256DH != "p256dh2" {
		t.Fatalf("subs=%+v", subs)
	}
	if err := d.DeletePushSubscription(ctx, appmodel.PushUnsubscribeRequest{TeamID: 1, UserID: 1, CallerID: 1, Endpoint: "https://push.example/1"}); err != nil {
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
	publicKey := append([]byte{4}, make([]byte, 64)...)
	authSecret := make([]byte, 16)
	resp = e.do("POST", "/api/push/subscribe", url.Values{
		"endpoint": {"https://push.example/x"},
		"p256dh":   {base64.RawURLEncoding.EncodeToString(publicKey)},
		"auth":     {base64.RawURLEncoding.EncodeToString(authSecret)},
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
	if !reactData[notifyPage](t, page).NotificationsReact || reactData[notifyPage](t, page).DeviceCount != 1 {
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
