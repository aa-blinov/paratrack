package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The member's own calendar connections (CRM-19): the settings page, the consent round
// trip to Google or Outlook, the disconnect and the on-demand sync. The provider's
// callback works only in the browser session that started the consent.

const calendarSyncPath = "/settings/calendar-sync"

// calendarSyncNoticeKeys are the notices the page may show, each the dictionary key of its
// line. A flash code outside this set is ignored.
var calendarSyncNoticeKeys = map[string]bool{
	"calsync.notice.connected":      true,
	"calsync.notice.disconnected":   true,
	"calsync.notice.denied":         true,
	"calsync.notice.stateInvalid":   true,
	"calsync.notice.providerFailed": true,
	"calsync.notice.disabled":       true,
	"calsync.notice.notConnected":   true,
	"calsync.notice.failed":         true,
}

type calendarSyncProviderView struct {
	// Key is the provider name the routes use: google or microsoft.
	Key string
	// Enabled is false when this server has no OAuth client for the provider.
	Enabled   bool
	Connected bool
	// LastSyncedAt is the workspace wall clock of the last attempt; empty before one.
	LastSyncedAt string
	// LastError is a code the page translates (token_rejected, unreachable, internal) or the
	// provider and its HTTP status, which name no account data.
	LastError string
}

type calendarSyncPage struct {
	pageData
	CalendarSyncReact bool
	Providers         []calendarSyncProviderView
	// Flash is the translated notice of the last action; FlashOK is false for a failure.
	Flash   string
	FlashOK bool
}

