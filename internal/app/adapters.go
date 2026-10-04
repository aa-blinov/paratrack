package app

import (
	"context"
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/invoicing"
	"github.com/aa-blinov/paratrack/internal/push"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	stripeadapter "github.com/aa-blinov/paratrack/internal/stripe"
	"github.com/aa-blinov/paratrack/internal/teams"
)

type workspaceLifecycle struct{ teams *teams.Service }

func (w workspaceLifecycle) DeleteAndListRemaining(ctx context.Context, request appmodel.WorkspaceDeleteRequest) ([]int64, error) {
	remaining, err := w.teams.DeleteAndListRemaining(ctx, request)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(remaining))
	for i, team := range remaining {
		ids[i] = team.ID
	}
	return ids, nil
}

type payrollPushNotifications struct{ push *push.Service }

type trackingPushNotifications struct{ push *push.Service }

func (n trackingPushNotifications) SessionStopped(_ context.Context, request appmodel.SessionStoppedNotification) error {
	return n.push.EnqueueNotification(push.EnqueueNotificationRequest{
		TeamID: request.TeamID, UserIDs: []int64{request.UserID},
		Notification: push.Notification{Title: "Session stopped", Body: request.ActivityName + " finished", URL: "/stats"},
	})
}

func (n trackingPushNotifications) GoalAchieved(ctx context.Context, request appmodel.GoalAchievedNotification) error {
	period := request.Goal.Goal.Period
	if key := map[string]string{
		"daily": "period.today", "weekly": "period.thisWeek", "monthly": "period.thisMonth",
	}[period]; key != "" {
		period = i18n.T(i18n.Normalize(requestctx.Locale(ctx)), key)
	}
	return n.push.EnqueueNotification(push.EnqueueNotificationRequest{
		TeamID: request.TeamID, UserIDs: []int64{request.UserID},
		Notification: push.Notification{Title: "Goal met", Body: request.Goal.ActivityName + ": " + period, URL: "/goals"},
	})
}

func (p payrollPushNotifications) EnqueuePayrollPaid(_ context.Context, request appmodel.PayrollPaidNotification) error {
	return p.push.EnqueueNotification(push.EnqueueNotificationRequest{
		TeamID: request.TeamID, UserIDs: request.Recipients,
		Notification: push.Notification{Title: "Payroll paid", Body: "Pay run marked paid", URL: "/stats"},
	})
}

type idleHTTPClients []*http.Client

// stripeProtocol adapts the provider package to the invoicing workflow's
// outbound port at the application composition boundary.
type stripeProtocol struct{ client *http.Client }

type stripeInvoiceProcessor struct{ service *invoicing.Service }

func (p stripeInvoiceProcessor) ProcessStripeWebhook(ctx context.Context, event appmodel.StripePaymentEvent) (int64, int64, bool, error) {
	result, err := p.service.ProcessStripeWebhook(ctx, event)
	return result.TeamID, result.InvoiceID, result.Changed, err
}

func (p stripeProtocol) CreateCheckout(ctx context.Context, key string, input invoicing.StripeCheckoutRequest) (string, string, error) {
	session, err := stripeadapter.CreateCheckout(ctx, p.client, key, stripeadapter.CheckoutRequest{
		InvoiceID: input.InvoiceID, TeamID: input.TeamID, TotalCents: input.TotalCents,
		Currency: input.Currency, InvoiceNumber: input.InvoiceNumber, SuccessURL: input.SuccessURL,
	})
	if err != nil {
		return "", "", err
	}
	return session.ID, session.URL, nil
}

func (p stripeProtocol) ExpireCheckout(ctx context.Context, key, sessionID string) error {
	return stripeadapter.ExpireCheckout(ctx, p.client, key, sessionID)
}

func (stripeProtocol) ParseWebhookEvent(body []byte) (invoicing.StripeWebhookEvent, error) {
	event, err := stripeadapter.ParseWebhookEvent(body)
	if err != nil {
		return invoicing.StripeWebhookEvent{}, err
	}
	return invoicing.StripeWebhookEvent{
		Type: event.Type, SessionID: event.SessionID, InvoiceID: event.InvoiceID,
		TeamID: event.TeamID, PaymentStatus: event.PaymentStatus,
	}, nil
}

func (stripeProtocol) VerifyWebhookSignature(signature string, body []byte, secret string, now time.Time) bool {
	return stripeadapter.VerifyWebhookSignature(signature, body, secret, now)
}

func (clients idleHTTPClients) Close() error {
	for _, client := range clients {
		client.CloseIdleConnections()
	}
	return nil
}
