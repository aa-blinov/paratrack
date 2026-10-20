package workspacefile

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// Decode reads a workspace export and hands its records to sink in file order.
// The format and version are checked before the first record goes out, so a file
// of another kind never reaches a sink. A file that does not have the export's
// shape is ErrUnsupportedFile; an error from the sink comes back as it is.
func Decode(r io.Reader, sink appmodel.WorkspaceSink) error {
	rd := &reader{dec: json.NewDecoder(r), sink: sink}
	return rd.file()
}

// Read collects a whole export in memory. The server streams files and never
// needs this; tests use it to compare one export with another.
func Read(r io.Reader) (appmodel.WorkspaceExport, error) {
	var c collector
	if err := Decode(r, &c); err != nil {
		return appmodel.WorkspaceExport{}, err
	}
	return c.Export, nil
}

type reader struct {
	dec        *json.Decoder
	sink       appmodel.WorkspaceSink
	format     string
	version    int
	exportedAt time.Time
	checked    bool
}

func (r *reader) file() error {
	if err := r.delim('{'); err != nil {
		return err
	}
	for r.dec.More() {
		key, err := r.key()
		if err != nil {
			return err
		}
		if err := r.field(key); err != nil {
			return err
		}
	}
	if err := r.delim('}'); err != nil {
		return err
	}
	return r.check()
}

// field reads the value of one top-level key. A key this reader does not know is
// skipped: the version decides what the keys mean, and it has been checked.
func (r *reader) field(key string) error {
	switch key {
	case "format":
		return r.header(&r.format)
	case "version":
		return r.header(&r.version)
	case "exported_at":
		return r.header(&r.exportedAt)
	}
	if err := r.check(); err != nil {
		return err
	}
	switch key {
	case "team":
		var t ExportTeam
		if err := r.value(&t); err != nil {
			return err
		}
		return r.sink.Team(decodeExportTeam(t))
	case "members":
		return each(r, func(v ExportMember) error { return r.sink.Member(decodeExportMember(v)) })
	case "clients":
		return each(r, func(v ExportClient) error { return r.sink.Client(decodeExportClient(v)) })
	case "projects":
		return each(r, func(v ExportProject) error { return r.sink.Project(decodeExportProject(v)) })
	case "tags":
		return each(r, func(v ExportTag) error { return r.sink.Tag(decodeExportTag(v)) })
	case "tasks":
		return each(r, func(v ExportTask) error { return r.sink.Task(decodeExportTask(v)) })
	case "activities":
		return each(r, func(v ExportActivity) error { return r.sink.Activity(decodeExportActivity(v)) })
	case "goals":
		return each(r, func(v ExportGoal) error { return r.sink.Goal(decodeExportGoal(v)) })
	case "invoices":
		return each(r, func(v ExportInvoice) error { return r.sink.Invoice(decodeExportInvoice(v)) })
	case "payroll_runs":
		return each(r, func(v ExportPayrollRun) error { return r.sink.PayrollRun(decodeExportPayrollRun(v)) })
	case "sessions":
		return each(r, func(v ExportSession) error { return r.sink.Session(decodeExportSession(v)) })
	case "schedule_entries":
		return each(r, func(v ExportSchedule) error { return r.sink.Schedule(decodeExportSchedule(v)) })
	case "schedule_lines":
		return each(r, func(v ExportScheduleRow) error { return r.sink.ScheduleRow(decodeExportScheduleRow(v)) })
	case "timesheet_approvals":
		return each(r, func(v ExportApproval) error { return r.sink.Approval(decodeExportApproval(v)) })
	case "saved_reports":
		return each(r, func(v ExportReport) error { return r.sink.Report(decodeExportReport(v)) })
	case "webhooks":
		return each(r, func(v ExportWebhook) error { return r.sink.Webhook(decodeExportWebhook(v)) })
	case "integrations":
		return each(r, func(v ExportIntegration) error { return r.sink.Integration(decodeExportIntegration(v)) })
	}
	var skipped json.RawMessage
	return r.value(&skipped)
}

// header reads one of the fields that come before the records.
func (r *reader) header(into any) error {
	if r.checked {
		return refuse(fmt.Errorf("a header field after the records"))
	}
	return r.value(into)
}

// check settles the header once, before anything reaches the sink: the format and
// version must be the ones this server reads.
func (r *reader) check() error {
	if r.checked {
		return nil
	}
	r.checked = true
	if r.format != appmodel.WorkspaceExportFormat || r.version != appmodel.WorkspaceExportVersion {
		return fmt.Errorf("%w: format %q version %d", ErrUnsupportedFile, r.format, r.version)
	}
	return r.sink.Header(r.exportedAt)
}

// each reads a JSON array one element at a time and hands each to fn.
func each[T any](r *reader, fn func(T) error) error {
	if err := r.delim('['); err != nil {
		return err
	}
	for r.dec.More() {
		var v T
		if err := r.value(&v); err != nil {
			return err
		}
		if err := fn(v); err != nil {
			return err
		}
	}
	return r.delim(']')
}

func (r *reader) value(into any) error {
	if err := r.dec.Decode(into); err != nil {
		return refuse(err)
	}
	return nil
}

func (r *reader) key() (string, error) {
	tok, err := r.dec.Token()
	if err != nil {
		return "", refuse(err)
	}
	key, ok := tok.(string)
	if !ok {
		return "", refuse(fmt.Errorf("expected a field name, got %v", tok))
	}
	return key, nil
}

func (r *reader) delim(want json.Delim) error {
	tok, err := r.dec.Token()
	if err != nil {
		return refuse(err)
	}
	if got, ok := tok.(json.Delim); !ok || got != want {
		return refuse(fmt.Errorf("expected %v, got %v", want, tok))
	}
	return nil
}