func (p *calendarSyncPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// calendarConnectionView shows the last attempt as the workspace wall clock, as the page does.
type calendarConnectionView struct {
	Provider     string `json:"provider"`
	LastSyncedAt string `json:"last_synced_at"`
	LastError    string `json:"last_error"`
}

type calendarSyncResponse struct {
	Connections []calendarConnectionView `json:"connections"`
}

func (calendarSyncResponse) isJSONResponse() {}

// calendarSyncRedirectURI is the callback the provider returns to. It is built the same way
// when the consent starts and when it completes, so the two always match.
func (s *Server) calendarSyncRedirectURI(r *http.Request, provider string) string {
	return s.publicBaseURL(r) + calendarSyncPath + "/" + provider + "/callback"
}

// calendarSessionToken is the browser session a consent is bound to.
func calendarSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) handleCalendarSyncPage(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	connections, err := s.services.CalendarSync.Connections(r.Context(), teamID(r), authenticatedUserID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := calendarSyncPage{
		pageData:          pageData{Title: "Calendar sync", Active: "settings-calendar-sync", Lang: lang, ReactApp: true},
		CalendarSyncReact: true,
		Providers:         calendarSyncProviderViews(s.services.CalendarSync.Providers(), connections, userLoc(r)),
	}
	data.CanManage = canManage(r)
	code, failed := strings.CutPrefix(r.URL.Query().Get("flash"), "e:")
	if calendarSyncNoticeKeys[code] {
		data.Flash, data.FlashOK = i18n.T(resolveLang(r), code), !failed
	}
	s.renderPageForRequest(w, r, data.Title, data.Active, &data)
}

// calendarSyncProviderViews shows both providers in a fixed order. A connection whose
// provider this server no longer configures still shows, so the member can disconnect it.
func calendarSyncProviderViews(enabled []string, connections []appmodel.CalendarConnection, loc *time.Location) []calendarSyncProviderView {
	views := make([]calendarSyncProviderView, 0, 2)
	for _, key := range []string{calendarport.ProviderGoogle, calendarport.ProviderMicrosoft} {
		view := calendarSyncProviderView{Key: key, Enabled: slices.Contains(enabled, key)}
		for _, connection := range connections {
			if connection.Provider != key {
				continue
			}
			view.Connected = true
			view.LastError = connection.LastError
			if !connection.LastSyncedAt.IsZero() {
				view.LastSyncedAt = connection.LastSyncedAt.In(loc).Format(calendarWallClock)
			}
		}
		views = append(views, view)
	}
	return views
}

func (s *Server) handleCalendarSyncConnect(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	authorizationURL, err := s.services.CalendarSync.BeginConnect(r.Context(), appmodel.CalendarConnectRequest{
		TeamID: teamID(r), UserID: authenticatedUserID(r), Provider: provider,
		SessionToken: calendarSessionToken(r), RedirectURI: s.calendarSyncRedirectURI(r, provider),
	})
	if err != nil {
		http.Redirect(w, r, calendarSyncPath+"?flash="+encodeFlash(false, calendarSyncErrorKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
}

func (s *Server) handleCalendarSyncCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	query := r.URL.Query()
	if query.Get("error") != "" {
		http.Redirect(w, r, calendarSyncPath+"?flash="+encodeFlash(false, "calsync.notice.denied"), http.StatusSeeOther)
		return
	}
	err := s.services.CalendarSync.CompleteConnect(r.Context(), appmodel.CalendarConnectCallbackRequest{
		TeamID: teamID(r), UserID: authenticatedUserID(r), Provider: provider,
		State: query.Get("state"), Code: query.Get("code"),
		SessionToken: calendarSessionToken(r), RedirectURI: s.calendarSyncRedirectURI(r, provider),
	})
	if err != nil {
		http.Redirect(w, r, calendarSyncPath+"?flash="+encodeFlash(false, calendarSyncErrorKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, calendarSyncPath+"?flash="+encodeFlash(true, "calsync.notice.connected"), http.StatusSeeOther)
}

func (s *Server) handleCalendarSyncDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.services.CalendarSync.Disconnect(r.Context(), teamID(r), authenticatedUserID(r), r.PathValue("provider")); err != nil {
		http.Redirect(w, r, calendarSyncPath+"?flash="+encodeFlash(false, calendarSyncErrorKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, calendarSyncPath+"?flash="+encodeFlash(true, "calsync.notice.disconnected"), http.StatusSeeOther)
}

// handleCalendarSyncNow runs the caller's connections in this workspace and answers with
// what each one reports afterwards.
func (s *Server) handleCalendarSyncNow(w http.ResponseWriter, r *http.Request) {
	loc := userLoc(r)
	connections, err := s.services.CalendarSync.SyncMine(r.Context(), teamID(r), authenticatedUserID(r))
	if err != nil {
		lang := resolveLang(r)
		switch {
		case errors.Is(err, model.ErrCalendarNotConnected):
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(lang, "calsync.err.notConnected")})
		case errors.Is(err, model.ErrCalendarSyncDisabled):
			s.writeJSONStatus(w, http.StatusServiceUnavailable, apiErrorResponse{Error: i18n.T(lang, "calsync.err.disabled")})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	response := calendarSyncResponse{Connections: make([]calendarConnectionView, 0, len(connections))}
	for _, connection := range connections {
		view := calendarConnectionView{Provider: connection.Provider, LastError: connection.LastError}
		if !connection.LastSyncedAt.IsZero() {
			view.LastSyncedAt = connection.LastSyncedAt.In(loc).Format(calendarWallClock)
		}
		response.Connections = append(response.Connections, view)
	}
	s.writeJSON(w, response)
}

// calendarSyncErrorKey is the notice for a failed connect or disconnect.
func calendarSyncErrorKey(err error) string {
	switch {
	case errors.Is(err, model.ErrCalendarSyncDisabled):
		return "calsync.notice.disabled"
	case errors.Is(err, model.ErrCalendarNotConnected):
		return "calsync.notice.notConnected"
	case errors.Is(err, model.ErrCalendarStateInvalid):
		return "calsync.notice.stateInvalid"
	case errors.Is(err, model.ErrCalendarProviderFailure):
		return "calsync.notice.providerFailed"
	default:
		return "calsync.notice.failed"
	}
}
