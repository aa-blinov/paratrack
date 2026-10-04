package web

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	data := &exportPage{
		pageData:       pageData{Title: "Export", Active: "export", ReactApp: true},
		ReportsEnabled: s.teamModules(r)["reports"],
	}
	s.renderPageForRequest(w, r, "Export", "export", "export", data)
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
