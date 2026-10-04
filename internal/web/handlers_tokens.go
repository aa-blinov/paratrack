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

// handleSettingsTokens renders /settings/tokens.
func (s *Server) handleSettingsTokens(w http.ResponseWriter, r *http.Request) {
	s.renderTokens(w, r, "")
}

// renderTokens shows the token list; justCreated is the raw value of a
// token minted by this very request, shown once and never put in a URL.
func (s *Server) renderTokens(w http.ResponseWriter, r *http.Request, justCreated string) {
	u, _ := UserFrom(r.Context())
	list, err := s.services.Auth.APITokens.ListAPITokens(r.Context(), appmodel.APITokenListRequest{UserID: u.ID, CallerID: u.ID})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	teamIDs := make([]int64, 0, len(list))
	for _, token := range list {
		if token.TeamID > 0 {
			teamIDs = append(teamIDs, token.TeamID)
		}
	}
	teamsByID, err := s.services.Teams.Directory.FindByIDs(r.Context(), teamIDs)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := string(resolveLang(r))
	data := tokensPage{
		pageData: pageData{Title: "API tokens", Active: "settings-tokens", Lang: lang},
	}
	for _, t := range list {
		row := tokenRow{
			ID: t.ID, Name: t.Name, Prefix: t.Prefix, ReadOnly: t.ReadOnly,
			Created: fmtDate(resolveLang(r), t.CreatedAt.In(userLoc(r))),
		}
		if t.ExpiresAt != nil {
			row.Expires = fmtDate(resolveLang(r), t.ExpiresAt.In(userLoc(r)))
			row.Expired = !t.ExpiresAt.After(userNow(r))
		}
		if t.TeamID > 0 {
			if tm, ok := teamsByID[t.TeamID]; ok {
				row.Team = tm.Name
			}
		}
		data.Tokens = append(data.Tokens, row)
	}
	if justCreated != "" {
		data.JustCreated = justCreated
		w.Header().Set("Cache-Control", "no-store")
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "API tokens", "settings-tokens", "tokens", &data)
}

type tokenRow struct {
	ID       int64
	Name     string
	Prefix   string
	Created  string
	Expires  string // "" = never
	Expired  bool
	Team     string
	ReadOnly bool
}

// tokensPage is the /settings/tokens envelope.
type tokensPage struct {
	pageData
	Tokens      []tokenRow
	JustCreated string
	Flash       string
	FlashOK     bool
}

func (p *tokensPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleAPITokenCreate mints a token and shows its raw value once, in the
// response itself (a redirect carried it in the URL: history, logs).
func (s *Server) handleAPITokenCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	u, _ := UserFrom(r.Context())
	name := strings.TrimSpace(r.PostForm.Get("name"))
	opts := appmodel.TokenOptions{TeamID: teamID(r), ReadOnly: r.PostForm.Get("read_only") == "1"}
	expiresDays := r.PostForm.Get("expires_days")
	if expiresDays == "" {
		expiresDays = "90" // Match the form's selected default for older clients.
	}
	days, parseErr := strconv.Atoi(expiresDays)
	if len(r.PostForm["expires_days"]) > 1 || parseErr != nil || (days != 0 && days != 30 && days != 90 && days != 365) {
		http.Redirect(w, r, "/settings/tokens?flash="+encodeFlash(false, i18n.T(resolveLang(r), "err.invalidInput")), http.StatusSeeOther)
		return
	}
	if days > 0 {
		t := userNow(r).AddDate(0, 0, days)
		opts.ExpiresAt = &t
	}
	raw, _, err := s.services.Auth.APITokens.CreateAPIToken(operationContext(r), appmodel.APITokenCreateRequest{UserID: u.ID, CallerID: u.ID, Name: name, Options: opts})
	if err != nil {
		http.Redirect(w, r, "/settings/tokens?flash="+encodeFlash(false, s.tokenErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	s.renderTokens(w, r, raw)
}

func (s *Server) handleAPITokenDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	u, _ := UserFrom(r.Context())
	if err := s.services.Auth.APITokens.DeleteAPIToken(operationContext(r), appmodel.APITokenDeleteRequest{UserID: u.ID, CallerID: u.ID, TokenID: id}); err != nil {
		msg := s.tokenErrorMessage(r, err)
		http.Redirect(w, r, "/settings/tokens?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/tokens?flash=removed", http.StatusSeeOther)
}

// tokenErrorMessage maps token service failures to safe user-facing text.
func (s *Server) tokenErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrAuthValidation):
		return i18n.T(resolveLang(r), "err.invalidInput")
	case errors.Is(err, appmodel.ErrAuthForbidden), errors.Is(err, model.ErrForbidden):
		return "workspace membership required"
	case errors.Is(err, appmodel.ErrAuthNotFound), errors.Is(err, model.ErrNotFound):
		return "token not found"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}
