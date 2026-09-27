package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Wave 3: billable rates + invoices
// ---------------------------------------------------------------------------

// Invoice is a client bill generated from tracked time.
type Invoice struct {
	ID          int64     `json:"id"`
	TeamID      int64     `json:"team_id"`
	Number      string    `json:"number"`
	ClientName  string    `json:"client_name"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Status      string    `json:"status"` // draft | sent | paid
	Notes       string    `json:"notes"`
	PaymentURL  string    `json:"payment_url"`
	Currency    string    `json:"currency"` // ISO 4217, fixed at creation
	// Snapshot at creation: later edits to the workspace don't rewrite an
	// issued document.
	SellerDetails string    `json:"seller_details"`
	ClientDetails string    `json:"client_details"`
	VATNote       string    `json:"vat_note"`
	ProjectID     int64     `json:"project_id,omitempty"`
	ClientEmail   string    `json:"client_email,omitempty"`
	Receipt       string    `json:"receipt,omitempty"` // "Мой налог" receipt number or link
	ByPerson      bool      `json:"by_person,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// InvoiceLine is one row on an invoice.
type InvoiceLine struct {
	ID          int64   `json:"id"`
	InvoiceID   int64   `json:"invoice_id"`
	Label       string  `json:"label"`
	Detail      string  `json:"detail"`
	Seconds     int     `json:"seconds"`
	RateCents   int     `json:"rate_cents"`
	AmountCents int     `json:"amount_cents"`
	Currency    string  `json:"currency,omitempty"` // project currency; not stored per line
	SessionIDs  []int64 `json:"-"`                  // sessions this line bills (stamped on create)
	ProjectID   int64   `json:"-"`
}

// SetProjectRate updates the billable rate for a project. rateCents is
// the hourly rate; pass nil to clear it. billable=nil leaves the flag.
func (d *DB) SetProjectRate(ctx context.Context, teamID, projectID int64, rateCents *int, billable *bool) error {
	q := `UPDATE projects SET updated_at = ?`
	args := []any{FormatTime(time.Now().UTC())}
	if rateCents != nil {
		if *rateCents < 0 {
			return fmt.Errorf("rate must be >= 0")
		}
		q += `, billable_rate_cents = ?`
		args = append(args, *rateCents)
	}
	if billable != nil {
		q += `, billable = ?`
		if *billable {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	q += ` WHERE id = ? AND team_id = ?`
	args = append(args, projectID, teamID)
	res, err := d.sql.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateInvoice builds an invoice with its lines in one shot.
// number is assigned by the caller (nextInvoiceNumber).
func (d *DB) CreateInvoice(ctx context.Context, teamID int64, number, clientName string,
	start, end time.Time, notes string, lines []InvoiceLine) (Invoice, error) {
	if number == "" {
		return Invoice{}, fmt.Errorf("number is required")
	}
	currency, err := d.invoiceCurrency(ctx, teamID, lines)
	if err != nil {
		return Invoice{}, err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return Invoice{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := FormatTime(time.Now().UTC())
	var invID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, notes, currency, created_at)
		 VALUES (?, ?, ?, ?, ?, 'draft', ?, ?, ?) RETURNING id`,
		teamID, number, clientName, FormatTime(start), FormatTime(end), notes, currency, now).Scan(&invID)
	if err != nil {
		if isUniqueViolation(err) {
			return Invoice{}, ErrDuplicate
		}
		return Invoice{}, err
	}
	if err := insertLines(ctx, tx, invID, lines); err != nil {
		return Invoice{}, err
	}
	if err := tx.Commit(); err != nil {
		return Invoice{}, err
	}
	return d.GetInvoice(ctx, teamID, invID)
}

// NextInvoiceNumber returns "INV-YYYY-NNN" for the team.
func (d *DB) NextInvoiceNumber(ctx context.Context, teamID int64) (string, error) {
	year := time.Now().Year()
	rules, _ := d.TeamBilling(ctx, teamID)
	prefix := fmt.Sprintf("%s-%d-", rules.InvoicePrefix, year)
	var maxNum int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(CAST(SUBSTR(number, LENGTH(?) + 1) AS INTEGER)), 0)
		 FROM invoices WHERE team_id = ? AND number LIKE ?`,
		prefix, teamID, prefix+"%").Scan(&maxNum)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%03d", prefix, maxNum+1), nil
}

// ListInvoices returns the team's invoices, newest first.
func (d *DB) ListInvoices(ctx context.Context, teamID int64) ([]Invoice, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, number, client_name, period_start, period_end, status, notes, payment_url, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = invoices.team_id), 'RUB'), seller_details, client_details, vat_note, COALESCE(project_id, 0), client_email, receipt, by_person, created_at
		 FROM invoices WHERE team_id = ? ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// GetInvoice fetches one invoice inside a team.
func (d *DB) GetInvoice(ctx context.Context, teamID, id int64) (Invoice, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, number, client_name, period_start, period_end, status, notes, payment_url, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = invoices.team_id), 'RUB'), seller_details, client_details, vat_note, COALESCE(project_id, 0), client_email, receipt, by_person, created_at
		 FROM invoices WHERE id = ? AND team_id = ?`, id, teamID)
	inv, err := scanInvoice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Invoice{}, ErrNotFound
	}
	return inv, err
}

// ListInvoiceLines returns the line items.
func (d *DB) ListInvoiceLines(ctx context.Context, invoiceID int64) ([]InvoiceLine, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, invoice_id, label, detail, seconds, rate_cents, amount_cents
		 FROM invoice_lines WHERE invoice_id = ? ORDER BY id`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InvoiceLine
	for rows.Next() {
		var l InvoiceLine
		if err := rows.Scan(&l.ID, &l.InvoiceID, &l.Label, &l.Detail, &l.Seconds, &l.RateCents, &l.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// UpdateInvoiceStatus flips draft → sent → paid.
func (d *DB) UpdateInvoiceStatus(ctx context.Context, teamID, id int64, status string) error {
	switch status {
	case "draft", "sent", "paid":
	default:
		return fmt.Errorf("bad status %q", status)
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE invoices SET status = ? WHERE id = ? AND team_id = ?`, status, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteInvoice removes an invoice and its lines.
func (d *DB) DeleteInvoice(ctx context.Context, teamID, id int64) error {
	// Its sessions become billable again.
	if _, err := d.sql.ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL WHERE invoice_id = ? AND team_id = ?`, id, teamID); err != nil {
		return err
	}
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM invoices WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPaymentURL stores a Stripe Checkout (or manual) payment link.
func (d *DB) SetPaymentURL(ctx context.Context, teamID, id int64, paymentURL, stripeSession string) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE invoices SET payment_url = ?, stripe_session_id = ? WHERE id = ? AND team_id = ?`,
		paymentURL, stripeSession, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkInvoicePaid flags the invoice paid (webhook / manual).
func (d *DB) MarkInvoicePaid(ctx context.Context, teamID, id int64) error {
	now := FormatTime(time.Now().UTC())
	res, err := d.sql.ExecContext(ctx,
		`UPDATE invoices SET status = 'paid', paid_at = ? WHERE id = ? AND team_id = ?`,
		now, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TeamStripe returns the team's Stripe credentials (empty when unset).
func (d *DB) TeamStripe(ctx context.Context, teamID int64) (key, webhookSecret string, err error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(stripe_key, ''), COALESCE(stripe_webhook_secret, '') FROM teams WHERE id = ?`, teamID)
	if err = row.Scan(&key, &webhookSecret); err != nil {
		return
	}
	return mustOpen(key), mustOpen(webhookSecret), nil
}

// SetTeamStripe stores Stripe credentials for the workspace.
func (d *DB) SetTeamStripe(ctx context.Context, teamID int64, key, webhookSecret string) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE teams SET stripe_key = ?, stripe_webhook_secret = ? WHERE id = ?`,
		sealSecret(key), sealSecret(webhookSecret), teamID)
	return err
}

// billedCutoff is when sessions started carrying invoice_id. Invoices
// made before it get their sessions stamped once, by the same rule the
// old code billed with: same workspace, start inside the period, same
// "project · activity" line, and already existing when the invoice was
// made. Later invoices are stamped at creation, so this never re-runs on them.
const billedCutoff = "2026-09-27T09:05:00"

// assignOrphanSessions gives sessions recorded before user_id was set on
// every path (backfill, timesheet, import, API) to the workspace owner,
// the only person who could have made them in a personal workspace and
// the least-wrong owner in a shared one. Idempotent.
func (d *DB) assignOrphanSessions(ctx context.Context) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE sessions SET user_id = (SELECT owner_id FROM teams WHERE teams.id = sessions.team_id)
		 WHERE user_id IS NULL AND team_id IS NOT NULL`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("sessions: %d without an author assigned to their workspace owner", n)
	}
	return nil
}

func (d *DB) stampLegacyInvoices(ctx context.Context) error {
	res, err := d.sql.ExecContext(ctx, `
		UPDATE sessions SET invoice_id = (
			SELECT i.id FROM invoices i
			JOIN invoice_lines l ON l.invoice_id = i.id
			JOIN activities a ON a.id = sessions.activity_id
			JOIN projects p ON p.id = a.project_id
			WHERE i.team_id = sessions.team_id AND i.created_at < ?
			  AND sessions.start_at >= i.period_start AND sessions.start_at < i.period_end
			  AND sessions.created_at <= i.created_at
			  AND l.label = p.name || ' · ' || a.name
			ORDER BY i.id LIMIT 1)
		WHERE invoice_id IS NULL AND end_at IS NOT NULL AND EXISTS (
			SELECT 1 FROM invoices i
			JOIN invoice_lines l ON l.invoice_id = i.id
			JOIN activities a ON a.id = sessions.activity_id
			JOIN projects p ON p.id = a.project_id
			WHERE i.team_id = sessions.team_id AND i.created_at < ?
			  AND sessions.start_at >= i.period_start AND sessions.start_at < i.period_end
			  AND sessions.created_at <= i.created_at
			  AND l.label = p.name || ' · ' || a.name)`, billedCutoff, billedCutoff)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("invoices: stamped %d sessions billed before invoice_id existed", n)
	}
	return nil
}

