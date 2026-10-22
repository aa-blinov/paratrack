package db

import (
	"context"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// SearchWorkspace finds the matches of one text in a workspace (UI-01): projects,
// clients, tasks and tags by name, people by name or, for managers, email, and the
// viewer's own sessions by the activity they were logged to. Archived projects,
// clients and tasks are left out.
func (d *DB) SearchWorkspace(ctx context.Context, query appmodel.SearchQuery) (appmodel.SearchResults, error) {
	if query.TeamID <= 0 || query.UserID <= 0 {
		return appmodel.SearchResults{}, ErrNotFound
	}
	pattern := "%" + likeEscape(query.Text) + "%"
	var out appmodel.SearchResults
	var err error
	if out.Projects, err = searchHits(ctx, d, `SELECT id, name, slug FROM projects WHERE team_id = ? AND archived = 0 AND name ILIKE ? ORDER BY name LIMIT ?`,
		[]any{query.TeamID, pattern, query.Limit}, func(h *appmodel.SearchHit, scan func(...any) error) error {
			return scan(&h.ID, &h.Title, &h.Slug)
		}); err != nil {
		return out, err
	}
	clientSQL := `SELECT id, name, email FROM clients WHERE team_id = ? AND archived = 0 AND name ILIKE ? ORDER BY name LIMIT ?`
	if query.Manager {
		clientSQL = `SELECT id, name, email FROM clients WHERE team_id = ? AND archived = 0 AND (name ILIKE ? OR email ILIKE ?) ORDER BY name LIMIT ?`
	}
	clientArgs := []any{query.TeamID, pattern, query.Limit}
	if query.Manager {
		clientArgs = []any{query.TeamID, pattern, pattern, query.Limit}
	}
	if out.Clients, err = searchHits(ctx, d, clientSQL, clientArgs, func(h *appmodel.SearchHit, scan func(...any) error) error {
		var email string
		if err := scan(&h.ID, &h.Title, &email); err != nil {
			return err
		}
		if query.Manager {
			h.Detail = email
		}
		return nil
	}); err != nil {
		return out, err
	}
	if out.Tasks, err = searchHits(ctx, d, `SELECT t.id, t.title, COALESCE(p.name, '') FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.team_id = ? AND t.archived = 0 AND t.title ILIKE ? ORDER BY t.id DESC LIMIT ?`,
		[]any{query.TeamID, pattern, query.Limit}, func(h *appmodel.SearchHit, scan func(...any) error) error {
			return scan(&h.ID, &h.Title, &h.Detail)
		}); err != nil {
		return out, err
	}
	if out.Tags, err = searchHits(ctx, d, `SELECT id, name FROM tags WHERE team_id = ? AND name ILIKE ? ORDER BY name LIMIT ?`,
		[]any{query.TeamID, pattern, query.Limit}, func(h *appmodel.SearchHit, scan func(...any) error) error {
			return scan(&h.ID, &h.Title)
		}); err != nil {
		return out, err
	}
	if query.Manager {
		if out.People, err = searchHits(ctx, d, `SELECT u.id, u.name, u.email FROM memberships m JOIN users u ON u.id = m.user_id
			WHERE m.team_id = ? AND (u.name ILIKE ? OR u.email ILIKE ?) ORDER BY u.name LIMIT ?`,
			[]any{query.TeamID, pattern, pattern, query.Limit}, func(h *appmodel.SearchHit, scan func(...any) error) error {
				return scan(&h.ID, &h.Title, &h.Detail)
			}); err != nil {
			return out, err
		}
	}
	if out.Sessions, err = searchHits(ctx, d, `SELECT s.id, a.name, s.start_at FROM sessions s JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		WHERE s.team_id = ? AND s.user_id = ? AND a.name ILIKE ? ORDER BY s.start_at DESC LIMIT ?`,
		[]any{query.TeamID, query.UserID, pattern, query.Limit}, func(h *appmodel.SearchHit, scan func(...any) error) error {
			var startAt string
			if err := scan(&h.ID, &h.Title, &startAt); err != nil {
				return err
			}
			at, err := ScanTime(startAt)
			h.At = at
			return err
		}); err != nil {
		return out, err
	}
	return out, nil
}

// searchHits runs one search query and maps each row to a hit.
func searchHits(ctx context.Context, d *DB, query string, args []any, fill func(*appmodel.SearchHit, func(...any) error) error) ([]appmodel.SearchHit, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.SearchHit
	for rows.Next() {
		var hit appmodel.SearchHit
		if err := fill(&hit, rows.Scan); err != nil {
			return nil, err
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

// likeEscape keeps what the person typed literal: % and _ match themselves.
func likeEscape(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}
