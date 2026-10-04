package memberadmin

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

type memberReaderStub struct {
	teamID int64
	result []model.TeamMember
	err    error
}

func (s *memberReaderStub) Members(_ context.Context, teamID int64) ([]model.TeamMember, error) {
	s.teamID = teamID
	return s.result, s.err
}

type payrollSettingsReaderStub struct {
	teamID int64
	result []model.MemberPayrollSettings
	err    error
}

func (s *payrollSettingsReaderStub) MemberSettings(_ context.Context, teamID int64) ([]model.MemberPayrollSettings, error) {
	s.teamID = teamID
	return s.result, s.err
}

func TestManagementAssemblesTeamScopedMemberAndPayrollSnapshot(t *testing.T) {
	members := &memberReaderStub{result: []model.TeamMember{{UserID: 5, Name: "Alex"}}}
	payroll := &payrollSettingsReaderStub{result: []model.MemberPayrollSettings{{UserID: 5, PayCents: 4200, CapacityMinutes: 360}}}
	service, err := New(Dependencies{Members: members, Payroll: payroll})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Management(context.Background(), 12)
	if err != nil {
		t.Fatalf("load management snapshot: %v", err)
	}
	if members.teamID != 12 || payroll.teamID != 12 {
		t.Fatalf("queries used member team=%d payroll team=%d, want both 12", members.teamID, payroll.teamID)
	}
	if len(snapshot.Members) != 1 || snapshot.Members[0].UserID != 5 || len(snapshot.PaySettings) != 1 || snapshot.PaySettings[0].PayCents != 4200 {
		t.Fatalf("management snapshot = %+v", snapshot)
	}
}

func TestManagementStopsWhenMemberReadFails(t *testing.T) {
	wantErr := errors.New("member store unavailable")
	members := &memberReaderStub{err: wantErr}
	payroll := &payrollSettingsReaderStub{}
	service, err := New(Dependencies{Members: members, Payroll: payroll})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Management(context.Background(), 12); !errors.Is(err, wantErr) {
		t.Fatalf("Management() error = %v, want %v", err, wantErr)
	}
	if payroll.teamID != 0 {
		t.Fatalf("payroll read ran after member error for team %d", payroll.teamID)
	}
}
