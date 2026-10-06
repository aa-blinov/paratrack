package web

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// The journal widens one window instead of stepping through pages, so a link
// from a notification reopens everything the reader had already scrolled past.
// The window stops below auditWindowMax because the handler asks for one row
// past it to learn whether the log was cut, and the store refuses more than 500.
const (
	auditWindowRows = 100
	auditWindowStep = 100
	auditWindowMax  = 400
	// auditOptionRows bounds the unfiltered read that builds the action
	// choices, so the list of actions does not shrink when another filter
	// narrows the events on screen.
	auditOptionRows = 500
)

func (s *Server) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	team := teamID(r)
	// The action choices come from the unfiltered trail: deriving them from the
	// filtered rows would make every other action unreachable without a reset.
	trail, err := s.services.AuditLog.List(r.Context(), appmodel.AuditListQuery{TeamID: team, Limit: auditOptionRows})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	members, err := s.services.Teams.Directory.Members(r.Context(), team)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	people := make([]auditPerson, 0, len(members))
	for _, member := range members {
		// A member may have no display name yet; the address identifies them.
		label := strings.TrimSpace(member.Name)
		if label == "" {
			label = member.Email
		}
		people = append(people, auditPerson{ID: member.UserID, Name: label})
	}
	actions := auditActionCodes(trail)
	filters := requestedAuditFilters(r, userNow(r), people, actions)
	list, err := s.services.AuditLog.List(r.Context(), appmodel.AuditListQuery{
		TeamID: team, From: filters.from, To: filters.to,
		UserID: filters.userID, Action: filters.action, Limit: filters.window + 1,
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	cut := len(list) > filters.window
	if cut {
		list = list[:filters.window]
	}
	lang := string(resolveLang(r))
	// An empty list must reach the page as an empty array: "nothing matched"
	// is a state the filter screen has to render, not a missing field.
	data := auditPage{pageData: pageData{Title: "Audit log", Active: "settings-audit", Lang: lang, ReactApp: true}, AuditReact: true, Items: []auditRow{}}
	for _, e := range list {
		data.Items = append(data.Items, auditRow{
			Time: e.CreatedAt.Format("2006-01-02 15:04"), Action: e.Action,
			Target: e.Target, IP: e.IP,
		})
	}
	data.From, data.To, data.UserID, data.Action = filters.fromRaw, filters.toRaw, filters.userID, filters.action
	data.People, data.Actions, data.Window = people, actions, filters.window
	data.Filtered = data.From != "" || data.To != "" || data.UserID > 0 || data.Action != ""
	data.EmptyFiltered = data.Filtered && len(data.Items) == 0
	if cut && filters.window < auditWindowMax {
		wider := filters.values()
		wider.Set("size", strconv.Itoa(filters.window+auditWindowStep))
		data.MoreURL = auditPath(wider)
	}
	s.renderPageForRequest(w, r, "Audit log", "settings-audit", "audit", &data)
}

// auditFilters is the filter state of one journal view. It is read from the
// address so that a shared link reopens the same list, and every field is
// echoed back into the controls that produced it.
type auditFilters struct {
	from    time.Time
	to      time.Time
	fromRaw string
	toRaw   string
	userID  int64
	action  string
	window  int
}

// requestedAuditFilters reads the filter state out of the address. Choices the
// log or the workspace does not contain are dropped instead of being shown as
// active filters that can never match — the treatment an unknown project slug
// already gets on the stats screen.
func requestedAuditFilters(r *http.Request, now time.Time, people []auditPerson, actions []string) auditFilters {
	query := r.URL.Query()
	filters := auditFilters{window: auditWindow(query.Get("size"))}
	if raw := strings.TrimSpace(query.Get("from")); raw != "" {
		if start, err := timeparse.ParseDateTime(raw, now); err == nil {
			filters.from, filters.fromRaw = start, raw
		}
	}
	if raw := strings.TrimSpace(query.Get("to")); raw != "" {
		// A bare date covers its whole day, so the bound is the next midnight.
		if end, err := timeparse.ParseDateTime(raw, now); err == nil {
			filters.to, filters.toRaw = end.AddDate(0, 0, 1), raw
		}
	}
	if !filters.from.IsZero() && !filters.to.IsZero() && !filters.to.After(filters.from) {
		// An inverted range would hide every event; read it as no period at all.
		filters.from, filters.to, filters.fromRaw, filters.toRaw = time.Time{}, time.Time{}, "", ""
	}
	for _, person := range people {
		if strconv.FormatInt(person.ID, 10) == strings.TrimSpace(query.Get("user")) {
			filters.userID = person.ID
		}
	}
	// The control is named "event", not "action": a form control named after an
	// HTMLFormElement property shadows it, and the browser would submit the form
	// to the control instead of the journal.
	if action := strings.TrimSpace(query.Get("event")); auditActionKnown(actions, action) {
		filters.action = action
	}
	return filters
}

// auditWindow rounds a requested ?size= down to a whole step, so the address
// says exactly how many rows the screen shows.
func auditWindow(size string) int {
	rows, err := strconv.Atoi(strings.TrimSpace(size))
	if err != nil || rows <= auditWindowRows {
		return auditWindowRows
	}
	if rows >= auditWindowMax {
		return auditWindowMax
	}
	return rows / auditWindowStep * auditWindowStep
}

// auditActionCodes lists the action types the workspace has actually recorded,
// alphabetically, so the filter offers codes instead of a free-text guess.
func auditActionCodes(trail []model.AuditEntry) []string {
	seen := make(map[string]bool, len(trail))
	codes := make([]string, 0, len(trail))
	for _, entry := range trail {
		if entry.Action == "" || seen[entry.Action] {
			continue
		}
		seen[entry.Action] = true
		codes = append(codes, entry.Action)
	}
	sort.Strings(codes)
	return codes
}

// values renders the filters that are actually in effect, so every link this
// page generates carries the state the screen shows. A hand-edited address that
// resolved to "no filter" must not leak its rejected junk into the next link.
func (f auditFilters) values() url.Values {
	values := url.Values{}
	if f.fromRaw != "" {
		values.Set("from", f.fromRaw)
	}
	if f.toRaw != "" {
		values.Set("to", f.toRaw)
	}
	if f.userID > 0 {
		values.Set("user", strconv.FormatInt(f.userID, 10))
	}
	if f.action != "" {
		values.Set("event", f.action)
	}
	if f.window != auditWindowRows {
		values.Set("size", strconv.Itoa(f.window))
	}
	return values
}

// auditPath is the journal address for a set of filter parameters.
func auditPath(values url.Values) string {
	if len(values) == 0 {
		return "/settings/audit"
	}
	return "/settings/audit?" + values.Encode()
}

// auditActionKnown reports whether the workspace has recorded an action code,
// so a hand-edited link cannot put the filter into a state nothing matches.
func auditActionKnown(actions []string, want string) bool {
	if want == "" {
		return false
	}
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

type auditRow struct {
	Time   string
	Action string
	Target string
	IP     string
}

// auditPerson is one actor choice of the filter.
type auditPerson struct {
	ID   int64
	Name string
}

type auditPage struct {
	pageData
	AuditReact bool
	Items      []auditRow
	Flash      string
	FlashOK    bool

	// Filter state, echoed into the controls so the current view reads without
	// relying on colour.
	From    string
	To      string
	UserID  int64
	Action  string
	People  []auditPerson
	Actions []string
	// Window is how many rows this view asked for; MoreURL widens it.
	Window        int
	MoreURL       string
	Filtered      bool // a filter is narrowing the trail
	EmptyFiltered bool // filters are on, but nothing matched them
}

func (p *auditPage) setCSRF(t string) { p.pageData.setCSRF(t) }
