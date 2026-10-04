package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Webhook management pages and mutations.
// handleWebhooksPage renders /settings/webhooks.
func (s *Server) handleWebhooksPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.services.Webhooks.List(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	recentDeliveries, err := s.services.Webhooks.RecentDeliveries(r.Context(), teamID(r), 5)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := string(resolveLang(r))
	resolvedLang := resolveLang(r)
	location := userLoc(r)
	now := userNow(r)
	data := webhooksPage{pageData: pageData{Title: "Webhooks", Active: "settings-webhooks", Lang: lang}}
	for _, h := range list {
		row := webhookRow{ID: h.ID, URL: h.URL, Events: h.Events, Active: h.Active}
		for _, d := range recentDeliveries[h.ID] {
			row.Deliveries = append(row.Deliveries, deliveryRow{
				When: fmtWhen(resolvedLang, d.CreatedAt.In(location), now), Event: d.Event,
				Status: d.Status, OK: d.Status >= 200 && d.Status < 300, Error: d.Error,
			})
		}
		data.Items = append(data.Items, row)
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolvedLang)
	}
	s.renderPageForRequest(w, r, "Webhooks", "settings-webhooks", "webhooks", &data)
}

type webhookRow struct {
	ID         int64
	URL        string
	Events     string
	Active     bool
	Deliveries []deliveryRow // newest first, last 5
}

type deliveryRow struct {
	When, Event, Error string
	Status             int
	OK                 bool
}

type webhooksPage struct {
	pageData
	Items   []webhookRow
	Flash   string
	FlashOK bool
}

func (p *webhooksPage) setCSRF(t string) { p.pageData.setCSRF(t) }

func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_, err := s.services.Webhooks.Create(operationContext(r), appmodel.WebhookCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r),
		URL:    strings.TrimSpace(r.PostForm.Get("url")),
		Secret: strings.TrimSpace(r.PostForm.Get("secret")),
		Events: r.PostForm["events"],
	})
	if err != nil {
		http.Redirect(w, r, "/settings/webhooks?flash="+encodeFlash(false, s.webhookErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/webhooks?flash=created", http.StatusSeeOther)
}

func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	if err := s.services.Webhooks.Delete(operationContext(r), appmodel.WebhookDeleteRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), WebhookID: id}); err != nil {
		http.Redirect(w, r, "/settings/webhooks?flash="+encodeFlash(false, s.webhookErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/webhooks?flash=removed", http.StatusSeeOther)
}

func (s *Server) webhookErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrInvalidWebhook):
		return "invalid webhook settings"
	case errors.Is(err, model.ErrForbidden):
		return "manager role required"
	case errors.Is(err, model.ErrNotFound):
		return "webhook not found"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}

// handleAuditPage renders /settings/audit (owner-only conceptually).