// insertLines writes an invoice's lines and stamps the sessions they bill.
func insertLines(ctx context.Context, tx *Tx, invID int64, lines []InvoiceLine) error {
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			invID, l.Label, l.Detail, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return err
		}
		if len(l.SessionIDs) == 0 {
			continue
		}
		// Only sessions still free take the stamp: if another invoice got
		// one first (two managers at once), this one fails whole.
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET invoice_id = ? WHERE id = ANY(?) AND invoice_id IS NULL`, invID, l.SessionIDs)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != int64(len(l.SessionIDs)) {
			return ErrAlreadyBilled
		}
	}
	return nil
}

// ErrAlreadyBilled: some of the time was put on another invoice meanwhile.
var ErrAlreadyBilled = errors.New("some of this time was just billed on another invoice; reload and try again")

// RebuildInvoice recomputes a draft's lines from its period and project:
// edited sessions and newly finished ones are picked up, its own billed
// sessions are released first and re-stamped.
func (d *DB) RebuildInvoice(ctx context.Context, teamID, id int64) error {
	inv, err := d.GetInvoice(ctx, teamID, id)
	if err != nil {
		return err
	}
	if inv.Status != "draft" {
		return fmt.Errorf("only a draft can be rebuilt")
	}
	lines, err := d.BuildInvoiceLinesFor(ctx, teamID, inv.PeriodStart, inv.PeriodEnd, inv.ProjectID, id, inv.ByPerson)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL WHERE invoice_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM invoice_lines WHERE invoice_id = ?`, id); err != nil {
		return err
	}
	if err := insertLines(ctx, tx, id, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateInvoiceMeta edits who the invoice is for and its notes. The
// numbers stay derived from sessions (RebuildInvoice).
func (d *DB) UpdateInvoiceMeta(ctx context.Context, teamID, id int64, client, details, email, notes string) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE invoices SET client_name = ?, client_details = ?, client_email = ?, notes = ? WHERE id = ? AND team_id = ?`,
		client, details, email, notes, id, teamID)
	return err
}

// SetInvoiceReceipt stores the "Мой налог" receipt (number or link).
func (d *DB) SetInvoiceReceipt(ctx context.Context, teamID, id int64, receipt string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE invoices SET receipt = ? WHERE id = ? AND team_id = ?`, receipt, id, teamID)
	return err
}

