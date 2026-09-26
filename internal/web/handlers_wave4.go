package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// ---------------------------------------------------------------------------
// Wave 4: audit helper + webhook dispatcher + API v1 + OIDC
// ---------------------------------------------------------------------------

// audit writes an audit_log row with the caller's IP. Safe to call from
// any handler; failures are swallowed (audit must never break a request).
func (s *Server) audit(r *http.Request, action, target, meta string) {
	uid := int64(0)
	if u, ok := UserFrom(r.Context()); ok {
		uid = u.ID
	}
	s.db.Audit(r.Context(), teamID(r), uid, action, target, meta, clientIP(r))
}

// fireWebhook delivers an event to every active endpoint subscribed to
// it. Delivery is synchronous but best-effort — a slow endpoint must
// not block the response, so we use a goroutine per endpoint.
func (s *Server) fireWebhook(r *http.Request, event string, payload map[string]any) {
	team := teamID(r)
	if team == 0 {
		return
	}
	hooks, err := s.db.ListWebhooks(r.Context(), team)
	if err != nil {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     event,
		"team_id":   team,
		"sent_at":   time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	for _, h := range hooks {
		if !h.Active || !hookSubscribes(h.Events, event) {
			continue
		}
		h := h
		body := body
		go s.deliverWebhook(h, event, body)
	}
}

func hookSubscribes(events, event string) bool {
	for _, e := range strings.Split(events, ",") {
		if strings.TrimSpace(e) == event || strings.TrimSpace(e) == "*" {
			return true
		}
	}
	return false
}

// deliverWebhook POSTs the payload with an HMAC signature header.
func (s *Server) deliverWebhook(h db.Webhook, event string, body []byte) {
	req, err := http.NewRequest("POST", h.URL, strings.NewReader(string(body)))
	if err != nil {
		s.db.LogWebhookDelivery(context.Background(), h.ID, event, string(body), 0, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Paratrack-Event", event)
	req.Header.Set("X-Paratrack-Signature", signPayload(h.Secret, body))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.db.LogWebhookDelivery(context.Background(), h.ID, event, string(body), 0, err.Error())
		return
	}
	defer resp.Body.Close()
	s.db.LogWebhookDelivery(context.Background(), h.ID, event, string(body), resp.StatusCode, "")
}

// signPayload returns hex(hmac-sha256(secret, body)).
func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// API v1 — stable JSON surface for scripts and the extension.
// Auth: Bearer pt_… or session cookie. All under /api/v1/.
// ---------------------------------------------------------------------------

// handleAPIv1Sessions — GET list / POST create.
func (s *Server) handleAPIv1Sessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		from, to := rangeFromQuery(r)
		list, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r), from, to, nil)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			w.WriteHeader(500)
			return
		}
		// Active (still running) sessions are not in the closed range query —
		// a client that POSTs a session must be able to GET it back.
		if open, err := s.db.ListActiveSessions(r.Context(), teamID(r)); err == nil {
			for _, as := range open {
				if as.Session.StartAt.Before(from) || !as.Session.StartAt.Before(to) {
					continue
				}
				list = append(list, as)
			}
		}
		type row struct {
			ID       int64  `json:"id"`
			Activity string `json:"activity"`
			Start    string `json:"start"`
			End      string `json:"end"`
			Seconds  int    `json:"seconds"`
			Note     string `json:"note"`
		}
		out := []row{}
		now := time.Now()
		for _, as := range list {
			note := ""
			if as.Session.Note != nil {
				note = *as.Session.Note
			}
			end := ""
			if as.Session.EndAt != nil {
				end = as.Session.EndAt.UTC().Format(time.RFC3339)
			}
			out = append(out, row{
				ID: as.Session.ID, Activity: as.Activity.Name,
				Start: as.Session.StartAt.UTC().Format(time.RFC3339), End: end,
				Seconds: as.Session.DurationSeconds(now), Note: note,
			})
		}
		writeJSON(w, map[string]any{"sessions": out})
	case http.MethodPost:
		_ = r.ParseForm()
		name := strings.TrimSpace(r.FormValue("activity"))
		if name == "" {
			writeJSON(w, map[string]string{"error": "activity is required"})
			w.WriteHeader(400)
			return
		}
		act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), name)
		if err != nil {
			w.WriteHeader(500)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		sess, err := s.db.CreateSession(r.Context(), teamID(r), act.ID, time.Now(), r.FormValue("note"))
		if err != nil {
			w.WriteHeader(500)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r, "session.start", fmt.Sprintf("%d", sess.ID), name)
		s.fireWebhook(r, "session.started", map[string]any{"session_id": sess.ID, "activity": name})
		writeJSON(w, map[string]any{"session_id": sess.ID, "activity": name})
	default:
		w.WriteHeader(405)
	}
}

