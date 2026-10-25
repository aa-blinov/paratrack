package calendarsync

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

var clockAt = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

// stub is a provider imitation: each test registers the answers it needs and
// records what the adapter sent.
type stub struct {
	t        *testing.T
	server   *httptest.Server
	handlers map[string]http.HandlerFunc
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{t: t, handlers: map[string]http.HandlerFunc{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, ok := s.handlers[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unexpected provider call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) on(method, path string, handler http.HandlerFunc) {
	s.handlers[method+" "+path] = handler
}

func (s *stub) url() string { return s.server.URL }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func config(endpoints Endpoints) Config {
	return Config{
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		Credentials: calendarport.Credentials{ClientID: "client-id", ClientSecret: "client-secret"},
		Now:         func() time.Time { return clockAt },
		Endpoints:   endpoints,
	}
}

func TestCalendarSyncGoogleAuthorizationURLCarriesPKCEAndOfflineAccess(t *testing.T) {
	g, err := NewGoogle(config(Endpoints{Authorization: "https://auth.example/google"}))
	if err != nil {
		t.Fatal(err)
	}
	raw := g.AuthorizationURL(calendarport.AuthorizationRequest{RedirectURI: "https://pt.example/cb", State: "st", Challenge: "ch"})
	for _, want := range []string{"client_id=client-id", "access_type=offline", "state=st", "code_challenge=ch", "code_challenge_method=S256", "response_type=code"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("authorization URL %q lacks %q", raw, want)
		}
	}
}

func TestCalendarSyncGoogleExchangeSendsVerifierAndDatesExpiry(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodPost, "/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("code_verifier") != "verifier-1" || r.PostForm.Get("client_secret") != "client-secret" || r.PostForm.Get("grant_type") != "authorization_code" {
			t.Errorf("token form = %v", r.PostForm)
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600})
	})
	g, err := NewGoogle(config(Endpoints{Token: s.url() + "/token"}))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := g.Exchange(t.Context(), "code-1", "verifier-1", "https://pt.example/cb")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "at-1" || tokens.RefreshToken != "rt-1" || !tokens.ExpiresAt.Equal(clockAt.Add(time.Hour)) {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestCalendarSyncGoogleRefreshRefusalIsTokenRejected(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodPost, "/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
	})
	g, err := NewGoogle(config(Endpoints{Token: s.url() + "/token"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Refresh(t.Context(), "revoked"); !errors.Is(err, calendarport.ErrTokenRejected) {
		t.Fatalf("refresh error = %v, want ErrTokenRejected", err)
	}
}

func TestCalendarSyncGooglePagesListAndSkipsCancelledAndAllDay(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodGet, "/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			t.Errorf("list sent Authorization %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("singleEvents") != "true" || r.URL.Query().Get("timeMin") == "" || r.URL.Query().Get("timeMax") == "" {
			t.Errorf("list query = %v", r.URL.Query())
		}
		if r.URL.Query().Get("pageToken") == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"id": "e1", "etag": "\"1\"", "summary": "Созвон", "location": "Офис", "description": "Повестка",
						"start": map[string]string{"dateTime": "2026-10-26T10:00:00+03:00"}, "end": map[string]string{"dateTime": "2026-10-26T11:00:00+03:00"}},
					{"id": "gone", "status": "cancelled", "start": map[string]string{"dateTime": "2026-10-26T10:00:00Z"}, "end": map[string]string{"dateTime": "2026-10-26T11:00:00Z"}},
					{"id": "day", "summary": "Отпуск", "start": map[string]string{"date": "2026-10-27"}, "end": map[string]string{"date": "2026-10-28"}},
				},
				"nextPageToken": "p2",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{
			{"id": "e2", "etag": "\"2\"", "summary": "Демо", "start": map[string]string{"dateTime": "2026-11-01T09:00:00Z"}, "end": map[string]string{"dateTime": "2026-11-01T10:00:00Z"}},
		}})
	})
	g, err := NewGoogle(config(Endpoints{API: s.url() + "/calendar/v3"}))
	if err != nil {
		t.Fatal(err)
	}
	events, err := g.List(t.Context(), "at", calendarport.Window{From: clockAt.Add(-time.Hour), To: clockAt.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID != "e1" || events[1].ID != "e2" {
		t.Fatalf("events = %+v, want e1 and e2 only", events)
	}
	if !events[0].StartsAt.Equal(time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC)) || events[0].Location != "Офис" || events[0].Notes != "Повестка" {
		t.Fatalf("first event mapped wrong: %+v", events[0])
	}
}

