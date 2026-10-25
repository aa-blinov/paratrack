package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// leadNamed reads one lead of the workspace back, the way the leads list does.
func leadNamed(t *testing.T, d *DB, ws richWorkspace, leadID int64) model.CRMLead {
	t.Helper()
	leads, err := d.ListCRMLeads(t.Context(), appmodel.CRMLeadListQuery{TeamID: ws.teamID, CallerID: ws.memberID})
	if err != nil {
		t.Fatal(err)
	}
	for _, lead := range leads {
		if lead.ID == leadID {
			return lead
		}
	}
	t.Fatalf("lead %d is not in the list", leadID)
	return model.CRMLead{}
}

// logLeadActivity logs an activity on a lead, dated at occurred, with the clock at created.
func logLeadActivity(t *testing.T, d *DB, ws richWorkspace, leadID int64, occurred time.Time) {
	t.Helper()
	// An activity is logged when it happened, so the clock stands at its moment.
	d.now = func() time.Time { return occurred }
	if _, err := d.CreateCRMActivity(t.Context(), appmodel.CRMActivityCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, LeadID: leadID, Kind: "call",
		Body: "Позвонил", OccurredAt: occurred,
	}); err != nil {
		t.Fatalf("log an activity on lead %d: %v", leadID, err)
	}
}

// The first activity on a lead sets its first response once; a later activity leaves it.
// A lead with no activity has no response yet (CRM-16).
func TestCRMLeadFirstResponseIsSetOnceByTheFirstActivity(t *testing.T) {
	d := openTestDB(t)
	created := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return created }
	ws := seedRichWorkspace(t, d, "crm-response-once")

	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Лотос"})
	if err != nil {
		t.Fatal(err)
	}
	if got := leadNamed(t, d, ws, lead.ID); !got.FirstResponseAt.IsZero() || got.ResponseTime() != 0 {
		t.Fatalf("a lead with no activity has a response: %+v", got)
	}

	first := created.Add(90 * time.Minute)
	logLeadActivity(t, d, ws, lead.ID, first)
	if got := leadNamed(t, d, ws, lead.ID); !got.FirstResponseAt.Equal(first) {
		t.Fatalf("first response = %v, want %v", got.FirstResponseAt, first)
	}

	logLeadActivity(t, d, ws, lead.ID, created.Add(5*time.Hour))
	if got := leadNamed(t, d, ws, lead.ID); !got.FirstResponseAt.Equal(first) {
		t.Fatalf("a later activity moved the first response to %v", got.FirstResponseAt)
	}
}

// The response time counts from creation to the first activity. Ninety minutes is within the
// two-hour target, three hours is late (CRM-16).
func TestCRMLeadResponseTimeAgainstTheTarget(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-response-target")
	created := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		company  string
		after    time.Duration
		within   bool
		response time.Duration
	}{
		{"Вовремя", 90 * time.Minute, true, 90 * time.Minute},
		{"Поздно", 3 * time.Hour, false, 3 * time.Hour},
		{"Ровно", 2 * time.Hour, true, 2 * time.Hour},
	}
	for _, c := range cases {
		d.now = func() time.Time { return created }
		lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: c.company})
		if err != nil {
			t.Fatal(err)
		}
		logLeadActivity(t, d, ws, lead.ID, created.Add(c.after))
		got := leadNamed(t, d, ws, lead.ID)
		if got.ResponseTime() != c.response {
			t.Fatalf("%s: response = %v, want %v", c.company, got.ResponseTime(), c.response)
		}
		if within := got.ResponseTime() <= model.LeadResponseTargetMinutes*time.Minute; within != c.within {
			t.Fatalf("%s: within target = %v, want %v", c.company, within, c.within)
		}
	}

	// A lead with no activity is still waiting, and its age is counted from creation.
	d.now = func() time.Time { return created }
	waiting, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Ждёт"})
	if err != nil {
		t.Fatal(err)
	}
	if got := leadNamed(t, d, ws, waiting.ID); !got.FirstResponseAt.IsZero() || !got.CreatedAt.Equal(created) {
		t.Fatalf("waiting lead = %+v, want no response and the creation time", got)
	}
}

// An activity dated before its lead existed answers at once, never with a negative time.
func TestCRMLeadResponseBeforeCreationCountsAsNone(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-response-backdated")
	created := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return created }
	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Ранняя"})
	if err != nil {
		t.Fatal(err)
	}
	logLeadActivity(t, d, ws, lead.ID, created.Add(-time.Hour))
	if got := leadNamed(t, d, ws, lead.ID); got.ResponseTime() != 0 {
		t.Fatalf("response before creation = %v, want 0", got.ResponseTime())
	}
}

