package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// workspaceRestorePath is the one route that takes a whole workspace file. It is
// posted as a raw JSON body with the CSRF header, so the CSRF check never has to
// read a large body. The body is streamed to disk and read record by record, so
// the limit is about disk and time, not memory.
const (
	workspaceRestorePath = "/api/workspace/restore"
	maxWorkspaceRestore  = 1 << 30
)

type restoredWebhookResponse struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

type workspaceRestoredResponse struct {
	TeamID   int64                     `json:"team_id"`
	Slug     string                    `json:"slug"`
	Webhooks []restoredWebhookResponse `json:"webhooks"`
}

func (workspaceRestoredResponse) isJSONResponse() {}

// handleWorkspaceRestore creates a new workspace from an export file. The caller
// becomes its owner and it becomes the current workspace. A file this server
// cannot read, or a file that names people who have no account here, is refused
// before anything is written. The webhook secrets come back once, here, and are
// not kept in the clear anywhere else.
func (s *Server) handleWorkspaceRestore(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		s.writeJSONStatus(w, http.StatusUnauthorized, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.generic")})
		return
	}
	result, err := s.services.WorkspaceFile.Restore(r.Context(), user.ID, r.Body)
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	setTeamCookie(w, r, result.TeamID)
	s.rememberWorkspace(r, user.ID, result.TeamID)
	hooks := make([]restoredWebhookResponse, 0, len(result.Webhooks))
	for _, hook := range result.Webhooks {
		hooks = append(hooks, restoredWebhookResponse{URL: hook.URL, Secret: hook.Secret})
	}
	s.writeJSON(w, workspaceRestoredResponse{TeamID: result.TeamID, Slug: result.Slug, Webhooks: hooks})
}

func (s *Server) writeRestoreError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		s.writeJSONStatus(w, http.StatusRequestEntityTooLarge, apiErrorResponse{Error: i18n.T(resolveLang(r), "imp.workspaceTooBig")})
	case errors.Is(err, appmodel.ErrRestorePeopleMissing):
		people := strings.TrimPrefix(err.Error(), appmodel.ErrRestorePeopleMissing.Error()+": ")
		s.writeJSONStatus(w, http.StatusUnprocessableEntity, apiErrorResponse{
			Error: fmt.Sprintf(i18n.T(resolveLang(r), "imp.workspaceMissing"), people)})
	case errors.Is(err, appmodel.ErrUnsupportedWorkspaceFile):
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "imp.workspaceBadFile")})
	default:
		s.writeInternalJSONError(w, err)
	}
}
