package web

import (
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// ---------------------------------------------------------------------------
// Wave 9: Web Push + offline-aware client
// ---------------------------------------------------------------------------

// handlePushKey returns the VAPID public key (JS needs it to subscribe).
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	pub, _, err := s.db.EnsureVAPIDKeys(r.Context())
	if err != nil {
		w.WriteHeader(500)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"publicKey": pub})
}

// handlePushSubscribe stores a browser subscription.
// Body: endpoint, p256dh, auth.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	endpoint := strings.TrimSpace(r.PostForm.Get("endpoint"))
	p256dh := strings.TrimSpace(r.PostForm.Get("p256dh"))
	auth := strings.TrimSpace(r.PostForm.Get("auth"))
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	if err := s.db.UpsertPushSubscription(r.Context(), teamID(r), u.ID, endpoint, p256dh, auth); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.audit(r, "push.subscribe", "", "")
	writeJSON(w, map[string]any{"ok": true})
}

// handlePushUnsubscribe drops a subscription by endpoint.
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	endpoint := strings.TrimSpace(r.PostForm.Get("endpoint"))
	if endpoint != "" {
		_ = s.db.DeletePushSubscription(r.Context(), endpoint)
		s.audit(r, "push.unsubscribe", "", "")
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleNotificationsPage renders the push settings card.
func (s *Server) handleNotificationsPage(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := notifyPage{pageData: pageData{Title: "Notifications", Active: "settings-notify", Lang: lang}}
	subs, _ := s.db.ListPushSubscriptions(r.Context(), teamID(r))
	data.DeviceCount = len(subs)
	s.renderPageForRequest(w, r, "Notifications", "settings-notify", "notifications", &data)
}

type notifyPage struct {
	pageData
	DeviceCount int
	Flash       string
	FlashOK     bool
}

func (p *notifyPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// sendPush delivers a notification to every subscription in the team.
// Failures (410 Gone etc.) clean up dead endpoints.
func (s *Server) sendPush(teamID int64, title, body, url string) {
	ctx := context.Background()
	subs, err := s.db.ListPushSubscriptions(ctx, teamID)
	if err != nil {
		return
	}
	pub, priv, err := s.db.EnsureVAPIDKeys(ctx)
	if err != nil {
		return
	}
	privKey, err := parseECDSAPrivate(priv)
	if err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"url":   url,
		"tag":   "paratrack-" + strings.ReplaceAll(title, " ", "-"),
	})
	for _, sub := range subs {
		ws := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
		}
		resp, err := webpush.SendNotification(payload, ws, &webpush.Options{
			Subscriber:      "paratrack",
			VAPIDPublicKey:  pub,
			VAPIDPrivateKey: privKey,
			TTL:             86400,
		})
		if err != nil {
			continue
		}
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			_ = s.db.DeletePushSubscription(ctx, sub.Endpoint)
		}
		_ = resp.Body.Close()
	}
}

// parseECDSAPrivate rebuilds an ECDSA P-256 key from the stored scalar.
func parseECDSAPrivate(b64 string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", err
	}
	d := new(big.Int).SetBytes(b)
	curve := elliptic.P256()
	x, y := curve.ScalarBaseMult(d.Bytes())
	_ = x
	_ = y
	// webpush-go accepts the raw base64url private scalar directly.
	return b64, nil
}

// handleServiceWorker serves sw.js from the root so its default scope is "/".
func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := assets.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}
