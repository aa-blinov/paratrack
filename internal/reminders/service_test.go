package reminders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type fakeStore struct {
	goalCandidates []appmodel.GoalLimitCandidate
	goalClaims     map[string]bool
	goalProgress   []appmodel.GoalProgress
	due            []appmodel.DueProject
	reminded       []string
	claimed        map[string]bool
	managers       []int64
	managersByTeam map[int64][]int64
	teams          []int64
	// members are the people with no time on the day asked about; dayClaims keys team:user:day.
	members   []int64
	dayClaims map[string]bool
	zone      *time.Location
	// userZones holds the hand-set zone of each member; empty means they never chose.
	userZones map[int64]string
	// running are the timers running now; longClaims keys the sessions reminded.
	running      []appmodel.RunningSession
	longClaims   map[int64]bool
	budget       []appmodel.BudgetProject
	budgetClaims map[string]bool
}

func (f *fakeStore) BudgetProjects(context.Context) ([]appmodel.BudgetProject, error) {
	return f.budget, nil
}

func (f *fakeStore) ClaimBudgetNotice(_ context.Context, request appmodel.BudgetNoticeRequest) (bool, error) {
	if f.budgetClaims == nil {
		f.budgetClaims = map[string]bool{}
	}
	key := fmt.Sprintf("%d:%d:%s", request.ProjectID, request.Level, request.Basis)
	if f.budgetClaims[key] {
		return false, nil
	}
	f.budgetClaims[key] = true
	return true, nil
}

func (f *fakeStore) GoalLimitCandidates(context.Context) ([]appmodel.GoalLimitCandidate, error) {
	return f.goalCandidates, nil
}

func (f *fakeStore) ClaimGoalLimitNotice(_ context.Context, request appmodel.GoalLimitNoticeRequest) (bool, error) {
	if f.goalClaims == nil {
		f.goalClaims = map[string]bool{}
	}
	key := fmt.Sprintf("%d:%s:%d", request.GoalID, request.PeriodStart.Format(time.RFC3339), request.Level)
	if f.goalClaims[key] {
		return false, nil
	}
	f.goalClaims[key] = true
	return true, nil
}

func (f *fakeStore) ProgressForGoals(context.Context, appmodel.GoalProgressQuery) ([]appmodel.GoalProgress, error) {
	return f.goalProgress, nil
}

