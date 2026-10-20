package web

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	data := &exportPage{
		pageData:           pageData{Title: "Export", Active: "export", ReactApp: true},
		ReportsEnabled:     s.teamModules(r)["reports"],
		CanExportWorkspace: canManage(r),
	}
	s.renderPageForRequest(w, r, "Export", "export", data)
}

func (s *Server) handleCSV(w http.ResponseWriter, r *http.Request) {
	start := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	end := userNow(r).Add(24 * time.Hour)
	var hasFrom, hasTo bool
	if value := r.URL.Query().Get("from"); value != "" {
		day, err := time.ParseInLocation("2006-01-02", value, userLoc(r))
		if err != nil {
			http.Error(w, i18n.T(resolveLang(r), "export.invalidPeriod"), http.StatusBadRequest)
			return
		}
		start, hasFrom = day, true
	}
	if value := r.URL.Query().Get("to"); value != "" {
		day, err := time.ParseInLocation("2006-01-02", value, userLoc(r))
		if err != nil || (hasFrom && day.Before(start)) {
			http.Error(w, i18n.T(resolveLang(r), "export.invalidPeriod"), http.StatusBadRequest)
			return
		}
		end, hasTo = day.AddDate(0, 0, 1), true
	}
	snapshot, err := s.services.ReportBuilder.BuildExport(r.Context(), appmodel.ExportBuildQuery{
		TeamID: teamID(r), Start: start, End: end, Now: userNow(r), HasFrom: hasFrom, HasTo: hasTo,
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="paratrack.csv"`)
	csvWriter := csv.NewWriter(w)
	if err := csvWriter.Write([]string{"id", "activity", "project", "start", "end", "duration_seconds", "note"}); err != nil {
		return
	}
	for _, row := range snapshot.Rows {
		end := time.Time{}
		if row.EndAt != nil {
			end = *row.EndAt
		}
		dur := ""
		if row.EndAt != nil {
			dur = strconv.Itoa(row.DurationSeconds)
		}
		if err := csvWriter.Write([]string{
			strconv.FormatInt(row.SessionID, 10), row.ActivityName, row.ProjectName,
			row.StartAt.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
			dur, row.Note,
		}); err != nil {
			return
		}
	}
	csvWriter.Flush()
}

// handleWorkspaceExport sends the whole workspace as one JSON file: the people,
// the work, the money and the plans, without secrets. Only owners and admins get
// it, because the file carries every member's rates and every invoice. The
// export is written to the audit log.
func (s *Server) handleWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	export, body, err := s.services.WorkspaceFile.Export(r.Context(), appmodel.WorkspaceExportRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r),
	})
	if err != nil {
		if errors.Is(err, model.ErrForbidden) {
			http.Error(w, i18n.T(resolveLang(r), "err.managersOnly"), http.StatusForbidden)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "workspace.export", export.Team.Slug, "")
	name := fmt.Sprintf("paratrack-%s-%s.json", fileSlug(export.Team.Slug), export.ExportedAt.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// fileSlug keeps the characters a file name can carry from a workspace slug.
func fileSlug(slug string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(slug) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "workspace"
	}
	return b.String()
}