// The stats count the leads created in the period on the workspace's day, how many were answered
// and how many within the target, and the median of the answered ones. A lead created on the
// next workspace day is outside the period, even though its UTC day is inside it.
func TestCRMLeadResponseStatsCountsCreatedWithinTargetAndMedian(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-response-stats")
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE teams SET timezone = 'Europe/Moscow' WHERE id = ?`, ws.teamID); err != nil {
		t.Fatal(err)
	}
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	lead := func(createdAt time.Time, answerAfter time.Duration) {
		t.Helper()
		d.now = func() time.Time { return createdAt }
		l, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Лид"})
		if err != nil {
			t.Fatal(err)
		}
		if answerAfter > 0 {
			logLeadActivity(t, d, ws, l.ID, createdAt.Add(answerAfter))
		}
	}
	// Inside the period: 13:00 Moscow on the 20th, 90 minutes; 01:30 Moscow on the 21st, 3 hours;
	// 09:00 Moscow on the 22nd, no answer.
	lead(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), 90*time.Minute)
	lead(time.Date(2026, 10, 20, 22, 30, 0, 0, time.UTC), 3*time.Hour)
	lead(time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC), 0)
	// Outside: 01:00 Moscow on the 26th, although it is the 25th in UTC.
	lead(time.Date(2026, 10, 25, 22, 0, 0, 0, time.UTC), 30*time.Minute)

	stats, err := d.CRMLeadResponseStats(t.Context(), appmodel.CRMLeadResponseStatsQuery{
		TeamID: ws.teamID, CallerID: ws.memberID,
		From: time.Date(2026, 10, 20, 0, 0, 0, 0, moscow), To: time.Date(2026, 10, 26, 0, 0, 0, 0, moscow),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := model.CRMLeadResponseStats{Created: 3, Answered: 2, WithinTarget: 1, MedianMinutes: 135}
	if stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}

	empty, err := d.CRMLeadResponseStats(t.Context(), appmodel.CRMLeadResponseStatsQuery{
		TeamID: ws.teamID, CallerID: ws.memberID,
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, moscow), To: time.Date(2026, 1, 2, 0, 0, 0, 0, moscow),
	})
	if err != nil {
		t.Fatal(err)
	}
	if empty != (model.CRMLeadResponseStats{}) {
		t.Fatalf("an empty period = %+v, want zeros", empty)
	}
}

// Any member reads the numbers, as the leads list does. A person outside the workspace does not.
func TestCRMLeadResponseStatsSeenByAMemberOnly(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-response-member")
	d.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	if _, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Лотос"}); err != nil {
		t.Fatal(err)
	}
	period := appmodel.CRMLeadResponseStatsQuery{
		TeamID: ws.teamID, From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}

	period.CallerID = ws.memberID
	stats, err := d.CRMLeadResponseStats(t.Context(), period)
	if err != nil || stats.Created != 1 {
		t.Fatalf("a member reads the stats: %+v, %v", stats, err)
	}

	outsider := seedApprovalUser(t, d, "crm-response-outsider@test.local")
	period.CallerID = outsider
	if _, err := d.CRMLeadResponseStats(t.Context(), period); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a person outside the workspace read the stats: %v", err)
	}
}

// The backfill gives a lead that already had an activity its first response: the first activity
// logged, which is the rule the live path follows. It runs once.
func TestCRMLeadFirstResponseBackfillFillsLeadsThatAlreadyHadAnActivity(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-response-backfill")
	created := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return created }
	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Старый"})
	if err != nil {
		t.Fatal(err)
	}
	silent, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Молчун"})
	if err != nil {
		t.Fatal(err)
	}

	// Activities written before the column existed: the first logged is dated 10:15, the second
	// is dated earlier, at 09:20, so the backfill must take the first logged, not the earliest.
	first := created.Add(75 * time.Minute)
	for _, occurred := range []time.Time{first, created.Add(20 * time.Minute)} {
		if _, err := d.TestSQL().ExecContext(t.Context(),
			`INSERT INTO crm_activities (team_id, lead_id, kind, body, result, occurred_at, author_id, created_at)
			 VALUES (?, ?, 'call', 'Старая запись', '', ?, ?, ?)`,
			ws.teamID, lead.ID, FormatTime(occurred), ws.memberID, FormatTime(created)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE crm_leads SET first_response_at = NULL WHERE team_id = ?`, ws.teamID); err != nil {
		t.Fatal(err)
	}

	const name = "20261025_lead_first_response"
	if _, err := d.TestSQL().ExecContext(t.Context(), `DELETE FROM paratrack_migrations WHERE name = ?`, name); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateLeadFirstResponses(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := leadNamed(t, d, ws, lead.ID); !got.FirstResponseAt.Equal(first) {
		t.Fatalf("backfilled first response = %v, want %v", got.FirstResponseAt, first)
	}
	if got := leadNamed(t, d, ws, silent.ID); !got.FirstResponseAt.IsZero() {
		t.Fatalf("a lead with no activity got a response: %v", got.FirstResponseAt)
	}

	// Once recorded, the backfill does not run again: a cleared value stays clear.
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE crm_leads SET first_response_at = NULL WHERE id = ?`, lead.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateLeadFirstResponses(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := leadNamed(t, d, ws, lead.ID); !got.FirstResponseAt.IsZero() {
		t.Fatalf("the backfill ran a second time and set %v", got.FirstResponseAt)
	}
}

// An activity dated in the future cannot make a lead answered before the moment it was
// logged: the response counts from now.
func TestCRMLeadFutureActivityCountsAsAnsweredNow(t *testing.T) {
	d := openTestDB(t)
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }
	ws := seedRichWorkspace(t, d, "crm-response-future")
	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Флокс", Email: "f@flox.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateCRMActivity(t.Context(), appmodel.CRMActivityCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID, Kind: "call", Body: "Позвонил", OccurredAt: now.Add(48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	var responded string
	if err := d.TestSQL().QueryRowContext(t.Context(), `SELECT first_response_at FROM crm_leads WHERE id = ?`, lead.ID).Scan(&responded); err != nil {
		t.Fatal(err)
	}
	if responded != FormatTime(now) {
		t.Fatalf("first response = %q, want the moment it was logged %q", responded, FormatTime(now))
	}
}
