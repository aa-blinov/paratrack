package web

import (
	"net/http"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// clientTagsBody is the whole tag set a client should carry after the write. Tags is a
// pointer so a body that leaves the list out is refused, not read as "clear them".
type clientTagsBody struct {
	Tags *[]string `json:"tags"`
}

// clientTagsResponse answers a write with the set as it was stored.
type clientTagsResponse struct {
	Tags []string `json:"tags"`
}

func (clientTagsResponse) isJSONResponse() {}

// handleClientTagsSet replaces the tags of one client (CRM-18). The route admits any
// signed-in member and leaves the manager rule to the transaction, so a member is
// refused with the same forbidden text as every other CRM write, and not with the
// page guard's manager-only message.
func (s *Server) handleClientTagsSet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.notFoundEntity")})
		return
	}
	var body clientTagsBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Tags == nil {
		s.writeCRMError(w, r, appmodel.FieldError{Field: "tags"})
		return
	}
	tags, err := s.services.Clients.SetTags(r.Context(), appmodel.ClientTagsRequest{
		TeamID: teamID(r), ClientID: id, CallerID: authenticatedUserID(r), Tags: *body.Tags,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, clientTagsResponse{Tags: tags})
}