// handleAPIv1Session — PATCH update / DELETE remove one session.
func (s *Server) handleAPIv1Session(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		w.WriteHeader(400)
		writeJSON(w, map[string]string{"error": "bad id"})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		// reuse the HTMX patch handler shape via form fields
		s.handleUpdateSession(w, r)
	case http.MethodDelete:
		if err := s.db.DeleteSession(r.Context(), teamID(r), id); err != nil {
			w.WriteHeader(404)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r, "session.delete", strconv.FormatInt(id, 10), "")
		writeJSON(w, map[string]any{"deleted": id})
	default:
		w.WriteHeader(405)
	}
}

// handleAPIv1Projects — GET list.
func (s *Server) handleAPIv1Projects(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListProjects(r.Context(), teamID(r), r.URL.Query().Get("archived") == "1")
	if err != nil {
		w.WriteHeader(500)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"projects": list})
}

// handleAPIv1Report — GET summary totals for a window.
func (s *Server) handleAPIv1Report(w http.ResponseWriter, r *http.Request) {
	from, to := rangeFromQuery(r)
	list, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r), from, to, nil)
	if err != nil {
		w.WriteHeader(500)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now()
	total := 0
	byActivity := map[string]int{}
	for _, as := range list {
		sec := as.Session.TrackedSecondsInWindow(from, to, now)
		total += sec
		byActivity[as.Activity.Name] += sec
	}
	writeJSON(w, map[string]any{
		"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"),
		"total_seconds": total,
		"by_activity":   byActivity,
	})
}

func rangeFromQuery(r *http.Request) (time.Time, time.Time) {
	now := time.Now()
	from := now.AddDate(0, 0, -30)
	to := now.Add(24 * time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.AddDate(0, 0, 1)
		}
	}
	return from, to
}

// ---------------------------------------------------------------------------
// Webhook management (HTMX pages)
// ---------------------------------------------------------------------------

// handleWebhooksPage renders /settings/webhooks.
func (s *Server) handleWebhooksPage(w http.ResponseWriter, r *http.Request) {
	list, _ := s.db.ListWebhooks(r.Context(), teamID(r))
	lang := string(resolveLang(r))
	data := webhooksPage{pageData: pageData{Title: "Webhooks", Active: "settings-webhooks", Lang: lang}}
	for _, h := range list {
		data.Items = append(data.Items, webhookRow{ID: h.ID, URL: h.URL, Events: h.Events, Active: h.Active})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Webhooks", "settings-webhooks", "webhooks", &data)
}

type webhookRow struct {
	ID     int64
	URL    string
	Events string
	Active bool
}

type webhooksPage struct {
	pageData
	Items   []webhookRow
	Flash   string
	FlashOK bool
}

func (p *webhooksPage) setCSRF(t string) { p.pageData.setCSRF(t) }

func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	u, _ := s.db.CreateWebhook(r.Context(), teamID(r),
		strings.TrimSpace(r.PostForm.Get("url")),
		strings.TrimSpace(r.PostForm.Get("secret")),
		// Checkboxes post one "events" value each; API clients may still
		// send a single comma list.
		strings.TrimSpace(strings.Join(r.PostForm["events"], ",")))
	if u.ID == 0 {
		http.Redirect(w, r, "/settings/webhooks?flash="+encodeFlash(false, "bad url"), http.StatusSeeOther)
		return
	}
	s.audit(r, "webhook.create", fmt.Sprintf("%d", u.ID), u.URL)
	http.Redirect(w, r, "/settings/webhooks?flash=created", http.StatusSeeOther)
}

func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/webhooks/"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	_ = s.db.DeleteWebhook(r.Context(), teamID(r), id)
	s.audit(r, "webhook.delete", strconv.FormatInt(id, 10), "")
	http.Redirect(w, r, "/settings/webhooks?flash=removed", http.StatusSeeOther)
}

