// Package web serves paratrack's embedded HTTP UI.
//
// All static assets and templates live under the project-root web/
// directory and are compiled into the binary via go:embed — the result
// is a single executable that opens a complete time-tracker at the
// configured address.
package web

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teams"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Server is the HTTP front-end for paratrack.
type Server struct {
	db    *db.DB
	auth  *auth.Service
	teams *teams.Service
	addr  string
	tmpl  *template.Template
	httpd *http.Server
}

// New constructs a Server but does not yet bind the socket.
func New(database *db.DB, addr string) (*Server, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	s := &Server{
		db:    database,
		auth:  auth.NewService(database),
		teams: teams.NewService(database),
		addr:  addr,
		tmpl:  tmpl,
	}
	s.httpd = &http.Server{
		Addr:              addr,
		Handler:           s.routes(),
		ReadHeaderTimeout:  5 * time.Second,
	}
	return s, nil
}

// ListenAndServe blocks until the server stops.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	log.Printf("paratrack web: listening on http://%s", ln.Addr())
	if err := s.httpd.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error { return s.httpd.Shutdown(ctx) }

// parseTemplates parses every *.html under templates/. Files define
// blocks ("base", "content") so child pages compose into the shared
// shell automatically.
func parseTemplates() (*template.Template, error) {
	root := template.New("").Funcs(funcMap)
	matches, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no templates found")
	}
	for _, name := range matches {
		b, err := assets.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if _, err := root.New(strings.TrimPrefix(name, "templates/")).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return root, nil
}

var funcMap = template.FuncMap{
	"fmtDuration": fmtDurationHuman,
}

// routes wires every HTTP route the server exposes. Go 1.22+ pattern
// routing means we don't pull in a third-party router.
//
// Phase 1 wire-up splits the routes into three buckets:
//
//   - Public — anyone can hit (login form, registration form, logout,
//     static files).
//   - Auth page — page routes redirect unauthenticated visitors to
//     /login so the browser can flow through the form normally.
//   - Auth API — every /api/* call (other than the auth flow itself)
//     returns a JSON body on auth failure, so HTMX / curl / Playwright
//     all behave sensibly.
//
// The middleware sets User and Team on r.Context() before dispatching
// to the actual handler.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	pageAuth := s.requireAuth(pageRedirect)
	apiAuth := s.requireAuth(apiUnauthorized)

	// ----- public -----
	mux.HandleFunc("GET /login",         s.handleLogin)
	mux.HandleFunc("GET /register",      s.handleRegister)
	mux.HandleFunc("POST /api/login",    s.handleAPILogin)
	mux.HandleFunc("POST /api/register", s.handleAPIRegister)
	mux.HandleFunc("POST /api/logout",   s.handleAPILogout)

	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// ----- protected pages -----
	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}",                    s.handleDashboard)
	pages.HandleFunc("GET /stats",                  s.handleStats)
	pages.HandleFunc("GET /graph",                  s.handleGraph)
	pages.HandleFunc("GET /goals",                  s.handleGoals)
	pages.HandleFunc("GET /tags",                   s.handleTagsPage)
	pages.HandleFunc("GET /tags-list-fragment",     s.handleTagsFragment)
	mux.Handle("/", pageAuth(pages))

	// ----- protected /api/* -----
	api := http.NewServeMux()
	api.HandleFunc("GET /active",                   s.handleAPIActive)
	api.HandleFunc("GET /reports.csv",              s.handleCSV)
	api.HandleFunc("POST /start",                   s.handleStart)
	api.HandleFunc("POST /sessions/{id}/stop",      s.handleStop)
	api.HandleFunc("POST /sessions/{id}/pause",     s.handlePause)
	api.HandleFunc("POST /sessions/{id}/resume",    s.handleResume)
	api.HandleFunc("POST /focus/{name}",            s.handleFocus)
	api.HandleFunc("PATCH /sessions/{id}",          s.handleUpdateSession)
	api.HandleFunc("DELETE /sessions/{id}",         s.handleDeleteSession)
	api.HandleFunc("GET /goals",                    s.handleGoalsList)
	api.HandleFunc("GET /goals/progress",           s.handleGoalsProgress)
	api.HandleFunc("POST /goals",                   s.handleGoalsUpsert)
	api.HandleFunc("DELETE /goals",                 s.handleGoalsDelete)
	api.HandleFunc("GET /tags",                     s.handleTagsList)
	api.HandleFunc("POST /tags",                    s.handleTagsCreate)
	api.HandleFunc("DELETE /tags",                  s.handleTagsDelete)
	api.HandleFunc("POST /sessions/{id}/tags",      s.handleSessionTagAdd)
	api.HandleFunc("DELETE /sessions/{id}/tags",   s.handleSessionTagRemove)
	mux.Handle("/api/", apiAuth(api))

	return logRequests(mux)
}

// logRequests is a tiny middleware: prints method, path, status, latency.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func fmtDurationHuman(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec / 60) % 60
	return fmt.Sprintf("%dh %02dm", h, m)
}

// DB access helpers (re-export so handlers don't repeat the import).
type sqlQuerier = sql.DB // alias for documentation only
var _ = model.Session{}
var _ = timeparse.Period{}