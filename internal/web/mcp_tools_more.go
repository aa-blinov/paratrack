package web

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpMoreTools are the goals, tags, members, weekly plan, invoices, payroll runs
// and approved weeks. Nothing here changes an invoice, a run or a week, and nothing
// deletes an invoice, a run or a workspace: the writes on invoices, runs and week
// approval are in mcpMoneyTools, and reopening a week stays in the screens.
var mcpMoreTools = []mcpTool{
	{
		Name: "goals_list",
		Description: "List the goals with their progress in the current day, week or month: the target and achieved minutes " +
			"and the percent complete. Filter by period to get one kind.",
		Schema: objectSchema(map[string]any{"period": enumProp("Only goals of this period", mcpGoalPeriods...)}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Period string `json:"period"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.Period != "" && !slices.Contains(mcpGoalPeriods, in.Period) {
				return nil, appmodel.FieldError{Field: "period"}
			}
			progress, err := s.services.Goals.Progress(c.ctx, appmodel.GoalProgressQuery{TeamID: c.teamID, Now: c.now})
			if err != nil {
				return nil, err
			}
			kept := slices.DeleteFunc(progress, func(p appmodel.GoalProgress) bool {
				return in.Period != "" && p.Goal.Period != in.Period
			})
			return struct {
				Goals []goalProgressResponse `json:"goals"`
			}{goalProgressFor(kept)}, nil
		},
	},
	{
		Name:        "tags_list",
		Description: "List the workspace's tags, with the number of sessions that carry each one.",
		Schema:      objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			tags, err := s.services.Tagging.Queries.ListWithCounts(c.ctx, appmodel.TagListQuery{TeamID: c.teamID})
			if err != nil {
				return nil, err
			}
			out := make([]tagResponse, 0, len(tags))
			for _, tag := range tags {
				out = append(out, tagResponse{ID: tag.ID, Name: tag.Name, TeamID: tag.TeamID, CreatedAt: tag.CreatedAt, SessionCount: tag.SessionCount})
			}
			return struct {
				Tags []tagResponse `json:"tags"`
			}{out}, nil
		},
	},
	{
		Name:        "members_list",
		Description: "List the members of the workspace with their user id, name, email and role. Managers only.",
		Schema:      objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			snapshot, err := s.services.MemberAdmin.Management(c.ctx, c.teamID)
			if err != nil {
				return nil, err
			}
			out := make([]apiV1Member, 0, len(snapshot.Members))
			for _, m := range snapshot.Members {
				out = append(out, apiV1Member{ID: m.UserID, Name: m.Name, Email: m.Email, Role: string(m.Role)})
			}
			return struct {
				Members []apiV1Member `json:"members"`
			}{out}, nil
		},
	},
	{
		Name: "schedule_entries_list",
		Description: "Read the weekly plan of the week that holds from: for each member, the minutes planned per project and per day, " +
			"and the load against the time the week can carry. The week starts on the workspace's first day.",
		Schema: objectSchema(map[string]any{"from": dateProp("Any day of the week to read")}, "from"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				From string `json:"from"`
			}](args)
			if err != nil {
				return nil, err
			}
			day, err := mcpLocalDate("from", in.From, c.now.Location())
			if err != nil {
				return nil, err
			}
			weekStart := mcpWeekStart(c.ctx, day)
			snapshot, err := s.services.Scheduling.List(c.ctx, appmodel.ScheduleQuery{TeamID: c.teamID, WeekStart: weekStart, Now: c.now})
			if err != nil {
				return nil, err
			}
			return mcpScheduleViewOf(snapshot, weekStart), nil
		},
	},
	{
		Name: "schedule_cell_set",
		Description: "Set the planned minutes of one member on one project for one day; zero clears that project's minutes on the day. " +
			"A manager can plan any project, anyone else only the projects they lead.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"project_id": integerProp("Project id"),
			"user_id":    integerProp("Member's user id"),
			"date":       dateProp("Day"),
			"minutes":    integerProp("Planned minutes, 0 to 1440"),
		}, "project_id", "user_id", "date", "minutes"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ProjectID int64  `json:"project_id"`
				UserID    int64  `json:"user_id"`
				Date      string `json:"date"`
				Minutes   int    `json:"minutes"`
			}](args)
			if err != nil {
				return nil, err
			}
			// A project id of 0 would make the workflow plan the first project, so it is refused here.
			if in.ProjectID <= 0 {
				return nil, appmodel.FieldError{Field: "project_id"}
			}
			if in.UserID <= 0 {
				return nil, appmodel.FieldError{Field: "user_id"}
			}
			if in.Minutes < 0 || in.Minutes > 24*60 {
				return nil, appmodel.FieldError{Field: "minutes"}
			}
			day, err := mcpLocalDate("date", in.Date, c.now.Location())
			if err != nil {
				return nil, err
			}
			if err := s.services.Scheduling.SetCell(c.ctx, appmodel.ScheduleCellRequest{
				TeamID: c.teamID, ActorID: c.userID, UserID: in.UserID, ProjectID: in.ProjectID, Day: day, Minutes: in.Minutes,
			}); err != nil {
				return nil, err
			}
			return struct {
				ProjectID int64  `json:"project_id"`
				UserID    int64  `json:"user_id"`
				Date      string `json:"date"`
				Minutes   int    `json:"minutes"`
			}{in.ProjectID, in.UserID, in.Date, in.Minutes}, nil
		},
	},
	{
		Name:        "invoices_list",
		Description: "List the invoices with their number, client, status, period, due date and totals in cents. Managers only.",
		Schema:      objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			snapshot, err := s.services.Invoicing.Queries.BuildIndex(c.ctx, appmodel.InvoiceIndexRequest{TeamID: c.teamID})
			if err != nil {
				return nil, err
			}
			out := make([]mcpInvoiceView, 0, len(snapshot.Invoices))
			for _, item := range snapshot.Invoices {
				out = append(out, mcpInvoiceViewOf(item.Invoice, item.TotalCents, item.PaidCents, item.TotalHoursHundredths))
			}
			return struct {
				Invoices []mcpInvoiceView `json:"invoices"`
			}{out}, nil
		},
	},
	{
		Name:        "invoice_get",
		Description: "Read one invoice with its lines and its payment journal. Managers only.",
		Schema:      objectSchema(map[string]any{"invoice_id": integerProp("Invoice id")}, "invoice_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				InvoiceID int64 `json:"invoice_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			snapshot, err := s.services.InvoiceDocuments.Build(c.ctx, appmodel.InvoiceDocumentRequest{TeamID: c.teamID, InvoiceID: in.InvoiceID})
			if err != nil {
				return nil, err
			}
			d := snapshot.Details
			// The invoice page leaves the members' names off a by-person invoice while the
			// workspace hides them (FIN-15); the agent sees the same lines the page shows.
			hidden, err := s.executorNamesToHide(c.req, d.Invoice)
			if err != nil {
				return nil, err
			}
			lines := make([]mcpInvoiceLineView, 0, len(d.Lines))
			for _, line := range d.Lines {
				lines = append(lines, mcpInvoiceLineView{Label: withoutExecutor(line.Label, hidden), Seconds: line.Seconds, RateCents: line.RateCents, AmountCents: line.AmountCents})
			}
			payments := make([]mcpPaymentView, 0, len(d.Payments))
			for _, p := range d.Payments {
				payments = append(payments, mcpPaymentView{PaidOn: p.PaidOn, AmountCents: p.AmountCents, Note: p.Note, Source: p.Source})
			}
			return mcpInvoiceDetailView{
				mcpInvoiceView: mcpInvoiceViewOf(d.Invoice, d.TotalCents, d.PaidCents, d.TotalHoursHundredths),
				Lines:          lines,
				Payments:       payments,
			}, nil
		},
	},
	{
		Name:        "payroll_runs_list",
		Description: "List the payroll runs with their number, period, status and totals: cents, and hundredths of an hour. Managers only.",
		Schema:      objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			runs, err := s.services.Payroll.ListRuns(c.ctx, appmodel.PayrollRunListQuery{TeamID: c.teamID})
			if err != nil {
				return nil, err
			}
			out := make([]mcpPayrollRunView, 0, len(runs))
			for _, item := range runs {
				out = append(out, mcpPayrollRunViewOf(item.Run, item.TotalCents, item.TotalHoursHundredths))
			}
			return struct {
				Runs []mcpPayrollRunView `json:"runs"`
			}{out}, nil
		},
	},
	{
		Name: "timesheet_approvals_list",
		Description: "List the approved timesheet weeks that overlap the days from to to, both inclusive. " +
			"Reopening a week stays in the timesheet screen.",
		Schema: objectSchema(map[string]any{
			"from": dateProp("First day"),
			"to":   dateProp("Last day"),
		}, "from", "to"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				From string `json:"from"`
				To   string `json:"to"`
			}](args)
			if err != nil {
				return nil, err
			}
			loc := c.now.Location()
			from, err := mcpLocalDate("from", in.From, loc)
			if err != nil {
				return nil, err
			}
			last, err := mcpLocalDate("to", in.To, loc)
			if err != nil {
				return nil, err
			}
			if last.Before(from) {
				return nil, appmodel.FieldError{Field: "to"}
			}
			approvals, err := s.services.Tracking.Queries.TimesheetApprovals(c.ctx, c.teamID, from, last.AddDate(0, 0, 1))
			if err != nil {
				return nil, err
			}
			out := make([]mcpApprovalView, 0, len(approvals))
			for _, a := range approvals {
				out = append(out, mcpApprovalView{
					Start: a.Start.In(loc).Format(time.DateOnly), End: a.End.In(loc).AddDate(0, 0, -1).Format(time.DateOnly),
					ApprovedAt: a.ApprovedAt,
				})
			}
			return struct {
				Approvals []mcpApprovalView `json:"approvals"`
			}{out}, nil
		},
	},
	{
		Name:        "tag_create",
		Description: "Create a tag, or get back the tag of the same name. Names are stored in lower case. Any member can create a tag.",
		Write:       true,
		Schema:      objectSchema(map[string]any{"name": stringProp("Tag name")}, "name"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Name string `json:"name"`
			}](args)
			if err != nil {
				return nil, err
			}
			tag, err := s.services.Tagging.Commands.CreateForMember(c.ctx, appmodel.TagCreateRequest{TeamID: c.teamID, CallerID: c.userID, Name: in.Name})
			if errors.Is(err, appmodel.ErrInvalidTag) {
				return nil, appmodel.FieldError{Field: "name"}
			}
			if err != nil {
				return nil, err
			}
			return tagFor(tag), nil
		},
	},
	{
		Name: "goal_set",
		Description: "Set the goal of an activity for a period: the minutes to reach each day, week or month. " +
			"The activity is created when the name is new. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"activity": stringProp("Activity name"),
			"period":   enumProp("Period of the goal", mcpGoalPeriods...),
			"minutes":  integerProp("Target in minutes, above zero"),
		}, "activity", "period", "minutes"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Activity string `json:"activity"`
				Period   string `json:"period"`
				Minutes  int    `json:"minutes"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			goal, err := s.services.Goals.UpsertForManager(c.ctx, appmodel.GoalUpsertRequest{
				TeamID: c.teamID, CallerID: c.userID, ActivityName: in.Activity, Period: in.Period, Minutes: in.Minutes,
			})
			if err != nil {
				return nil, mcpGoalError(err)
			}
			return goalFor(goal), nil
		},
	},
	{
		Name: "goal_delete",
		Description: "Delete the goal of an activity for one period. The time already tracked stays. " +
			"Managers only; the deletion cannot be undone.",
		Write:       true,
		Destructive: true,
		Schema: objectSchema(map[string]any{
			"activity": stringProp("Activity name"),
			"period":   enumProp("Period of the goal", mcpGoalPeriods...),
		}, "activity", "period"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Activity string `json:"activity"`
				Period   string `json:"period"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if err := s.services.Goals.DeleteForManager(c.ctx, appmodel.GoalDeleteRequest{
				TeamID: c.teamID, CallerID: c.userID, ActivityName: in.Activity, Period: in.Period,
			}); err != nil {
				return nil, mcpGoalError(err)
			}
			return struct {
				Activity string `json:"activity"`
				Period   string `json:"period"`
				Deleted  bool   `json:"deleted"`
			}{in.Activity, in.Period, true}, nil
		},
	},
}