// SetInvoiceByPerson remembers that the lines are split per person.
func (d *DB) SetInvoiceByPerson(ctx context.Context, teamID, id int64, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE invoices SET by_person = ? WHERE id = ? AND team_id = ?`, v, id, teamID)
	return err
}

// SetInvoiceProject records which project an invoice was made for (0 = all)
// and the client's email, so a rebuild and "send" know them.
func (d *DB) SetInvoiceProject(ctx context.Context, teamID, id, projectID int64, email string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE invoices SET project_id = ?, client_email = ? WHERE id = ? AND team_id = ?`,
		nullableInt64(projectID), email, id, teamID)
	return err
}

// OverlappingInvoices lists other invoices whose period overlaps
// [start, end) and that bill any of the same lines (project · activity):
// the same hours may be on two documents.
func (d *DB) OverlappingInvoices(ctx context.Context, teamID, exceptID int64, start, end time.Time, labels []string) ([]string, error) {
	want := map[string]bool{}
	for _, l := range labels {
		want[l] = true
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT DISTINCT i.number, l.label FROM invoices i JOIN invoice_lines l ON l.invoice_id = i.id
		 WHERE i.team_id = ? AND i.id <> ? AND i.period_start < ? AND i.period_end > ?
		 ORDER BY i.number`,
		teamID, exceptID, FormatTime(end), FormatTime(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	seen := map[string]bool{}
	for rows.Next() {
		var num, label string
		if err := rows.Scan(&num, &label); err != nil {
			return nil, err
		}
		if want[label] && !seen[num] {
			seen[num] = true
			out = append(out, num)
		}
	}
	return out, rows.Err()
}

// ErrMixedCurrency: one document holds one currency; amounts in different
// currencies are never added up.
var ErrMixedCurrency = errors.New("lines in different currencies")

// invoiceCurrency is the single currency of the lines (each line carries
// its project's), or the workspace currency when lines don't say.
func (d *DB) invoiceCurrency(ctx context.Context, teamID int64, lines []InvoiceLine) (string, error) {
	team, err := d.TeamCurrency(ctx, teamID)
	if err != nil {
		return "", err
	}
	cur := ""
	for _, l := range lines {
		lc := l.Currency
		if lc == "" {
			lc = team // the project inherits the workspace currency
		}
		if cur != "" && lc != cur {
			return "", ErrMixedCurrency
		}
		cur = lc
	}
	if cur == "" {
		return team, nil
	}
	return cur, nil
}

// UserPrefs is the raw JSON of a user's preferences ("" = defaults).
func (d *DB) UserPrefs(ctx context.Context, userID int64) (string, error) {
	var p string
	err := d.sql.QueryRowContext(ctx, `SELECT prefs FROM users WHERE id = ?`, userID).Scan(&p)
	return p, err
}

func (d *DB) SetUserPrefs(ctx context.Context, userID int64, prefs string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE users SET prefs = ? WHERE id = ?`, prefs, userID)
	return err
}