func TestCalendarSyncGoogleWritesReturnTheProviderIDAndEtag(t *testing.T) {
	s := newStub(t)
	event := calendarport.Event{Title: "Планёрка", StartsAt: clockAt, EndsAt: clockAt.Add(time.Hour), Notes: "текст"}
	s.on(http.MethodPost, "/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["summary"] != "Планёрка" {
			t.Errorf("insert body = %v", body)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "new-id", "etag": "\"e-new\""})
	})
	s.on(http.MethodPatch, "/calendar/v3/calendars/primary/events/gone-id", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	s.on(http.MethodDelete, "/calendar/v3/calendars/primary/events/old-id", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	s.on(http.MethodDelete, "/calendar/v3/calendars/primary/events/gone-id", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	})
	g, err := NewGoogle(config(Endpoints{API: s.url() + "/calendar/v3"}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := g.Create(t.Context(), "at", event)
	if err != nil || created.ID != "new-id" || created.Etag != "\"e-new\"" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	if _, err := g.Update(t.Context(), "at", "gone-id", event); !errors.Is(err, calendarport.ErrRemoteEventGone) {
		t.Fatalf("update of a missing event = %v, want ErrRemoteEventGone", err)
	}
	if err := g.Delete(t.Context(), "at", "old-id"); err != nil {
		t.Fatalf("delete = %v", err)
	}
	if err := g.Delete(t.Context(), "at", "gone-id"); !errors.Is(err, calendarport.ErrRemoteEventGone) {
		t.Fatalf("delete of a gone event = %v, want ErrRemoteEventGone", err)
	}
}

func TestCalendarSyncGoogleRejectedAccessTokenOnListIsTokenRejected(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodGet, "/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	g, err := NewGoogle(config(Endpoints{API: s.url() + "/calendar/v3"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.List(t.Context(), "expired", calendarport.Window{From: clockAt, To: clockAt.Add(time.Hour)}); !errors.Is(err, calendarport.ErrTokenRejected) {
		t.Fatalf("list error = %v, want ErrTokenRejected", err)
	}
}

func TestCalendarSyncMicrosoftListFollowsPagingOnTheGraphOnly(t *testing.T) {
	s := newStub(t)
	var sawPrefer string
	s.on(http.MethodGet, "/v1.0/me/calendarView", func(w http.ResponseWriter, r *http.Request) {
		sawPrefer = r.Header.Get("Prefer")
		if r.URL.Query().Get("$skiptoken") == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"value": []map[string]any{{"id": "m1", "changeKey": "ck1", "subject": "Ревью", "body": map[string]string{"content": "заметка"},
					"location": map[string]string{"displayName": "Зум"}, "start": map[string]string{"dateTime": "2026-10-26T07:00:00.0000000", "timeZone": "UTC"},
					"end": map[string]string{"dateTime": "2026-10-26T08:00:00.0000000", "timeZone": "UTC"}}},
				"@odata.nextLink": s.url() + "/v1.0/me/calendarView?$skiptoken=next",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"value": []map[string]any{{"id": "m2", "changeKey": "ck2", "subject": "Кофе", "isCancelled": true}}})
	})
	m, err := NewMicrosoft(config(Endpoints{API: s.url() + "/v1.0"}))
	if err != nil {
		t.Fatal(err)
	}
	events, err := m.List(t.Context(), "at", calendarport.Window{From: clockAt.Add(-time.Hour), To: clockAt.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "m1" || events[0].Etag != "ck1" || events[0].Notes != "заметка" {
		t.Fatalf("events = %+v", events)
	}
	if !strings.Contains(sawPrefer, `outlook.timezone="UTC"`) || !strings.Contains(sawPrefer, `outlook.body-content-type="text"`) {
		t.Fatalf("Prefer = %q", sawPrefer)
	}
}

func TestCalendarSyncMicrosoftRefusesPagingLinkOutsideGraph(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodGet, "/v1.0/me/calendarView", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"value": []any{}, "@odata.nextLink": "https://attacker.example/steal"})
	})
	m, err := NewMicrosoft(config(Endpoints{API: s.url() + "/v1.0"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.List(t.Context(), "at", calendarport.Window{From: clockAt, To: clockAt.Add(time.Hour)}); err == nil {
		t.Fatal("a paging link outside the Graph API was followed")
	}
}

func TestCalendarSyncMicrosoftPartialListingFailsWholeList(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodGet, "/v1.0/me/calendarView", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skiptoken") != "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"value": []map[string]any{{"id": "m1"}}, "@odata.nextLink": s.url() + "/v1.0/me/calendarView?$skiptoken=x"})
	})
	m, err := NewMicrosoft(config(Endpoints{API: s.url() + "/v1.0"}))
	if err != nil {
		t.Fatal(err)
	}
	events, err := m.List(t.Context(), "at", calendarport.Window{From: clockAt, To: clockAt.Add(time.Hour)})
	var status *providerstatus.Error
	if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable || events != nil {
		t.Fatalf("events = %v, err = %v; want no events and a 503 status error", events, err)
	}
}