// mcpGoalPeriods are the goal periods. The goal workflow refuses any other.
var mcpGoalPeriods = []string{"daily", "weekly", "monthly"}

// mcpGoalError names the field behind a refused goal write, and reads a goal that
// is not there as a missing entity.
func mcpGoalError(err error) error {
	switch {
	case errors.Is(err, appmodel.ErrInvalidGoalActivity):
		return appmodel.FieldError{Field: "activity"}
	case errors.Is(err, appmodel.ErrInvalidGoalPeriod):
		return appmodel.FieldError{Field: "period"}
	case errors.Is(err, appmodel.ErrInvalidGoalTarget):
		return appmodel.FieldError{Field: "minutes"}
	case errors.Is(err, model.ErrGoalNotFound):
		return model.ErrNotFound
	}
	return err
}

// mcpLocalDate reads a YYYY-MM-DD argument as a day in the workspace zone, which
// is where the timesheet and the plan draw their week boundaries.
func mcpLocalDate(field, value string, loc *time.Location) (time.Time, error) {
	day, err := time.ParseInLocation(time.DateOnly, value, loc)
	if err != nil {
		return time.Time{}, appmodel.FieldError{Field: field}
	}
	return day, nil
}

// mcpWeekStart is the first day of the week that holds day, on the workspace's
// week start. The request's team carries it; a blank setting means Monday.
func mcpWeekStart(ctx context.Context, day time.Time) time.Time {
	team, _ := TeamFrom(ctx)
	return weekStartOn(team.WeekStart == "sun", day)
}