func (f *fakeStore) RunningSessions(context.Context) ([]appmodel.RunningSession, error) {
	var out []appmodel.RunningSession
	for _, r := range f.running {
		if !f.longClaims[r.SessionID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) ClaimLongTimerNotice(_ context.Context, request appmodel.LongTimerNoticeRequest) (bool, error) {
	sessionID := request.SessionID
	if f.longClaims == nil {
		f.longClaims = map[int64]bool{}
	}
	if f.longClaims[sessionID] {
		return false, nil
	}
	f.longClaims[sessionID] = true
	return true, nil
}

func (f *fakeStore) ProjectsDueBetween(_ context.Context, _, _ string) ([]appmodel.DueProject, error) {
	// The real read returns everything not marked yet, so a reminder the night
	// gate held back is still due — the same list, minus the marked ones.
	reminded := make(map[string]bool, len(f.reminded))
	for _, deadline := range f.reminded {
		reminded[deadline] = true
	}
	var out []appmodel.DueProject
	for _, project := range f.due {
		if !reminded[project.Deadline] {
			out = append(out, project)
		}
	}
	return out, nil
}
func (f *fakeStore) MarkDeadlineReminded(_ context.Context, request appmodel.DeadlineReminderRequest) error {
	f.reminded = append(f.reminded, request.Deadline)
	return nil
}
func (f *fakeStore) ManagerIDs(_ context.Context, teamID int64) ([]int64, error) {
	if byTeam, ok := f.managersByTeam[teamID]; ok {
		return byTeam, nil
	}
	return f.managers, nil
}
func (f *fakeStore) TasksDueOn(context.Context, string) ([]appmodel.DueTask, error) { return nil, nil }
func (f *fakeStore) MarkTaskDueReminded(context.Context, appmodel.TaskDueReminderRequest) error {
	return nil
}
func (f *fakeStore) TeamIDs(context.Context) ([]int64, error)                        { return f.teams, nil }
func (f *fakeStore) CRMStepsDue(context.Context, string) ([]appmodel.CRMStep, error) { return nil, nil }
func (f *fakeStore) ClaimCRMReminder(context.Context, appmodel.CRMReminderClaimRequest) (bool, error) {
	return false, nil
}
func (f *fakeStore) ClaimOverloadNotice(_ context.Context, request appmodel.OverloadNoticeRequest) (bool, error) {
	key := request.Week
	if f.claimed[key] {
		return false, nil
	}
	f.claimed[key] = true
	return true, nil
}

type fakePlanner struct{ snapshot appmodel.ScheduleSnapshot }

func (f fakePlanner) List(context.Context, appmodel.ScheduleQuery) (appmodel.ScheduleSnapshot, error) {
	return f.snapshot, nil
}

type recorder struct{ sent []Notification }

func (r *recorder) Send(_ context.Context, n Notification) error {
	r.sent = append(r.sent, n)
	return nil
}

// A deadline reminder goes to the managers and the lead, once. An overload
// reminder goes to the managers, once per person per week.
func TestPassSendsEachNudgeOnce(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{
		due:      []appmodel.DueProject{{TeamID: 1, ProjectID: 7, Name: "Сайт", Slug: "site", Deadline: "2026-11-04", LeadUserID: 5}},
		managers: []int64{1},
		teams:    []int64{1},
		claimed:  map[string]bool{},
	}
	planner := fakePlanner{snapshot: appmodel.ScheduleSnapshot{Rows: []appmodel.ScheduleViewRow{
		{ScheduleRow: appmodel.ScheduleRow{UserID: 5, UserName: "Маша", Total: 600}, AvailableMinutes: 480},
	}}}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: planner, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	// The planner answers the same rows for this week and the next, so the
	// overload is told for each week once.
	if len(rec.sent) != 3 {
		t.Fatalf("first pass sent %d notifications, want 3 (deadline and an overload per week)", len(rec.sent))
	}
	deadline := rec.sent[0]
	if deadline.Topic != TopicDeadline || len(deadline.UserIDs) != 2 {
		t.Fatalf("deadline reminder = %+v, want to the manager and the lead", deadline)
	}
	if len(store.reminded) != 1 || store.reminded[0] != "2026-11-04" {
		t.Fatalf("deadline not marked reminded: %v", store.reminded)
	}

	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(rec.sent) != 3 {
		t.Fatalf("second pass sent again: %d notifications in total, want still 3", len(rec.sent))
	}
}

// DayFillCandidates answers as the store does for the default week: nobody
// works a Saturday or a Sunday.
func (f *fakeStore) DayFillCandidates(_ context.Context, query appmodel.DayFillCandidatesQuery) ([]int64, error) {
	if day := query.From.Weekday(); day == time.Saturday || day == time.Sunday {
		return nil, nil
	}
	return f.members, nil
}

func (f *fakeStore) ClaimDayFillNotice(_ context.Context, request appmodel.DayFillNoticeRequest) (bool, error) {
	if f.dayClaims == nil {
		f.dayClaims = map[string]bool{}
	}
	key := fmt.Sprintf("%d:%d:%s", request.TeamID, request.UserID, request.Day)
	if f.dayClaims[key] {
		return false, nil
	}
	f.dayClaims[key] = true
	return true, nil
}

func (f *fakeStore) TeamLocation(context.Context, int64) (*time.Location, error) {
	if f.zone == nil {
		return time.UTC, nil
	}
	return f.zone, nil
}

func (f *fakeStore) UserTimeZones(_ context.Context, userIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(userIDs))
	for _, id := range userIDs {
		out[id] = f.userZones[id]
	}
	return out, nil
}

type funcLogger struct{ logf func(string, ...any) }

func (l funcLogger) Printf(format string, args ...any) { l.logf(format, args...) }

// failingDeadlinesStore breaks only the deadlines read; the task, overload and
// day sections still answer.
type failingDeadlinesStore struct {
	*fakeStore
	tasks []appmodel.DueTask
}

func (f *failingDeadlinesStore) ProjectsDueBetween(context.Context, string, string) ([]appmodel.DueProject, error) {
	return nil, errors.New("the deadlines table is unreachable")
}

func (f *failingDeadlinesStore) TasksDueOn(context.Context, string) ([]appmodel.DueTask, error) {
	return f.tasks, nil
}

// One failing section must not silence the rest: a broken deadlines read used
// to end the pass before the task nudges ran, and the swallowed error left no
// trace of why every nudge had stopped.
func TestAFailingSectionDoesNotStopTheOthers(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	base := &fakeStore{managers: []int64{1}, teams: []int64{1}, claimed: map[string]bool{}}
	store := &failingDeadlinesStore{fakeStore: base, tasks: []appmodel.DueTask{
		{TeamID: 1, TaskID: 9, AssigneeID: 5, Title: "Правки", Due: "2026-11-02"},
	}}
	rec := &recorder{}
	var logged []string
	service, err := New(Dependencies{
		Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now },
		Logger: funcLogger{func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = service.Pass(context.Background())
	if err == nil || !strings.Contains(err.Error(), "deadline reminders") {
		t.Fatalf("pass error = %v, want the deadline failure named in it", err)
	}
	if len(rec.sent) != 1 || rec.sent[0].Topic != TopicTaskDue {
		t.Fatalf("the failing deadlines section silenced the task nudge: %+v", rec.sent)
	}
	if len(logged) == 0 || !strings.Contains(logged[0], "deadline reminders") {
		t.Fatalf("the failure left no log line: %q", logged)
	}
}

// The fill-your-day nudge goes out once per person on a weekday evening in the
// workspace zone. Not in the morning, not at the weekend, not twice the same day.
func TestDayFillNudgesOncePerWeekdayEvening(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{teams: []int64{1}, members: []int64{5, 6}, claimed: map[string]bool{}, zone: tokyo}
	rec := &recorder{}
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC) // Wed 19:00 in Tokyo
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := service.Pass(ctx); err != nil {
		t.Fatalf("evening pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("evening pass sent %d notifications, want one per member (2)", len(rec.sent))
	}
	for i, want := range []int64{5, 6} {
		got := rec.sent[i]
		if got.Topic != TopicDayFill || len(got.UserIDs) != 1 || got.UserIDs[0] != want {
			t.Fatalf("notification %d = %+v, want the day fill nudge to member %d", i, got, want)
		}
	}

	if err := service.Pass(ctx); err != nil {
		t.Fatalf("second evening pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("second pass sent again: %d notifications in total, want still 2", len(rec.sent))
	}

	morning := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) // Wed 09:00 in Tokyo
	service.now = func() time.Time { return morning }
	store.dayClaims = map[string]bool{}
	if err := service.Pass(ctx); err != nil {
		t.Fatalf("morning pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("a morning pass sent a day fill nudge: %d in total", len(rec.sent))
	}

	saturday := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) // Sat 19:00 in Tokyo
	service.now = func() time.Time { return saturday }
	if err := service.Pass(ctx); err != nil {
		t.Fatalf("weekend pass: %v", err)
	}
	if len(rec.sent) != 2 {
		t.Fatalf("a weekend pass sent a day fill nudge: %d in total", len(rec.sent))
	}
}

type recordingMailer struct{ sent []Notification }

func (m *recordingMailer) Email(_ context.Context, n Notification) error {
	m.sent = append(m.sent, n)
	return nil
}

// The fill-your-day nudge goes by push and by email, the same nudge in both, and only in the window.
func TestDayFillAlsoGoesByEmail(t *testing.T) {
	store := &fakeStore{teams: []int64{1}, members: []int64{5}, claimed: map[string]bool{}}
	rec := &recorder{}
	mail := &recordingMailer{}
	now := time.Date(2026, 11, 11, 19, 0, 0, 0, time.UTC) // Wed 19:00, workspace zone UTC
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Mailer: mail, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Pass(context.Background()); err != nil {
		t.Fatalf("evening pass: %v", err)
	}
	if len(rec.sent) != 1 || len(mail.sent) != 1 {
		t.Fatalf("push %d, email %d; want one of each", len(rec.sent), len(mail.sent))
	}
	if mail.sent[0].Topic != TopicDayFill || mail.sent[0].Body != rec.sent[0].Body {
		t.Fatalf("email = %+v, want the same day fill nudge as the push", mail.sent[0])
	}
}

// The night gate (UX-33): however late the pass runs, a member whose local
// clock says night keeps their push until their morning. The workspace that
// stays awake hears its reminder in the night pass; the sleeping one is
// neither pushed nor claimed, and the next pass — its morning — delivers it.
func TestNightGateKeepsPushUntilTheRecipientsMorning(t *testing.T) {
	// 18:00 Tuesday in UTC is 03:00 Wednesday in Tokyo: the same pass is an
	// awake afternoon for the UTC workspace and night for the Tokyo one.
	now := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	store := &fakeStore{
		teams: []int64{1, 2},
		managersByTeam: map[int64][]int64{
			1: {5},
			2: {6},
		},
		claimed:   map[string]bool{},
		userZones: map[int64]string{5: "", 6: "Asia/Tokyo"},
		due: []appmodel.DueProject{
			{TeamID: 1, ProjectID: 7, Name: "Сайт", Slug: "site", Deadline: "2026-11-15", LeadUserID: 5},
			{TeamID: 2, ProjectID: 8, Name: "Платформа", Slug: "platform", Deadline: "2026-11-16", LeadUserID: 6},
		},
	}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := service.Pass(ctx); err != nil {
		t.Fatalf("night pass: %v", err)
	}
	if len(rec.sent) != 1 || rec.sent[0].UserIDs[0] != 5 {
		t.Fatalf("night pass sent %+v, want only the UTC workspace's reminder", rec.sent)
	}
	if len(store.reminded) != 1 || store.reminded[0] != "2026-11-15" {
		t.Fatalf("the night pass marked %v, want only the delivered deadline", store.reminded)
	}

	// 00:30 UTC is 09:30 in Tokyo: the Tokyo workspace hears its reminder now.
	morning := time.Date(2026, 11, 12, 0, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return morning }
	if err := service.Pass(ctx); err != nil {
		t.Fatalf("morning pass: %v", err)
	}
	if len(rec.sent) != 2 || rec.sent[1].UserIDs[0] != 6 {
		t.Fatalf("morning pass sent %+v, want the Tokyo reminder added", rec.sent)
	}
	if len(store.reminded) != 2 {
		t.Fatalf("both deadlines reminded: %v", store.reminded)
	}
}

// A timer running past its owner's threshold gets one reminder; under the
// threshold, or with the reminder turned off (0), it gets none.
func TestLongTimerReminder(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC) // Wed 15:00, workspace zone UTC
	at := func(hoursAgo int) *time.Time { v := now.Add(-time.Duration(hoursAgo) * time.Hour); return &v }
	store := &fakeStore{teams: []int64{1}, claimed: map[string]bool{}, running: []appmodel.RunningSession{
		{TeamID: 1, UserID: 5, SessionID: 51, Activity: "Вёрстка", LastResumeAt: at(5), ThresholdHours: 4},
		{TeamID: 1, UserID: 6, SessionID: 61, Activity: "Созвон", LastResumeAt: at(3), ThresholdHours: 4},
		{TeamID: 1, UserID: 7, SessionID: 71, Activity: "Чтение", LastResumeAt: at(9), ThresholdHours: 0},
		{TeamID: 1, UserID: 8, SessionID: 81, Activity: "Пауза", LastResumeAt: at(1), Accumulated: 5 * 3600, ThresholdHours: 4},
	}}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := service.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var long []Notification
	for _, n := range rec.sent {
		if n.Topic == TopicLongTimer {
			long = append(long, n)
		}
	}
	if len(long) != 2 || long[0].UserIDs[0] != 5 || long[1].UserIDs[0] != 8 {
		t.Fatalf("long timer reminders: %+v, want members 5 and 8", long)
	}
	if !strings.Contains(long[0].Title, "5 ч") || !strings.Contains(long[0].Body, "Вёрстка") {
		t.Fatalf("reminder text: %+v", long[0])
	}
	if err := service.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range rec.sent {
		if n.Topic == TopicLongTimer {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("a second pass reminded again: %d", count)
	}
}

// A project warns at four fifths of its estimate and again when it is used
// up, each once; the money budget counts at the project's rate.
func TestBudgetReminders(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{teams: []int64{1}, claimed: map[string]bool{}, managers: []int64{1}, budget: []appmodel.BudgetProject{
		{TeamID: 1, ProjectID: 10, Name: "Сайт", Slug: "sayt", EstimateMinutes: 600, TrackedSeconds: 8 * 3600},
		{TeamID: 1, ProjectID: 20, Name: "Лендинг", Slug: "landing", BudgetCents: 1000000, RateCents: 200000, TrackedSeconds: 6 * 3600, SpentCents: 1200000},
		{TeamID: 1, ProjectID: 30, Name: "Тихий", Slug: "quiet", EstimateMinutes: 600, TrackedSeconds: 2 * 3600},
	}}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := service.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	budget := func() []Notification {
		var out []Notification
		for _, n := range rec.sent {
			if n.Topic == TopicBudget {
				out = append(out, n)
			}
		}
		return out
	}
	got := budget()
	if len(got) != 2 || !strings.Contains(got[0].Body, "Сайт") || !strings.Contains(got[0].Body, "80%") || !strings.Contains(got[1].Title, "вышел") {
		t.Fatalf("budget warnings: %+v", got)
	}
	if err := service.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	if len(budget()) != 2 {
		t.Fatalf("a second pass warned again: %+v", budget())
	}
	store.budget[0].TrackedSeconds = 11 * 3600
	if err := service.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	if got := budget(); len(got) != 3 || !strings.Contains(got[2].Title, "вышел") {
		t.Fatalf("used up after four fifths: %+v", got)
	}
}

// A limit goal warns while it is used up: at 80% once and when crossed once,
// to whoever is timing the activity; a target goal is not warned about here.
func TestGoalLimitsWarnAtEightyAndWhenCrossed(t *testing.T) {
	sender := &recorder{}
	store := &fakeStore{
		goalCandidates: []appmodel.GoalLimitCandidate{{TeamID: 1, UserID: 7, ActivityID: 3}},
	}
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	svc, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: sender, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	period := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	limit := model.Goal{ID: 9, ActivityID: 3, Period: "daily", TargetMinutes: 60, Kind: "max"}
	store.goalProgress = []appmodel.GoalProgress{
		{Goal: limit, ActivityName: "Соцсети", AchievedMinutes: 50, PercentComplete: 83, PeriodStart: period},
		{Goal: model.Goal{ID: 10, ActivityID: 3, Period: "daily", TargetMinutes: 30, Kind: "min"}, AchievedMinutes: 50, PercentComplete: 166, PeriodStart: period},
	}
	for i := 0; i < 2; i++ {
		if err := svc.goalLimits(t.Context(), now); err != nil {
			t.Fatal(err)
		}
	}
	store.goalProgress[0].AchievedMinutes, store.goalProgress[0].PercentComplete = 61, 101
	if err := svc.goalLimits(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range sender.sent {
		titles = append(titles, n.Title)
		if len(n.UserIDs) != 1 || n.UserIDs[0] != 7 {
			t.Fatalf("sent to %v, want the person timing it", n.UserIDs)
		}
	}
	if strings.Join(titles, "|") != "Лимит почти исчерпан|Лимит превышен" {
		t.Fatalf("limit pushes: %q", titles)
	}
}

// What the schedule still plans counts: a project not over yet, whose plan
// would take it over, is warned about before it happens; raising the
// estimate lets the warnings come again for the new figure.
func TestBudgetForecastAndRaisedEstimate(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
	store := &fakeStore{teams: []int64{1}, claimed: map[string]bool{}, managers: []int64{1}, budget: []appmodel.BudgetProject{
		{TeamID: 1, ProjectID: 10, Name: "Сайт", Slug: "sayt", EstimateMinutes: 600, TrackedSeconds: 5 * 3600, PlannedMinutes: 6 * 60},
	}}
	rec := &recorder{}
	service, err := New(Dependencies{Store: store, Planner: fakePlanner{}, Sender: rec, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rec.sent) != 1 || !strings.Contains(rec.sent[0].Title, "По плану") || !strings.Contains(rec.sent[0].Body, "110%") {
		t.Fatalf("forecast warning: %+v", rec.sent)
	}
	store.budget[0].EstimateMinutes = 1200
	store.budget[0].TrackedSeconds = 17 * 3600
	if err := service.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rec.sent) != 2 || !strings.Contains(rec.sent[1].Title, "выйдет") {
		t.Fatalf("after raising the estimate: %+v", rec.sent)
	}
}