func TestCalendarSyncMicrosoftWritesAndMissingEvents(t *testing.T) {
	s := newStub(t)
	s.on(http.MethodPost, "/v1.0/me/events", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"timeZone":"UTC"`) || !strings.Contains(string(raw), `"contentType":"text"`) {
			t.Errorf("create body = %s", raw)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": "graph-1", "changeKey": "ck-1"})
	})
	s.on(http.MethodPatch, "/v1.0/me/events/graph-1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": "graph-1", "changeKey": "ck-2"})
	})
	s.on(http.MethodPatch, "/v1.0/me/events/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	s.on(http.MethodDelete, "/v1.0/me/events/graph-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m, err := NewMicrosoft(config(Endpoints{API: s.url() + "/v1.0"}))
	if err != nil {
		t.Fatal(err)
	}
	event := calendarport.Event{Title: "Демо", StartsAt: clockAt, EndsAt: clockAt.Add(time.Hour)}
	created, err := m.Create(t.Context(), "at", event)
	if err != nil || created.ID != "graph-1" || created.Etag != "ck-1" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	updated, err := m.Update(t.Context(), "at", "graph-1", event)
	if err != nil || updated.Etag != "ck-2" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if _, err := m.Update(t.Context(), "at", "missing", event); !errors.Is(err, calendarport.ErrRemoteEventGone) {
		t.Fatalf("update of a missing event = %v, want ErrRemoteEventGone", err)
	}
	if err := m.Delete(t.Context(), "at", "graph-1"); err != nil {
		t.Fatalf("delete = %v", err)
	}
}

func TestCalendarSyncAdaptersNeedAnOAuthClient(t *testing.T) {
	cfg := config(Endpoints{})
	cfg.Credentials = calendarport.Credentials{}
	if _, err := NewGoogle(cfg); err == nil {
		t.Fatal("a Google adapter started without an OAuth client")
	}
	if _, err := NewMicrosoft(cfg); err == nil {
		t.Fatal("a Microsoft adapter started without an OAuth client")
	}
}
