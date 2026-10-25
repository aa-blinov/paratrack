package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/invoicing"
	"github.com/aa-blinov/paratrack/internal/mail"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/push"
	"github.com/aa-blinov/paratrack/internal/reminders"
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

// reminderEmail mails a reminder to the people it names who have not muted its
// topic: the same filter the push channel applies (TIME-14). A person with no
// email address is skipped.
type reminderEmail struct {
	push       *push.Service
	identities interface {
		FindIdentitiesByID(context.Context, []int64) (map[int64]appmodel.UserIdentity, error)
	}
	sender    mail.Sender
	publicURL string
}

func (e reminderEmail) Email(ctx context.Context, notification reminders.Notification) error {
	if e.sender == nil || !mail.Available(e.sender) {
		return nil
	}
	recipients, err := e.push.MailRecipients(ctx, notification.TeamID, notification.UserIDs, notification.Topic)
	if err != nil || len(recipients) == 0 {
		return err
	}
	identities, err := e.identities.FindIdentitiesByID(ctx, recipients)
	if err != nil {
		return err
	}
	body := notification.Body + "\n\n" + e.publicURL + notification.URL
	var sendErrs []error
	for _, id := range recipients {
		user, ok := identities[id]
		if !ok || user.Email == "" {
			continue
		}
		if err := e.sender.Send(ctx, user.Email, notification.Title, body); err != nil {
			sendErrs = append(sendErrs, err)
		}
	}
	return errors.Join(sendErrs...)
}

// crmAudience is what the next-step reminders need from the rest of the app: the
// topic switch, the language a person last used, and the mailbox a letter goes to.
type crmAudience struct {
	push       *push.Service
	identities interface {
		FindIdentitiesByID(context.Context, []int64) (map[int64]appmodel.UserIdentity, error)
	}
	preferences interface {
		Load(context.Context, int64) (appmodel.UserPreferences, error)
	}
	queue interface {
		EnqueueReminder(context.Context, mailport.ReminderQueueRequest) error
	}
	sender mail.Sender
}

// Wanting applies the mute filter the email channel uses (TIME-14). Push delivery
// applies the same filter again, so a muted person gets nothing on either channel.
func (a crmAudience) Wanting(ctx context.Context, teamID int64, userIDs []int64, topic string) ([]int64, error) {
	return a.push.MailRecipients(ctx, teamID, userIDs, topic)
}

func (a crmAudience) Language(ctx context.Context, userID int64) string {
	prefs, err := a.preferences.Load(ctx, userID)
	if err != nil {
		return ""
	}
	return prefs.Language
}

// QueueLetter queues one letter through the mail queue. With no mail configured,
// or no address on the account, there is nothing to send and nothing to queue.
func (a crmAudience) QueueLetter(ctx context.Context, teamID, userID int64, subject, text string) error {
	if a.sender == nil || !mail.Available(a.sender) {
		return nil
	}
	identities, err := a.identities.FindIdentitiesByID(ctx, []int64{userID})
	if err != nil {
		return err
	}
	user, ok := identities[userID]
	if !ok || user.Email == "" {
		return nil
	}
	return a.queue.EnqueueReminder(ctx, mailport.ReminderQueueRequest{
		TeamID: teamID, Recipient: user.Email,
		Message: mailport.Message{To: user.Email, Subject: subject, Text: text},
	})
}

// reminderPushNotifications sends the project and overload reminders through
// the push queue, like every other event.
type reminderPushNotifications struct{ push *push.Service }

func (n reminderPushNotifications) Send(_ context.Context, notification reminders.Notification) error {
	return n.push.EnqueueNotification(push.EnqueueNotificationRequest{
		TeamID: notification.TeamID, UserIDs: notification.UserIDs,
		Notification: push.Notification{Title: notification.Title, Body: notification.Body, URL: notification.URL, Topic: notification.Topic},
	})
}

type trackingPushNotifications struct{ push *push.Service }

func (n trackingPushNotifications) SessionStopped(_ context.Context, request appmodel.SessionStoppedNotification) error {
	return n.push.EnqueueNotification(push.EnqueueNotificationRequest{
		TeamID: request.TeamID, UserIDs: []int64{request.UserID},
		Notification: push.Notification{Title: "Session stopped", Body: request.ActivityName + " finished", URL: "/stats", Topic: appmodel.NotificationTopicSessionStopped},
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
		Notification: push.Notification{Title: "Goal met", Body: request.Goal.ActivityName + ": " + period, URL: "/goals", Topic: appmodel.NotificationTopicGoalAchieved},
	})
}

func (p payrollPushNotifications) EnqueuePayrollPaid(_ context.Context, request appmodel.PayrollPaidNotification) error {
	return p.push.EnqueueNotification(push.EnqueueNotificationRequest{
		TeamID: request.TeamID, UserIDs: request.Recipients,
		Notification: push.Notification{Title: "Payroll paid", Body: "Pay run marked paid", URL: "/stats", Topic: appmodel.NotificationTopicPayrollPaid},
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
		// A key Stripe refuses is a fault the owner can fix in the settings; an outage is not.
		// The wrap keeps Stripe's text for the log, and the caller tells the two apart.
		var status *stripeadapter.StatusError
		if errors.As(err, &status) && (status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden) {
			return "", "", fmt.Errorf("%w: %w", appmodel.ErrStripeKeyRejected, err)
		}
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
