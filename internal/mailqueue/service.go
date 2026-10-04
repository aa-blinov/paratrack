// Package mailqueue provides a durable, retryable delivery boundary for
// outbound invoice email.
package mailqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"time"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var ErrInvoiceChanged = model.ErrInvoiceChanged
var ErrIncompleteDependencies = errors.New("mail queue service dependencies are incomplete")

const invoiceEmailPayloadVersion = 1

type invoiceEmailPayload struct {
	Version int `json:"version"`
	// Keep the message fields at the top level so older binaries can decode
	// new rows and ignore the additive version field during rolling deploys.
	mailport.Message
}

type Store interface {
	EnqueueInvoiceEmail(context.Context, appmodel.InvoiceEmailEnqueueRequest) error
	ClaimInvoiceEmail(context.Context) (mailport.InvoiceEmailJob, bool, error)
	CompleteInvoiceEmail(context.Context, mailport.InvoiceEmailJob) error
	RetryInvoiceEmail(context.Context, appmodel.InvoiceEmailRetryRequest) error
}

// Logger is the minimal process logging capability used by the queue worker.
type Logger interface {
	Printf(string, ...any)
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Service struct {
	store  Store
	now    func() time.Time
	logger Logger
	audit  AuditRecorder
}

func New(store Store, now func() time.Time, logger Logger, audit AuditRecorder) (*Service, error) {
	if depcheck.IsNil(store) || now == nil || depcheck.IsNil(logger) || depcheck.IsNil(audit) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: store, now: now, logger: logger, audit: audit}, nil
}

func (s *Service) EnqueueInvoice(ctx context.Context, request mailport.InvoiceQueueRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.Revision <= 0 || request.InvoiceNumber == "" || request.Recipient == "" || request.Message.To != request.Recipient || request.Message.Subject == "" || request.Message.Text == "" {
		return fmt.Errorf("invalid invoice email request")
	}
	payload, err := json.Marshal(invoiceEmailPayload{Version: invoiceEmailPayloadVersion, Message: request.Message})
	if err != nil {
		return fmt.Errorf("encode invoice email payload: %w", err)
	}
	if err := s.store.EnqueueInvoiceEmail(ctx, appmodel.InvoiceEmailEnqueueRequest{
		TeamID: request.TeamID, InvoiceID: request.InvoiceID, Revision: request.Revision,
		Recipient: request.Recipient, Payload: payload,
	}); err != nil {
		return fmt.Errorf("enqueue invoice email: %w", err)
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: request.TeamID, UserID: requestctx.ActorID(ctx), Action: "invoice.send_queued",
		Target: request.InvoiceNumber, Meta: request.Recipient, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("invoice mail queue: record send_queued audit for invoice %s: %v", request.InvoiceNumber, err)
	}
	return nil
}

// Run drains pending deliveries and polls again on interval. Store leases let
// another process reclaim work left in processing after a crash.
func (s *Service) Run(ctx context.Context, deliver func(context.Context, mailport.Message) error, interval time.Duration) {
	if ctx == nil {
		s.logger.Printf("invoice mail queue: worker not started: nil context")
		return
	}
	if deliver == nil {
		s.logger.Printf("invoice mail queue: worker not started: nil delivery function")
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.drain(ctx, deliver)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) drain(ctx context.Context, deliver func(context.Context, mailport.Message) error) {
	for i := 0; i < 25; i++ {
		if ctx.Err() != nil {
			return
		}
		job, ok, err := s.store.ClaimInvoiceEmail(ctx)
		if err != nil {
			s.logger.Printf("invoice mail queue: claim: %v", err)
			return
		}
		if !ok {
			return
		}
		message, err := decodeInvoiceEmailPayload(job.Payload)
		if err == nil {
			err = deliver(ctx, message)
		}
		if err != nil {
			delay := 5 * time.Second * time.Duration(1<<min(job.Attempts-1, 8))
			if delay > 30*time.Minute {
				delay = 30 * time.Minute
			}
			persistCtx, cancel := postcommit.NewContextWithTimeout(ctx, 5*time.Second)
			retryErr := s.store.RetryInvoiceEmail(persistCtx, appmodel.InvoiceEmailRetryRequest{
				Job: job, Message: err.Error(), AvailableAt: s.now().UTC().Add(delay), MaxAttempts: 10,
			})
			cancel()
			if retryErr != nil {
				s.logger.Printf("invoice mail queue: schedule retry: %v (delivery: %v)", retryErr, err)
			}
			continue
		}
		persistCtx, cancel := postcommit.NewContextWithTimeout(ctx, 5*time.Second)
		completeErr := s.store.CompleteInvoiceEmail(persistCtx, job)
		cancel()
		if completeErr != nil {
			// Delivery may have succeeded. Leave the lease to expire so a
			// replacement worker can reconcile it; this is at-least-once.
			s.logger.Printf("invoice mail queue: complete job %d: %v", job.ID, completeErr)
			return
		}
	}
}

func decodeInvoiceEmailPayload(payload []byte) (mailport.Message, error) {
	var envelope invoiceEmailPayload
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return mailport.Message{}, fmt.Errorf("decode invoice email payload: %w", err)
	}
	if envelope.Version == 0 {
		// Rows written before the payload envelope was introduced contain a
		// mailport.Message directly. Keep them deliverable across upgrades.
		var legacy mailport.Message
		if err := json.Unmarshal(payload, &legacy); err != nil {
			return mailport.Message{}, fmt.Errorf("decode legacy invoice email payload: %w", err)
		}
		if legacy.To == "" || legacy.Subject == "" || legacy.Text == "" {
			return mailport.Message{}, errors.New("invalid legacy invoice email payload")
		}
		return legacy, nil
	}
	if envelope.Version != invoiceEmailPayloadVersion {
		return mailport.Message{}, fmt.Errorf("unsupported invoice email payload version %d", envelope.Version)
	}
	if envelope.Message.To == "" || envelope.Message.Subject == "" || envelope.Message.Text == "" {
		return mailport.Message{}, errors.New("invalid invoice email payload")
	}
	return envelope.Message, nil
}
