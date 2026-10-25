package db

import (
	"context"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// clientTagNames is the client's tags as one comma-separated string. A tag never holds a
// comma (model.NormalizeClientTags), so the split in the caller is exact.
const clientTagNames = `COALESCE((SELECT string_agg(t.name, ',' ORDER BY t.name) FROM client_tags t WHERE t.team_id = c.team_id AND t.client_id = c.id), '')`

// SearchWorkspace finds the matches of one text in a workspace (UI-01, CRM-18): projects,
// clients, contacts, tasks and tags by name, people by name or, for managers, email, and
// the viewer's own sessions by the activity they were logged to. A client also matches by
// its tags; for managers, by its billing email and details. A contact matches by name,
// role, email or phone, and a phone also matches with spaces, dashes and brackets left out
// of the text or the number. Archived projects, clients and tasks are left out, and so are
// the contacts of an archived client. Query.Tag keeps only the clients that carry the tag,
// and their contacts.
func (d *DB) SearchWorkspace(ctx context.Context, query appmodel.SearchQuery) (appmodel.SearchResults, error) {
	if query.TeamID <= 0 || query.UserID <= 0 {
		return appmodel.SearchResults{}, ErrNotFound
	}
	role, err := readTeamRole(ctx, d.sql, query.TeamID, query.UserID)
	if err != nil {
		return appmodel.SearchResults{}, err
	}
	manager := role.CanManage()
	pattern := "%" + likeEscape(query.Text) + "%"
	tag := strings.ToLower(strings.TrimSpace(query.Tag))
	var out appmodel.SearchResults
	if out.Projects, err = searchHits(ctx, d, `SELECT id, name, slug FROM projects WHERE team_id = ? AND archived = 0 AND name ILIKE ? ORDER BY name LIMIT ?`,
		[]any{query.TeamID, pattern, query.Limit}, func(h *appmodel.SearchHit, scan func(...any) error) error {
			return scan(&h.ID, &h.Title, &h.Slug)
		}); err != nil {
		return out, err
	}

	clientWhere := `c.team_id = ? AND c.archived = 0 AND (c.name ILIKE ? OR EXISTS (SELECT 1 FROM client_tags t WHERE t.team_id = c.team_id AND t.client_id = c.id AND t.name ILIKE ?))`
	clientArgs := []any{query.TeamID, pattern, pattern}
	if manager {
		clientWhere = `c.team_id = ? AND c.archived = 0 AND (c.name ILIKE ? OR c.email ILIKE ? OR c.details ILIKE ? OR EXISTS (SELECT 1 FROM client_tags t WHERE t.team_id = c.team_id AND t.client_id = c.id AND t.name ILIKE ?))`
		clientArgs = []any{query.TeamID, pattern, pattern, pattern, pattern}
	}
	if tag != "" {
		clientWhere += ` AND EXISTS (SELECT 1 FROM client_tags t WHERE t.team_id = c.team_id AND t.client_id = c.id AND t.name = ?)`
		clientArgs = append(clientArgs, tag)
	}
	if out.Clients, err = searchHits(ctx, d, `SELECT c.id, c.name, c.email, `+clientTagNames+` FROM clients c WHERE `+clientWhere+` ORDER BY c.name LIMIT ?`,
		append(clientArgs, query.Limit), func(h *appmodel.SearchHit, scan func(...any) error) error {
			var email, tags string
			if err := scan(&h.ID, &h.Title, &email, &tags); err != nil {
				return err
			}
			if manager {
				h.Detail = email
			}
			if tags != "" {
				h.Tags = strings.Split(tags, ",")
			}
			return nil
		}); err != nil {
		return out, err
	}

	if query.CRM {
		// The text with the separators a phone is written with left out. An empty result
		// here would match every number, so the digit match is off for such a text.
		digits := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(query.Text)
		contactWhere := `k.team_id = ? AND cl.archived = 0 AND (k.name ILIKE ? OR k.role ILIKE ? OR k.email ILIKE ? OR k.phone ILIKE ? OR (CAST(? AS TEXT) <> '' AND regexp_replace(k.phone, '[ ()-]', '', 'g') ILIKE ?))`
		contactArgs := []any{query.TeamID, pattern, pattern, pattern, pattern, digits, "%" + likeEscape(digits) + "%"}
		if tag != "" {
			contactWhere += ` AND EXISTS (SELECT 1 FROM client_tags t WHERE t.team_id = k.team_id AND t.client_id = k.client_id AND t.name = ?)`
			contactArgs = append(contactArgs, tag)
		}
		if out.Contacts, err = searchRows(ctx, d, `SELECT k.id, k.client_id, cl.name, k.name, k.role, k.email, k.phone
			FROM crm_contacts k JOIN clients cl ON cl.id = k.client_id AND cl.team_id = k.team_id
			WHERE `+contactWhere+` ORDER BY k.name, k.id LIMIT ?`,
			append(contactArgs, query.Limit), func(scan func(...any) error) (appmodel.SearchContact, error) {
				var c appmodel.SearchContact
				err := scan(&c.ID, &c.ClientID, &c.ClientName, &c.Name, &c.Role, &c.Email, &c.Phone)
				return c, err
			}); err != nil {
			return out, err
		}
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
	if manager {
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
	return searchRows(ctx, d, query, args, func(scan func(...any) error) (appmodel.SearchHit, error) {
		var hit appmodel.SearchHit
		err := fill(&hit, scan)
		return hit, err
	})
}

// searchRows runs one search query and maps each row with fill.
func searchRows[T any](ctx context.Context, d *DB, query string, args []any, fill func(func(...any) error) (T, error)) ([]T, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		item, err := fill(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// likeEscape keeps what the person typed literal: % and _ match themselves.
func likeEscape(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}
