package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

const clientColumns = `id, team_id, name, email, details, currency, notes, archived, created_at, updated_at`

func scanClient(r row) (model.Client, error) {
	var (
		c        model.Client
		archived int
		created  string
		updated  string
	)
	if err := r.Scan(&c.ID, &c.TeamID, &c.Name, &c.Email, &c.Details, &c.Currency, &c.Notes, &archived, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Client{}, ErrNotFound
		}
		return model.Client{}, err
	}
	c.Archived = archived != 0
	createdAt, err := ScanTime(created)
	if err != nil {
		return model.Client{}, fmt.Errorf("parse client creation time: %w", err)
	}
	c.CreatedAt = createdAt
	if c.UpdatedAt, err = ScanTime(updated); err != nil {
		return model.Client{}, fmt.Errorf("parse client update time: %w", err)
	}
	return c, nil
}

// checkClientInTeam guards every write that links something to a client. A
// client id arriving from a form is untrusted input: without this check a
// project could be attached to another workspace's client by guessing an id.
func checkClientInTeam(ctx context.Context, tx *Tx, clientID, teamID int64) error {
	if clientID <= 0 || teamID <= 0 {
		return ErrNotFound
	}
	var found int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM clients WHERE id = ? AND team_id = ?`, clientID, teamID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// ListClients returns the client directory for one workspace, archived last
// so the rows still being worked with stay on top.
func (d *DB) ListClients(ctx context.Context, query appmodel.ClientCatalogQuery) ([]model.Client, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	q := `SELECT ` + clientColumns + ` FROM clients WHERE team_id = ?`
	if !query.IncludeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY archived ASC, name`
	rows, err := d.sql.QueryContext(ctx, q, query.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetClientInTeam fetches one client scoped to its workspace.
func (d *DB) GetClientInTeam(ctx context.Context, query appmodel.ClientScopeQuery) (model.Client, error) {
	if query.TeamID <= 0 || query.ClientID <= 0 {
		return model.Client{}, ErrNotFound
	}
	return scanClient(d.sql.QueryRowContext(ctx,
		`SELECT `+clientColumns+` FROM clients WHERE id = ? AND team_id = ?`, query.ClientID, query.TeamID))
}

// GetClientForProject resolves the client card a project points at. It returns
// ErrNotFound when the project has no card, which the invoice flow treats as
// "fall back to the project's own recipient snapshot".
func (d *DB) GetClientForProject(ctx context.Context, query appmodel.ProjectScopeQuery) (model.Client, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return model.Client{}, ErrNotFound
	}
	return scanClient(d.sql.QueryRowContext(ctx,
		`SELECT `+clientColumns+` FROM clients c
		 WHERE c.id = (SELECT client_id FROM projects WHERE id = ? AND team_id = ? AND client_id IS NOT NULL)`, query.ProjectID, query.TeamID))
}

// CreateClient mints a client card. The name is unique per workspace, which
// is what stops the duplicate "Кошелька ООО" cards that the old free-text
// recipient columns invited.
func (d *DB) CreateClient(ctx context.Context, request appmodel.ClientCreateRequest) (model.Client, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Client{}, ErrNotFound
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return model.Client{}, fmt.Errorf("client name cannot be empty")
	}
	if err := validClientCurrency(request.Currency); err != nil {
		return model.Client{}, err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Client{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Client{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO clients (team_id, name, email, details, currency, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		request.TeamID, name, strings.TrimSpace(request.Email), strings.TrimSpace(request.Details),
		request.Currency, strings.TrimSpace(request.Notes), now, now).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return model.Client{}, ErrDuplicate
		}
		return model.Client{}, err
	}
	created, err := scanClient(tx.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM clients WHERE id = ?`, id))
	if err != nil {
		return model.Client{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Client{}, err
	}
	return created, nil
}

// UpdateClient applies the editable client fields in one statement.
func (d *DB) UpdateClient(ctx context.Context, request appmodel.ClientUpdateRequest) (model.Client, error) {
	if request.TeamID <= 0 || request.ClientID <= 0 || request.CallerID <= 0 {
		return model.Client{}, ErrNotFound
	}
	name := strings.TrimSpace(request.Update.Name)
	if name == "" {
		return model.Client{}, fmt.Errorf("client name cannot be empty")
	}
	currency := ""
	if request.Update.Currency != nil {
		currency = *request.Update.Currency
		if err := validClientCurrency(currency); err != nil {
			return model.Client{}, err
		}
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Client{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Client{}, err
	}
	// Lock the row first so a concurrent edit cannot slip between the read
	// and the write below.
	var locked int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM clients WHERE id = ? AND team_id = ? FOR UPDATE`, request.ClientID, request.TeamID).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Client{}, ErrNotFound
		}
		return model.Client{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	query := `UPDATE clients SET name = ?, email = ?, details = ?, notes = ?, updated_at = ?`
	args := []any{name, strings.TrimSpace(request.Update.Email), strings.TrimSpace(request.Update.Details), strings.TrimSpace(request.Update.Notes), now}
	if request.Update.Currency != nil {
		query += `, currency = ?`
		args = append(args, currency)
	}
	if request.Update.Archived != nil {
		query += `, archived = ?`
		args = append(args, boolInt(*request.Update.Archived))
	}
	query += ` WHERE id = ? AND team_id = ?`
	args = append(args, request.ClientID, request.TeamID)
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		if isUniqueViolation(err) {
			return model.Client{}, ErrDuplicate
		}
		return model.Client{}, err
	}
	updated, err := scanClient(tx.QueryRowContext(ctx,
		`SELECT `+clientColumns+` FROM clients WHERE id = ? AND team_id = ?`, request.ClientID, request.TeamID))
	if err != nil {
		return model.Client{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Client{}, err
	}
	return updated, nil
}

func validClientCurrency(currency string) error {
	if currency != "" && (len(currency) != 3 || strings.ToUpper(currency) != currency) {
		return fmt.Errorf("currency must be a 3-letter uppercase code")
	}
	return nil
}

// ProjectBudgetSummaries prices what each project has consumed so far.
//
// "Consumed" means every finished session at the project rate — not only the
// ones still waiting on an invoice — so the ceiling is measured against the
// whole job. Unbilled time is split out because that is the part a manager
// can still put on a document; the split adds up exactly to SpentCents.
//
// The per-second expression and the rounding path mirror db.Unbilled and
// buildLines on purpose: if budget and invoice disagreed by a rounding rule,
// the budget screen would be arguing with the document it is supposed to
// protect.
func (d *DB) ProjectBudgetSummaries(ctx context.Context, query appmodel.ProjectBudgetQuery) (map[int64]appmodel.ProjectBudget, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT p.id, p.budget_cents, COALESCE(NULLIF(p.currency, ''), t.currency, 'RUB'),
		        COALESCE(p.billable_rate_cents, 0), x.total_secs, x.unbilled_secs
		 FROM projects p
		 JOIN teams t ON t.id = p.team_id
		 LEFT JOIN (
		     SELECT a.project_id AS pid,
		            SUM(CASE WHEN s.accumulated_seconds > 0 THEN s.accumulated_seconds
		                     ELSE GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (s.end_at::timestamp - s.start_at::timestamp)))) END) AS total_secs,
		            SUM(CASE WHEN s.invoice_id IS NULL
		                     THEN (CASE WHEN s.accumulated_seconds > 0 THEN s.accumulated_seconds
		                           ELSE GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (s.end_at::timestamp - s.start_at::timestamp)))) END)
		                     ELSE 0 END) AS unbilled_secs
		     FROM sessions s
		     JOIN activities a ON a.id = s.activity_id
		     WHERE s.team_id = ? AND s.end_at IS NOT NULL
		     GROUP BY a.project_id
		 ) x ON x.pid = p.id
		 WHERE p.team_id = ?`,
		query.TeamID, query.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules, err := d.TeamBilling(ctx, query.TeamID)
	if err != nil {
		return nil, fmt.Errorf("load billing rules for project budgets: %w", err)
	}
	out := map[int64]appmodel.ProjectBudget{}
	for rows.Next() {
		var (
			budget       appmodel.ProjectBudget
			budgetCents  sql.NullInt64
			totalSecs    sql.NullInt64
			unbilledSecs sql.NullInt64
			rate         int
		)
		if err := rows.Scan(&budget.ProjectID, &budgetCents, &budget.Currency, &rate, &totalSecs, &unbilledSecs); err != nil {
			return nil, err
		}
		if budgetCents.Valid {
			v := int(budgetCents.Int64)
			budget.BudgetCents = &v
		}
		spent, err := priceProjectSeconds(int(totalSecs.Int64), rate, rules)
		if err != nil {
			return nil, fmt.Errorf("price project %d budget: %w", budget.ProjectID, err)
		}
		unbilled, err := priceProjectSeconds(int(unbilledSecs.Int64), rate, rules)
		if err != nil {
			return nil, fmt.Errorf("price project %d unbilled: %w", budget.ProjectID, err)
		}
		budget.TrackedSeconds = totalSecs.Int64
		budget.SpentCents = spent
		budget.UnbilledCents = unbilled
		budget.BilledCents = spent - unbilled
		if budget.BilledCents < 0 {
			budget.BilledCents = 0
		}
		budget.OverBudget = budget.BudgetCents != nil && budget.SpentCents > *budget.BudgetCents
		if budget.BudgetCents != nil && *budget.BudgetCents > 0 {
			budget.Percent = budget.SpentCents * 100 / *budget.BudgetCents
		}
		out[budget.ProjectID] = budget
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// priceProjectSeconds runs tracked seconds through the workspace's billing
// rounding rules and the project rate, exactly as an invoice line does.
func priceProjectSeconds(seconds, rate int, rules BillingRules) (int, error) {
	if seconds <= 0 {
		return 0, nil
	}
	amount, err := PriceCents(RoundBilled(seconds, rules), rate)
	if err != nil {
		return 0, err
	}
	return money.AddCents(0, amount)
}
