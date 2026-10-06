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
	snapshot, err := s.services.Webhooks.Management(r.Context(), appmodel.WebhookManagementQuery{TeamID: teamID(r), DeliveriesPerEndpoint: 5})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := string(resolveLang(r))
	resolvedLang := resolveLang(r)
	location := userLoc(r)
	now := userNow(r)
	// The test run is written to the history like any delivery, so the screen
	// reopens on the same screen and points at the attempt that was just made.
	testedWebhookID, _ := strconv.ParseInt(r.URL.Query().Get("test"), 10, 64)
	data := webhooksPage{
		pageData:      pageData{Title: "Webhooks", Active: "settings-webhooks", Lang: lang, ReactApp: true},
		WebhooksReact: true,
		Items:         make([]webhookRow, 0, len(snapshot.Endpoints)),
	}
	for _, h := range snapshot.Endpoints {
		row := webhookRow{ID: h.ID, URL: h.URL, Events: h.Events, Active: h.Active, Deliveries: make([]deliveryRow, 0, len(snapshot.Deliveries[h.ID]))}
		for position, d := range snapshot.Deliveries[h.ID] {
			row.Deliveries = append(row.Deliveries, deliveryRow{
				ID: d.ID, When: fmtWhen(resolvedLang, d.CreatedAt.In(location), now), Event: d.Event,
				Status: d.Status, OK: d.Status >= 200 && d.Status < 300, Error: d.Error,
				RequestBody: d.RequestBody, ResponseBody: d.ResponseBody, BodyTruncated: d.BodyTruncated,
				Test: h.ID == testedWebhookID && position == 0,
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
	ID                        int64
	When, Event, Error        string
	Status                    int
	OK                        bool
	RequestBody, ResponseBody string
	BodyTruncated             bool
	Test                      bool
}

type webhooksPage struct {
	pageData
	WebhooksReact bool
	Items         []webhookRow
	Flash         string
	FlashOK       bool
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

// handleWebhookTest sends one synthetic event to an endpoint and reopens the
// screen on the attempt it made, so the answer is read where the button was
// pressed instead of somewhere in the history.
func (s *Server) handleWebhookTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, webhookTestRedirect(id, encodeFlash(false, "bad_request")), http.StatusSeeOther)
		return
	}
	result, err := s.services.Webhooks.SendTest(operationContext(r), appmodel.WebhookTestRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), WebhookID: id,
		Event: strings.TrimSpace(r.PostForm.Get("event")),
	})
	if err != nil {
		http.Redirect(w, r, webhookTestRedirect(id, encodeFlash(false, s.webhookErrorMessage(r, err))), http.StatusSeeOther)
		return
	}
	flash := "test_sent"
	if result.Error != "" || result.Status < 200 || result.Status >= 300 {
		flash = "test_failed"
	}
	http.Redirect(w, r, webhookTestRedirect(id, flash), http.StatusSeeOther)
}

func webhookTestRedirect(webhookID int64, flash string) string {
	return "/settings/webhooks?test=" + strconv.FormatInt(webhookID, 10) + "&flash=" + flash
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
