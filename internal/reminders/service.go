// Package reminders sends the nudges that need no app to be open: a project
// deadline is close, a person is planned past the time they have this week, a
// task is due, a deal or lead has its next step due today, or a weekday evening
// passes with no time logged. Each nudge goes out once; a next step goes out
// once a day.
package reminders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// Topic keys: a person who switched a topic off is skipped by the sender.
const (
	TopicDeadline  = "project.deadline"
	TopicOverload  = "schedule.overload"
	TopicTaskDue   = "task.due"
	TopicDayFill   = appmodel.NotificationTopicDayFill
	TopicLongTimer = appmodel.NotificationTopicLongTimer
	// TopicCRMNextStep is the topic a person switches off for next-step reminders.
	TopicCRMNextStep = appmodel.NotificationTopicCRMNextStep
)

// Store is the persistence the reminders need. Each claim is idempotent, so a
// restart or a second process cannot send the same nudge twice.
type Store interface {
	ProjectsDueBetween(ctx context.Context, from, to string) ([]appmodel.DueProject, error)
	MarkDeadlineReminded(ctx context.Context, request appmodel.DeadlineReminderRequest) error
	ManagerIDs(ctx context.Context, teamID int64) ([]int64, error)
	TeamIDs(ctx context.Context) ([]int64, error)
	ClaimOverloadNotice(ctx context.Context, request appmodel.OverloadNoticeRequest) (bool, error)
	TasksDueOn(ctx context.Context, through string) ([]appmodel.DueTask, error)
	MarkTaskDueReminded(ctx context.Context, request appmodel.TaskDueReminderRequest) error
	CRMStepsDue(ctx context.Context, through string) ([]appmodel.CRMStep, error)
	ClaimCRMReminder(ctx context.Context, claim appmodel.CRMReminderClaimRequest) (bool, error)
	DayFillCandidates(ctx context.Context, query appmodel.DayFillCandidatesQuery) ([]int64, error)
	ClaimDayFillNotice(ctx context.Context, request appmodel.DayFillNoticeRequest) (bool, error)
	RunningSessions(ctx context.Context) ([]appmodel.RunningSession, error)
	ClaimLongTimerNotice(ctx context.Context, request appmodel.LongTimerNoticeRequest) (bool, error)
	TeamLocation(ctx context.Context, teamID int64) (*time.Location, error)
	// UserTimeZones reads the hand-set zone of each named member ("tz" of their
	// preferences, empty when they never chose). The night gate needs it for
	// workspaces that left their own zone blank (UX-33).
	UserTimeZones(ctx context.Context, userIDs []int64) (map[int64]string, error)
}

// Planner reads a team's week, the same way the schedule page does.
type Planner interface {
	List(ctx context.Context, query appmodel.ScheduleQuery) (appmodel.ScheduleSnapshot, error)
}

// Notification is one push: who gets it, what it says, and where it leads.
type Notification struct {
	TeamID  int64
	UserIDs []int64
	Title   string
	Body    string
	URL     string
	Topic   string
}

// Sender delivers a notification through the push queue.
type Sender interface {
	Send(ctx context.Context, notification Notification) error
}

// Mailer sends a nudge by email to the people it names. The implementation drops
// those who muted the topic, as the push channel does (TIME-14).
type Mailer interface {
	Email(ctx context.Context, notification Notification) error
}

// Logger is the process-owned diagnostics sink; a nil logger keeps the
// worker silent, which the tests rely on.
type Logger interface {
	Printf(string, ...any)
}

// Service runs one pass of reminders on an interval.
type Service struct {
	store     Store
	planner   Planner
	sender    Sender
	mailer    Mailer
	audience  CRMAudience
	publicURL string
	now       func() time.Time
	logger    Logger
}

// Dependencies are the ports a reminder pass needs. Mailer and Audience are
// optional; without Audience the next-step reminders do not run. PublicURL is
// the address the letters link to.
type Dependencies struct {
	Store     Store
	Planner   Planner
	Sender    Sender
	Mailer    Mailer
	Audience  CRMAudience
	PublicURL string
	Now       func() time.Time
	Logger    Logger
}

// New builds the reminder worker. The clock comes from the caller, the same
// as every other workflow: a reminder run against the wall clock could not
// be frozen in a test or kept deterministic across a nightly pass.
func New(deps Dependencies) (*Service, error) {
	if deps.Store == nil || deps.Planner == nil || deps.Sender == nil {
		return nil, fmt.Errorf("reminders dependencies are incomplete")
	}
	if deps.Now == nil {
		return nil, fmt.Errorf("reminders need a clock")
	}
	now := deps.Now
	return &Service{
		store: deps.Store, planner: deps.Planner, sender: deps.Sender, mailer: deps.Mailer,
		audience: deps.Audience, publicURL: deps.PublicURL, now: now, logger: deps.Logger,
	}, nil
}

