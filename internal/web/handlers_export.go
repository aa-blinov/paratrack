package web

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	data := &exportPage{
		pageData:       pageData{Title: "Export", Active: "export"},
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
	sessions, err := s.services.Tracking.Queries.ClosedSessions(r.Context(), teamID(r), start, end, nil)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	// Look up project names in one IN-list query so the per-row join
	// is O(1) instead of one extra round-trip per session.
	projNameByID := map[int64]string{}
	if len(sessions) > 0 {
		seen := map[int64]struct{}{}
		var pids []int64
		for _, as := range sessions {
			if as.Activity.ProjectID == 0 {
				continue
			}
			if _, ok := seen[as.Activity.ProjectID]; ok {
				continue
			}
			seen[as.Activity.ProjectID] = struct{}{}
			pids = append(pids, as.Activity.ProjectID)
		}
		if len(pids) > 0 {
			projects, err := s.services.Projects.Queries.Summaries(r.Context(), teamID(r), pids)
			if err != nil {
				s.writeInternalError(w, err)
				return
			}
			for id, project := range projects {
				projNameByID[id] = project.Name
			}
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="paratrack.csv"`)
	csvWriter := csv.NewWriter(w)
	if err := csvWriter.Write([]string{"id", "activity", "project", "start", "end", "duration_seconds", "note"}); err != nil {
		return
	}
	for _, as := range sessions {
		if (hasFrom && as.Session.StartAt.Before(start)) || (hasTo && !as.Session.StartAt.Before(end)) {
			continue
		}
		end := time.Time{}
		if as.Session.EndAt != nil {
			end = *as.Session.EndAt
		}
		dur := ""
		if as.Session.EndAt != nil {
			dur = strconv.Itoa(as.Session.DurationSeconds(userNow(r)))
		}
		note := ""
		if as.Session.Note != nil {
			note = *as.Session.Note
		}
		project := "" // "" = Uncategorized in the CSV
		if name, ok := projNameByID[as.Activity.ProjectID]; ok {
			project = name
		}
		if err := csvWriter.Write([]string{
			strconv.FormatInt(as.Session.ID, 10), as.Activity.Name, project,
			as.Session.StartAt.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
			dur, note,
		}); err != nil {
			return
		}
	}
	csvWriter.Flush()
}
