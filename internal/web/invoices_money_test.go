package web

import (
	"log"
	"strconv"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/payroll"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestBillableRateAndInvoiceFlow(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)

	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: "Client A", Slug: "", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	// 10000 cents/hour = 100.00
	rate := 10000
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, RateCents: &rate}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetProjectByID(ctx, p.ID)
	if got.BillableRateCents == nil || *got.BillableRateCents != 10000 {
		t.Fatalf("rate=%v", got.BillableRateCents)
	}

	// 2h of work in the project
	act, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "consulting"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: act.ID, ProjectID: p.ID, CallerID: 1}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: act.ID, Start: start, End: end, Note: "billable work"}); err != nil {
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
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, Billable: &bFalse}); err != nil {
		t.Fatal(err)
	}
	lines, _ = d.BuildInvoiceLines(ctx, 1, start.Add(-time.Hour), end.Add(time.Hour), 0)
	if len(lines) != 0 {
		t.Fatalf("non-billable leaked: %+v", lines)
	}

	// invoice create + number
	bTrue := true
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, Billable: &bTrue}); err != nil {
		t.Fatal(err)
	}
	num, err := d.NextInvoiceNumber(ctx, 1)
	if err != nil || num == "" {
		t.Fatalf("number=%q err=%v", num, err)
	}
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: num, Client: "Acme", Start: start, End: end, Notes: "net 14", Lines: []db.InvoiceLine{{
		Label: "Client A · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Number != num || inv.ClientName != "Acme" {
		t.Fatalf("inv=%+v", inv)
	}
	details, _ := d.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: 1, InvoiceID: inv.ID})
	if len(details.Lines) != 1 || details.Lines[0].AmountCents != 20000 {
		t.Fatalf("lines=%+v", details.Lines)
	}
	if _, _, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: 1, InvoiceID: inv.ID, CallerID: 1}); err != nil {
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
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)
	if cur, _ := d.TeamCurrency(ctx, 1); cur != "RUB" {
		t.Fatalf("default team currency %q, want RUB", cur)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	rate := 5000
	mk := func(name, cur string) {
		p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{
			TeamID: 1, CallerID: 1, Name: name, Color: "#7c3aed", RateCents: &rate, Currency: cur,
		})
		if err != nil {
			t.Fatal(err)
		}
		a, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: name + " work"})
		d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: a.ID, ProjectID: p.ID, CallerID: 1})
		d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: a.ID, Start: start, End: start.Add(time.Hour)})
	}
	mk("Acme US", "USD")
	mk("Ромашка", "")
	lines, _ := d.BuildInvoiceLines(ctx, 1, start, start.Add(2*time.Hour), 0)
	if _, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: "INV-T-2", Client: "Mix", Start: start, End: start.Add(2 * time.Hour), Lines: lines}); err != db.ErrMixedCurrency {
		t.Errorf("mixed currencies: err %v, want ErrMixedCurrency", err)
	}
	var usd int64
	d.TestSQL().QueryRowContext(ctx, `SELECT id FROM projects WHERE name = 'Acme US'`).Scan(&usd)
	lines, _ = d.BuildInvoiceLines(ctx, 1, start, start.Add(2*time.Hour), usd)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: "INV-T-1", Client: "Acme", Start: start, End: start.Add(2 * time.Hour), Lines: lines})
	if err != nil || inv.Currency != "USD" {
		t.Fatalf("invoice currency %q err %v, want USD", inv.Currency, err)
	}
	if got := moneyL(i18n.En, 5000, inv.Currency); got != "$50.00" {
		t.Errorf("moneyL = %q", got)
	}
	d.SetTeamCurrency(ctx, appmodel.TeamCurrencyRequest{TeamID: 1, CallerID: 1, Currency: "EUR"})
	auditService, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	payrollService, err := payroll.NewServiceWithClock(d, time.Now, auditService, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	pay := 5000
	if err := payrollService.UpdateMemberPay(ctx, appmodel.PayrollMemberPayRequest{
		TeamID: 1, UserID: 1, CallerID: 1, PayCents: &pay,
	}); err != nil {
		t.Fatal(err)
	}
	run, overlaps, err := payrollService.CreateRun(ctx, appmodel.PayrollDraftRequest{
		TeamID: 1, CallerID: 1, Start: start, End: start.Add(time.Hour),
	})
	if len(overlaps) != 0 {
		t.Fatalf("unexpected payroll overlaps: %+v", overlaps)
	}
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
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	rate := 5000
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: "Ромашка", Slug: "", Color: "#7c3aed"})
	d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, RateCents: &rate})
	a, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "вёрстка"})
	d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: a.ID, ProjectID: p.ID, CallerID: 1})
	d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: a.ID, Start: start, End: start.Add(time.Hour)})
	d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: a.ID, Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)})
	end := start.Add(4 * time.Hour)
	first, _ := d.BuildInvoiceLines(ctx, 1, start, end, p.ID)
	second, _ := d.BuildInvoiceLines(ctx, 1, start, end, p.ID) // read before the first is saved
	if _, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: "INV-R-1", Client: "A", Start: start, End: end, Lines: first}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: "INV-R-2", Client: "B", Start: start, End: end, Lines: second}); err != db.ErrAlreadyBilled {
		t.Fatalf("second invoice over the same time: err %v, want ErrAlreadyBilled", err)
	}
	var n int
	d.TestSQL().QueryRowContext(ctx, `SELECT count(*) FROM invoices WHERE number = 'INV-R-2'`).Scan(&n)
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
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)
	d.SetTeamBilling(ctx, appmodel.TeamBillingRequest{TeamID: 1, CallerID: 1, Rules: db.BillingRules{RoundMinutes: 15, RoundMode: "up", InvoicePrefix: "INV"}})
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	for pi, name := range []string{"Ромашка", "Лютик", "Acme"} {
		rate := 3000 + pi*1700
		p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: name, Slug: "", Color: "#7c3aed"})
		d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, RateCents: &rate})
		for ai := 0; ai < 3; ai++ {
			a, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: name + " задача " + strconv.Itoa(ai)})
			d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: a.ID, ProjectID: p.ID, CallerID: 1})
			for k := 0; k < 7; k++ {
				st := start.Add(time.Duration(pi*100+ai*10+k) * 97 * time.Minute)
				sess, _ := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: a.ID, Start: st, End: st.Add(time.Duration(5+k*13)*time.Minute + 17*time.Second)})
				if k%3 == 0 { // paused sessions carry their own tracked total
					d.TestSQL().ExecContext(ctx, `UPDATE sessions SET accumulated_seconds = ? WHERE id = ?`, 60*(k+1)+11, sess.ID)
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
