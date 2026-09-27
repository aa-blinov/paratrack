package db

import "context"

// Who a request acts as, carried in the context so every query and insert
// honours it without threading a user id through each call.
//
//   - actor: stamped as sessions.user_id on every new session (timer,
//     backfill, timesheet, import, API), so payroll and "by person"
//     reports know whose time it is.
//   - scope: when set, session reads and deletes see only that user's
//     sessions. A member is always scoped to self; an owner/admin only on
//     personal screens (dashboard, timers, timesheet).
type ctxKey int

const (
	actorKey ctxKey = iota
	scopeKey
)

func WithActor(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, actorKey, userID)
}

func WithScope(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, scopeKey, userID)
}

// ScopedTo reports the user a context is scoped to (0 = whole team).
func ScopedTo(ctx context.Context) int64 {
	v, _ := ctx.Value(scopeKey).(int64)
	return v
}

func actorOf(ctx context.Context) any {
	if v, _ := ctx.Value(actorKey).(int64); v > 0 {
		return v
	}
	return nil
}

// scopeSQL is the extra filter for a sessions alias ("s." or "").
func scopeSQL(ctx context.Context, col string, args []any) (string, []any) {
	if uid := ScopedTo(ctx); uid > 0 {
		return " AND " + col + " = ?", append(args, uid)
	}
	return "", args
}

func actorID(ctx context.Context) int64 {
	v, _ := ctx.Value(actorKey).(int64)
	return v
}
