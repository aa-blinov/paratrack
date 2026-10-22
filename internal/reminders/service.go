// Package reminders sends the two nudges a project lead and a manager need
// without opening the app: a project deadline is close, or a person is planned
// past the time they have this week. Each nudge goes out once.
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
	TopicDeadline = "project.deadline"
	TopicOverload = "schedule.overload"
	TopicTaskDue  = "task.due"
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

// Service runs one pass of reminders on an interval.
type Service struct {
	store   Store
	planner Planner
	sender  Sender
	mailer  Mailer
	now     func() time.Time
}

// Dependencies are the ports a reminder pass needs. Mailer is optional.
type Dependencies struct {
	Store   Store
	Planner Planner
	Sender  Sender
	Mailer  Mailer
	Now     func() time.Time
}

// New builds the reminder worker. Now defaults to the wall clock.
func New(deps Dependencies) (*Service, error) {
	if deps.Store == nil || deps.Planner == nil || deps.Sender == nil {
		return nil, fmt.Errorf("reminders dependencies are incomplete")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: deps.Store, planner: deps.Planner, sender: deps.Sender, mailer: deps.Mailer, now: now}, nil
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
			_ = err // a failed pass retries on the next tick; nothing is marked sent
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Pass sends every nudge that is due and not yet sent.
func (s *Service) Pass(ctx context.Context) error {
	now := s.now().UTC()
	if err := s.deadlines(ctx, now); err != nil {
		return fmt.Errorf("deadline reminders: %w", err)
	}
	if err := s.taskDues(ctx, now); err != nil {
		return fmt.Errorf("task due reminders: %w", err)
	}
	if err := s.overloads(ctx, now); err != nil {
		return fmt.Errorf("overload reminders: %w", err)
	}
	return nil
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
		if err := s.sender.Send(ctx, Notification{
			TeamID: project.TeamID, UserIDs: recipients, Topic: TopicDeadline,
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
			claimed, err := s.store.ClaimOverloadNotice(ctx, appmodel.OverloadNoticeRequest{TeamID: teamID, UserID: row.UserID, Week: week.Format("2006-01-02")})
			if err != nil {
				return err
			}
			if !claimed {
				continue
			}
			notification := Notification{
				TeamID: teamID, UserIDs: managers, Topic: TopicOverload,
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

// weekStart is the Monday of the week containing t, in UTC.
func weekStart(t time.Time) time.Time {
	day := t.UTC()
	offset := (int(day.Weekday()) + 6) % 7
	return time.Date(day.Year(), day.Month(), day.Day()-offset, 0, 0, 0, 0, time.UTC)
}

func contains(ids []int64, id int64) bool {
	for _, value := range ids {
		if value == id {
			return true
		}
	}
	return false
}
