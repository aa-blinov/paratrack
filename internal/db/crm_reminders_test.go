package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The next-step list keeps only what can still be worked: open deals and
// unfinished leads with an owner. A won or lost deal, and a lost or converted lead,
// never reaches the reminder pass (CRM-15).
func TestCRMStepsDueKeepsOnlyWorkableDealsAndLeads(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	ws := seedRichWorkspace(t, d, "crm-steps-due")
	if _, err := d.TestSQL().ExecContext(t.Context(), `UPDATE teams SET timezone = 'Europe/Moscow' WHERE id = ?`, ws.teamID); err != nil {
		t.Fatal(err)
	}
	deal := func(title, stage, reason string) {
		t.Helper()
		created, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
			TeamID: ws.teamID, CallerID: ws.memberID, Title: title, Stage: model.DealStageNew, NextStepAt: "2026-10-25",
		})
		if err != nil {
			t.Fatalf("deal %q: %v", title, err)
		}
		if stage == model.DealStageNew {
			return
		}
		if _, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{
			TeamID: ws.teamID, CallerID: ws.memberID, DealID: created.ID, Stage: stage, LostReason: reason,
		}); err != nil {
			t.Fatalf("move %q to %s: %v", title, stage, err)
		}
	}
	deal("Открытая", model.DealStageNew, "")
	deal("Выиграна", model.DealStageWon, "")
	deal("Проиграна", model.DealStageLost, "нет бюджета")

	fresh, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Лотос", NextStepAt: "2026-10-25"})
	if err != nil {
		t.Fatal(err)
	}
	lost, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Ирис", NextStepAt: "2026-10-25"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lost.ID, Status: model.LeadStatusLost, LostReason: "нет бюджета"}); err != nil {
		t.Fatal(err)
	}
	converted, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Роза", NextStepAt: "2026-10-25"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: ws.teamID, CallerID: ws.ownerID, LeadID: converted.ID}); err != nil {
		t.Fatalf("convert: %v", err)
	}

	steps, err := d.CRMStepsDue(t.Context(), "2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	// The open deal, and the deal the conversion made (it is open now); the lead that
	// was converted, the lead that was lost and the won and lost deals stay out.
	var deals, leads []appmodel.CRMStep
	for _, step := range steps {
		if step.Kind == appmodel.CRMItemDeal {
			deals = append(deals, step)
		} else {
			leads = append(leads, step)
		}
	}
	if len(deals) != 2 || deals[0].Name != "Открытая" || deals[0].OwnerID != ws.memberID || deals[1].Name != "Роза" {
		t.Fatalf("deals due = %+v, want the open deal and the deal the conversion made", deals)
	}
	if len(leads) != 1 || leads[0].ItemID != fresh.ID || leads[0].Name != "Лотос" {
		t.Fatalf("leads due = %+v, want only the new lead, named by its company", leads)
	}
	for _, step := range steps {
		if step.Zone != "Europe/Moscow" {
			t.Fatalf("step %+v: zone = %q, want the workspace zone the pass reads the day in", step, step.Zone)
		}
	}

	later, err := d.CRMStepsDue(t.Context(), "2026-10-24")
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 0 {
		t.Fatalf("steps due before their day = %+v, want none", later)
	}

	// The member leaves the workspace: their deals and leads are no longer reminded.
	if _, err := d.TestSQL().ExecContext(t.Context(), `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, ws.teamID, ws.memberID); err != nil {
		t.Fatal(err)
	}
	left, err := d.CRMStepsDue(t.Context(), "2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("steps of a former member = %+v, want none", left)
	}
}

// A claim is the once-a-day gate: the same item, channel and day is claimed once,
// while another channel or another day is a new claim (CRM-15).
func TestCRMReminderClaimIsOncePerItemChannelAndDay(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	ws := seedRichWorkspace(t, d, "crm-reminder-claim")
	claim := appmodel.CRMReminderClaimRequest{TeamID: ws.teamID, Kind: appmodel.CRMItemDeal, ItemID: 7, Day: "2026-10-25", Channel: appmodel.CRMChannelPush}

	want := []struct {
		name  string
		claim appmodel.CRMReminderClaimRequest
		ok    bool
	}{
		{"first claim of the day", claim, true},
		{"second claim the same day", claim, false},
		{"the letter channel is claimed on its own", withChannel(claim, appmodel.CRMChannelEmail), true},
		{"the next day is a new claim", withDay(claim, "2026-10-26"), true},
	}
	for _, step := range want {
		got, err := d.ClaimCRMReminder(t.Context(), step.claim)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got != step.ok {
			t.Fatalf("%s: claimed = %v, want %v", step.name, got, step.ok)
		}
	}
}

func withChannel(claim appmodel.CRMReminderClaimRequest, channel string) appmodel.CRMReminderClaimRequest {
	claim.Channel = channel
	return claim
}

func withDay(claim appmodel.CRMReminderClaimRequest, day string) appmodel.CRMReminderClaimRequest {
	claim.Day = day
	return claim
}

// A reminder letter waits in the invoice outbox and is delivered from it. It has no
// invoice, so completing it changes no invoice (CRM-15).
func TestMailReminderLetterIsDeliveredFromTheOutboxWithoutAnInvoice(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	ws := seedRichWorkspace(t, d, "mail-reminder-letter")
	payload := []byte(`{"version":1,"To":"owner@test.local","Subject":"Следующие шаги","Text":"Сделка «Открытая»"}`)
	if err := d.EnqueueReminderEmail(t.Context(), appmodel.ReminderEmailEnqueueRequest{TeamID: ws.teamID, Recipient: "owner@test.local", Payload: payload}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, ok, err := d.ClaimInvoiceEmail(t.Context())
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v, want the reminder letter", ok, err)
	}
	if job.InvoiceID != 0 || job.TeamID != ws.teamID || job.Recipient != "owner@test.local" {
		t.Fatalf("job = %+v, want the team and recipient with no invoice", job)
	}
	if err := d.CompleteInvoiceEmail(t.Context(), job); err != nil {
		t.Fatalf("complete a letter with no invoice: %v", err)
	}
	if _, ok, _ := d.ClaimInvoiceEmail(t.Context()); ok {
		t.Fatal("a delivered letter was claimed again")
	}
}

// Next-step reminders are a topic a person can switch off, like the others (CRM-15).
func TestNotifCRMNextStepTopicCanBeMuted(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "notif-crm-topic")
	ctx := requestctx.WithActor(t.Context(), ws.memberID)
	topic := appmodel.NotificationTopicCRMNextStep
	if err := d.SetMutedNotificationTopics(ctx, appmodel.NotificationTopicsCommand{
		TeamID: ws.teamID, UserID: ws.memberID, CallerID: ws.memberID, Topics: []string{topic},
	}); err != nil {
		t.Fatalf("mute the next-step topic: %v", err)
	}
	muted, err := d.MutedNotificationTopics(ctx, appmodel.NotificationTopicsQuery{TeamID: ws.teamID, UserID: ws.memberID})
	if err != nil {
		t.Fatal(err)
	}
	if len(muted) != 1 || muted[0] != topic {
		t.Fatalf("muted = %v, want the next-step topic", muted)
	}
	if err := d.SetMutedNotificationTopics(ctx, appmodel.NotificationTopicsCommand{
		TeamID: ws.teamID, UserID: ws.memberID, CallerID: ws.memberID, Topics: []string{"crm.unknown"},
	}); !errors.Is(err, appmodel.ErrInvalidNotificationTopic) {
		t.Fatalf("unknown topic = %v, want ErrInvalidNotificationTopic", err)
	}
}
