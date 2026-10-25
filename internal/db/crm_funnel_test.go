package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The funnel test period: the week of 20 to 26 October 2026, in UTC.
var (
	funnelFrom = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	funnelTo   = time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
)

const funnelToday = "2026-10-25"

// atMoment sets the clock the next write reads.
func atMoment(d *DB, when time.Time) {
	d.now = func() time.Time { return when }
}

// makeDeal records a deal at when, with the given caller, stage and fields.
func makeDeal(t *testing.T, d *DB, when time.Time, req appmodel.CRMDealCreateRequest) model.CRMDeal {
	t.Helper()
	atMoment(d, when)
	deal, err := d.CreateCRMDeal(t.Context(), req)
	if err != nil {
		t.Fatalf("create deal %q: %v", req.Title, err)
	}
	return deal
}

// moveDeal moves a deal to stage at when.
func moveDeal(t *testing.T, d *DB, w richWorkspace, when time.Time, caller, dealID int64, stage, reason string) {
	t.Helper()
	atMoment(d, when)
	if _, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{TeamID: w.teamID, CallerID: caller, DealID: dealID, Stage: stage, LostReason: reason}); err != nil {
		t.Fatalf("move deal %d to %s: %v", dealID, stage, err)
	}
}

// funnelReport reads the report of a period for one caller, today being funnelToday.
func funnelReport(t *testing.T, d *DB, w richWorkspace, caller int64, from, to time.Time) model.CRMFunnel {
	t.Helper()
	report, err := d.CRMFunnelReport(t.Context(), appmodel.CRMFunnelQuery{
		TeamID: w.teamID, CallerID: caller, From: from, To: to, Today: funnelToday,
	})
	if err != nil {
		t.Fatalf("funnel report: %v", err)
	}
	return report
}

// stageTotal finds one stage of the pipeline by key.
func stageTotal(t *testing.T, report model.CRMFunnel, key string) model.CRMFunnelStage {
	t.Helper()
	for _, stage := range report.Pipeline {
		if stage.Key == key {
			return stage
		}
	}
	t.Fatalf("pipeline has no stage %q: %+v", key, report.Pipeline)
	return model.CRMFunnelStage{}
}

// The pipeline lists the workspace's stages in order, and each count is the number of deals in
// that stage, the same count the deal list gives.
func TestCRMFunnelPipelineCountsMatchTheDeals(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-pipeline")
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "A"})
	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "B"})
	c := makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "C"})
	moveDeal(t, d, w, day, w.ownerID, c.ID, model.DealStageProposal, "")

	report := funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo)
	wantOrder := []string{model.DealStageNew, model.DealStageWorking, model.DealStageProposal, model.DealStageWon, model.DealStageLost}
	for i, key := range wantOrder {
		if report.Pipeline[i].Key != key {
			t.Fatalf("pipeline order %d = %q, want %q", i, report.Pipeline[i].Key, key)
		}
	}
	deals, err := d.ListCRMDeals(t.Context(), appmodel.CRMDealListQuery{TeamID: w.teamID, CallerID: w.ownerID})
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range report.Pipeline {
		var want int64
		for _, deal := range deals {
			if deal.Stage == stage.Key {
				want++
			}
		}
		if stage.Total.Count != want {
			t.Fatalf("stage %s count = %d, deals say %d", stage.Key, stage.Total.Count, want)
		}
	}
	if got := stageTotal(t, report, model.DealStageNew).Total.Count; got != 2 {
		t.Fatalf("new count = %d, want 2", got)
	}
}

// A deal that became won inside the period counts once, however often it moved within the period.
// A deal that became won before the period does not count, and a move within the same stage
// does not restart the stage's time.
func TestCRMFunnelWonInThePeriodCountsOnce(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-won")

	inside := makeDeal(t, d, time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC),
		appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Внутри", AmountCents: 100000, Currency: "RUB"})
	moveDeal(t, d, w, time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), w.ownerID, inside.ID, model.DealStageWon, "")
	moveDeal(t, d, w, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), w.ownerID, inside.ID, model.DealStageWorking, "")
	moveDeal(t, d, w, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), w.ownerID, inside.ID, model.DealStageWon, "")

	before := makeDeal(t, d, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC),
		appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Раньше", AmountCents: 50000, Currency: "RUB"})
	moveDeal(t, d, w, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), w.ownerID, before.ID, model.DealStageWon, "")

	report := funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo)
	if report.Won.Count != 1 {
		t.Fatalf("won in the period = %d, want 1", report.Won.Count)
	}
	if len(report.Won.Amounts) != 1 || report.Won.Amounts[0].AmountCents != 100000 {
		t.Fatalf("won amounts = %+v, want the one deal's 100000", report.Won.Amounts)
	}

	// The same-stage move on the 25th leaves the time at the 23rd, so a period ending on the 24th
	// still holds the deal.
	moveDeal(t, d, w, time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), w.ownerID, inside.ID, model.DealStageWon, "")
	early := funnelReport(t, d, w, w.ownerID, funnelFrom, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if early.Won.Count != 1 {
		t.Fatalf("won before the 24th = %d, want 1: a same-stage move restarted the stage time", early.Won.Count)
	}
}

