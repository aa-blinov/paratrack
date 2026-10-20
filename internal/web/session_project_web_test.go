package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A project correction sent by the session editor changes that session's project,
// and the audit row for it carries the client address of the request.
func TestSessionEditorChangesOneSessionsProjectAndAuditsTheClient(t *testing.T) {
	e := newAPIEnv(t)
	e.register("session-project-web@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Вторая"}}, nil))
	ctx := context.Background()
	var project int64
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT id FROM projects WHERE name = 'Вторая'`).Scan(&project); err != nil {
		t.Fatalf("find the project: %v", err)
	}

	readBody(t, e.do("POST", "/api/start", url.Values{"activity": {"editor"}}, map[string]string{"HX-Request": "true"}))
	ids := activeSessionIDs(t, e)
	if len(ids) == 0 {
		t.Fatal("no running session after start")
	}
	id := strconv.FormatInt(ids[0], 10)
	readBody(t, e.do("POST", "/api/sessions/"+id+"/stop", nil, nil))

	start := ""
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT start_at FROM sessions WHERE id = ?`, ids[0]).Scan(&start); err != nil {
		t.Fatalf("read the session: %v", err)
	}
	resp := e.do("PATCH", "/api/sessions/"+id, url.Values{
		"start_at": {""}, "end_at": {""}, "duration": {""}, "note": {""},
		"project_id": {strconv.FormatInt(project, 10)},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("correct the project: %d %s", resp.StatusCode, readBody(t, resp))
	}

	var stored int64
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT COALESCE(project_id, 0) FROM sessions WHERE id = ?`, ids[0]).Scan(&stored); err != nil {
		t.Fatalf("read the session project: %v", err)
	}
	if stored != project {
		t.Fatalf("the session has project %d, want %d", stored, project)
	}

	var meta, ip string
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT meta, ip FROM audit_log WHERE action = 'session.edit' AND target = ? ORDER BY id DESC LIMIT 1`,
		"session:"+id).Scan(&meta, &ip); err != nil {
		t.Fatalf("read the audit row: %v", err)
	}
	if !strings.HasPrefix(meta, "project_id: 0 -> ") || ip == "" {
		t.Fatalf("the audit row is %q from %q, want a project change from a client address", meta, ip)
	}
}