// logf names a failure on the process logger. A silently dead reminder pass
// leaves deadlines and due dates unnudged with no trace anywhere, which is
// worse than the failure itself.
func (s *Service) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

// taskDues nudges the assignee of an open task that is due today or tomorrow,
// and once more when it is overdue (TASK-05). Each marker goes out once per due
// date, so a change of the due date or of the state sends a fresh nudge.
func (s *Service) taskDues(ctx context.Context, now time.Time) error {
	today := now.Format("2006-01-02")
	tasks, err := s.store.TasksDueOn(ctx, now.AddDate(0, 0, 1).Format("2006-01-02"))
	if err != nil {
		return err
	}
	var mailErrs []error
	for _, task := range tasks {
		marker := "soon:" + task.Due
		title, body := "Срок задачи близко", fmt.Sprintf("«%s»: срок %s", task.Title, task.Due)
		if task.Due < today {
			marker = "overdue:" + task.Due
			title, body = "Задача просрочена", fmt.Sprintf("«%s»: срок был %s", task.Title, task.Due)
		}
		if task.Reminded == marker {
			continue
		}
		night, err := s.teamNightOnes(ctx, task.TeamID, []int64{task.AssigneeID}, now)
		if err != nil {
			return err
		}
		if night[task.AssigneeID] {
			// The push would wake the assignee: the nudge is not claimed and
			// waits for the next pass, which delivers it in their morning.
			continue
		}
		notification := Notification{
			TeamID: task.TeamID, UserIDs: []int64{task.AssigneeID}, Topic: TopicTaskDue,
			Title: title, Body: body, URL: "/tasks",
		}
		if err := s.sender.Send(ctx, notification); err != nil {
			return err
		}
		if s.mailer != nil {
			if err := s.mailer.Email(ctx, notification); err != nil {
				mailErrs = append(mailErrs, err)
			}
		}
		if err := s.store.MarkTaskDueReminded(ctx, appmodel.TaskDueReminderRequest{TeamID: task.TeamID, TaskID: task.TaskID, Marker: marker}); err != nil {
			return err
		}
	}
	return errors.Join(mailErrs...)
}