// A custom stage counts as the kind it was given, under its own key.
func TestCRMFunnelCustomStageCountsByKind(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-custom")
	signed, err := d.CreateCRMStage(t.Context(), appmodel.CRMStageCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Name: "Подписан", Kind: model.StageKindWon})
	if err != nil {
		t.Fatalf("create the custom stage: %v", err)
	}
	deal := makeDeal(t, d, time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC),
		appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Контракт", Stage: model.DealStageNew, AmountCents: 70000, Currency: "RUB"})
	moveDeal(t, d, w, time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), w.ownerID, deal.ID, signed.Key, "")

	report := funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo)
	if report.Won.Count != 1 {
		t.Fatalf("won with a custom won-kind stage = %d, want 1", report.Won.Count)
	}
	if got := stageTotal(t, report, signed.Key).Total.Count; got != 1 {
		t.Fatalf("custom stage count = %d, want 1", got)
	}
}

// Overdue counts open deals whose next step is before today. A won or lost deal is never overdue.
func TestCRMFunnelOverdueIgnoresWonAndLost(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-overdue")
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Открыта", NextStepAt: "2026-10-20"})
	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Сегодня", NextStepAt: funnelToday})
	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Выиграна", Stage: model.DealStageWon, NextStepAt: "2026-10-20"})
	lost := makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Проиграна", NextStepAt: "2026-10-20"})
	moveDeal(t, d, w, day, w.ownerID, lost.ID, model.DealStageLost, "дорого")

	report := funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo)
	if report.Overdue.Count != 1 {
		t.Fatalf("overdue = %d, want 1 (the open deal dated before today)", report.Overdue.Count)
	}
}

// Amounts are summed per currency. Two currencies give two entries, never one mixed sum.
func TestCRMFunnelSumsStayInTheirCurrency(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-currency")
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Рубли", AmountCents: 100000, Currency: "RUB"})
	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Доллары", AmountCents: 5000, Currency: "USD"})
	makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Ещё рубли", AmountCents: 20000, Currency: "RUB"})

	stage := stageTotal(t, funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo), model.DealStageNew)
	if stage.Total.Count != 3 {
		t.Fatalf("new count = %d, want 3", stage.Total.Count)
	}
	want := []model.CRMAmount{{Currency: "RUB", AmountCents: 120000}, {Currency: "USD", AmountCents: 5000}}
	if len(stage.Total.Amounts) != len(want) {
		t.Fatalf("amounts = %+v, want %+v", stage.Total.Amounts, want)
	}
	for i := range want {
		if stage.Total.Amounts[i] != want[i] {
			t.Fatalf("amount %d = %+v, want %+v", i, stage.Total.Amounts[i], want[i])
		}
	}
}

// A member reads the same counts as a manager, and no amount at all.
func TestCRMFunnelMemberSeesCountsOnly(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-member")
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	won := makeDeal(t, d, day, appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Сумма", AmountCents: 90000, Currency: "RUB", NextStepAt: "2026-10-20"})
	moveDeal(t, d, w, day, w.ownerID, won.ID, model.DealStageWon, "")

	manager := funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo)
	member := funnelReport(t, d, w, w.memberID, funnelFrom, funnelTo)
	if !manager.ShowAmounts || member.ShowAmounts {
		t.Fatalf("show amounts: manager %v, member %v", manager.ShowAmounts, member.ShowAmounts)
	}
	if manager.Won.Count != 1 || member.Won.Count != 1 || member.Overdue.Count != manager.Overdue.Count {
		t.Fatalf("counts differ: manager %+v, member %+v", manager.Won, member.Won)
	}
	if len(manager.Won.Amounts) != 1 {
		t.Fatalf("manager won amounts = %+v, want one entry", manager.Won.Amounts)
	}
	if len(member.Won.Amounts) != 0 || len(member.Overdue.Amounts) != 0 || len(member.Lost.Amounts) != 0 {
		t.Fatalf("a member got amounts: %+v", member)
	}
	for _, stage := range member.Pipeline {
		if len(stage.Total.Amounts) != 0 {
			t.Fatalf("member pipeline stage %s has amounts %+v", stage.Key, stage.Total.Amounts)
		}
	}
}

