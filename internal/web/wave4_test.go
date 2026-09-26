package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

func TestAuditAndWebhooks(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)

	d.Audit(ctx, 1, 1, "auth.login", "a@x.t", "", "127.0.0.1")
	d.Audit(ctx, 1, 1, "project.delete", "acme", "", "127.0.0.1")
	list, err := d.ListAudit(ctx, 1, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("audit=%+v err=%v", list, err)
	}
	if list[0].Action != "project.delete" { // newest first
		t.Fatalf("order=%+v", list[0])
	}

	h, err := d.CreateWebhook(ctx, 1, "https://example.com/hook", "topsecret", "session.stopped")
	if err != nil {
		t.Fatal(err)
	}
	if h.URL != "https://example.com/hook" {
		t.Fatalf("url=%s", h.URL)
	}
	if _, err := d.CreateWebhook(ctx, 1, "notaurl", "", ""); err == nil {
		t.Fatal("bad url accepted")
	}
	// signature
	body := []byte(`{"event":"session.stopped"}`)
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if got := signPayload("topsecret", body); got != want {
		t.Fatalf("sig=%s want %s", got, want)
	}
	if !hookSubscribes(h.Events, "session.stopped") || hookSubscribes(h.Events, "invoice.created") {
		t.Fatalf("events filter broken: %s", h.Events)
	}
	if err := d.DeleteWebhook(ctx, 1, h.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAPIv1Endpoints(t *testing.T) {
	e := newAPIEnv(t)
	e.register("apiv1@x.test")
	// GET sessions
	resp := e.do("GET", "/api/v1/sessions", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "sessions") {
		t.Fatalf("body=%s", body)
	}
	// POST session
	resp = e.do("POST", "/api/v1/sessions", url.Values{"activity": {"api-test"}}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("create: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()
	// summary
	resp = e.do("GET", "/api/v1/reports/summary", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("summary: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// projects
	resp = e.do("GET", "/api/v1/projects", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("projects: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
