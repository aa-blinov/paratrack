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
// read a large body, and it may be bigger than the general request limit.
const (
	workspaceRestorePath = "/api/workspace/restore"
	maxWorkspaceRestore  = 64 << 20
)

type workspaceRestoredResponse struct {
	TeamID int64  `json:"team_id"`
	Slug   string `json:"slug"`
}

func (workspaceRestoredResponse) isJSONResponse() {}

// handleWorkspaceRestore creates a new workspace from an export file. The caller
// becomes its owner and it becomes the current workspace. A file this server
// cannot read, or a file that names people who have no account here, is refused
// before anything is written.
func (s *Server) handleWorkspaceRestore(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		s.writeJSONStatus(w, http.StatusUnauthorized, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.generic")})
		return
	}
	export, err := s.services.WorkspaceFile.Decode(r.Body)
	if err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "imp.workspaceBadFile")})
		return
	}
	result, err := s.services.WorkspaceFile.Restore(r.Context(), appmodel.WorkspaceRestoreRequest{CallerID: user.ID, Export: export})
	if err != nil {
		if errors.Is(err, appmodel.ErrRestorePeopleMissing) {
			people := strings.TrimPrefix(err.Error(), appmodel.ErrRestorePeopleMissing.Error()+": ")
			s.writeJSONStatus(w, http.StatusUnprocessableEntity, apiErrorResponse{
				Error: fmt.Sprintf(i18n.T(resolveLang(r), "imp.workspaceMissing"), people)})
			return
		}
		if errors.Is(err, appmodel.ErrUnsupportedWorkspaceFile) {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "imp.workspaceBadFile")})
			return
		}
		s.writeInternalJSONError(w, err)
		return
	}
	setTeamCookie(w, r, result.TeamID)
	s.rememberWorkspace(r, user.ID, result.TeamID)
	s.writeJSON(w, workspaceRestoredResponse{TeamID: result.TeamID, Slug: result.Slug})
}
