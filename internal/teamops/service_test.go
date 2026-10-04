package teamops

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type mutationStub struct{ err error }

func (s *mutationStub) SetRole(context.Context, appmodel.TeamMemberRoleRequest) error {
	return s.err
}
func (s *mutationStub) SetStripeCredentials(context.Context, appmodel.TeamStripeCredentialsRequest) error {
	return s.err
}
func (s *mutationStub) TransferOwnership(context.Context, appmodel.TeamOwnershipTransferRequest) error {
	return s.err
}
func (s *mutationStub) UpdateBilling(context.Context, appmodel.TeamBillingRequest) error {
	return s.err
}
func (s *mutationStub) UpdateCurrency(context.Context, appmodel.TeamCurrencyRequest) error {
	return s.err
}

type workspaceStub struct {
	remaining []int64
	err       error
}

func (s *workspaceStub) DeleteAndListRemaining(context.Context, appmodel.WorkspaceDeleteRequest) ([]int64, error) {
	return s.remaining, s.err
}

type sessionRevokerStub struct {
	calls  int
	err    error
	ctxErr error
}

func (s *sessionRevokerStub) DeleteByUser(ctx context.Context, _ int64) error {
	s.calls++
	s.ctxErr = ctx.Err()
	return s.err
}

type auditStub struct {
	calls  int
	team   int64
	actor  int64
	action string
	target string
	meta   string
	ip     string
}

func (s *auditStub) Record(_ context.Context, record model.AuditRecord) error {
	s.calls++
	s.team, s.actor, s.action, s.target, s.meta, s.ip = record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP
	return nil
}

func TestUpdateBillingAuditsSuccessfulMutation(t *testing.T) {
	audit := &auditStub{}
	service, err := New(Dependencies{
		Mutations: &mutationStub{}, Workspace: &workspaceStub{}, Sessions: &sessionRevokerStub{},
		Audit: audit, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.4")
	rules := model.BillingRules{RoundMinutes: 15, RoundMode: "up", InvoicePrefix: "INV"}
	if err := service.UpdateBilling(ctx, appmodel.TeamBillingRequest{TeamID: 3, CallerID: 7, Rules: rules}); err != nil {
		t.Fatal(err)
	}
	if audit.calls != 1 || audit.team != 3 || audit.actor != 7 || audit.action != "team.billing" || audit.target != "15 up INV" || audit.ip != "203.0.113.4" {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestFailedMutationDoesNotWriteAudit(t *testing.T) {
	audit := &auditStub{}
	service, err := New(Dependencies{
		Mutations: &mutationStub{err: errors.New("write failed")},
		Workspace: &workspaceStub{}, Sessions: &sessionRevokerStub{},
		Audit: audit, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetStripeCredentials(context.Background(), appmodel.TeamStripeCredentialsRequest{TeamID: 3, CallerID: 7, Key: "secret", Secret: "webhook-secret"}); err == nil {
		t.Fatal("mutation error was swallowed")
	}
	if audit.calls != 0 {
		t.Fatalf("audit calls = %d, want 0", audit.calls)
	}
}

func TestDeleteWorkspaceRevokesSessionsOnlyWhenNoMembershipRemains(t *testing.T) {
	workspace := &workspaceStub{remaining: []int64{8}}
	sessions := &sessionRevokerStub{}
	service, err := New(Dependencies{
		Mutations: &mutationStub{}, Workspace: workspace, Sessions: sessions,
		Audit: &auditStub{}, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := service.DeleteWorkspace(context.Background(), appmodel.WorkspaceDeleteRequest{TeamID: 3, CallerID: 7})
	if err != nil || len(remaining) != 1 || remaining[0] != 8 || sessions.calls != 0 {
		t.Fatalf("remaining=%v err=%v revoke calls=%d", remaining, err, sessions.calls)
	}
	workspace.remaining = nil
	sessions.err = errors.New("session store unavailable")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.DeleteWorkspace(canceled, appmodel.WorkspaceDeleteRequest{TeamID: 3, CallerID: 7}); err != nil {
		t.Fatalf("best-effort session revocation changed deletion result: %v", err)
	}
	if sessions.calls != 1 || sessions.ctxErr != nil {
		t.Fatalf("revoke calls = %d with context err %v, want one live detached call", sessions.calls, sessions.ctxErr)
	}
}
