package reminders

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// crmStore serves due next steps and keeps the once-a-day claims in memory.
type crmStore struct {
	*fakeStore
	steps  []appmodel.CRMStep
	claims map[string]bool
}

func (s *crmStore) CRMStepsDue(context.Context, string) ([]appmodel.CRMStep, error) {
	return s.steps, nil
}

func (s *crmStore) ClaimCRMReminder(_ context.Context, claim appmodel.CRMReminderClaimRequest) (bool, error) {
	key := fmt.Sprintf("%d|%s|%d|%s|%s", claim.TeamID, claim.Kind, claim.ItemID, claim.Day, claim.Channel)
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}

type crmLetter struct {
	teamID, userID int64
	subject, text  string
}

// crmAudienceStub answers the topic switch, the language and the mailbox.
type crmAudienceStub struct {
	muted   map[int64]bool
	lang    map[int64]string
	letters []crmLetter
}

func (a *crmAudienceStub) Wanting(_ context.Context, _ int64, userIDs []int64, _ string) ([]int64, error) {
	var out []int64
	for _, id := range userIDs {
		if !a.muted[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (a *crmAudienceStub) Language(_ context.Context, userID int64) string { return a.lang[userID] }

func (a *crmAudienceStub) QueueLetter(_ context.Context, teamID, userID int64, subject, text string) error {
	a.letters = append(a.letters, crmLetter{teamID: teamID, userID: userID, subject: subject, text: text})
	return nil
}

func crmPass(t *testing.T, store *crmStore, audience *crmAudienceStub, rec *recorder, now time.Time) *Service {
	t.Helper()
	service, err := New(Dependencies{
		Store: store, Planner: fakePlanner{}, Sender: rec, Audience: audience,
		PublicURL: "https://paratrack.test", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func newCRMStore(steps ...appmodel.CRMStep) *crmStore {
	return &crmStore{fakeStore: &fakeStore{}, steps: steps, claims: map[string]bool{}}
}

// A deal whose next step is today reminds its owner once: one push and one letter.
// A second pass the same day sends nothing (CRM-15).
func TestCRMNextStepIsRemindedOncePerDay(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	store := newCRMStore(appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemDeal, ItemID: 9, Name: "Сайт", OwnerID: 5, NextStepAt: "2026-10-25"})
	audience := &crmAudienceStub{}
	rec := &recorder{}
	service := crmPass(t, store, audience, rec, now)

	if err := service.Pass(t.Context()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if len(rec.sent) != 1 {
		t.Fatalf("first pass sent %d pushes, want 1", len(rec.sent))
	}
	push := rec.sent[0]
	if push.Topic != TopicCRMNextStep || len(push.UserIDs) != 1 || push.UserIDs[0] != 5 || push.URL != "/crm" {
		t.Fatalf("push = %+v, want the owner, the next-step topic and /crm", push)
	}
	if !strings.Contains(push.Body, "Сделка «Сайт»: следующий шаг на 2026-10-25") {
		t.Fatalf("push body = %q, want the deal and its date", push.Body)
	}
	if len(audience.letters) != 1 {
		t.Fatalf("first pass queued %d letters, want 1", len(audience.letters))
	}
	letter := audience.letters[0]
	if letter.teamID != 1 || letter.userID != 5 || !strings.Contains(letter.text, "- Сделка «Сайт»") || !strings.Contains(letter.text, "https://paratrack.test/crm") {
		t.Fatalf("letter = %+v, want one letter to the owner listing the deal and linking /crm", letter)
	}

	if err := service.Pass(t.Context()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(rec.sent) != 1 || len(audience.letters) != 1 {
		t.Fatalf("second pass the same day sent again: %d pushes, %d letters", len(rec.sent), len(audience.letters))
	}
}

// A next step due tomorrow is not reminded today.
func TestCRMNextStepDueTomorrowIsNotReminded(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	store := newCRMStore(appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemLead, ItemID: 3, Name: "Лотос", OwnerID: 5, NextStepAt: "2026-10-26"})
	audience := &crmAudienceStub{}
	rec := &recorder{}

	if err := crmPass(t, store, audience, rec, now).Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rec.sent) != 0 || len(audience.letters) != 0 || len(store.claims) != 0 {
		t.Fatalf("a step due tomorrow was reminded: pushes %d, letters %d, claims %v", len(rec.sent), len(audience.letters), store.claims)
	}
}

// The workspace zone decides "today". At 22:30 UTC on the 24th it is already the
// 25th in Moscow, so a step due on the 25th is reminded in that workspace and not in
// the UTC one (CRM-15).
func TestCRMWorkspaceZoneDecidesToday(t *testing.T) {
	now := time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC)
	store := newCRMStore(
		appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemDeal, ItemID: 1, Name: "Москва", OwnerID: 5, NextStepAt: "2026-10-25", Zone: "Europe/Moscow"},
		appmodel.CRMStep{TeamID: 2, Kind: appmodel.CRMItemDeal, ItemID: 2, Name: "Лондон", OwnerID: 6, NextStepAt: "2026-10-25", Zone: ""},
	)
	audience := &crmAudienceStub{}
	rec := &recorder{}

	if err := crmPass(t, store, audience, rec, now).Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rec.sent) != 1 || rec.sent[0].UserIDs[0] != 5 {
		t.Fatalf("pushes = %+v, want only the Moscow workspace owner", rec.sent)
	}
	if len(audience.letters) != 1 || audience.letters[0].userID != 5 {
		t.Fatalf("letters = %+v, want only the Moscow workspace owner", audience.letters)
	}
	if store.claims["2|deal|2|2026-10-24|push"] {
		t.Fatal("the UTC workspace claimed a step that is not due in its day")
	}
}

// A person who switched the topic off gets neither the push nor the letter, and no
// claim is spent on them. The others still get theirs (CRM-15).
func TestCRMSwitchedOffTopicGetsNeitherPushNorLetter(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	store := newCRMStore(
		appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemDeal, ItemID: 9, Name: "Сайт", OwnerID: 5, NextStepAt: "2026-10-25"},
		appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemLead, ItemID: 3, Name: "Лотос", OwnerID: 6, NextStepAt: "2026-10-25"},
	)
	audience := &crmAudienceStub{muted: map[int64]bool{5: true}}
	rec := &recorder{}

	if err := crmPass(t, store, audience, rec, now).Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, push := range rec.sent {
		if push.UserIDs[0] == 5 {
			t.Fatalf("a push went to the owner who muted the topic: %+v", push)
		}
	}
	if len(rec.sent) != 1 || rec.sent[0].UserIDs[0] != 6 {
		t.Fatalf("pushes = %+v, want the one for the other owner", rec.sent)
	}
	if len(audience.letters) != 1 || audience.letters[0].userID != 6 {
		t.Fatalf("letters = %+v, want the one for the other owner", audience.letters)
	}
	if store.claims["1|deal|9|2026-10-25|push"] || store.claims["1|deal|9|2026-10-25|email"] {
		t.Fatal("a claim was spent on the owner who muted the topic")
	}
}