// BillingRules are how a workspace turns tracked time into billed time.
type BillingRules struct {
	RoundMinutes  int    // 0 = exact to 0.01 h; else 6, 15, 30, 60
	RoundMode     string // "up" | "nearest"
	InvoicePrefix string // "INV" → INV-2026-001
	Logo          string // data: URL of a small PNG/JPEG, "" = none
}

func (d *DB) TeamBilling(ctx context.Context, teamID int64) (BillingRules, error) {
	var b BillingRules
	err := d.sql.QueryRowContext(ctx, `SELECT round_minutes, round_mode, invoice_prefix, logo FROM teams WHERE id = ?`, teamID).
		Scan(&b.RoundMinutes, &b.RoundMode, &b.InvoicePrefix, &b.Logo)
	if errors.Is(err, sql.ErrNoRows) {
		return BillingRules{InvoicePrefix: "INV", RoundMode: "nearest"}, nil
	}
	if b.InvoicePrefix == "" {
		b.InvoicePrefix = "INV"
	}
	return b, err
}

func (d *DB) SetTeamBilling(ctx context.Context, teamID int64, b BillingRules) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE teams SET round_minutes = ?, round_mode = ?, invoice_prefix = ? WHERE id = ?`,
		b.RoundMinutes, b.RoundMode, b.InvoicePrefix, teamID)
	return err
}

func (d *DB) SetTeamLogo(ctx context.Context, teamID int64, dataURL string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE teams SET logo = ? WHERE id = ?`, dataURL, teamID)
	return err
}