// The views below are the JSON shapes of the entities added with the tool set.
// Dates are YYYY-MM-DD, times are RFC 3339, money is integer cents.

type mcpScheduleView struct {
	Days         []string                `json:"days"`
	TotalMinutes int                     `json:"total_minutes"`
	People       []mcpSchedulePersonView `json:"people"`
}

type mcpSchedulePersonView struct {
	UserID           int64                 `json:"user_id"`
	UserName         string                `json:"user_name"`
	AdminMinutes     int                   `json:"admin_minutes"`
	AvailableMinutes int                   `json:"available_minutes"`
	PlannedMinutes   int                   `json:"planned_minutes"`
	LoadPercent      int                   `json:"load_percent"`
	Lines            []mcpScheduleLineView `json:"lines"`
}

type mcpScheduleLineView struct {
	ProjectID    int64  `json:"project_id"`
	ProjectName  string `json:"project_name"`
	Minutes      [7]int `json:"minutes"`
	TotalMinutes int    `json:"total_minutes"`
}

func mcpScheduleViewOf(snapshot appmodel.ScheduleSnapshot, weekStart time.Time) mcpScheduleView {
	days := make([]string, 7)
	for i := range days {
		days[i] = weekStart.AddDate(0, 0, i).Format(time.DateOnly)
	}
	view := mcpScheduleView{Days: days, TotalMinutes: snapshot.TotalMinutes, People: make([]mcpSchedulePersonView, 0, len(snapshot.Rows))}
	for _, row := range snapshot.Rows {
		person := mcpSchedulePersonView{
			UserID: row.UserID, UserName: row.UserName, AdminMinutes: row.AdminMinutes,
			AvailableMinutes: row.AvailableMinutes, PlannedMinutes: row.Total, LoadPercent: row.LoadPercent,
			Lines: []mcpScheduleLineView{},
		}
		for _, pid := range slices.Sorted(maps.Keys(row.ByProject)) {
			cells := row.ByProject[pid]
			line := mcpScheduleLineView{ProjectID: pid, ProjectName: snapshot.ProjectNames[pid], Minutes: cells}
			for _, minutes := range cells {
				line.TotalMinutes += minutes
			}
			person.Lines = append(person.Lines, line)
		}
		view.People = append(view.People, person)
	}
	return view
}