// Run does a pass now and then every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Pass(ctx); err != nil && ctx.Err() == nil {
			// A failed pass retries on the next tick; nothing is marked sent.
			// The error is logged because a dead worker is otherwise invisible
			// until someone notices a deadline that went unnudged.
			s.logf("reminders: pass failed, retrying next tick: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Pass sends every nudge that is due and not yet sent. Each section runs on
// its own: one failing store read used to end the pass, taking the task,
// overload and day nudges down with the deadline section for as long as the
// failure lasted.
func (s *Service) Pass(ctx context.Context) error {
	now := s.now().UTC()
	sections := []struct {
		name string
		run  func(context.Context, time.Time) error
	}{
		{"deadline reminders", s.deadlines},
		{"task due reminders", s.taskDues},
		{"overload reminders", s.overloads},
		{"crm next step reminders", s.crmSteps},
		{"day fill reminders", s.dayFill},
		{"long timer reminders", s.longTimers},
	}
	var errs []error
	for _, section := range sections {
		if err := section.run(ctx, now); err != nil {
			s.logf("reminders: %s: %v", section.name, err)
			errs = append(errs, fmt.Errorf("%s: %w", section.name, err))
		}
	}
	return errors.Join(errs...)
}

// deadlines warns about projects whose deadline is within three days, once per
// deadline. The lead and the managers hear about it.
func (s *Service) deadlines(ctx context.Context, now time.Time) error {
	from := now.Format("2006-01-02")
	to := now.AddDate(0, 0, 3).Format("2006-01-02")
	due, err := s.store.ProjectsDueBetween(ctx, from, to)
	if err != nil {
		return err
	}
	for _, project := range due {
		managers, err := s.store.ManagerIDs(ctx, project.TeamID)
		if err != nil {
			return err
		}
		recipients := managers
		if project.LeadUserID > 0 && !contains(recipients, project.LeadUserID) {
			recipients = append(recipients, project.LeadUserID)
		}
		night, err := s.teamNightOnes(ctx, project.TeamID, recipients, now)
		if err != nil {
			return err
		}
		awake := without(recipients, night)
		if len(awake) == 0 {
			// All recipients are asleep: the reminder is not claimed, so a
			// later pass delivers it when someone is awake again.
			continue
		}
		if err := s.sender.Send(ctx, Notification{
			TeamID: project.TeamID, UserIDs: awake, Topic: TopicDeadline,
			Title: "Срок проекта близко",
			Body:  fmt.Sprintf("%s: срок %s", project.Name, project.Deadline),
			URL:   "/projects/" + project.Slug,
		}); err != nil {
			return err
		}
		if err := s.store.MarkDeadlineReminded(ctx, appmodel.DeadlineReminderRequest{TeamID: project.TeamID, ProjectID: project.ProjectID, Deadline: project.Deadline}); err != nil {
			return err
		}
	}
	return nil
}

// overloads tells the managers, once per person per week, when someone is
// planned past what they have this week.
func (s *Service) overloads(ctx context.Context, now time.Time) error {
	teams, err := s.store.TeamIDs(ctx)
	if err != nil {
		return err
	}
	week := weekStart(now)
	var mailErrs []error
	for _, teamID := range teams {
		snapshot, err := s.planner.List(ctx, appmodel.ScheduleQuery{TeamID: teamID, WeekStart: week, Now: now})
		if err != nil {
			return err
		}
		managers, err := s.store.ManagerIDs(ctx, teamID)
		if err != nil {
			return err
		}
		for _, row := range snapshot.Rows {
			if row.AvailableMinutes <= 0 || row.Total <= row.AvailableMinutes {
				continue
			}
			night, err := s.teamNightOnes(ctx, teamID, managers, now)
			if err != nil {
				return err
			}
			awake := without(managers, night)
			if len(awake) == 0 {
				// Every manager is asleep: the notice is not claimed, so a
				// later pass delivers it into an awake workspace.
				continue
			}
			claimed, err := s.store.ClaimOverloadNotice(ctx, appmodel.OverloadNoticeRequest{TeamID: teamID, UserID: row.UserID, Week: week.Format("2006-01-02")})
			if err != nil {
				return err
			}
			if !claimed {
				continue
			}
			notification := Notification{
				TeamID: teamID, UserIDs: awake, Topic: TopicOverload,
				Title: "Перегрузка на неделе",
				Body:  fmt.Sprintf("%s: запланировано %d мин из %d доступных", row.UserName, row.Total, row.AvailableMinutes),
				URL:   "/schedule?date=" + week.Format("2006-01-02"),
			}
			if err := s.sender.Send(ctx, notification); err != nil {
				return err
			}
			// Email repeats the same nudge. One failed mailbox must not hold back
			// the other managers' reminders, so its error is kept, not returned early.
			if s.mailer != nil {
				if err := s.mailer.Email(ctx, notification); err != nil {
					mailErrs = append(mailErrs, err)
				}
			}
		}
	}
	return errors.Join(mailErrs...)
}

// The "fill your day" nudge goes out on a weekday evening in the workspace's zone,
// once per person per day, to a member who started no session that day. The
// worker ticks hourly, so the first tick inside the window sends it. A claim is
// stored before the send: a failed send is not retried that day, the same choice
// as the overload nudge makes.
const (
	dayFillFromHour = 18
	dayFillToHour   = 22
)

func (s *Service) dayFill(ctx context.Context, now time.Time) error {
	teams, err := s.store.TeamIDs(ctx)
	if err != nil {
		return err
	}
	var mailErrs []error
	for _, teamID := range teams {
		loc, err := s.store.TeamLocation(ctx, teamID)
		if err != nil {
			return err
		}
		local := now.In(loc)
		if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
			continue
		}
		if local.Hour() < dayFillFromHour || local.Hour() >= dayFillToHour {
			continue
		}
		day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		users, err := s.store.DayFillCandidates(ctx, appmodel.DayFillCandidatesQuery{
			TeamID: teamID, From: day, To: day.AddDate(0, 0, 1), Day: day.Format("2006-01-02"),
		})
		if err != nil {
			return err
		}
		for _, userID := range users {
			night, err := s.nightOnes(ctx, loc, []int64{userID}, now)
			if err != nil {
				return err
			}
			if night[userID] {
				// The workspace's evening is the member's night: the push
				// waits. The nudge is not claimed, so a later pass sends it
				// while the window is still open for them.
				continue
			}
			claimed, err := s.store.ClaimDayFillNotice(ctx, appmodel.DayFillNoticeRequest{
				TeamID: teamID, UserID: userID, Day: day.Format("2006-01-02"),
			})
			if err != nil {
				return err
			}
			if !claimed {
				continue
			}
			notification := Notification{
				TeamID: teamID, UserIDs: []int64{userID}, Topic: TopicDayFill,
				Title: "За сегодня нет записей",
				Body:  "За сегодня нет записей. Отметьте время, если работали",
				URL:   "/timesheet",
			}
			if err := s.sender.Send(ctx, notification); err != nil {
				return err
			}
			// The email repeats the push. A failed mailbox does not hold back the others.
			if s.mailer != nil {
				if err := s.mailer.Email(ctx, notification); err != nil {
					mailErrs = append(mailErrs, err)
				}
			}
		}
	}
	return errors.Join(mailErrs...)
}