// RoundBilled applies a workspace rounding rule to a line's seconds.
func RoundBilled(sec int, b BillingRules) int {
	if b.RoundMinutes <= 0 || sec <= 0 {
		return sec
	}
	step := b.RoundMinutes * 60
	if b.RoundMode == "up" {
		return (sec + step - 1) / step * step
	}
	return (sec + step/2) / step * step
}

// TeamModules is the workspace's section list ("" = everything).
func (d *DB) TeamModules(ctx context.Context, teamID int64) (string, error) {
	var m string
	err := d.sql.QueryRowContext(ctx, `SELECT modules FROM teams WHERE id = ?`, teamID).Scan(&m)
	return m, err
}

func (d *DB) SetTeamModules(ctx context.Context, teamID int64, modules string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE teams SET modules = ? WHERE id = ?`, modules, teamID)
	return err
}

// TeamRequisites are the issuer's details and VAT line for documents.
func (d *DB) TeamRequisites(ctx context.Context, teamID int64) (requisites, vatNote string, err error) {
	err = d.sql.QueryRowContext(ctx, `SELECT requisites, vat_note FROM teams WHERE id = ?`, teamID).Scan(&requisites, &vatNote)
	return
}

func (d *DB) SetTeamRequisites(ctx context.Context, teamID int64, requisites, vatNote string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE teams SET requisites = ?, vat_note = ? WHERE id = ?`, requisites, vatNote, teamID)
	return err
}

// SetInvoiceParties stores the document's seller/client details and VAT
// line (called once, at creation).
func (d *DB) SetInvoiceParties(ctx context.Context, teamID, id int64, seller, client, vat string) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE invoices SET seller_details = ?, client_details = ?, vat_note = ? WHERE id = ? AND team_id = ?`,
		seller, client, vat, id, teamID)
	return err
}

// TeamCurrency is the workspace default (RUB when unset or no team).
func (d *DB) TeamCurrency(ctx context.Context, teamID int64) (string, error) {
	var cur string
	err := d.sql.QueryRowContext(ctx, `SELECT currency FROM teams WHERE id = ?`, teamID).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) || cur == "" {
		return "RUB", nil
	}
	return cur, err
}

// SetTeamCurrency changes the default for new projects and documents;
// existing invoices and pay runs keep the currency they were made in.
func (d *DB) SetTeamCurrency(ctx context.Context, teamID int64, cur string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE teams SET currency = ? WHERE id = ?`, cur, teamID)
	return err
}

