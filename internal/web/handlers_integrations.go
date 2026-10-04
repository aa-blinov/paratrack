package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

// Integration pages
// ---------------------------------------------------------------------------

// handleIntegrations lists connected providers and the connect form.
func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.services.Integrations.Queries.Management(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := string(resolveLang(r))
	data := integrationsPage{
		pageData:          pageData{Title: "Integrations", Active: "integrations", Lang: lang, ReactApp: true},
		IntegrationsReact: true,
	}
	for _, item := range snapshot.Items {
		it := item.Integration
		data.Items = append(data.Items, integrationRow{
			ID: it.ID, Provider: it.Provider, Name: it.Name,
			TaskCount: item.TaskCount,
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Integrations", "integrations", "integrations", &data)
}

type integrationRow struct {
	ID        int64
	Provider  string
	Name      string
	TaskCount int
}

// integrationsPage is the /integrations envelope.
type integrationsPage struct {
	pageData
	IntegrationsReact bool
	Items             []integrationRow
	Flash             string
	FlashOK           bool
}

func (p *integrationsPage) setCSRF(t string)  { p.pageData.setCSRF(t) }
func (p integrationsPage) usesReactApp() bool { return p.IntegrationsReact }

// handleIntegrationDetail lists imported tasks with a one-click Start.
func (s *Server) handleIntegrationDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	snapshot, err := s.services.Integrations.Queries.Detail(r.Context(), appmodel.IntegrationLookupQuery{TeamID: teamID(r), IntegrationID: id})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	it, tasks := snapshot.Integration, snapshot.Tasks
	lang := string(resolveLang(r))
	data := integrationDetailPage{
		pageData:         pageData{Title: it.Name, Active: "integrations", Lang: lang, ReactApp: true},
		IntegrationReact: true,
		Integration:      integrationRow{ID: it.ID, Provider: it.Provider, Name: it.Name, TaskCount: len(tasks)},
	}
	for _, t := range tasks {
		data.Tasks = append(data.Tasks, integrationTaskRow{
			ID: t.ID, Title: t.Title, URL: t.URL, Status: t.Status, ExternalID: t.ExternalID,
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, it.Name, "integrations", "integration-detail", &data)
}

type integrationTaskRow struct {
	ID         int64
	Title      string
	URL        string
	Status     string
	ExternalID string
}

// integrationDetailPage is the /integrations/{id} envelope.
type integrationDetailPage struct {
	pageData
	IntegrationReact bool
	Integration      integrationRow
	Tasks            []integrationTaskRow
	Flash            string
	FlashOK          bool
}

func (p *integrationDetailPage) setCSRF(t string)  { p.pageData.setCSRF(t) }
func (p integrationDetailPage) usesReactApp() bool { return p.IntegrationReact }

// handleIntegrationConnect stores a credential and imports items.
//
// Form: provider=github|trello, name, secret, extra (repo or board id).
func (s *Server) handleIntegrationConnect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	provider := strings.TrimSpace(r.PostForm.Get("provider"))
	name := strings.TrimSpace(r.PostForm.Get("name"))
	secret := strings.TrimSpace(r.PostForm.Get("secret"))
	extra := strings.TrimSpace(r.PostForm.Get("extra"))
	callerID := authenticatedUserID(r)
	connected, err := s.services.Integrations.Commands.ConnectAndSync(operationContext(r), appmodel.IntegrationCreateRequest{
		TeamID: teamID(r), CallerID: callerID, Provider: provider, Name: name, Secret: secret,
		Config: appmodel.IntegrationConfig{Target: extra},
	})
	if err != nil {
		http.Redirect(w, r, "/integrations?flash="+encodeFlash(false, s.integrationErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/integrations/%d?flash=%s", connected.Integration.ID, s.importFlash(r, connected.Imported, connected.SyncError)), http.StatusSeeOther)
}

func (s *Server) integrationErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrInvalidIntegration):
		return i18n.T(resolveLang(r), "err.invalidInput")
	case errors.Is(err, model.ErrForbidden):
		return "manager role required"
	case errors.Is(err, model.ErrNotFound):
		return "integration not found"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}

func (s *Server) handleIntegrationDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	request := appmodel.IntegrationMutationRequest{TeamID: teamID(r), IntegrationID: id, CallerID: authenticatedUserID(r)}
	if err := s.services.Integrations.Commands.Delete(operationContext(r), request); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	http.Redirect(w, r, "/integrations?flash=removed", http.StatusSeeOther)
}

func (s *Server) handleIntegrationSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	n, ierr := s.services.Integrations.Commands.SyncProvider(operationContext(r), appmodel.IntegrationMutationRequest{TeamID: teamID(r), IntegrationID: id, CallerID: authenticatedUserID(r)})
	if errors.Is(ierr, model.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/integrations/%d?flash=%s", id, s.importFlash(r, n, ierr)), http.StatusSeeOther)
}

// handleIntegrationStart starts a timer on an imported task's activity.
func (s *Server) handleIntegrationStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tid, err := strconv.ParseInt(r.PostForm.Get("task_id"), 10, 64)
	if err != nil || tid <= 0 {
		http.Error(w, "task_id required", 400)
		return
	}
	activity, _, err := s.services.ImportedTaskTracking.Start(operationContext(r), appmodel.ImportedTaskStartRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), TaskID: tid,
		ProjectID: projectIDFromForm(r), At: actionTime(r), Note: strings.TrimSpace(r.FormValue("note")),
	})
	if err != nil {
		s.writeTimerStartError(w, r, activity.Name, err)
		return
	}
	s.toastL(w, r, "toast.started", activity.Name, "success")
	s.respondActiveList(w, r)
}

// importFlash is the result banner after connect / sync.
func (s *Server) importFlash(r *http.Request, n int, err error) string {
	lang := resolveLang(r)
	if err != nil {
		message := i18n.T(lang, "err.internal")
		var statusErr *providerstatus.Error
		switch {
		case errors.Is(err, appmodel.ErrInvalidExternalTask), errors.Is(err, appmodel.ErrIncompleteTaskSnapshot):
			message = err.Error()
		case errors.Is(err, appmodel.ErrIntegrationSyncSuperseded):
			message = i18n.T(lang, "int.syncSuperseded")
		case errors.As(err, &statusErr):
			message = statusErr.Error()
		default:
			s.logInternalError(err)
		}
		return url.QueryEscape(encodeFlash(false, fmt.Sprintf(i18n.T(lang, "int.importFailed"), message)))
	}
	return url.QueryEscape(encodeFlash(true, fmt.Sprintf(i18n.T(lang, "int.imported"), n)))
}

// ---------------------------------------------------------------------------
// JSON for the browser extension (Bearer auth)
// ---------------------------------------------------------------------------

// handleAPIMe returns the token owner — lets the extension confirm auth.
func (s *Server) handleAPIMe(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	team, _ := TeamFrom(r.Context())
	s.writeJSON(w, apiMeResponse{
		User: apiMeUserResponse{ID: u.ID, Name: u.Name, Email: u.Email},
		Team: apiMeTeamResponse{ID: team.ID, Name: team.Name},
	})
}

// handleAPIExternalTasks lists imported tasks across integrations.
func (s *Server) handleAPIExternalTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.services.Integrations.Queries.TasksForTeam(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	out := []externalTaskResponse{}
	for _, t := range tasks {
		out = append(out, externalTaskResponse{ID: t.ID, Title: t.Title, URL: t.URL, Status: t.Status, Provider: t.Provider})
	}
	s.writeJSON(w, externalTasksResponse{Tasks: out})
}