type mcpInvoiceView struct {
	ID                   int64  `json:"id"`
	Number               string `json:"number"`
	ClientName           string `json:"client_name"`
	Status               string `json:"status"`
	Currency             string `json:"currency"`
	PeriodStart          string `json:"period_start"`
	PeriodEnd            string `json:"period_end"`
	DueDate              string `json:"due_date"`
	TotalCents           int    `json:"total_cents"`
	PaidCents            int    `json:"paid_cents"`
	TotalHoursHundredths int    `json:"total_hours_hundredths"`
}

// mcpInvoiceViewOf reads the period end as the last day it covers, not the day after.
func mcpInvoiceViewOf(inv model.Invoice, totalCents, paidCents, hoursHundredths int) mcpInvoiceView {
	return mcpInvoiceView{
		ID: inv.ID, Number: inv.Number, ClientName: inv.ClientName, Status: inv.Status, Currency: inv.Currency,
		PeriodStart: inv.PeriodStart.Format(time.DateOnly), PeriodEnd: inv.PeriodEnd.AddDate(0, 0, -1).Format(time.DateOnly),
		DueDate: inv.DueDate, TotalCents: totalCents, PaidCents: paidCents, TotalHoursHundredths: hoursHundredths,
	}
}

type mcpInvoiceLineView struct {
	Label       string `json:"label"`
	Seconds     int    `json:"seconds"`
	RateCents   int    `json:"rate_cents"`
	AmountCents int    `json:"amount_cents"`
}