// refuse marks a file that is not a workspace export as ErrUnsupportedFile.
func refuse(err error) error {
	return fmt.Errorf("%w: %w", ErrUnsupportedFile, err)
}

// emailCollector keeps the emails an export uses, and nothing else. A restore
// reads the file once with it to learn which people must already have accounts.
type emailCollector struct {
	emails map[string]bool
}

func (c *emailCollector) add(email string) {
	if email == "" {
		return
	}
	if c.emails == nil {
		c.emails = map[string]bool{}
	}
	c.emails[strings.ToLower(email)] = true
}

func (c *emailCollector) list() []string {
	out := make([]string, 0, len(c.emails))
	for email := range c.emails {
		out = append(out, email)
	}
	sort.Strings(out)
	return out
}

func (c *emailCollector) Header(time.Time) error         { return nil }
func (c *emailCollector) Team(appmodel.ExportTeam) error { return nil }
func (c *emailCollector) Member(m appmodel.ExportMember) error {
	c.add(m.Email)
	return nil
}
func (c *emailCollector) Client(appmodel.ExportClient) error     { return nil }
func (c *emailCollector) Tag(appmodel.ExportTag) error           { return nil }
func (c *emailCollector) Goal(appmodel.ExportGoal) error         { return nil }
func (c *emailCollector) Activity(appmodel.ExportActivity) error { return nil }
func (c *emailCollector) Invoice(appmodel.ExportInvoice) error   { return nil }
func (c *emailCollector) Webhook(appmodel.ExportWebhook) error   { return nil }
func (c *emailCollector) Integration(appmodel.ExportIntegration) error {
	return nil
}
func (c *emailCollector) Project(p appmodel.ExportProject) error {
	c.add(p.LeadEmail)
	return nil
}
func (c *emailCollector) Task(t appmodel.ExportTask) error {
	c.add(t.AssigneeEmail)
	c.add(t.CreatedByEmail)
	return nil
}
func (c *emailCollector) PayrollRun(r appmodel.ExportPayrollRun) error {
	for _, l := range r.Lines {
		c.add(l.UserEmail)
	}
	return nil
}
func (c *emailCollector) Session(s appmodel.ExportSession) error {
	c.add(s.UserEmail)
	return nil
}
func (c *emailCollector) Schedule(e appmodel.ExportSchedule) error {
	c.add(e.UserEmail)
	return nil
}
func (c *emailCollector) ScheduleRow(l appmodel.ExportScheduleRow) error {
	c.add(l.UserEmail)
	return nil
}
func (c *emailCollector) Approval(a appmodel.ExportApproval) error {
	c.add(a.ApprovedByEmail)
	return nil
}
func (c *emailCollector) Report(rep appmodel.ExportReport) error {
	c.add(rep.CreatedByEmail)
	return nil
}

// collector keeps a whole export in memory, for Read.
type collector struct {
	Export appmodel.WorkspaceExport
}

func (c *collector) Header(exportedAt time.Time) error {
	c.Export.Format = appmodel.WorkspaceExportFormat
	c.Export.Version = appmodel.WorkspaceExportVersion
	c.Export.ExportedAt = exportedAt
	return nil
}
func (c *collector) Team(t appmodel.ExportTeam) error { c.Export.Team = t; return nil }
func (c *collector) Member(v appmodel.ExportMember) error {
	c.Export.Members = append(c.Export.Members, v)
	return nil
}
func (c *collector) Client(v appmodel.ExportClient) error {
	c.Export.Clients = append(c.Export.Clients, v)
	return nil
}
func (c *collector) Project(v appmodel.ExportProject) error {
	c.Export.Projects = append(c.Export.Projects, v)
	return nil
}
func (c *collector) Tag(v appmodel.ExportTag) error {
	c.Export.Tags = append(c.Export.Tags, v)
	return nil
}
func (c *collector) Task(v appmodel.ExportTask) error {
	c.Export.Tasks = append(c.Export.Tasks, v)
	return nil
}
func (c *collector) Activity(v appmodel.ExportActivity) error {
	c.Export.Activities = append(c.Export.Activities, v)
	return nil
}
func (c *collector) Goal(v appmodel.ExportGoal) error {
	c.Export.Goals = append(c.Export.Goals, v)
	return nil
}
func (c *collector) Invoice(v appmodel.ExportInvoice) error {
	c.Export.Invoices = append(c.Export.Invoices, v)
	return nil
}
func (c *collector) PayrollRun(v appmodel.ExportPayrollRun) error {
	c.Export.PayrollRuns = append(c.Export.PayrollRuns, v)
	return nil
}
func (c *collector) Session(v appmodel.ExportSession) error {
	c.Export.Sessions = append(c.Export.Sessions, v)
	return nil
}
func (c *collector) Schedule(v appmodel.ExportSchedule) error {
	c.Export.ScheduleEntries = append(c.Export.ScheduleEntries, v)
	return nil
}
func (c *collector) ScheduleRow(v appmodel.ExportScheduleRow) error {
	c.Export.ScheduleLines = append(c.Export.ScheduleLines, v)
	return nil
}
func (c *collector) Approval(v appmodel.ExportApproval) error {
	c.Export.TimesheetApprovals = append(c.Export.TimesheetApprovals, v)
	return nil
}
func (c *collector) Report(v appmodel.ExportReport) error {
	c.Export.SavedReports = append(c.Export.SavedReports, v)
	return nil
}
func (c *collector) Webhook(v appmodel.ExportWebhook) error {
	c.Export.Webhooks = append(c.Export.Webhooks, v)
	return nil
}
func (c *collector) Integration(v appmodel.ExportIntegration) error {
	c.Export.Integrations = append(c.Export.Integrations, v)
	return nil
}