// SetProjectCurrency sets a project's own currency (” = the workspace's).
func (d *DB) SetProjectCurrency(ctx context.Context, teamID, projectID int64, cur string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE projects SET currency = ? WHERE id = ? AND team_id = ?`, cur, projectID, teamID)
	return err
}

// ProjectClient is who a project bills: name, requisites, email.
type ProjectClient struct{ Name, Details, Email string }

func (d *DB) GetProjectClient(ctx context.Context, teamID, projectID int64) (ProjectClient, error) {
	var c ProjectClient
	err := d.sql.QueryRowContext(ctx, `SELECT client_name, client_details, client_email FROM projects WHERE id = ? AND team_id = ?`,
		projectID, teamID).Scan(&c.Name, &c.Details, &c.Email)
	return c, err
}

// SetProjectClient remembers the client for the next invoice.
func (d *DB) SetProjectClient(ctx context.Context, teamID, projectID int64, c ProjectClient) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE projects SET client_name = ?, client_details = ?, client_email = ? WHERE id = ? AND team_id = ?`,
		c.Name, c.Details, c.Email, projectID, teamID)
	return err
}

// ProjectCurrency is the project's own currency, ” when it inherits.
func (d *DB) ProjectCurrency(ctx context.Context, teamID, projectID int64) (string, error) {
	var cur string
	err := d.sql.QueryRowContext(ctx, `SELECT currency FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&cur)
	return cur, err
}

func scanInvoice(r interface{ Scan(...any) error }) (Invoice, error) {
	var byPerson int
	var (
		inv            Invoice
		start, end, ct string
		paymentURL     sql.NullString
	)
	if err := r.Scan(&inv.ID, &inv.TeamID, &inv.Number, &inv.ClientName,
		&start, &end, &inv.Status, &inv.Notes, &paymentURL, &inv.Currency,
		&inv.SellerDetails, &inv.ClientDetails, &inv.VATNote, &inv.ProjectID, &inv.ClientEmail, &inv.Receipt, &byPerson, &ct); err != nil {
		return Invoice{}, err
	}
	inv.PeriodStart, _ = ScanTime(start)
	inv.PeriodEnd, _ = ScanTime(end)
	if paymentURL.Valid {
		inv.PaymentURL = paymentURL.String
	}
	inv.CreatedAt, _ = ScanTime(ct)
	inv.ByPerson = byPerson == 1
	return inv, nil
}

// BuildInvoiceLines aggregates billable time in [start,end) into invoice
// lines grouped by (project, activity). projectID=0 means all projects.
func (d *DB) BuildInvoiceLines(ctx context.Context, teamID int64, start, end time.Time, projectID int64) ([]InvoiceLine, error) {
	return d.BuildInvoiceLinesFor(ctx, teamID, start, end, projectID, 0)
}

// BuildInvoiceLinesFor with byPerson splits each line per team member
// ("Project · activity · Name"): a studio shows the client who did what.
func (d *DB) BuildInvoiceLinesFor(ctx context.Context, teamID int64, start, end time.Time, projectID, reuse int64, byPerson ...bool) ([]InvoiceLine, error) {
	return d.buildLines(ctx, teamID, start, end, projectID, reuse, len(byPerson) > 0 && byPerson[0])
}

// BuildInvoiceLinesFor is the billing rule. A session is billed once and
// whole: it belongs to the period its start falls in, it must be finished
// (a running timer isn't billed until it stops), and it must not already
// be on another invoice (invoice_id). Rebuilding a draft passes its own
// id as reuse, so its sessions count again. Each line carries the session
// ids it covers; CreateInvoice stamps them.
func (d *DB) buildLines(ctx context.Context, teamID int64, start, end time.Time, projectID, reuse int64, byPerson bool) ([]InvoiceLine, error) {
	q := `
		SELECT s.id, p.id, a.name, COALESCE(p.name, ''), s.start_at, s.end_at, s.accumulated_seconds,
		       COALESCE(p.billable_rate_cents, 0), COALESCE(p.billable, 1), COALESCE(p.currency, ''),
		       COALESCE(NULLIF(u.name, ''), u.email, '')
		FROM sessions s
		JOIN activities a ON a.id = s.activity_id
		JOIN projects p ON p.id = a.project_id
		LEFT JOIN users u ON u.id = s.user_id
		WHERE s.start_at >= ? AND s.start_at < ? AND s.end_at IS NOT NULL
		  AND (s.invoice_id IS NULL OR s.invoice_id = ?)`
	args := []any{FormatTime(start), FormatTime(end), reuse}
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	if projectID > 0 {
		q += ` AND a.project_id = ?`
		args = append(args, projectID)
	}
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct {
		projID            int64
		act, proj, person string
	}
	type acc struct {
		secs     int
		rate     int
		detail   string
		currency string
		ids      []int64
	}
	buckets := map[key]*acc{}
	for rows.Next() {
		var (
			id, projID            int64
			actName, projName     string
			startAt, endAt        string
			accum, rate, billable int
			currency, person      string
		)
		if err := rows.Scan(&id, &projID, &actName, &projName, &startAt, &endAt, &accum, &rate, &billable, &currency, &person); err != nil {
			return nil, err
		}
		if billable == 0 {
			continue // non-billable projects never land on an invoice
		}
		st, err1 := ScanTime(startAt)
		en, err2 := ScanTime(endAt)
		if err1 != nil || err2 != nil {
			continue
		}
		sec := model.Session{StartAt: st, EndAt: &en, AccumulatedSeconds: accum}.DurationSeconds(time.Now())
		if sec <= 0 {
			continue
		}
		k := key{projID: projID, act: actName, proj: projName}
		if byPerson {
			k.person = person
		}
		a := buckets[k]
		if a == nil {
			a = &acc{rate: rate, detail: projName, currency: currency}
			buckets[k] = a
		}
		a.secs += sec
		a.ids = append(a.ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rules, _ := d.TeamBilling(ctx, teamID)
	lines := make([]InvoiceLine, 0, len(buckets))
	for k, a := range buckets {
		// The workspace's rounding (per line, so hours × rate = amount
		// still holds on the document).
		a.secs = RoundBilled(a.secs, rules)
		if HoursHundredths(a.secs) == 0 {
			continue // under 0.01 h: a 0.00 line is noise on a client document
		}
		label := k.proj + " · " + k.act
		if k.person != "" {
			label += " · " + k.person
		}
		lines = append(lines, InvoiceLine{
			Label: label, Detail: a.detail, Seconds: a.secs,
			RateCents: a.rate, AmountCents: PriceCents(a.secs, a.rate), Currency: a.currency,
			SessionIDs: a.ids, ProjectID: k.projID,
		})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Label < lines[j].Label })
	return lines, nil
}

// UnbilledProject is billable time not yet on any invoice.
type UnbilledProject struct {
	ProjectID   int64
	ProjectName string
	ProjectSlug string
	Currency    string
	Hundredths  int // billable hours × 100, rounded per line like an invoice
	AmountCents int
	Since       time.Time // earliest unbilled session start
}

// Unbilled sums, per project, what an invoice for all unbilled time up to
// now would say: the same lines rule as BuildInvoiceLinesFor.
func (d *DB) Unbilled(ctx context.Context, teamID int64, projectID int64) ([]UnbilledProject, error) {
	// Summed in SQL per invoice line (project × activity), then rounded
	// and priced per line in Go exactly like buildLines, so a manager's
	// dashboard doesn't pull every unbilled session. TestUnbilledMatchesInvoice
	// keeps the two in step.
	// Sessions fold per activity first (cheap keys, "C" order for the
	// ISO strings), then per line.
	q := `SELECT p.id, p.name, p.slug, COALESCE(NULLIF(p.currency, ''), t.currency, 'RUB'),
		       COALESCE(p.billable_rate_cents, 0), MIN(x.since), SUM(x.secs)::bigint
		FROM (SELECT s.activity_id, MIN(s.start_at COLLATE "C") AS since,
		             SUM(CASE WHEN s.accumulated_seconds > 0 THEN s.accumulated_seconds
		                      ELSE GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (s.end_at::timestamp - s.start_at::timestamp)))) END) AS secs
		      FROM sessions s
		      WHERE s.team_id = ? AND s.end_at IS NOT NULL AND s.invoice_id IS NULL AND s.start_at < ?
		      GROUP BY s.activity_id) x
		JOIN activities a ON a.id = x.activity_id JOIN projects p ON p.id = a.project_id
		JOIN teams t ON t.id = p.team_id
		WHERE COALESCE(p.billable, 1) = 1 AND COALESCE(p.billable_rate_cents, 0) > 0 AND p.archived = 0`
	args := []any{teamID, FormatTime(time.Now().Add(time.Hour))}
	if projectID > 0 {
		q += ` AND p.id = ?`
		args = append(args, projectID)
	}
	q += ` GROUP BY p.id, p.name, p.slug, p.currency, t.currency, p.billable_rate_cents, a.name ORDER BY p.name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules, _ := d.TeamBilling(ctx, teamID)
	var out []UnbilledProject
	at := map[int64]int{}
	for rows.Next() {
		var u UnbilledProject
		var rate int
		var since string
		var secs int64
		if err := rows.Scan(&u.ProjectID, &u.ProjectName, &u.ProjectSlug, &u.Currency, &rate, &since, &secs); err != nil {
			return nil, err
		}
		i, ok := at[u.ProjectID]
		if !ok {
			i = len(out)
			at[u.ProjectID] = i
			out = append(out, u)
		}
		if st, err := ScanTime(since); err == nil && (out[i].Since.IsZero() || st.Before(out[i].Since)) {
			out[i].Since = st
		}
		line := RoundBilled(int(secs), rules)
		if HoursHundredths(line) == 0 {
			continue
		}
		out[i].Hundredths += HoursHundredths(line)
		out[i].AmountCents += PriceCents(line, rate)
	}
	return out, rows.Err()
}

