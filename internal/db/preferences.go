package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// UserPrefs is the raw JSON of a user's preferences ("" = defaults).
func (d *DB) UserPrefs(ctx context.Context, userID int64) (string, error) {
	var p string
	err := d.sql.QueryRowContext(ctx, `SELECT prefs FROM users WHERE id = ?`, userID).Scan(&p)
	return p, err
}

func (d *DB) SetUserPrefs(ctx context.Context, request appmodel.UserPrefsSaveCommand) error {
	if request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	return execRequireRows(ctx, d.sql, `UPDATE users SET prefs = ? WHERE id = ?`, request.JSON, request.UserID)
}

// ---------------------------------------------------------------------------
// Muted notification topics (see /settings/notifications).
// ---------------------------------------------------------------------------

// notificationTopics lists every topic the application can be asked about. A
// topic the code never sends is deliberately absent: offering it as a choice
// would promise a notification that cannot arrive.
var notificationTopics = []string{
	appmodel.NotificationTopicSessionStopped,
	appmodel.NotificationTopicGoalAchieved,
	appmodel.NotificationTopicPayrollPaid,
}

func isNotificationTopic(topic string) bool {
	for _, known := range notificationTopics {
		if known == topic {
			return true
		}
	}
	return false
}

// parseMutedTopics reads the comma-separated list stored on the user row.
// Unknown entries are dropped instead of failing the read: a topic the app
// stopped sending must not break someone's notification settings.
func parseMutedTopics(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if topic := strings.TrimSpace(part); isNotificationTopic(topic) {
			out = append(out, topic)
		}
	}
	return out
}

func formatMutedTopics(topics []string) string {
	return strings.Join(topics, ",")
}

// MutedNotificationTopics reports which topics one person switched off. The
// result is empty for an account that never touched the setting, which is
// exactly the state that keeps every notification arriving as before.
func (d *DB) MutedNotificationTopics(ctx context.Context, request appmodel.NotificationTopicsQuery) ([]string, error) {
	if request.TeamID <= 0 || request.UserID <= 0 {
		return nil, model.ErrNotFound
	}
	var raw string
	err := d.sql.QueryRowContext(ctx, `SELECT muted_notifications FROM users WHERE id = ?`, request.UserID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return parseMutedTopics(raw), nil
}

// SetMutedNotificationTopics replaces the caller's selection with the whole
// list, so an empty request restores every topic instead of storing a stale
// partial answer. Like the rest of the personal preferences it is one row of
// the caller's own account: the owner check is the authorization boundary.
func (d *DB) SetMutedNotificationTopics(ctx context.Context, request appmodel.NotificationTopicsCommand) error {
	if request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	var muted []string
	for _, topic := range request.Topics {
		if !isNotificationTopic(topic) {
			return appmodel.ErrInvalidNotificationTopic
		}
		muted = append(muted, topic)
	}
	return execRequireRows(ctx, d.sql,
		`UPDATE users SET muted_notifications = ? WHERE id = ?`, formatMutedTopics(muted), request.UserID)
}
