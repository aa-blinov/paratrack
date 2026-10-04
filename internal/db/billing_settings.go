package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// BillingRules is an alias to the shared team preference model.
type BillingRules = model.BillingRules

func (d *DB) TeamBilling(ctx context.Context, teamID int64) (BillingRules, error) {
	return billingRules(ctx, d.sql, teamID)
}

type invoiceQueryer interface {
	queryRower
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func billingRules(ctx context.Context, queryer invoiceQueryer, teamID int64) (BillingRules, error) {
	var b BillingRules
	err := queryer.QueryRowContext(ctx, `SELECT round_minutes, round_mode, invoice_prefix, logo FROM teams WHERE id = ?`, teamID).
		Scan(&b.RoundMinutes, &b.RoundMode, &b.InvoicePrefix, &b.Logo)
	if errors.Is(err, sql.ErrNoRows) {
		return BillingRules{}, model.ErrNotFound
	}
	if b.InvoicePrefix == "" {
		b.InvoicePrefix = "INV"
	}
	return b, err
}

func (d *DB) SetTeamBilling(ctx context.Context, request appmodel.TeamBillingRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET round_minutes = ?, round_mode = ?, invoice_prefix = ? WHERE id = ?`,
		request.Rules.RoundMinutes, request.Rules.RoundMode, request.Rules.InvoicePrefix, request.TeamID)
}

func (d *DB) SetTeamLogo(ctx context.Context, request appmodel.TeamLogoRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET logo = ? WHERE id = ?`, request.DataURL, request.TeamID)
}

// RoundBilled applies a workspace rounding rule to a line's seconds.
func RoundBilled(sec int, b BillingRules) int {
	if b.RoundMinutes <= 0 || sec <= 0 {
		return sec
	}
	maxInt := int(^uint(0) >> 1)
	if b.RoundMinutes > maxInt/60 {
		return sec
	}
	step := b.RoundMinutes * 60
	quotient, remainder := sec/step, sec%step
	if b.RoundMode == "up" {
		if remainder != 0 {
			quotient++
		}
	} else if remainder >= (step+1)/2 {
		quotient++
	}
	if quotient > maxInt/step {
		return sec
	}
	return quotient * step
}

// TeamModules is the workspace's section list ("" = everything).
func (d *DB) TeamModules(ctx context.Context, teamID int64) (string, error) {
	var m string
	err := d.sql.QueryRowContext(ctx, `SELECT modules FROM teams WHERE id = ?`, teamID).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return "", model.ErrNotFound
	}
	return m, err
}

func (d *DB) SetTeamModules(ctx context.Context, request appmodel.TeamModulesRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET modules = ? WHERE id = ?`, appmodel.EncodeSections(request.Selected), request.TeamID)
}

// TeamRequisites are the issuer's details and VAT line for documents.
func (d *DB) TeamRequisites(ctx context.Context, teamID int64) (requisites, vatNote string, err error) {
	err = d.sql.QueryRowContext(ctx, `SELECT requisites, vat_note FROM teams WHERE id = ?`, teamID).Scan(&requisites, &vatNote)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", model.ErrNotFound
	}
	return
}

func (d *DB) SetTeamRequisites(ctx context.Context, request appmodel.TeamRequisitesRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET requisites = ?, vat_note = ? WHERE id = ?`, request.Requisites, request.VATNote, request.TeamID)
}

// TeamCurrency is the workspace default (RUB when unset or no team).
func (d *DB) TeamCurrency(ctx context.Context, teamID int64) (string, error) {
	var cur string
	err := d.sql.QueryRowContext(ctx, `SELECT currency FROM teams WHERE id = ?`, teamID).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return "RUB", nil
	}
	if err != nil {
		return "", err
	}
	if cur == "" {
		return "RUB", nil
	}
	return cur, nil
}

