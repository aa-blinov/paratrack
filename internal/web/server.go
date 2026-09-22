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

	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Server is the HTTP front-end for paratrack.
type Server struct {
	db    *db.DB
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
	s := &Server{db: database, addr: addr, tmpl: tmpl}
	s.httpd = &http.Server{
		Addr:              addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
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
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("GET /{$}",          s.handleDashboard)
	mux.HandleFunc("GET /stats",        s.handleStats)
	mux.HandleFunc("GET /graph",        s.handleGraph)
	mux.HandleFunc("GET /goals",        s.handleGoals)

	// JSON / fragments
	mux.HandleFunc("GET /api/active",   s.handleAPIActive)
	mux.HandleFunc("GET /api/reports.csv", s.handleCSV)

	// Actions
	mux.HandleFunc("POST /api/start",                      s.handleStart)
	mux.HandleFunc("POST /api/sessions/{id}/stop",         s.handleStop)
	mux.HandleFunc("POST /api/sessions/{id}/pause",        s.handlePause)
	mux.HandleFunc("POST /api/sessions/{id}/resume",       s.handleResume)
	mux.HandleFunc("POST /api/focus/{name}",               s.handleFocus)
	mux.HandleFunc("PATCH /api/sessions/{id}",             s.handleUpdateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}",            s.handleDeleteSession)

	// Goals — read on dashboard, full CRUD via JSON.
	mux.HandleFunc("GET  /api/goals",          s.handleGoalsList)
	mux.HandleFunc("GET  /api/goals/progress", s.handleGoalsProgress)
	mux.HandleFunc("POST /api/goals",          s.handleGoalsUpsert)
	mux.HandleFunc("DELETE /api/goals",        s.handleGoalsDelete)

	// Static files — served from the embedded FS, mounted at /static/.
	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

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
