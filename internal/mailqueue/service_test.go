package mailqueue

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"io"
	"log"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type retryStore struct {
	job      mailport.InvoiceEmailJob
	claimed  bool
	retryAt  time.Time
	maxTries int
	payload  []byte
}

type auditSpy struct {
	action, target, meta, ip string
	team, actor              int64
}

func (s *auditSpy) Record(_ context.Context, record model.AuditRecord) error {
	s.team, s.actor, s.action, s.target, s.meta, s.ip = record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP
	return nil
}

type noopAudit struct{}

func (noopAudit) Record(context.Context, model.AuditRecord) error {
	return nil
}

func (s *retryStore) EnqueueInvoiceEmail(_ context.Context, request appmodel.InvoiceEmailEnqueueRequest) error {
	s.payload = append([]byte(nil), request.Payload...)
	return nil
}

func (s *retryStore) ClaimInvoiceEmail(context.Context) (mailport.InvoiceEmailJob, bool, error) {
	if s.claimed {
		return mailport.InvoiceEmailJob{}, false, nil
	}
	s.claimed = true
	return s.job, true, nil
}

func (*retryStore) CompleteInvoiceEmail(context.Context, mailport.InvoiceEmailJob) error {
	return nil
}

func (s *retryStore) RetryInvoiceEmail(_ context.Context, request appmodel.InvoiceEmailRetryRequest) error {
	s.retryAt = request.AvailableAt
	s.maxTries = request.MaxAttempts
	return nil
}

func TestDrainSchedulesRetryFromInjectedClock(t *testing.T) {
	now := time.Date(2025, time.April, 5, 6, 7, 8, 0, time.UTC)
	store := &retryStore{job: mailport.InvoiceEmailJob{ID: 1, Attempts: 1, Payload: []byte(`{"To":"a@example.test","Subject":"Invoice","Text":"Body"}`)}}
	service, err := New(store, func() time.Time { return now }, log.New(io.Discard, "", 0), noopAudit{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	service.drain(context.Background(), func(context.Context, mailport.Message) error { return errors.New("temporary SMTP failure") })
	if !store.retryAt.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("retry time = %s, want %s", store.retryAt, now.Add(5*time.Second))
	}
	if store.maxTries != 10 {
		t.Fatalf("max attempts = %d, want 10", store.maxTries)
	}
}

func TestEnqueueInvoiceAuditsOnlyAfterQueueAcceptsJob(t *testing.T) {
	audit := &auditSpy{}
	store := &retryStore{}
	service, err := New(store, time.Now, log.New(io.Discard, "", 0), audit)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 8)
	ctx = requestctx.WithClientIP(ctx, "203.0.113.12")
	message := mailport.Message{To: "client@example.test", Subject: "Invoice", Text: "Frozen body"}
	if err := service.EnqueueInvoice(ctx, mailport.InvoiceQueueRequest{
		TeamID: 5, InvoiceID: 19, Revision: 3, InvoiceNumber: "INV-19", Recipient: "client@example.test", Message: message,
	}); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeInvoiceEmailPayload(store.payload)
	if err != nil || decoded.To != message.To || decoded.Text != message.Text {
		t.Fatalf("decode queued message = %+v, err %v", decoded, err)
	}
	var oldReader mailport.Message
	if err := json.Unmarshal(store.payload, &oldReader); err != nil || oldReader.To != message.To || oldReader.Text != message.Text {
		t.Fatalf("old reader could not decode new payload: %+v, err %v", oldReader, err)
	}
	want := auditSpy{action: "invoice.send_queued", target: "INV-19", meta: "client@example.test", ip: "203.0.113.12", team: 5, actor: 8}
	if *audit != want {
		t.Fatalf("audit = %+v, want %+v", *audit, want)
	}
}

func TestDecodeInvoiceEmailPayloadSupportsLegacyRows(t *testing.T) {
	message, err := decodeInvoiceEmailPayload([]byte(`{"To":"client@example.test","Subject":"Invoice","Text":"Frozen body"}`))
	if err != nil {
		t.Fatalf("decode legacy payload: %v", err)
	}
	if message.To != "client@example.test" || message.Subject != "Invoice" || message.Text != "Frozen body" {
		t.Fatalf("decoded legacy message = %+v", message)
	}
}