// SetTeamCurrency changes the default for new projects and documents;
// existing invoices and pay runs keep the currency they were made in.
func (d *DB) SetTeamCurrency(ctx context.Context, request appmodel.TeamCurrencyRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET currency = ? WHERE id = ?`, request.Currency, request.TeamID)
}

// SetProjectRate updates the hourly rate and billable flag under the
// workspace lock. Nil fields preserve their current values.
func (d *DB) SetProjectRate(ctx context.Context, request appmodel.ProjectRateRequest) error {
	if request.TeamID <= 0 || request.ProjectID <= 0 || request.CallerID <= 0 || (request.RateCents == nil && request.Billable == nil) {
		return model.ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	query := `UPDATE projects SET updated_at = ?`
	args := []any{FormatTime(d.currentTime().UTC())}
	if request.RateCents != nil {
		if *request.RateCents < 0 {
			return fmt.Errorf("rate must be >= 0")
		}
		query += `, billable_rate_cents = ?`
		args = append(args, *request.RateCents)
	}
	if request.Billable != nil {
		query += `, billable = ?`
		if *request.Billable {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	query += ` WHERE id = ? AND team_id = ?`
	args = append(args, request.ProjectID, request.TeamID)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return model.ErrNotFound
	}
	return tx.Commit()
}

// ProjectClient is an alias to the project invoice defaults model.
type ProjectClient = model.ProjectClient

func (d *DB) GetProjectClient(ctx context.Context, teamID, projectID int64) (ProjectClient, error) {
	var c ProjectClient
	err := d.sql.QueryRowContext(ctx, `SELECT client_name, client_details, client_email FROM projects WHERE id = ? AND team_id = ?`,
		projectID, teamID).Scan(&c.Name, &c.Details, &c.Email)
	return c, err
}

// ListProjectClients loads invoice defaults for a team's non-archived
// projects in one query, for invoice-form rendering.
func (d *DB) ListProjectClients(ctx context.Context, teamID int64) (map[int64]model.ProjectClient, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, client_name, client_details, client_email FROM projects WHERE team_id = ? AND archived = 0`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clients := make(map[int64]model.ProjectClient)
	for rows.Next() {
		var id int64
		var client model.ProjectClient
		if err := rows.Scan(&id, &client.Name, &client.Details, &client.Email); err != nil {
			return nil, err
		}
		clients[id] = client
	}
	return clients, rows.Err()
}

// ProjectCurrency returns the project currency, or an empty value when it inherits.
func (d *DB) ProjectCurrency(ctx context.Context, query appmodel.ProjectScopeQuery) (string, error) {
	var cur string
	err := d.sql.QueryRowContext(ctx, `SELECT currency FROM projects WHERE id = ? AND team_id = ?`, query.ProjectID, query.TeamID).Scan(&cur)
	return cur, err
}

// TeamStripe returns the team's Stripe credentials (empty when unset).
func (d *DB) TeamStripe(ctx context.Context, teamID int64) (key, webhookSecret string, err error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(stripe_key, ''), COALESCE(stripe_webhook_secret, '') FROM teams WHERE id = ?`, teamID)
	if err = row.Scan(&key, &webhookSecret); err != nil {
		return
	}
	return d.mustOpen(key), d.mustOpen(webhookSecret), nil
}

// SetTeamStripe stores Stripe credentials for the workspace.
func (d *DB) SetTeamStripe(ctx context.Context, request appmodel.TeamStripeCredentialsRequest) error {
	sealedKey, err := d.sealSecret(request.Key)
	if err != nil {
		return fmt.Errorf("seal Stripe key: %w", err)
	}
	sealedWebhookSecret, err := d.sealSecret(request.Secret)
	if err != nil {
		return fmt.Errorf("seal Stripe webhook secret: %w", err)
	}
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID,
		`UPDATE teams SET stripe_key = ?, stripe_webhook_secret = ? WHERE id = ?`,
		sealedKey, sealedWebhookSecret, request.TeamID)
}