// A lead's source is counted as typed, and an empty source is a row of its own. A lead converted
// in the period counts toward the period's conversion, and the response figures are the CRM-16
// stats for the same period.
func TestCRMFunnelLeadSourcesAndResponseMatchTheStats(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-leads")
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

	lead := func(when time.Time, company, source string) model.CRMLead {
		t.Helper()
		atMoment(d, when)
		l, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Company: company, Source: source})
		if err != nil {
			t.Fatalf("create lead %s: %v", company, err)
		}
		return l
	}
	first := lead(day, "Лотос", "")
	lead(day, "Ромашка", "")
	lead(day, "Сайт", "сайт")
	lead(time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC), "Старый", "сайт")

	logLeadActivity(t, d, w, first.ID, day.Add(time.Hour))
	atMoment(d, day.Add(2*time.Hour))
	if _, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: w.teamID, CallerID: w.ownerID, LeadID: first.ID}); err != nil {
		t.Fatalf("convert the lead: %v", err)
	}

	report := funnelReport(t, d, w, w.ownerID, funnelFrom, funnelTo)
	if report.LeadsCreated != 3 || report.LeadsConverted != 1 {
		t.Fatalf("leads created %d converted %d, want 3 and 1", report.LeadsCreated, report.LeadsConverted)
	}
	wantSources := []model.CRMLeadSource{{Source: "", Created: 2}, {Source: "сайт", Created: 1}}
	if len(report.LeadSources) != len(wantSources) {
		t.Fatalf("sources = %+v, want %+v", report.LeadSources, wantSources)
	}
	for i := range wantSources {
		if report.LeadSources[i] != wantSources[i] {
			t.Fatalf("source %d = %+v, want %+v", i, report.LeadSources[i], wantSources[i])
		}
	}
	// Statuses are the state now, so the lead created before the period is in them too.
	if report.LeadStatuses[model.LeadStatusConverted] != 1 || report.LeadStatuses[model.LeadStatusNew] != 3 {
		t.Fatalf("lead statuses = %+v", report.LeadStatuses)
	}

	stats, err := d.CRMLeadResponseStats(t.Context(), appmodel.CRMLeadResponseStatsQuery{
		TeamID: w.teamID, CallerID: w.ownerID, From: funnelFrom, To: funnelTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Response != stats {
		t.Fatalf("response in the report = %+v, the stats say %+v", report.Response, stats)
	}
	if report.Response.Created != 3 || report.Response.WithinTarget != 1 {
		t.Fatalf("response = %+v, want 3 created and 1 within the target", report.Response)
	}
}

// The stage time of rows written before the column is their updated_at. The backfill runs once:
// a row that gets no time later is not filled again.
func TestCRMStageTimesBackfillRunsOnce(t *testing.T) {
	d := openTestDB(t)
	w := seedRichWorkspace(t, d, "crm-funnel-backfill")
	deal := makeDeal(t, d, time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC),
		appmodel.CRMDealCreateRequest{TeamID: w.teamID, CallerID: w.ownerID, Title: "Старая"})
	ctx := t.Context()
	sql := d.TestSQL()
	if _, err := sql.ExecContext(ctx, `UPDATE crm_deals SET stage_changed_at = NULL, updated_at = '2026-10-22T08:00:00Z' WHERE id = ?`, deal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sql.ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = '20261025_crm_stage_times'`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateCRMStageTimes(ctx); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := sql.QueryRowContext(ctx, `SELECT stage_changed_at FROM crm_deals WHERE id = ?`, deal.ID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "2026-10-22T08:00:00Z" {
		t.Fatalf("backfilled stage time = %q, want the updated_at", got)
	}

	if _, err := sql.ExecContext(ctx, `UPDATE crm_deals SET stage_changed_at = NULL WHERE id = ?`, deal.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateCRMStageTimes(ctx); err != nil {
		t.Fatal(err)
	}
	var again *string
	if err := sql.QueryRowContext(ctx, `SELECT stage_changed_at FROM crm_deals WHERE id = ?`, deal.ID).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Fatalf("a second run filled the stage time again: %q", *again)
	}
}
