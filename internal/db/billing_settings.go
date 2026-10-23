package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"

	"github.com/aa-blinov/paratrack/internal/money"
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
	return d.execOwnerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET round_minutes = ?, round_mode = ?, invoice_prefix = ? WHERE id = ?`,
		request.Rules.RoundMinutes, request.Rules.RoundMode, request.Rules.InvoicePrefix, request.TeamID)
}

func (d *DB) SetTeamLogo(ctx context.Context, request appmodel.TeamLogoRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET logo = ? WHERE id = ?`, request.DataURL, request.TeamID)
}

// RoundBilled applies a workspace rounding rule to a line's seconds.
func RoundBilled(sec int, b BillingRules) int {
	return money.RoundBilledSeconds(sec, b.RoundMinutes, b.RoundMode)
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

// SetTeamModules saves the section list and marks it chosen: from now on the
// workspace opens its menu rather than the /welcome choice.
func (d *DB) SetTeamModules(ctx context.Context, request appmodel.TeamModulesRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET modules = ?, sections_chosen = 1 WHERE id = ?`, appmodel.EncodeSections(request.Selected), request.TeamID)
}

// TeamSectionsChosen reports whether the workspace has picked its sections.
func (d *DB) TeamSectionsChosen(ctx context.Context, teamID int64) (bool, error) {
	var chosen int64
	err := d.sql.QueryRowContext(ctx, `SELECT sections_chosen FROM teams WHERE id = ?`, teamID).Scan(&chosen)
	if errors.Is(err, sql.ErrNoRows) {
		return false, model.ErrNotFound
	}
	return chosen != 0, err
}

// TeamRequisites are the issuer's details and VAT line for documents.
func (d *DB) TeamRequisites(ctx context.Context, teamID int64) (requisites, vatNote string, hideExecutor bool, err error) {
	var hide int64
	err = d.sql.QueryRowContext(ctx, `SELECT requisites, vat_note, hide_executor_names FROM teams WHERE id = ?`, teamID).Scan(&requisites, &vatNote, &hide)
	hideExecutor = hide != 0
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, model.ErrNotFound
	}
	return
}

func (d *DB) SetTeamRequisites(ctx context.Context, request appmodel.TeamRequisitesRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET requisites = ?, vat_note = ?, hide_executor_names = ? WHERE id = ?`, request.Requisites, request.VATNote, boolInt(request.HideExecutorNames), request.TeamID)
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
	return d.execOwnerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET currency = ? WHERE id = ?`, request.Currency, request.TeamID)
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

// ListProjectClients loads invoice defaults for a team's non-archived
// projects in one query, for invoice-form rendering.
//
// A linked client card outranks the project's own snapshot columns: that
// snapshot predates cards and stays empty on every project a person attached
// a customer to afterwards. Reading only the snapshot produced an invoice with
// no recipient address — a mailto with no addressee and a PDF with no VAT
// number, which is exactly the document the customer is supposed to trust.
func (d *DB) ListProjectClients(ctx context.Context, teamID int64) (map[int64]model.ProjectClient, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT p.id,
		        COALESCE(NULLIF(cl.name, ''), p.client_name),
		        COALESCE(NULLIF(cl.details, ''), p.client_details),
		        COALESCE(NULLIF(cl.email, ''), p.client_email)
		 FROM projects p
		 LEFT JOIN clients cl ON cl.id = p.client_id AND cl.team_id = p.team_id
		 WHERE p.team_id = ? AND p.archived = 0`, teamID)
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

// ProjectCurrency returns the project currency, or an empty value when it
// inherits. A client card outranks the project's own column, matching the
// rule the invoice itself uses.
func (d *DB) ProjectCurrency(ctx context.Context, query appmodel.ProjectScopeQuery) (string, error) {
	var cur string
	err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(NULLIF(cl.currency, ''), NULLIF(p.currency, ''), '')
		 FROM projects p
		 LEFT JOIN clients cl ON cl.id = p.client_id AND cl.team_id = p.team_id
		 WHERE p.id = ? AND p.team_id = ?`, query.ProjectID, query.TeamID).Scan(&cur)
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
	if request.Clear {
		return d.execOwnerTeamUpdate(ctx, request.TeamID, request.CallerID,
			`UPDATE teams SET stripe_key = '', stripe_webhook_secret = '' WHERE id = ?`, request.TeamID)
	}
	sealedKey, err := d.sealSecret(request.Key)
	if err != nil {
		return fmt.Errorf("seal Stripe key: %w", err)
	}
	sealedWebhookSecret, err := d.sealSecret(request.Secret)
	if err != nil {
		return fmt.Errorf("seal Stripe webhook secret: %w", err)
	}
	// A blank field keeps what is stored: saving only the key used to wipe the
	// webhook secret, and an empty form wiped both while saying «Сохранено».
	return d.execOwnerTeamUpdate(ctx, request.TeamID, request.CallerID,
		`UPDATE teams SET
		   stripe_key = CASE WHEN ? = '' THEN stripe_key ELSE ? END,
		   stripe_webhook_secret = CASE WHEN ? = '' THEN stripe_webhook_secret ELSE ? END
		 WHERE id = ?`,
		request.Key, sealedKey, request.Secret, sealedWebhookSecret, request.TeamID)
}
