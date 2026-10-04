package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

var ErrInvalidInvoiceQuery = errors.New("invalid invoice line query")

type invoiceLineBuildOptions struct {
	byPerson bool
	lockRows bool
}

// BuildInvoiceLines aggregates billable time in [start,end) into invoice
// lines grouped by (project, activity). projectID=0 means all projects.
func (d *DB) BuildInvoiceLines(ctx context.Context, teamID int64, start, end time.Time, projectID int64) ([]InvoiceLine, error) {
	return d.BuildInvoiceLinesFor(ctx, teamID, start, end, projectID, 0)
}

// BuildInvoiceLinesFor with byPerson splits each line per team member
// ("Project · activity · Name"): a studio shows the client who did what.
func (d *DB) BuildInvoiceLinesFor(ctx context.Context, teamID int64, start, end time.Time, projectID, reuse int64, byPerson ...bool) ([]InvoiceLine, error) {
	if teamID <= 0 || start.IsZero() || !end.After(start) || projectID < 0 || reuse < 0 {
		return nil, ErrInvalidInvoiceQuery
	}
	rules, err := d.TeamBilling(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("load invoice billing rules: %w", err)
	}
	return buildInvoiceLines(ctx, d.sql, rules, teamID, start, end, projectID, reuse, d.currentTime(), invoiceLineBuildOptions{
		byPerson: len(byPerson) > 0 && byPerson[0],
	})
}

// buildInvoiceLines applies the billing rule. A session is billed once and whole:
// it belongs to the period its start falls in, it must be finished (a running
// timer isn't billed until it stops), and it must not already be on another
// invoice. Rebuilding a draft passes its own id as reuse. Each line carries
// the session IDs that invoice creation stamps transactionally.
func buildInvoiceLines(ctx context.Context, queryer invoiceQueryer, rules BillingRules, teamID int64, start, end time.Time, projectID, reuse int64, now time.Time, options invoiceLineBuildOptions) ([]InvoiceLine, error) {
	if teamID <= 0 || start.IsZero() || !end.After(start) || projectID < 0 || reuse < 0 || now.IsZero() {
		return nil, ErrInvalidInvoiceQuery
	}
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
	q += ` AND s.team_id = ?`
	args = append(args, teamID)
	if projectID > 0 {
		q += ` AND a.project_id = ?`
		args = append(args, projectID)
	}
	q += ` AND a.team_id = ? AND p.team_id = ?`
	args = append(args, teamID, teamID)
	if options.lockRows {
		q += ` ORDER BY s.id FOR UPDATE OF s, a, p`
	}
	rows, err := queryer.QueryContext(ctx, q, args...)
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
		st, err := ScanTime(startAt)
		if err != nil {
			return nil, fmt.Errorf("parse invoice session %d start: %w", id, err)
		}
		en, err := ScanTime(endAt)
		if err != nil {
			return nil, fmt.Errorf("parse invoice session %d end: %w", id, err)
		}
		sec := model.Session{StartAt: st, EndAt: &en, AccumulatedSeconds: accum}.DurationSeconds(now)
		if sec <= 0 {
			continue
		}
		k := key{projID: projID, act: actName, proj: projName}
		if options.byPerson {
			k.person = person
		}
		a := buckets[k]
		if a == nil {
			a = &acc{rate: rate, detail: projName, currency: currency}
			buckets[k] = a
		}
		a.secs, err = money.AddInt(a.secs, sec)
		if err != nil {
			return nil, fmt.Errorf("sum invoice time for project %d activity %q: %w", projID, actName, err)
		}
		a.ids = append(a.ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
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
		amount, err := PriceCents(a.secs, a.rate)
		if err != nil {
			return nil, fmt.Errorf("price invoice line %q: %w", label, err)
		}
		lines = append(lines, InvoiceLine{
			Label: label, Detail: a.detail, Seconds: a.secs,
			RateCents: a.rate, AmountCents: amount, Currency: a.currency,
			SessionIDs: a.ids, ProjectID: k.projID,
		})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Label < lines[j].Label })
	return lines, nil
}

// Unbilled sums, per project, what an invoice for all unbilled time up to
// now would say: the same lines rule as BuildInvoiceLinesFor.
func (d *DB) Unbilled(ctx context.Context, query appmodel.UnbilledProjectQuery) ([]UnbilledProject, error) {
	if query.TeamID <= 0 || (query.ProjectID != nil && *query.ProjectID <= 0) {
		return nil, ErrInvalidInvoiceQuery
	}
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
		JOIN activities a ON a.id = x.activity_id AND a.team_id = ?
		JOIN projects p ON p.id = a.project_id AND p.team_id = ?
		JOIN teams t ON t.id = p.team_id
		WHERE COALESCE(p.billable, 1) = 1 AND COALESCE(p.billable_rate_cents, 0) > 0 AND p.archived = 0`
	args := []any{query.TeamID, FormatTime(d.currentTime().Add(time.Hour)), query.TeamID, query.TeamID}
	if query.ProjectID != nil {
		q += ` AND p.id = ?`
		args = append(args, *query.ProjectID)
	}
	q += ` GROUP BY p.id, p.name, p.slug, p.currency, t.currency, p.billable_rate_cents, a.name ORDER BY p.name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules, err := d.TeamBilling(ctx, query.TeamID)
	if err != nil {
		return nil, fmt.Errorf("load unbilled billing rules: %w", err)
	}
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
		st, err := ScanTime(since)
		if err != nil {
			return nil, fmt.Errorf("parse unbilled session start: %w", err)
		}
		if out[i].Since.IsZero() || st.Before(out[i].Since) {
			out[i].Since = st
		}
		line := RoundBilled(int(secs), rules)
		if HoursHundredths(line) == 0 {
			continue
		}
		out[i].Hundredths, err = money.AddInt(out[i].Hundredths, HoursHundredths(line))
		if err != nil {
			return nil, fmt.Errorf("sum unbilled project %d hours: %w", u.ProjectID, err)
		}
		amount, err := PriceCents(line, rate)
		if err != nil {
			return nil, fmt.Errorf("price unbilled project %d: %w", u.ProjectID, err)
		}
		out[i].AmountCents, err = money.AddCents(out[i].AmountCents, amount)
		if err != nil {
			return nil, fmt.Errorf("sum unbilled project %d: %w", u.ProjectID, err)
		}
	}
	return out, rows.Err()
}

// HoursHundredths rounds tracked seconds to billable hundredths of an
// hour (0.01 h = 36 s), half up. Money is priced from this rounded
// quantity so "hours × rate = amount" holds on every document.
func HoursHundredths(sec int) int {
	return money.HoursHundredths(sec)
}

// PriceCents is the amount for sec of work at rateCents per hour,
// computed from the rounded hours and rounded half up to the cent.
func PriceCents(sec, rateCents int) (int, error) {
	return money.PriceCents(sec, rateCents)
}