// weekStart is the Monday of the week containing t, in UTC.
func weekStart(t time.Time) time.Time {
	day := t.UTC()
	offset := (int(day.Weekday()) + 6) % 7
	return time.Date(day.Year(), day.Month(), day.Day()-offset, 0, 0, 0, 0, time.UTC)
}

// The night gate (UX-33): a reminder's push must not wake a person, however
// late the pass runs — a restart at three in the morning ran the first pass
// immediately, and an hour tick can land inside someone's night. Night is
// 22:00–08:00 by the recipient's own days: the zone they set by hand answers
// first (it is literally their night), and the workspace's zone — the caller
// resolves it, as every section already does — answers for those who never
// chose. The email twin goes regardless — a letter in a mailbox does not wake
// anyone.
const (
	quietFromHour = 22
	quietToHour   = 8
)

// teamNightOnes resolves the workspace's zone the way every section does and
// runs the night gate with it as the members' fallback.
func (s *Service) teamNightOnes(ctx context.Context, teamID int64, userIDs []int64, now time.Time) (map[int64]bool, error) {
	teamLoc, err := s.store.TeamLocation(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("read the workspace zone for the night gate: %w", err)
	}
	return s.nightOnes(ctx, teamLoc, userIDs, now)
}

// nightOnes answers, for each named member, whether push is quiet for them at
// this instant. workspaceLoc is the zone the caller's section already keeps its
// days in; a member without a hand-set zone reads its answer, so one
// unresolvable person cannot stay awake by accident.
func (s *Service) nightOnes(ctx context.Context, workspaceLoc *time.Location, userIDs []int64, now time.Time) (map[int64]bool, error) {
	night := make(map[int64]bool, len(userIDs))
	if len(userIDs) == 0 {
		return night, nil
	}
	zones, err := s.store.UserTimeZones(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("read member zones for the night gate: %w", err)
	}
	for _, id := range userIDs {
		if id <= 0 {
			continue
		}
		loc := workspaceLoc
		if name := zones[id]; name != "" {
			if resolved, err := time.LoadLocation(name); err == nil {
				loc = resolved
			}
		}
		hour := now.In(loc).Hour()
		night[id] = hour >= quietFromHour || hour < quietToHour
	}
	return night, nil
}

// without drops the night ones from a push recipient list.
func without(ids []int64, night map[int64]bool) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !night[id] {
			out = append(out, id)
		}
	}
	return out
}

func contains(ids []int64, id int64) bool {
	for _, value := range ids {
		if value == id {
			return true
		}
	}
	return false
}

// longTimers tells a person, once per session, that a timer has been running
// longer than they set (four hours unless they chose otherwise): a timer left
// on overnight otherwise turns into hours nobody worked, found the next day.
// At night it waits, unclaimed, for the person's morning.
func (s *Service) longTimers(ctx context.Context, now time.Time) error {
	sessions, err := s.store.RunningSessions(ctx)
	if err != nil {
		return err
	}
	for _, r := range sessions {
		if r.ThresholdHours <= 0 {
			continue
		}
		seconds := r.Accumulated
		if r.LastResumeAt != nil && now.After(*r.LastResumeAt) {
			seconds += int(now.Sub(*r.LastResumeAt) / time.Second)
		}
		if seconds < r.ThresholdHours*3600 {
			continue
		}
		loc, err := s.store.TeamLocation(ctx, r.TeamID)
		if err != nil {
			return err
		}
		night, err := s.nightOnes(ctx, loc, []int64{r.UserID}, now)
		if err != nil {
			return err
		}
		if night[r.UserID] {
			continue
		}
		claimed, err := s.store.ClaimLongTimerNotice(ctx, appmodel.LongTimerNoticeRequest{SessionID: r.SessionID})
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		hours := seconds / 3600
		if err := s.sender.Send(ctx, Notification{
			TeamID: r.TeamID, UserIDs: []int64{r.UserID}, Topic: TopicLongTimer,
			Title: fmt.Sprintf("Таймер идёт уже %d ч", hours),
			Body:  fmt.Sprintf("«%s» всё ещё идёт. Если вы его забыли, остановите и поправьте время.", r.Activity),
			URL:   "/",
		}); err != nil {
			return err
		}
	}
	return nil
}
