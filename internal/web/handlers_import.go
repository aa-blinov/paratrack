package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

// ---------------------------------------------------------------------------
// Time-entry import from Toggl, Harvest and Clockify.
// ---------------------------------------------------------------------------

// handlePWA registers the service worker path (served from static) —
// this handler is the /sw.js alias if needed; the file is embedded.

// importedEntry is one external time entry ready to become a session.
type importedEntry struct {
	ExtID    string // "toggl:123": the entry's id in the source tracker
	Activity string
	Start    time.Time
	End      time.Time
	Note     string
}

// handleImport renders the migration page.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := importPage{pageData: pageData{Title: "Import", Active: "import", Lang: lang}}
	s.renderPageForRequest(w, r, "Import", "import", "import", &data)
}

type importPage struct {
	pageData
	Provider string
	From, To string
	Secret   string
	Extra    string
	TZ       string
	Entries  []importedEntry
	Error    string
}

func (p *importPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleImportPreview fetches entries and shows them. Rendered straight
// from the POST: a redirect put the provider token into the URL (history,
// proxy logs).
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.PostForm.Get(k)) }
	data := importPage{pageData: pageData{Title: "Import", Active: "import", Lang: string(resolveLang(r))}}
	entries, err := s.services.Imports.Preview(r.Context(), importport.ProviderRequest{
		Provider: f("provider"), Secret: f("secret"), Extra: f("extra"),
		From: f("from"), To: f("to"), Timezone: f("tz"),
	})
	if err != nil {
		data.Error = s.importFailureMessage(r, err)
	} else {
		data.Entries = make([]importedEntry, len(entries))
		for i, entry := range entries {
			data.Entries[i] = importedEntry{
				ExtID: entry.ExternalID, Activity: entry.Activity,
				Start: entry.Start, End: entry.End, Note: entry.Note,
			}
		}
		data.Provider = f("provider")
		data.From, data.To, data.Secret, data.Extra, data.TZ = f("from"), f("to"), f("secret"), f("extra"), f("tz")
	}
	s.renderPageForRequest(w, r, "Import", "import", "import", &data)
}

// handleImportRun creates closed sessions from the fetched entries.
func (s *Server) handleImportRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/import?flash=bad_request", http.StatusSeeOther)
		return
	}
	provider := strings.TrimSpace(r.PostForm.Get("provider"))
	secret := strings.TrimSpace(r.PostForm.Get("secret"))
	extra := strings.TrimSpace(r.PostForm.Get("extra"))
	from := strings.TrimSpace(r.PostForm.Get("from"))
	to := strings.TrimSpace(r.PostForm.Get("to"))
	result, err := s.services.Imports.RunFromProvider(operationContext(r), appmodel.ProviderImportRunRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r),
		Provider: importport.ProviderRequest{
			Provider: provider, Secret: secret, Extra: extra,
			From: from, To: to, Timezone: strings.TrimSpace(r.PostForm.Get("tz")),
		},
	})
	if err != nil {
		msg := s.importFailureMessage(r, err)
		http.Redirect(w, r, "/import?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	msg := fmt.Sprintf(i18n.T(resolveLang(r), "imp.done"), result.Imported, result.Skipped)
	http.Redirect(w, r, "/stats?flash="+url.QueryEscape(encodeFlash(true, msg)), http.StatusSeeOther)
}

func (s *Server) importFailureMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, importport.ErrInvalidEntry),
		errors.Is(err, importport.ErrInvalidInput),
		errors.Is(err, importport.ErrPaginationLimit):
		return err.Error()
	case errors.Is(err, importport.ErrApplyFailed):
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
	var statusErr *providerstatus.Error
	if errors.As(err, &statusErr) {
		return statusErr.Error()
	}
	s.logInternalError(err)
	return i18n.T(resolveLang(r), "err.internal")
}
