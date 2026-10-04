package web

import (
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleAPIMemberRole(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	target, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/members?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.services.TeamOps.SetRole(operationContext(r), appmodel.TeamMemberRoleRequest{TeamID: team.ID, TargetUserID: target, CallerID: user.ID, Role: model.TeamRole(r.FormValue("role"))}); err != nil {
		http.Redirect(w, r, "/settings/members?flash=forbidden", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/members?flash=updated", http.StatusSeeOther)
}

// handleAPITeamTransfer hands the workspace to another member (owner only).

func (s *Server) handleAPITeamTransfer(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	target, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/members?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.services.TeamOps.TransferOwnership(operationContext(r), appmodel.TeamOwnershipTransferRequest{TeamID: team.ID, CallerID: user.ID, NewOwnerID: target}); err != nil {
		code := "forbidden"
		if errors.Is(err, appmodel.ErrTeamValidation) {
			code = "transfer_personal"
		}
		http.Redirect(w, r, "/settings/members?flash="+code, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/members?flash=transferred", http.StatusSeeOther)
}

func (s *Server) handleAPIMemberRemove(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	caller, _ := UserFrom(r.Context())
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/team/members/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if err := s.services.Teams.Administration.RemoveMember(r.Context(), appmodel.TeamMemberRemovalRequest{TeamID: team.ID, TargetUserID: uid, CallerID: caller.ID}); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/members?flash=removed", http.StatusSeeOther)
}
