package web

import (
	"net/http"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	data, err := s.buildStatsData(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.render(w, r, "stats-content", &data)
}

// The editable log shows the newest statsLogRows; "show all" lifts it to
// statsLogAll (past that the CSV export has everything).
const (
	statsLogRows = 50
	statsLogAll  = 500
)

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	data, err := s.buildGraphData(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.render(w, r, "graph-content", &data)
}
