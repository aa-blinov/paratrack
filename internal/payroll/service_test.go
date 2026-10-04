package payroll

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type payrollClockStore struct {
	Store
	at      time.Time
	run     model.PayrollRun
	details model.PayrollRunDetails
	runs    []model.PayrollRunDetails
	lookup  appmodel.PayrollRunLookupQuery
	list    appmodel.PayrollRunListQuery
}

type payrollAuditNoop struct{}

func (payrollAuditNoop) Record(context.Context, model.AuditRecord) error {
	return nil
}

type payrollLoggerNoop struct{}

func (payrollLoggerNoop) Printf(string, ...any) {}

type payrollAuditCall struct {
	team, actor        int64
	action, target, ip string
}

type payrollAuditSpy struct{ calls []payrollAuditCall }

func (s *payrollAuditSpy) Record(_ context.Context, record model.AuditRecord) error {
	s.calls = append(s.calls, payrollAuditCall{record.TeamID, record.UserID, record.Action, record.Target, record.IP})
	return nil
}

func (s *payrollClockStore) CreatePayrollDraft(_ context.Context, request appmodel.PayrollDraftRequest) (model.PayrollRun, []model.PayrollRun, error) {
	s.at = request.CreatedAt
	return s.run, nil, nil
}

func (s *payrollClockStore) ListPayrollRunDetails(_ context.Context, query appmodel.PayrollRunListQuery) ([]model.PayrollRunDetails, error) {
	s.list = query
	return s.runs, nil
}

func (s *payrollClockStore) GetPayrollRunDetails(_ context.Context, query appmodel.PayrollRunLookupQuery) (model.PayrollRunDetails, error) {
	s.lookup = query
	return s.details, nil
}

func TestCreateRunRecordsAuditOnlyWhenDraftWasCreated(t *testing.T) {
	store := &payrollClockStore{run: model.PayrollRun{ID: 19, Number: "PAY-2026-001"}}
	audit := &payrollAuditSpy{}
	service, err := NewServiceWithClock(store, func() time.Time { return time.Date(2026, time.April, 2, 0, 0, 0, 0, time.UTC) }, audit, payrollLoggerNoop{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.11")
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	request := appmodel.PayrollDraftRequest{TeamID: 4, CallerID: 7, Start: start, End: start.AddDate(0, 1, 0)}
	if _, _, err := service.CreateRun(ctx, request); err != nil {
		t.Fatal(err)
	}
	if len(audit.calls) != 1 {
		t.Fatalf("audit calls = %+v, want one", audit.calls)
	}
	got := audit.calls[0]
	want := payrollAuditCall{team: 4, actor: 7, action: "payroll.create", target: "PAY-2026-001", ip: "203.0.113.11"}
	if got != want {
		t.Fatalf("audit call = %+v, want %+v", got, want)
	}

	audit.calls = nil
	store.run = model.PayrollRun{}
	if _, _, err := service.CreateRun(ctx, request); err != nil {
		t.Fatal(err)
	}
	if len(audit.calls) != 0 {
		t.Fatalf("overlap-only result recorded audit: %+v", audit.calls)
	}
}

func TestCreateRunPassesOneUTCClockInstantToStore(t *testing.T) {
	localTime := time.Date(2026, time.March, 1, 2, 30, 0, 0, time.FixedZone("test", 2*60*60))
	store := &payrollClockStore{}
	clockCalls := 0
	service, err := NewServiceWithClock(store, func() time.Time {
		clockCalls++
		return localTime
	}, payrollAuditNoop{}, payrollLoggerNoop{})
	if err != nil {
		t.Fatalf("NewServiceWithClock: %v", err)
	}
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	if _, _, err := service.CreateRun(context.Background(), appmodel.PayrollDraftRequest{TeamID: 1, CallerID: 2, Start: start, End: end}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if !store.at.Equal(localTime) || store.at.Location() != time.UTC {
		t.Fatalf("store clock = %s (%s), want %s in UTC", store.at, store.at.Location(), localTime.UTC())
	}
	if clockCalls != 1 {
		t.Fatalf("clock called %d times, want once", clockCalls)
	}
}

func TestPayrollReadResultsIncludeTotals(t *testing.T) {
	lines := []model.PayrollLine{{Seconds: 1800, AmountCents: 1250}, {Seconds: 5400, AmountCents: 3750}}
	run := model.PayrollRun{ID: 5, Number: "PAY-5"}
	store := &payrollClockStore{
		runs:    []model.PayrollRunDetails{{Run: run, Lines: lines}},
		details: model.PayrollRunDetails{Run: run, Lines: lines},
	}
	service, err := NewServiceWithClock(store, time.Now, payrollAuditNoop{}, payrollLoggerNoop{})
	if err != nil {
		t.Fatal(err)
	}

	summaries, err := service.ListRuns(context.Background(), appmodel.PayrollRunListQuery{TeamID: 2})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	detail, err := service.GetRun(context.Background(), 2, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].TotalCents != 5000 || summaries[0].TotalHoursHundredths != 200 {
		t.Fatalf("ListRuns() summary = %+v", summaries)
	}
	if detail.TotalCents != 5000 || detail.TotalHoursHundredths != 200 || len(detail.Lines) != 2 {
		t.Fatalf("GetRun() detail = %+v", detail)
	}
	if want := (appmodel.PayrollRunLookupQuery{TeamID: 2, RunID: run.ID}); store.lookup != want {
		t.Fatalf("GetPayrollRunDetails query = %+v, want %+v", store.lookup, want)
	}
	if want := (appmodel.PayrollRunListQuery{TeamID: 2}); store.list != want {
		t.Fatalf("ListPayrollRunDetails query = %+v, want %+v", store.list, want)
	}
}

func TestPayrollLineTotalsRejectOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	_, _, err := payrollLineTotals([]model.PayrollLine{{AmountCents: maxInt}, {AmountCents: 1}})
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("payrollLineTotals() error = %v, want %v", err, money.ErrOverflow)
	}
}
