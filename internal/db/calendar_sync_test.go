package db

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/calendar"
	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/calendarsync"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/netclients"
)

// googleStub imitates the Google endpoints the sync calls: the token endpoint with
// its PKCE check, and events on the primary calendar with the bearer check.
type googleStub struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	challenge string
	accepted  string
	events    map[string]stubEvent
	seq       int
	listFail  int
	inserts   int
	patches   int
	deletes   []string
}

type stubEvent struct {
	summary, location, description string
	start, end                     string
	etag                           string
}

const stubCode = "code-1"

func newGoogleStub(t *testing.T) *googleStub {
	t.Helper()
	g := &googleStub{t: t, events: map[string]stubEvent{}, accepted: "access-secret-123"}
	g.server = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.server.Close)
	return g
}

func (g *googleStub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.URL.Path == "/token" {
		g.token(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+g.accepted {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const base = "/calendar/v3/calendars/primary/events"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == base:
		if g.listFail != 0 {
			w.WriteHeader(g.listFail)
			return
		}
		items := []map[string]any{}
		for id, e := range g.events {
			items = append(items, map[string]any{"id": id, "etag": e.etag, "summary": e.summary, "location": e.location,
				"description": e.description, "start": map[string]string{"dateTime": e.start}, "end": map[string]string{"dateTime": e.end}})
		}
		writeStubJSON(w, map[string]any{"items": items})
	case r.Method == http.MethodPost && r.URL.Path == base:
		g.seq++
		g.inserts++
		id := fmt.Sprintf("ev%d", g.seq)
		g.events[id] = g.decode(r, id)
		writeStubJSON(w, map[string]any{"id": id, "etag": g.events[id].etag})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, base+"/"):
		id := strings.TrimPrefix(r.URL.Path, base+"/")
		if _, ok := g.events[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		g.patches++
		g.events[id] = g.decode(r, id)
		writeStubJSON(w, map[string]any{"id": id, "etag": g.events[id].etag})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, base+"/"):
		id := strings.TrimPrefix(r.URL.Path, base+"/")
		g.deletes = append(g.deletes, id)
		if _, ok := g.events[id]; !ok {
			w.WriteHeader(http.StatusGone)
			return
		}
		delete(g.events, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		g.t.Errorf("unexpected provider call %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (g *googleStub) decode(r *http.Request, id string) stubEvent {
	var body struct {
		Summary     string `json:"summary"`
		Location    string `json:"location"`
		Description string `json:"description"`
		Start       struct {
			DateTime string `json:"dateTime"`
		} `json:"start"`
		End struct {
			DateTime string `json:"dateTime"`
		} `json:"end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		g.t.Errorf("provider body: %v", err)
	}
	g.seq++
	return stubEvent{summary: body.Summary, location: body.Location, description: body.Description,
		start: body.Start.DateTime, end: body.End.DateTime, etag: fmt.Sprintf("\"e%d\"", g.seq)}
}

func (g *googleStub) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		digest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != stubCode || base64.RawURLEncoding.EncodeToString(digest[:]) != g.challenge {
			w.WriteHeader(http.StatusBadRequest)
			writeStubJSON(w, map[string]string{"error": "invalid_grant"})
			return
		}
		g.accepted = "access-secret-123"
		writeStubJSON(w, map[string]any{"access_token": g.accepted, "refresh_token": "refresh-secret-456", "expires_in": 3600})
	case "refresh_token":
		if r.PostForm.Get("refresh_token") != "refresh-secret-456" {
			w.WriteHeader(http.StatusBadRequest)
			writeStubJSON(w, map[string]string{"error": "invalid_grant"})
			return
		}
		g.accepted = "access-refreshed-789"
		writeStubJSON(w, map[string]any{"access_token": g.accepted, "expires_in": 3600})
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func writeStubJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// put adds an event on the provider side.
func (g *googleStub) put(id, summary, start, end string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	g.events[id] = stubEvent{summary: summary, start: start, end: end, etag: fmt.Sprintf("\"e%d\"", g.seq)}
}

// change edits an event on the provider side, which moves its etag.
func (g *googleStub) change(id, summary string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.events[id]
	g.seq++
	e.summary, e.etag = summary, fmt.Sprintf("\"e%d\"", g.seq)
	g.events[id] = e
}

func (g *googleStub) remove(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.events, id)
}

func (g *googleStub) snapshot() (map[string]stubEvent, []string, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]stubEvent, len(g.events))
	for id, e := range g.events {
		out[id] = e
	}
	return out, append([]string(nil), g.deletes...), g.inserts
}

// calendarRig is a workspace with an owner, a member who connects, and a second member
// who does not, wired to the real database and the stub-backed adapter.
type calendarRig struct {
	d       *DB
	team    int64
	owner   int64
	member  int64
	other   int64
	clock   time.Time
	stub    *googleStub
	service *calendar.SyncService
}

func (r *calendarRig) now() time.Time { return r.clock }

func newCalendarRig(t *testing.T) *calendarRig {
	t.Helper()
	d := openTestDB(t)
	d.secrets = newSecretCodec("calendar-sync-test-key")
	r := &calendarRig{d: d, clock: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}
	d.now = r.now
	ctx := t.Context()
	r.owner = seedApprovalUser(t, d, "cal-owner@test.local")
	r.member = seedApprovalUser(t, d, "cal-member@test.local")
	r.other = seedApprovalUser(t, d, "cal-other@test.local")
	team, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "calendar-sync", Name: "Студия календаря", OwnerID: r.owner})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	r.team = team
	now := FormatTime(r.clock)
	for _, user := range []int64{r.member, r.other} {
		if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes) VALUES (?, ?, 'member', ?, 0, 2400, 60)`, team, user, now); err != nil {
			t.Fatalf("membership: %v", err)
		}
	}
	r.stub = newGoogleStub(t)
	google, err := calendarsync.NewGoogle(calendarsync.Config{
		HTTPClient:  netclients.External(),
		Credentials: calendarport.Credentials{ClientID: "client-id", ClientSecret: "client-secret"},
		Now:         r.now,
		Endpoints: calendarsync.Endpoints{
			Authorization: r.stub.server.URL + "/auth", Token: r.stub.server.URL + "/token", API: r.stub.server.URL + "/calendar/v3",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.service, err = calendar.NewSync(calendar.SyncDependencies{
		Store: d, Adapters: map[string]calendarport.Adapter{calendarport.ProviderGoogle: google},
		Now: r.now, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// connect runs the consent for the member in the given browser session and returns the
// connection's state. The stub checks the PKCE challenge the adapter sent.
func (r *calendarRig) connect(t *testing.T, userID int64, session string) {
	t.Helper()
	ctx := t.Context()
	authURL, err := r.service.BeginConnect(ctx, appmodel.CalendarConnectRequest{
		TeamID: r.team, UserID: userID, Provider: calendarport.ProviderGoogle, SessionToken: session, RedirectURI: "https://pt.example/cb",
	})
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	r.stub.mu.Lock()
	r.stub.challenge = parsed.Query().Get("code_challenge")
	r.stub.mu.Unlock()
	if err := r.service.CompleteConnect(ctx, appmodel.CalendarConnectCallbackRequest{
		TeamID: r.team, UserID: userID, Provider: calendarport.ProviderGoogle, State: parsed.Query().Get("state"),
		Code: stubCode, SessionToken: session, RedirectURI: "https://pt.example/cb",
	}); err != nil {
		t.Fatalf("complete connect: %v", err)
	}
}

func (r *calendarRig) sync(t *testing.T, userID int64) {
	t.Helper()
	if err := r.service.Sync(t.Context(), r.team, userID, calendarport.ProviderGoogle); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// eventsOf lists the events the viewer sees in the window of the stub events.
func (r *calendarRig) eventsOf(t *testing.T, viewer int64) map[string]model.CalendarEvent {
	t.Helper()
	events, err := r.d.ListCalendarEvents(t.Context(), appmodel.CalendarEventListQuery{
		TeamID: r.team, CallerID: viewer, From: r.clock.Add(-40 * 24 * time.Hour), To: r.clock.Add(120 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]model.CalendarEvent{}
	for _, e := range events {
		out[e.Title] = e
	}
	return out
}

func (r *calendarRig) linkCount(t *testing.T, eventID int64) int {
	t.Helper()
	var n int
	if err := r.d.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM calendar_external_links WHERE event_id = ?`, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const (
	pulledStart = "2026-10-26T10:00:00+03:00"
	pulledEnd   = "2026-10-26T11:00:00+03:00"
)

func TestCalendarSyncConnectStoresSealedTokensOnce(t *testing.T) {
	r := newCalendarRig(t)
	r.connect(t, r.member, "session-A")

	var access, refresh string
	if err := r.d.TestSQL().QueryRowContext(t.Context(), `SELECT access_token, refresh_token FROM calendar_connections WHERE team_id = ? AND user_id = ?`, r.team, r.member).Scan(&access, &refresh); err != nil {
		t.Fatal(err)
	}
	for _, stored := range []string{access, refresh} {
		if !strings.HasPrefix(stored, "enc:v1:") || strings.Contains(stored, "secret") {
			t.Fatalf("token stored as %q, want a sealed value", stored)
		}
	}
	creds, err := r.d.CalendarCredentials(t.Context(), r.team, r.member, calendarport.ProviderGoogle)
	if err != nil || creds.Tokens.AccessToken != "access-secret-123" || creds.Tokens.RefreshToken != "refresh-secret-456" {
		t.Fatalf("open credentials = %+v, %v", creds.Tokens, err)
	}

	// A consent is bound to the browser session that started it: a callback from another
	// session is refused and leaves the consent open for the right one, which then
	// succeeds exactly once.
	authURL, err := r.service.BeginConnect(t.Context(), appmodel.CalendarConnectRequest{TeamID: r.team, UserID: r.member, Provider: calendarport.ProviderGoogle, SessionToken: "session-B", RedirectURI: "https://pt.example/cb"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(authURL)
	state := parsed.Query().Get("state")
	r.stub.mu.Lock()
	r.stub.challenge = parsed.Query().Get("code_challenge")
	r.stub.mu.Unlock()
	callback := func(session string) error {
		return r.service.CompleteConnect(t.Context(), appmodel.CalendarConnectCallbackRequest{TeamID: r.team, UserID: r.member, Provider: calendarport.ProviderGoogle, State: state, Code: stubCode, SessionToken: session, RedirectURI: "https://pt.example/cb"})
	}
	if err := callback("session-OTHER"); !errors.Is(err, model.ErrCalendarStateInvalid) {
		t.Fatalf("callback from another session = %v, want ErrCalendarStateInvalid", err)
	}
	if err := callback("session-B"); err != nil {
		t.Fatalf("callback from the starting session = %v", err)
	}
	if err := callback("session-B"); !errors.Is(err, model.ErrCalendarStateInvalid) {
		t.Fatalf("replayed callback = %v, want ErrCalendarStateInvalid: the state is single-use", err)
	}
}

func TestCalendarSyncPullsProviderEventAsPersonalLinkedEvent(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Созвон с клиентом", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")

	events := r.eventsOf(t, r.member)
	event, ok := events["Созвон с клиентом"]
	if !ok {
		t.Fatalf("the pulled event is missing for its member: %v", events)
	}
	if event.Shared || event.CreatedBy != r.member || r.linkCount(t, event.ID) != 1 {
		t.Fatalf("pulled event = %+v, want a personal event of the member with one link", event)
	}
	if _, ok := r.eventsOf(t, r.other)["Созвон с клиентом"]; ok {
		t.Fatal("another member sees a personal pulled event")
	}
	if _, ok := r.eventsOf(t, r.owner)["Созвон с клиентом"]; !ok {
		t.Fatal("a manager does not see the member's personal event")
	}
}

func TestCalendarSyncRemovesPulledEventOnlyWhenProviderDroppedIt(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Отменённая встреча", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")
	teamEvent, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.member, Title: "Личная заметка", StartsAt: r.clock.Add(24 * time.Hour), EndsAt: r.clock.Add(25 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	r.stub.remove("g-1")
	r.sync(t, r.member)

	events := r.eventsOf(t, r.member)
	if _, ok := events["Отменённая встреча"]; ok {
		t.Fatal("a pulled event the provider dropped is still in the workspace")
	}
	if _, ok := events["Личная заметка"]; !ok || teamEvent.ID == 0 {
		t.Fatal("a team-made event was removed by the sync")
	}
}

func TestCalendarSyncPushesTeamEventOnceAndLinksIt(t *testing.T) {
	r := newCalendarRig(t)
	r.connect(t, r.member, "session-A")
	created, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.member, Title: "Демо клиенту", Location: "Zoom", StartsAt: r.clock.Add(48 * time.Hour), EndsAt: r.clock.Add(49 * time.Hour), SyncPush: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	r.sync(t, r.member)
	r.sync(t, r.member)

	remote, _, inserts := r.stub.snapshot()
	if inserts != 1 || len(remote) != 1 {
		t.Fatalf("provider holds %d events after %d inserts, want one", len(remote), inserts)
	}
	for _, e := range remote {
		if e.summary != "Демо клиенту" || e.location != "Zoom" {
			t.Fatalf("pushed event = %+v", e)
		}
	}
	if r.linkCount(t, created.ID) != 1 {
		t.Fatal("the pushed event has no link")
	}
	if n := len(r.eventsOf(t, r.member)); n != 1 {
		t.Fatalf("the pushed copy came back as %d local events, want only the team's own", n)
	}
}

func TestCalendarSyncProviderChangeWinsForPulledEvent(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Черновик", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")

	// Two hours later the access token has expired: the sync refreshes it first.
	r.clock = r.clock.Add(2 * time.Hour)
	r.stub.change("g-1", "Согласовано у клиента")
	r.sync(t, r.member)

	events := r.eventsOf(t, r.member)
	if _, ok := events["Согласовано у клиента"]; !ok {
		t.Fatalf("the provider's change did not reach the pulled event: %v", events)
	}
	if _, ok := events["Черновик"]; ok {
		t.Fatal("the old title survived the provider's change")
	}
	var access string
	if err := r.d.TestSQL().QueryRowContext(t.Context(), `SELECT access_token FROM calendar_connections WHERE team_id = ? AND user_id = ?`, r.team, r.member).Scan(&access); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(access, "enc:v1:") {
		t.Fatal("the refreshed access token was stored in the clear")
	}
}

func TestCalendarSyncFailingProviderRecordsErrorAndKeepsData(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Встреча", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")

	// The provider drops the event, but its listing fails: nothing may be deleted.
	r.stub.remove("g-1")
	r.stub.mu.Lock()
	r.stub.listFail = http.StatusInternalServerError
	r.stub.mu.Unlock()
	err := r.service.Sync(t.Context(), r.team, r.member, calendarport.ProviderGoogle)
	if err == nil {
		t.Fatal("a failing listing reported success")
	}
	if _, ok := r.eventsOf(t, r.member)["Встреча"]; !ok {
		t.Fatal("a failed sync removed a pulled event")
	}
	connections, err := r.service.Connections(t.Context(), r.team, r.member)
	if err != nil || len(connections) != 1 || connections[0].LastError != "google 500" {
		t.Fatalf("connections = %+v, %v; want last_error google 500", connections, err)
	}
	if connections[0].LastSyncedAt.IsZero() {
		t.Fatal("a failed attempt was not recorded as a sync time")
	}
}

func TestCalendarSyncDisconnectKeepsImportedEvents(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Импортированная встреча", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")
	pushed, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.member, Title: "Отправленная встреча", StartsAt: r.clock.Add(48 * time.Hour), EndsAt: r.clock.Add(49 * time.Hour), SyncPush: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.sync(t, r.member)

	if err := r.service.Disconnect(t.Context(), r.team, r.member, calendarport.ProviderGoogle); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := r.d.CalendarCredentials(t.Context(), r.team, r.member, calendarport.ProviderGoogle); !errors.Is(err, model.ErrCalendarNotConnected) {
		t.Fatalf("credentials after disconnect = %v, want not connected", err)
	}
	events := r.eventsOf(t, r.member)
	imported, ok := events["Импортированная встреча"]
	if !ok || r.linkCount(t, imported.ID) != 0 {
		t.Fatalf("the imported event is gone or still linked: %+v", imported)
	}
	if _, ok := events["Отправленная встреча"]; !ok || r.linkCount(t, pushed.ID) != 0 {
		t.Fatal("the pushed event is gone or still linked after disconnect")
	}
	if remote, _, _ := r.stub.snapshot(); len(remote) != 2 {
		t.Fatalf("disconnect changed the provider calendar: %d events, want both left in place", len(remote))
	}
}

func TestCalendarSyncMemberWithoutConnectionIsRefused(t *testing.T) {
	r := newCalendarRig(t)
	if _, err := r.service.SyncMine(t.Context(), r.team, r.other); !errors.Is(err, model.ErrCalendarNotConnected) {
		t.Fatalf("sync for a member without a connection = %v, want ErrCalendarNotConnected", err)
	}
	_, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.other, Title: "Без подключения", StartsAt: r.clock, EndsAt: r.clock.Add(time.Hour), SyncPush: true,
	})
	if !errors.Is(err, model.ErrCalendarNotConnected) {
		t.Fatalf("push switch without a connection = %v, want ErrCalendarNotConnected", err)
	}
}

func TestCalendarSyncNeverTouchesAnotherMembersEvents(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Встреча участника", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")
	memberEvent := r.eventsOf(t, r.member)["Встреча участника"]

	// The owner's own event, linked to nothing, must survive the member's sync.
	ownerEvent, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.owner, Title: "Планёрка руководителя", StartsAt: r.clock.Add(24 * time.Hour), EndsAt: r.clock.Add(25 * time.Hour), Shared: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.stub.remove("g-1")
	r.sync(t, r.member)

	if _, err := r.d.CalendarCredentials(t.Context(), r.team, r.other, calendarport.ProviderGoogle); !errors.Is(err, model.ErrCalendarNotConnected) {
		t.Fatal("the other member has a connection they never made")
	}
	if err := r.d.RemovePulledCalendarEvent(t.Context(), appmodel.CalendarSyncRemovalRequest{TeamID: r.team, UserID: r.other, Provider: calendarport.ProviderGoogle, EventID: memberEvent.ID}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("another member removed a pulled event: %v", err)
	}
	if _, err := r.d.UpdateCalendarEvent(t.Context(), appmodel.CalendarEventUpdateRequest{TeamID: r.team, CallerID: r.owner, EventID: ownerEvent.ID, Title: strPtr("Переименовано")}); err != nil {
		t.Fatalf("owner edit: %v", err)
	}
	if _, ok := r.eventsOf(t, r.member)["Планёрка руководителя"]; ok {
		t.Fatal("a personal manager event leaked to a member")
	}
	if _, ok := r.eventsOf(t, r.owner)["Переименовано"]; !ok {
		t.Fatal("the member's sync removed or changed the owner's event")
	}
}

func TestCalendarSyncDeletingPushedEventDeletesProviderCopy(t *testing.T) {
	r := newCalendarRig(t)
	r.connect(t, r.member, "session-A")
	created, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.member, Title: "Удаляемая встреча", StartsAt: r.clock.Add(48 * time.Hour), EndsAt: r.clock.Add(49 * time.Hour), SyncPush: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.sync(t, r.member)
	if err := r.d.DeleteCalendarEvent(t.Context(), appmodel.CalendarEventDeleteRequest{TeamID: r.team, CallerID: r.member, EventID: created.ID}); err != nil {
		t.Fatal(err)
	}
	r.sync(t, r.member)
	if remote, deletes, _ := r.stub.snapshot(); len(remote) != 0 || len(deletes) != 1 {
		t.Fatalf("provider holds %d events and saw %d deletes; the pushed copy should be gone", len(remote), len(deletes))
	}
}

func TestCalendarSyncLocallyDeletedPulledEventStaysDeleted(t *testing.T) {
	r := newCalendarRig(t)
	r.stub.put("g-1", "Лишняя встреча", pulledStart, pulledEnd)
	r.connect(t, r.member, "session-A")
	imported := r.eventsOf(t, r.member)["Лишняя встреча"]
	if err := r.d.DeleteCalendarEvent(t.Context(), appmodel.CalendarEventDeleteRequest{TeamID: r.team, CallerID: r.member, EventID: imported.ID}); err != nil {
		t.Fatal(err)
	}
	r.sync(t, r.member)
	if _, ok := r.eventsOf(t, r.member)["Лишняя встреча"]; ok {
		t.Fatal("a deleted pulled event came back on the next sync")
	}
	if remote, deletes, _ := r.stub.snapshot(); len(remote) != 1 || len(deletes) != 0 {
		t.Fatal("deleting an imported event changed the provider calendar")
	}
}

func strPtr(s string) *string { return &s }

func TestCalendarSyncSwitchingPushOffDeletesProviderCopy(t *testing.T) {
	r := newCalendarRig(t)
	r.connect(t, r.member, "session-A")
	created, err := r.d.CreateCalendarEvent(t.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: r.team, CallerID: r.member, Title: "Снятая с отправки", StartsAt: r.clock.Add(48 * time.Hour), EndsAt: r.clock.Add(49 * time.Hour), SyncPush: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.sync(t, r.member)

	// A manager may not switch the author's push on or off.
	off := false
	if _, err := r.d.UpdateCalendarEvent(t.Context(), appmodel.CalendarEventUpdateRequest{TeamID: r.team, CallerID: r.owner, EventID: created.ID, SyncPush: &off}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("manager switched the author's push off: %v", err)
	}

	if _, err := r.d.UpdateCalendarEvent(t.Context(), appmodel.CalendarEventUpdateRequest{TeamID: r.team, CallerID: r.member, EventID: created.ID, SyncPush: &off}); err != nil {
		t.Fatalf("author switches push off: %v", err)
	}
	r.sync(t, r.member)
	if remote, deletes, _ := r.stub.snapshot(); len(remote) != 0 || len(deletes) != 1 {
		t.Fatalf("provider holds %d events and saw %d deletes after push was switched off", len(remote), len(deletes))
	}
	if r.linkCount(t, created.ID) != 0 {
		t.Fatal("the event kept a link after push was switched off")
	}
}
