package appmodel

import "errors"

// Notification topics are the event kinds a person can switch off on
// /settings/notifications. The values are stored verbatim, so a producer that
// does not name a topic keeps today's behavior: the notification is delivered.
const (
	NotificationTopicSessionStopped = "session.stopped"
	NotificationTopicGoalAchieved   = "goal.achieved"
	NotificationTopicPayrollPaid    = "payroll.paid"
)

// ErrInvalidNotificationTopic means a caller asked to mute something the
// application does not notify about, so the stored selection would keep
// growing with dead keys.
var ErrInvalidNotificationTopic = errors.New("unknown notification topic")

// ErrNotificationNotDelivered means the push channel was reached and refused
// the message. A verification request turns this into a named outcome instead
// of a generic failure, because "the channel answered no" is the answer.
var ErrNotificationNotDelivered = errors.New("notification was not delivered")

// NotificationTopicsQuery reads the topics one person muted. An empty result
// means nothing is muted: either the account predates the setting or it wants
// every topic.
type NotificationTopicsQuery struct {
	TeamID int64
	UserID int64
}

// NotificationTopicsCommand replaces the caller's muted topics with the whole
// selection, so clearing the last one restores the pre-setting behavior.
type NotificationTopicsCommand struct {
	TeamID   int64
	UserID   int64
	CallerID int64
	Topics   []string
}

// NotificationTargetsQuery narrows a delivery batch to the recipients who did
// not mute the topic.
type NotificationTargetsQuery struct {
	TeamID  int64
	UserIDs []int64
	Topic   string // "" = keep every recipient whatever their selection
}

// NotificationTestRequest asks for a verification notification on the
// caller's own devices. The caller supplies the localized text so delivery
// stays free of a language of its own.
type NotificationTestRequest struct {
	TeamID   int64
	UserID   int64
	CallerID int64
	Title    string
	Body     string
	URL      string
}

// NotificationTestResult reports what the push channel actually did, so the
// settings screen can name the outcome instead of assuming delivery. Zero
// delivered with no error means the account has no subscribed device the
// server can reach: the channel is not set up, not silently broken.
type NotificationTestResult struct {
	Delivered int // devices that accepted the notification
}

// SessionStoppedNotification describes the post-commit notification for a
// timer transition without coupling the tracking workflow to a delivery
// provider.
type SessionStoppedNotification struct {
	TeamID       int64
	UserID       int64
	ActivityName string
}

// GoalAchievedNotification carries the goal result and recipient to the
// notification adapter after a successful tracking transition.
type GoalAchievedNotification struct {
	TeamID int64
	UserID int64
	Goal   GoalProgress
}

// PayrollPaidNotification identifies recipients of a completed payroll run.
type PayrollPaidNotification struct {
	TeamID     int64
	Recipients []int64
}