// Every due item of one owner goes into one letter, in that owner's language. A step
// that is not due yet is left out of it (CRM-15).
func TestCRMOneLetterListsEveryDueItemOfAnOwner(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	store := newCRMStore(
		appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemDeal, ItemID: 9, Name: "Сайт", OwnerID: 5, NextStepAt: "2026-10-25"},
		appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemLead, ItemID: 3, Name: "Лотос", OwnerID: 5, NextStepAt: "2026-10-24"},
		appmodel.CRMStep{TeamID: 1, Kind: appmodel.CRMItemDeal, ItemID: 10, Name: "Потом", OwnerID: 5, NextStepAt: "2026-10-27"},
	)
	audience := &crmAudienceStub{lang: map[int64]string{5: "en"}}
	rec := &recorder{}

	if err := crmPass(t, store, audience, rec, now).Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("pushes = %d, want one per due item", len(rec.sent))
	}
	if len(audience.letters) != 1 {
		t.Fatalf("letters = %d, want one per owner", len(audience.letters))
	}
	letter := audience.letters[0]
	if letter.subject != "Next steps for your deals and leads" {
		t.Fatalf("subject = %q, want the English letter for an English owner", letter.subject)
	}
	for _, want := range []string{`Deal "Сайт": next step on 2026-10-25`, `Lead "Лотос": next step on 2026-10-24`} {
		if !strings.Contains(letter.text, want) {
			t.Fatalf("letter text %q lacks %q", letter.text, want)
		}
	}
	if strings.Contains(letter.text, "Потом") {
		t.Fatalf("letter text lists a step that is not due: %q", letter.text)
	}
	if strings.Contains(letter.text, "\u2014") {
		t.Fatalf("letter text has an em dash: %q", letter.text)
	}
}