// InvoiceLockFor names the sent or paid invoice a session is billed on
// ("" = editable). Drafts don't lock: they can be rebuilt.
func (d *DB) InvoiceLockFor(ctx context.Context, teamID, sessionID int64) string {
	var num string
	_ = d.sql.QueryRowContext(ctx,
		`SELECT i.number FROM sessions s JOIN invoices i ON i.id = s.invoice_id
		 WHERE s.id = ? AND s.team_id = ? AND i.status IN ('sent', 'paid')`, sessionID, teamID).Scan(&num)
	return num
}

// DayLockFor is InvoiceLockFor for a timesheet cell (activity, day).
func (d *DB) DayLockFor(ctx context.Context, teamID, activityID int64, dayStart, dayEnd time.Time) string {
	var num string
	_ = d.sql.QueryRowContext(ctx,
		`SELECT i.number FROM sessions s JOIN invoices i ON i.id = s.invoice_id
		 WHERE s.team_id = ? AND s.activity_id = ? AND s.start_at >= ? AND s.start_at < ?
		   AND i.status IN ('sent', 'paid') AND (? = 0 OR s.user_id = ?) LIMIT 1`,
		teamID, activityID, FormatTime(dayStart), FormatTime(dayEnd), actorID(ctx), actorID(ctx)).Scan(&num)
	return num
}

// HoursHundredths rounds tracked seconds to billable hundredths of an
// hour (0.01 h = 36 s), half up. Money is priced from this rounded
// quantity so "hours × rate = amount" holds on every document.
func HoursHundredths(sec int) int {
	if sec <= 0 {
		return 0
	}
	return (sec*100 + 1800) / 3600
}

// PriceCents is the amount for sec of work at rateCents per hour,
// computed from the rounded hours and rounded half up to the cent.
func PriceCents(sec, rateCents int) int {
	return (HoursHundredths(sec)*rateCents + 50) / 100
}