type mcpPaymentView struct {
	PaidOn      string `json:"paid_on"`
	AmountCents int    `json:"amount_cents"`
	Note        string `json:"note"`
	Source      string `json:"source"`
}

type mcpInvoiceDetailView struct {
	mcpInvoiceView
	Lines    []mcpInvoiceLineView `json:"lines"`
	Payments []mcpPaymentView     `json:"payments"`
}

type mcpPayrollRunView struct {
	ID                   int64  `json:"id"`
	Number               string `json:"number"`
	Status               string `json:"status"`
	Currency             string `json:"currency"`
	PeriodStart          string `json:"period_start"`
	PeriodEnd            string `json:"period_end"`
	TotalCents           int    `json:"total_cents"`
	TotalHoursHundredths int    `json:"total_hours_hundredths"`
}

// mcpPayrollRunViewOf reads the period end as the last day it covers, as the invoice view does.
func mcpPayrollRunViewOf(run model.PayrollRun, totalCents, hoursHundredths int) mcpPayrollRunView {
	return mcpPayrollRunView{
		ID: run.ID, Number: run.Number, Status: run.Status, Currency: run.Currency,
		PeriodStart: run.PeriodStart.Format(time.DateOnly), PeriodEnd: run.PeriodEnd.AddDate(0, 0, -1).Format(time.DateOnly),
		TotalCents: totalCents, TotalHoursHundredths: hoursHundredths,
	}
}

type mcpApprovalView struct {
	Start      string    `json:"start"`
	End        string    `json:"end"`
	ApprovedAt time.Time `json:"approved_at"`
}
