package web

import (
	"strconv"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
)



func TestBillableRateAndInvoiceFlow(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)

	p, err := d.CreateProject(ctx, 1, "Client A", "", "#7c3aed")
	if err != nil {
		t.Fatal(err)
	}
	// 10000 cents/hour = 100.00
	rate := 10000
	if err := d.SetProjectRate(ctx, 1, p.ID, &rate, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetProject(ctx, p.ID)
	if got.BillableRateCents == nil || *got.BillableRateCents != 10000 {
		t.Fatalf("rate=%v", got.BillableRateCents)
	}

	// 2h of work in the project
	act, err := d.CreateActivity(ctx, 1, "consulting")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, 1, act.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	if _, err := d.CreateClosedSession(ctx, 1, act.ID, start, end, "billable work"); err != nil {
		t.Fatal(err)
	}

	// build lines: 2h × 100.00 = 200.00
	lines, err := d.BuildInvoiceLines(ctx, 1, start.Add(-time.Hour), end.Add(time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines=%+v", lines)
	}
	if lines[0].AmountCents != 20000 {
		t.Errorf("amount=%d, want 20000", lines[0].AmountCents)
	}
	if lines[0].Seconds != 7200 {
		t.Errorf("seconds=%d, want 7200", lines[0].Seconds)
	}

	// non-billable project is excluded
	bFalse := false
	if err := d.SetProjectRate(ctx, 1, p.ID, nil, &bFalse); err != nil {
		t.Fatal(err)
	}
	lines, _ = d.BuildInvoiceLines(ctx, 1, start.Add(-time.Hour), end.Add(time.Hour), 0)
	if len(lines) != 0 {
		t.Fatalf("non-billable leaked: %+v", lines)
	}

	// invoice create + number
	bTrue := true
	if err := d.SetProjectRate(ctx, 1, p.ID, nil, &bTrue); err != nil {
		t.Fatal(err)
	}
	num, err := d.NextInvoiceNumber(ctx, 1)
	if err != nil || num == "" {
		t.Fatalf("number=%q err=%v", num, err)
	}
	inv, err := d.CreateInvoice(ctx, 1, num, "Acme", start, end, "net 14", []db.InvoiceLine{{
		Label: "Client A · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Number != num || inv.ClientName != "Acme" {
		t.Fatalf("inv=%+v", inv)
	}
	ls, _ := d.ListInvoiceLines(ctx, inv.ID)
	if len(ls) != 1 || ls[0].AmountCents != 20000 {
		t.Fatalf("lines=%+v", ls)
	}
	if err := d.UpdateInvoiceStatus(ctx, 1, inv.ID, "paid"); err != nil {
		t.Fatal(err)
	}
	got2, _ := d.GetInvoice(ctx, 1, inv.ID)
	if got2.Status != "paid" {
		t.Fatalf("status=%s", got2.Status)
	}
	// second number increments
	num2, _ := d.NextInvoiceNumber(ctx, 1)
	if num2 <= num {
		t.Errorf("number sequence: %s → %s", num, num2)
	}
}

func TestFormatMoney(t *testing.T) {
	cases := map[int]string{
		0:     "0.00",
		5:     "0.05",
		100:   "1.00",
		12345: "123.45",
		-50:   "-0.50",
	}
	for cents, want := range cases {
		if got := formatMoney(cents); got != want {
			t.Errorf("formatMoney(%d)=%q want %q", cents, got, want)
		}
	}
}

func TestFormatMoneyL(t *testing.T) {
	cases := []struct {
		lang  i18n.Lang
		cents int
		want  string
	}{
		{i18n.Ru, 123456, "1 234,56"},
		{i18n.Ru, 5, "0,05"},
		{i18n.Ru, -100000000, "-1 000 000,00"},
		{i18n.En, 123456789, "1,234,567.89"},
		{i18n.En, 99900, "999.00"},
	}
	for _, c := range cases {
		if got := formatMoneyL(c.lang, c.cents); got != c.want {
			t.Errorf("formatMoneyL(%s, %d) = %q, want %q", c.lang, c.cents, got, c.want)
		}
	}
}

// One document, one currency: an invoice takes its project's currency,
// refuses to mix, and a pay run takes the workspace's.
func TestInvoiceAndPayrollCurrency(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	if cur, _ := d.TeamCurrency(ctx, 1); cur != "RUB" {
		t.Fatalf("default team currency %q, want RUB", cur)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	rate := 5000
	mk := func(name, cur string) {
		p, err := d.CreateProject(ctx, 1, name, "", "#7c3aed")
		if err != nil {
			t.Fatal(err)
		}
		d.SetProjectRate(ctx, 1, p.ID, &rate, nil)
		d.SetProjectCurrency(ctx, 1, p.ID, cur)
		a, _ := d.CreateActivity(ctx, 1, name+" work")
		d.AssignActivityProject(ctx, 1, a.ID, p.ID)
		d.CreateClosedSession(ctx, 1, a.ID, start, start.Add(time.Hour), "")
	}
	mk("Acme US", "USD")
	mk("Ромашка", "")
	lines, _ := d.BuildInvoiceLines(ctx, 1, start, start.Add(2*time.Hour), 0)
	if _, err := d.CreateInvoice(ctx, 1, "INV-T-2", "Mix", start, start.Add(2*time.Hour), "", lines); err != db.ErrMixedCurrency {
		t.Errorf("mixed currencies: err %v, want ErrMixedCurrency", err)
	}
	var usd int64
	d.SQL().QueryRowContext(ctx, `SELECT id FROM projects WHERE name = 'Acme US'`).Scan(&usd)
	lines, _ = d.BuildInvoiceLines(ctx, 1, start, start.Add(2*time.Hour), usd)
	inv, err := d.CreateInvoice(ctx, 1, "INV-T-1", "Acme", start, start.Add(2*time.Hour), "", lines)
	if err != nil || inv.Currency != "USD" {
		t.Fatalf("invoice currency %q err %v, want USD", inv.Currency, err)
	}
	if got := moneyL(i18n.En, 5000, inv.Currency); got != "$50.00" {
		t.Errorf("moneyL = %q", got)
	}
	d.SetTeamCurrency(ctx, 1, "EUR")
	run, err := d.CreatePayrollRun(ctx, 1, "PAY-T-1", "", start, start.Add(time.Hour), nil)
	if err != nil || run.Currency != "EUR" {
		t.Errorf("pay run currency %q err %v, want EUR", run.Currency, err)
	}
	if got, _ := d.GetInvoice(ctx, 1, inv.ID); got.Currency != "USD" {
		t.Errorf("issued invoice must keep USD after team change, got %q", got.Currency)
	}
}

// Two managers build the same unbilled time at once: the second invoice
// is refused, nothing is billed twice.
func TestInvoiceRaceBillsOnce(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	rate := 5000
	p, _ := d.CreateProject(ctx, 1, "Ромашка", "", "#7c3aed")
	d.SetProjectRate(ctx, 1, p.ID, &rate, nil)
	a, _ := d.CreateActivity(ctx, 1, "вёрстка")
	d.AssignActivityProject(ctx, 1, a.ID, p.ID)
	d.CreateClosedSession(ctx, 1, a.ID, start, start.Add(time.Hour), "")
	d.CreateClosedSession(ctx, 1, a.ID, start.Add(2*time.Hour), start.Add(3*time.Hour), "")
	end := start.Add(4 * time.Hour)
	first, _ := d.BuildInvoiceLines(ctx, 1, start, end, p.ID)
	second, _ := d.BuildInvoiceLines(ctx, 1, start, end, p.ID) // read before the first is saved
	if _, err := d.CreateInvoice(ctx, 1, "INV-R-1", "A", start, end, "", first); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateInvoice(ctx, 1, "INV-R-2", "B", start, end, "", second); err != db.ErrAlreadyBilled {
		t.Fatalf("second invoice over the same time: err %v, want ErrAlreadyBilled", err)
	}
	var n int
	d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM invoices WHERE number = 'INV-R-2'`).Scan(&n)
	if n != 0 {
		t.Error("the refused invoice was saved anyway")
	}
}

// The dashboard's "not invoiced yet" sums in SQL; it must say exactly
// what an invoice for the same time would.
func TestUnbilledMatchesInvoice(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.SetTeamBilling(ctx, 1, db.BillingRules{RoundMinutes: 15, RoundMode: "up", InvoicePrefix: "INV"})
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	var pids []int64
	for pi, name := range []string{"Ромашка", "Лютик", "Acme"} {
		rate := 3000 + pi*1700
		p, _ := d.CreateProject(ctx, 1, name, "", "#7c3aed")
		d.SetProjectRate(ctx, 1, p.ID, &rate, nil)
		pids = append(pids, p.ID)
		for ai := 0; ai < 3; ai++ {
			a, _ := d.CreateActivity(ctx, 1, name+" задача "+strconv.Itoa(ai))
			d.AssignActivityProject(ctx, 1, a.ID, p.ID)
			for k := 0; k < 7; k++ {
				st := start.Add(time.Duration(pi*100+ai*10+k) * 97 * time.Minute)
				sess, _ := d.CreateClosedSession(ctx, 1, a.ID, st, st.Add(time.Duration(5+k*13)*time.Minute+17*time.Second), "")
				if k%3 == 0 { // paused sessions carry their own tracked total
					d.SQL().ExecContext(ctx, `UPDATE sessions SET accumulated_seconds = ? WHERE id = ?`, 60*(k+1)+11, sess.ID)
				}
			}
		}
	}
	got, err := d.Unbilled(ctx, 1, 0)
	if err != nil || len(got) != 3 {
		t.Fatalf("unbilled: %v, %d projects", err, len(got))
	}
	for _, u := range got {
		lines, _ := d.BuildInvoiceLines(ctx, 1, u.Since, time.Now().Add(time.Hour), u.ProjectID)
		h, cents := 0, 0
		for _, l := range lines {
			h += db.HoursHundredths(l.Seconds)
			cents += l.AmountCents
		}
		if u.Hundredths != h || u.AmountCents != cents || h == 0 {
			t.Errorf("%s: unbilled %d h/100, %d cents; invoice %d h/100, %d cents", u.ProjectName, u.Hundredths, u.AmountCents, h, cents)
		}
	}
}
