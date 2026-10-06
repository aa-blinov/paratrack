package web

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// ---------------------------------------------------------------------------
// Web Push subscriptions and notifications.
// ---------------------------------------------------------------------------

// handlePushKey returns the VAPID public key (JS needs it to subscribe).
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	pub, err := s.services.Push.PublicKey(r.Context())
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, pushPublicKeyResponse{PublicKey: pub})
}

// handlePushSubscribe stores a browser subscription.
// Body: endpoint, p256dh, auth.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	endpoint := strings.TrimSpace(r.PostForm.Get("endpoint"))
	p256dh := strings.TrimSpace(r.PostForm.Get("p256dh"))
	auth := strings.TrimSpace(r.PostForm.Get("auth"))
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.services.Push.Subscribe(operationContext(r), appmodel.PushSubscribeRequest{
		TeamID: teamID(r), UserID: u.ID, CallerID: u.ID, Endpoint: endpoint, PublicKey: p256dh, AuthSecret: auth,
	}); err != nil {
		if errors.Is(err, appmodel.ErrInvalidPushSubscription) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			s.writeInternalError(w, err)
		}
		return
	}
	s.writeJSON(w, operationOKResponse{OK: true})
}

// handlePushUnsubscribe drops a subscription by endpoint.
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	endpoint := strings.TrimSpace(r.PostForm.Get("endpoint"))
	if endpoint == "" {
		http.Error(w, "endpoint is required", http.StatusBadRequest)
		return
	}
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.services.Push.UnsubscribeForMember(operationContext(r), appmodel.PushUnsubscribeRequest{TeamID: teamID(r), UserID: u.ID, CallerID: u.ID, Endpoint: endpoint}); err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.writeJSON(w, operationOKResponse{OK: true})
}

// handleNotificationsPage renders the push settings card.
func (s *Server) handleNotificationsPage(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := notifyPage{pageData: pageData{Title: "Notifications", Active: "settings-notify", Lang: lang, ReactApp: true}, NotificationsReact: true}
	deviceCount, err := s.services.Push.SubscriptionCount(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.DeviceCount = deviceCount
	// Someone without management rights can never see an invoice or a pay run,
	// so offering those events would promise notifications that never arrive.
	data.CanManage = canManage(r)
	// Which events to hear about is the person's own choice, not a workspace
	// setting, so it comes from the push workflow rather than the request
	// preferences.
	user, ok := UserFrom(r.Context())
	if ok {
		muted, err := s.services.Push.MutedTopics(r.Context(), appmodel.NotificationTopicsQuery{TeamID: teamID(r), UserID: user.ID})
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		data.Topics = notificationTopicViews(muted)
	}
	s.renderPageForRequest(w, r, "Notifications", "settings-notify", "notifications", &data)
}

type notifyPage struct {
	pageData
	NotificationsReact bool
	DeviceCount        int
	Topics             []notificationTopic
	Flash              string
	FlashOK            bool
}

func (p *notifyPage) setCSRF(t string) { p.pageData.setCSRF(t) }

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
	// Per-deploy cache name: a changed asset set installs a new worker,
	// whose activate step drops the old cache.
	w.Write(bytes.Replace(data, []byte(`"paratrack-v6"`), []byte(`"paratrack-`+assetVersion+`"`), 1))
}

// handleManifest serves the PWA manifest in the visitor's language so
// app shortcuts read naturally. No orientation lock: the same app runs
// in a desktop window.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	t := func(k string) string { return i18n.T(lang, k) }
	icon := func(src, sizes, purpose string) webManifestIcon {
		return webManifestIcon{Source: src, Sizes: sizes, Type: "image/png", Purpose: purpose}
	}
	shortcut := func(name, url string) webManifestShortcut {
		return webManifestShortcut{Name: name, ShortName: name, URL: url,
			Icons: []webManifestIcon{icon("/static/icon192.png", "192x192", "any")}}
	}
	w.Header().Set("Cache-Control", "no-cache")
	s.writeJSONContentType(w, "application/manifest+json", webManifestResponse{
		ID: "/", Name: "paratrack", ShortName: "paratrack",
		Description: t("pwa.description"), Language: string(lang),
		StartURL: "/", Scope: "/", Display: "standalone",
		BackgroundColor: "#f9fafb", ThemeColor: "#6366f1",
		Categories: []string{"productivity", "business"},
		Icons: []webManifestIcon{
			icon("/static/icon128.png", "128x128", "any"),
			icon("/static/icon192.png", "192x192", "any"),
			icon("/static/icon512.png", "512x512", "any"),
			icon("/static/icon512-maskable.png", "512x512", "maskable"),
		},
		Shortcuts: []webManifestShortcut{
			shortcut(t("pwa.newTimer"), "/?focus=activity"),
			shortcut(t("nav.stats"), "/stats"),
			shortcut(t("nav.timesheet"), "/timesheet"),
		},
	})
}
