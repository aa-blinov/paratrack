package web

import (
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// workdayView is today's working day on the dashboard (TIME-06): the time it started and,
// once given, the time it ended. Empty before the start.
type workdayView struct {
	Started string
	Ended   string
}

// workdayOf reads the viewer's working day for today.
func (s *Server) workdayOf(r *http.Request) workdayView {
	day := startOfDay(userNow(r)).Format("2006-01-02")
	mark, ok, err := s.services.Scheduling.WorkdayOf(r.Context(), teamID(r), authenticatedUserID(r), day)
	if err != nil {
		s.logInternalError(err)
		return workdayView{}
	}
	if !ok {
		return workdayView{}
	}
	loc := userLoc(r)
	view := workdayView{Started: clockOf(mark.StartedAt, loc)}
	if !mark.EndedAt.IsZero() {
		view.Ended = clockOf(mark.EndedAt, loc)
	}
	return view
}

// clockOf shows a time of day in the viewer's zone, as hours and minutes.
func clockOf(at time.Time, loc *time.Location) string {
	return at.In(loc).Format("15:04")
}

// handleWorkday marks the start or the end of the viewer's working day (TIME-06). The
// start is kept once; the end is the last one given.
func (s *Server) handleWorkday(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	err := s.services.Scheduling.MarkWorkday(r.Context(), appmodel.WorkdayRequest{
		TeamID: teamID(r), UserID: authenticatedUserID(r), Day: startOfDay(now).Format("2006-01-02"),
		Action: r.FormValue("action"), Now: now,
	})
	if err != nil {
		http.Redirect(w, r, "/?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