// handleAuditPage renders /settings/audit (owner-only conceptually).
func (s *Server) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	list, _ := s.db.ListAudit(r.Context(), teamID(r), 100)
	lang := string(resolveLang(r))
	data := auditPage{pageData: pageData{Title: "Audit log", Active: "settings-audit", Lang: lang}}
	for _, e := range list {
		data.Items = append(data.Items, auditRow{
			Time: e.CreatedAt.Format("2006-01-02 15:04"), Action: e.Action,
			Target: e.Target, IP: e.IP,
		})
	}
	s.renderPageForRequest(w, r, "Audit log", "settings-audit", "audit", &data)
}

type auditRow struct {
	Time   string
	Action string
	Target string
	IP     string
}

type auditPage struct {
	pageData
	Items   []auditRow
	Flash   string
	FlashOK bool
}

func (p *auditPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// ---------------------------------------------------------------------------
// OIDC SSO (optional, env-configured)
// ---------------------------------------------------------------------------

// handleSSOLogin redirects to the OIDC provider (authorization endpoint).
// Configured via PARATRACK_OIDC_ISSUER / _CLIENT_ID / _CLIENT_SECRET.
func (s *Server) handleSSOLogin(w http.ResponseWriter, r *http.Request) {
	iss := strings.TrimRight(os.Getenv("PARATRACK_OIDC_ISSUER"), "/")
	clientID := os.Getenv("PARATRACK_OIDC_CLIENT_ID")
	if iss == "" || clientID == "" {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	state := signPayload("oidc", []byte(time.Now().String()))[:16]
	http.SetCookie(w, &http.Cookie{Name: "paratrack_oidc_state", Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	redirect := publicBaseURL(r) + "/sso/callback"
	authURL := iss + "/authorize?client_id=" + url.QueryEscape(clientID) +
		"&response_type=code&scope=openid%20email%20profile" +
		"&redirect_uri=" + url.QueryEscape(redirect) +
		"&state=" + state
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// handleSSOCallback exchanges the code, resolves the email, and logs
// the user in (creating an account on first use).
func (s *Server) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	iss := strings.TrimRight(os.Getenv("PARATRACK_OIDC_ISSUER"), "/")
	clientID := os.Getenv("PARATRACK_OIDC_CLIENT_ID")
	clientSecret := os.Getenv("PARATRACK_OIDC_CLIENT_SECRET")
	q := r.URL.Query()
	if st, err := r.Cookie("paratrack_oidc_state"); err != nil || st.Value != q.Get("state") {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	// token exchange
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", publicBaseURL(r)+"/sso/callback")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	resp, err := http.PostForm(iss+"/token", form)
	if err != nil {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	if tok.AccessToken == "" {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	// userinfo
	req, _ := http.NewRequest("GET", iss+"/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uresp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	defer uresp.Body.Close()
	var info struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Sub   string `json:"sub"`
	}
	_ = json.NewDecoder(uresp.Body).Decode(&info)
	if info.Email == "" {
		http.Redirect(w, r, "/login?error=bad_email", http.StatusSeeOther)
		return
	}
	// find-or-create
	user, err := s.auth.FindByEmail(r.Context(), info.Email)
	if err != nil {
		name := info.Name
		if name == "" {
			name = strings.SplitN(info.Email, "@", 2)[0]
		}
		// random password — SSO users never type it
		pw := signPayload("sso", []byte(info.Sub+time.Now().String()))
		uid, _, cerr := s.auth.CreateUser(r.Context(), info.Email, pw[:32], name)
		if cerr != nil {
			http.Redirect(w, r, "/register?error=could_not_register", http.StatusSeeOther)
			return
		}
		user, _ = s.auth.FindByID(r.Context(), uid)
		s.audit(r, "auth.sso_register", user.Email, info.Sub)
	}
	sess, err := s.auth.NewSession(r.Context(), user.ID)
	if err != nil {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, sess.Token)
	s.audit(r, "auth.sso_login", user.Email, info.Sub)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ssoConfigured reports whether OIDC env is present (drives the button).
func ssoConfigured() bool {
	return os.Getenv("PARATRACK_OIDC_ISSUER") != "" && os.Getenv("PARATRACK_OIDC_CLIENT_ID") != ""
}
