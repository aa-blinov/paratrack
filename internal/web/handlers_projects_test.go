package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/db"
)

// --- helper ---------------------------------------------------------
//
// newProjectTestServer wires up a Server on an httptest listener with
// the real mux, seeds a user + auth_session + team + membership, and
// returns everything the caller needs to drive authenticated
// /api/projects requests.

type projectTestEnv struct {
	srv    *Server
	ts     *httptest.Server
	t      *testing.T
	cookie string
	teamID int64
}

func newProjectTestEnv(t *testing.T) *projectTestEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open :memory: db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	srv, err := New(d, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	ctx := context.Background()

	ures, err := srv.db.SQL().ExecContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?)`,
		"projects@test.local", "x", "Projects Tester")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	uid, _ := ures.LastInsertId()

	const tok = "test-token-projects"
	if _, err := srv.db.SQL().ExecContext(ctx,
		`INSERT INTO auth_sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		tok, uid, "2099-01-01T00:00:00Z"); err != nil {
		t.Fatalf("seed auth_session: %v", err)
	}

	tres, err := srv.db.SQL().ExecContext(ctx,
		`INSERT INTO teams (slug, name, owner_id) VALUES (?, ?, ?)`,
		"projects-team", "Projects Team", uid)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}
	tid, _ := tres.LastInsertId()

	if _, err := srv.db.SQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role) VALUES (?, ?, 'owner')`,
		tid, uid); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	// Pre-select this team as the active one so middleware picks it up.
	if _, err := srv.db.SQL().ExecContext(ctx,
		`UPDATE users SET name = name WHERE id = ?`, uid); err != nil {
		// no-op; just to make the variable "used" if we extend later
	}
	_ = srv

	return &projectTestEnv{srv: srv, ts: ts, t: t, cookie: tok, teamID: tid}
}

func (e *projectTestEnv) do(method, path string, body []byte, ct string) *httptest.ResponseRecorder {
	var br *bytes.Buffer
	if body != nil {
		br = bytes.NewBuffer(body)
	} else {
		br = &bytes.Buffer{}
	}
	r := httptest.NewRequest(method, path, br)
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	r.AddCookie(&http.Cookie{Name: "paratrack_session", Value: e.cookie})
	// Team cookie so the middleware picks our seeded team.
	r.AddCookie(&http.Cookie{Name: "paratrack_team", Value: fmt.Sprintf("%d", e.teamID)})
	// Double-submit CSRF (cookie + header).
	csrfTok, csrfCk := seedCSRF(e.t, e.srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	w := httptest.NewRecorder()
	e.srv.routes().ServeHTTP(w, r)
	return w
}

// --- tests ----------------------------------------------------------

func TestAPIProjects_FullCRUD(t *testing.T) {
	e := newProjectTestEnv(t)

	// Create.
	w := e.do("POST", "/api/projects",
		[]byte(`{"name":"EORA RAG","color":"#7c3aed"}`), "application/json")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: code=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "EORA RAG" || created.Slug != "eora-rag" {
		t.Errorf("expected slug eora-rag / name EORA RAG, got %q / %q", created.Slug, created.Name)
	}

	// List.
	w = e.do("GET", "/api/projects", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "EORA RAG") {
		t.Errorf("list response missing the project: %s", w.Body.String())
	}

	// Update.
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", created.ID),
		[]byte(`{"color":"#06b6d4"}`), "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("update: code=%d body=%s", w.Code, w.Body.String())
	}

	// Delete.
	w = e.do("DELETE", fmt.Sprintf("/api/projects/%d", created.ID), nil, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: code=%d body=%s", w.Code, w.Body.String())
	}

	// List again — empty.
	w = e.do("GET", "/api/projects", nil, "")
	if strings.Contains(w.Body.String(), "EORA RAG") {
		t.Errorf("project should be gone: %s", w.Body.String())
	}
}

func TestAPIProjects_DuplicateReturns409(t *testing.T) {
	e := newProjectTestEnv(t)
	if w := e.do("POST", "/api/projects",
		[]byte(`{"name":"A"}`), "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("first create: code=%d body=%s", w.Code, w.Body.String())
	}
	w := e.do("POST", "/api/projects",
		[]byte(`{"name":"A"}`), "application/json")
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate should be 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIProjects_RejectsBadColor(t *testing.T) {
	e := newProjectTestEnv(t)
	w := e.do("POST", "/api/projects",
		[]byte(`{"name":"X","color":"red"}`), "application/json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad color should be 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIProjects_UnauthenticatedRejected(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	srv, err := New(d, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	r := httptest.NewRequest("GET", "/api/projects", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusUnauthorized {
		t.Errorf("expected redirect/unauth without session, got %d", w.Code)
	}
}
