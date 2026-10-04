package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type reportPersonMember struct {
	UserID int64
	Name   string
	Email  string
}

type reportPersonScope struct {
	Context context.Context
	Members []reportPersonMember
	UserID  int64
	Name    string
}

// resolveReportPersonScope applies the optional manager-only member filter
// consistently across report pages. Member options are loaded only by views
// that display the full selector.
func (s *Server) resolveReportPersonScope(r *http.Request, includeMembers bool) (reportPersonScope, error) {
	scope := reportPersonScope{Context: r.Context()}
	if !canManage(r) {
		return scope, nil
	}
	requestedID, _ := strconv.ParseInt(r.URL.Query().Get("person"), 10, 64)
	if !includeMembers && requestedID <= 0 {
		return scope, nil
	}
	members, err := s.services.Teams.Directory.Members(r.Context(), teamID(r))
	if err != nil {
		return reportPersonScope{}, fmt.Errorf("list workspace members for report scope: %w", err)
	}
	scope.Members = make([]reportPersonMember, 0, len(members))
	for _, member := range members {
		scope.Members = append(scope.Members, reportPersonMember{
			UserID: member.UserID, Name: member.Name, Email: member.Email,
		})
	}
	scope.Context, scope.UserID, scope.Name = applyPersonScope(scope.Context, members, requestedID)
	return scope, nil
}

func applyPersonScope(ctx context.Context, members []model.TeamMember, requestedID int64) (context.Context, int64, string) {
	if requestedID <= 0 {
		return ctx, 0, ""
	}
	for _, member := range members {
		if member.UserID != requestedID {
			continue
		}
		name := member.Name
		if name == "" {
			name = member.Email
		}
		if name == "" {
			return ctx, 0, ""
		}
		return requestctx.WithScope(ctx, member.UserID), member.UserID, name
	}
	return ctx, 0, ""
}
