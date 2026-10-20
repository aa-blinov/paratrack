package workspacefile

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// fileWriter writes an export as its records arrive, so the file grows while the
// database is read. Nothing is written before the team: begin hears the slug and
// the export time first, because a download needs its headers before its first
// byte, and a refused export then never starts one.
type fileWriter struct {
	out        *bufio.Writer
	begin      func(slug string, exportedAt time.Time)
	exportedAt time.Time
	fields     int    // top-level fields written so far
	list       string // the list whose array is open, "" when none is
	items      int    // records written into the open list
}

func newFileWriter(w io.Writer, begin func(slug string, exportedAt time.Time)) *fileWriter {
	return &fileWriter{out: bufio.NewWriterSize(w, 64<<10), begin: begin}
}

// Header keeps the export time. It is written with the team, once the download
// has started.
func (f *fileWriter) Header(exportedAt time.Time) error {
	f.exportedAt = exportedAt
	return nil
}

// Team opens the file: the object, its format and version, the export time and
// the team settings.
func (f *fileWriter) Team(t appmodel.ExportTeam) error {
	if f.begin != nil {
		f.begin(t.Slug, f.exportedAt)
	}
	if _, err := f.out.WriteString("{"); err != nil {
		return err
	}
	if err := f.field("format", appmodel.WorkspaceExportFormat); err != nil {
		return err
	}
	if err := f.field("version", appmodel.WorkspaceExportVersion); err != nil {
		return err
	}
	if err := f.field("exported_at", f.exportedAt); err != nil {
		return err
	}
	return f.field("team", encodeExportTeam(t))
}

func (f *fileWriter) Member(m appmodel.ExportMember) error {
	return f.item("members", encodeExportMember(m))
}

func (f *fileWriter) Client(c appmodel.ExportClient) error {
	return f.item("clients", encodeExportClient(c))
}

func (f *fileWriter) Project(p appmodel.ExportProject) error {
	return f.item("projects", encodeExportProject(p))
}

func (f *fileWriter) Tag(t appmodel.ExportTag) error {
	return f.item("tags", encodeExportTag(t))
}

func (f *fileWriter) Task(t appmodel.ExportTask) error {
	return f.item("tasks", encodeExportTask(t))
}

func (f *fileWriter) Activity(a appmodel.ExportActivity) error {
	return f.item("activities", encodeExportActivity(a))
}

func (f *fileWriter) Goal(g appmodel.ExportGoal) error {
	return f.item("goals", encodeExportGoal(g))
}

func (f *fileWriter) Invoice(inv appmodel.ExportInvoice) error {
	return f.item("invoices", encodeExportInvoice(inv))
}

func (f *fileWriter) PayrollRun(run appmodel.ExportPayrollRun) error {
	return f.item("payroll_runs", encodeExportPayrollRun(run))
}

func (f *fileWriter) Session(s appmodel.ExportSession) error {
	return f.item("sessions", encodeExportSession(s))
}

func (f *fileWriter) Schedule(e appmodel.ExportSchedule) error {
	return f.item("schedule_entries", encodeExportSchedule(e))
}

func (f *fileWriter) ScheduleRow(l appmodel.ExportScheduleRow) error {
	return f.item("schedule_lines", encodeExportScheduleRow(l))
}

func (f *fileWriter) Approval(a appmodel.ExportApproval) error {
	return f.item("timesheet_approvals", encodeExportApproval(a))
}

func (f *fileWriter) Report(rep appmodel.ExportReport) error {
	return f.item("saved_reports", encodeExportReport(rep))
}

func (f *fileWriter) Webhook(w appmodel.ExportWebhook) error {
	return f.item("webhooks", encodeExportWebhook(w))
}

func (f *fileWriter) Integration(i appmodel.ExportIntegration) error {
	return f.item("integrations", encodeExportIntegration(i))
}

// Close ends the file. A download cut short by an error never reaches Close, so
// it is never taken for a whole one.
func (f *fileWriter) Close() error {
	if f.fields == 0 {
		return fmt.Errorf("export wrote no workspace")
	}
	if err := f.closeList(); err != nil {
		return err
	}
	if _, err := f.out.WriteString("}\n"); err != nil {
		return err
	}
	return f.out.Flush()
}

// field writes one top-level field whose value is complete.
func (f *fileWriter) field(name string, v any) error {
	if err := f.key(name); err != nil {
		return err
	}
	return f.value(v)
}

// item writes one record into the list called name, opening the list first when
// the previous record was in another one.
func (f *fileWriter) item(name string, v any) error {
	if f.list != name {
		if err := f.key(name); err != nil {
			return err
		}
		if _, err := f.out.WriteString("["); err != nil {
			return err
		}
		f.list = name
	}
	if f.items > 0 {
		if _, err := f.out.WriteString(","); err != nil {
			return err
		}
	}
	f.items++
	return f.value(v)
}

// key starts the next top-level field. A list still open is closed first.
func (f *fileWriter) key(name string) error {
	if err := f.closeList(); err != nil {
		return err
	}
	if f.fields > 0 {
		if _, err := f.out.WriteString(","); err != nil {
			return err
		}
	}
	f.fields++
	if err := f.value(name); err != nil {
		return err
	}
	_, err := f.out.WriteString(":")
	return err
}

func (f *fileWriter) closeList() error {
	if f.list == "" {
		return nil
	}
	f.list, f.items = "", 0
	_, err := f.out.WriteString("]")
	return err
}

func (f *fileWriter) value(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode workspace export: %w", err)
	}
	_, err = f.out.Write(b)
	return err
}
