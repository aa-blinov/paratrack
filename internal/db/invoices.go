package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	CreatedAt   time.Time `json:"created_at"`
}

// InvoiceLine is one row on an invoice.
type InvoiceLine struct {
	ID          int64  `json:"id"`
	InvoiceID   int64  `json:"invoice_id"`
	Label       string `json:"label"`
	Detail      string `json:"detail"`
	Seconds     int    `json:"seconds"`
	RateCents   int    `json:"rate_cents"`
	AmountCents int    `json:"amount_cents"`
	Currency    string `json:"currency,omitempty"` // project currency; not stored per line
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
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			invID, l.Label, l.Detail, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return Invoice{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Invoice{}, err
	}
	return d.GetInvoice(ctx, teamID, invID)
}

// NextInvoiceNumber returns "INV-YYYY-NNN" for the team.
func (d *DB) NextInvoiceNumber(ctx context.Context, teamID int64) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("INV-%d-", year)
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
		`SELECT id, team_id, number, client_name, period_start, period_end, status, notes, payment_url, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = invoices.team_id), 'RUB'), created_at
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
		`SELECT id, team_id, number, client_name, period_start, period_end, status, notes, payment_url, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = invoices.team_id), 'RUB'), created_at
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
	err = row.Scan(&key, &webhookSecret)
	return
}

// SetTeamStripe stores Stripe credentials for the workspace.
func (d *DB) SetTeamStripe(ctx context.Context, teamID int64, key, webhookSecret string) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE teams SET stripe_key = ?, stripe_webhook_secret = ? WHERE id = ?`,
		key, webhookSecret, teamID)
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

// SetProjectCurrency sets a project's own currency ('' = the workspace's).
func (d *DB) SetProjectCurrency(ctx context.Context, teamID, projectID int64, cur string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE projects SET currency = ? WHERE id = ? AND team_id = ?`, cur, projectID, teamID)
	return err
}

// ProjectCurrency is the project's own currency, '' when it inherits.
func (d *DB) ProjectCurrency(ctx context.Context, teamID, projectID int64) (string, error) {
	var cur string
	err := d.sql.QueryRowContext(ctx, `SELECT currency FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&cur)
	return cur, err
}

func scanInvoice(r interface{ Scan(...any) error }) (Invoice, error) {
	var (
		inv            Invoice
		start, end, ct string
		paymentURL     sql.NullString
	)
	if err := r.Scan(&inv.ID, &inv.TeamID, &inv.Number, &inv.ClientName,
		&start, &end, &inv.Status, &inv.Notes, &paymentURL, &inv.Currency, &ct); err != nil {
		return Invoice{}, err
	}
	inv.PeriodStart, _ = ScanTime(start)
	inv.PeriodEnd, _ = ScanTime(end)
	if paymentURL.Valid {
		inv.PaymentURL = paymentURL.String
	}
	inv.CreatedAt, _ = ScanTime(ct)
	return inv, nil
}

// BuildInvoiceLines aggregates tracked time in [start,end) into invoice
// lines grouped by (project, activity). projectID=0 means all projects.
// The rate comes from the project's billable_rate_cents (0 if unset).
func (d *DB) BuildInvoiceLines(ctx context.Context, teamID int64, start, end time.Time, projectID int64) ([]InvoiceLine, error) {
	q := `
		SELECT a.name AS activity_name, a.project_id,
		       s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at,
		       COALESCE(p.name, '') AS project_name,
		       COALESCE(p.billable_rate_cents, 0) AS rate,
		       COALESCE(p.billable, 1) AS billable,
		       COALESCE(p.currency, '') AS currency
		FROM sessions s
		JOIN activities a ON a.id = s.activity_id
		LEFT JOIN projects p ON p.id = a.project_id
		WHERE s.start_at < ?
		  AND (s.end_at IS NULL OR s.end_at >= ?)`
	args := []any{FormatTime(end), FormatTime(start)}
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
		act string
		proj string
	}
	type acc struct {
		secs     int
		rate     int
		detail   string
		currency string
	}
	buckets := map[key]*acc{}
	now := time.Now()

	for rows.Next() {
		var (
			actName, projName            string
			projID                       sql.NullInt64
			startAt, endAt, lastResume   sql.NullString
			accum, paused, rate, billable int
			currency                      string
		)
		if err := rows.Scan(&actName, &projID, &startAt, &endAt, &accum, &paused, &lastResume,
			&projName, &rate, &billable, &currency); err != nil {
			return nil, err
		}
		if billable == 0 {
			continue // non-billable projects never land on an invoice
		}
		if !projID.Valid || projID.Int64 == 0 {
			continue // unassigned time has no rate and no client — never invoiceable
		}
		st, err := ScanTime(startAt.String)
		if err != nil {
			continue
		}
		sess := model.Session{
			StartAt:            st,
			AccumulatedSeconds: accum,
			Paused:             paused == 1,
		}
		if endAt.Valid {
			if t, err := ScanTime(endAt.String); err == nil {
				sess.EndAt = &t
			}
		}
		if lastResume.Valid {
			if t, err := ScanTime(lastResume.String); err == nil {
				sess.LastResumeAt = &t
			}
		}
		sec := sess.TrackedSecondsInWindow(start, end, now)
		if sec <= 0 {
			continue
		}
		k := key{act: actName, proj: projName}
		a := buckets[k]
		if a == nil {
			a = &acc{rate: rate, detail: projName, currency: currency}
			buckets[k] = a
		}
		a.secs += sec
	}

	lines := make([]InvoiceLine, 0, len(buckets))
	for k, a := range buckets {
		label := k.act
		if k.proj != "" {
			label = k.proj + " · " + k.act
		}
		if HoursHundredths(a.secs) == 0 {
			continue // under 0.01 h: a 0.00 line is noise on a client document
		}
		amount := PriceCents(a.secs, a.rate)
		lines = append(lines, InvoiceLine{
			Label:       label,
			Detail:      a.detail,
			Seconds:     a.secs,
			RateCents:   a.rate,
			AmountCents: amount,
			Currency:    a.currency,
		})
	}
	// stable order by label
	for i := 0; i < len(lines); i++ {
		for j := i + 1; j < len(lines); j++ {
			if lines[j].Label < lines[i].Label {
				lines[i], lines[j] = lines[j], lines[i]
			}
		}
	}
	return lines, nil
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
