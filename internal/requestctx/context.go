// Package requestctx carries authenticated actor and session-scope metadata
// across application workflows and persistence calls.
package requestctx

import "context"

type key uint8

const (
	actorKey key = iota
	scopeKey
	clientIPKey
	teamIDKey
	localeKey
)

func WithActor(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, actorKey, userID)
}

func WithScope(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, scopeKey, userID)
}

// ActorID returns the authenticated user ID, or zero when the request has no actor.
func ActorID(ctx context.Context) int64 {
	id, _ := ctx.Value(actorKey).(int64)
	return id
}

// ActorValue returns nil when no actor is present, ready for nullable SQL args.
func ActorValue(ctx context.Context) any {
	if id := ActorID(ctx); id > 0 {
		return id
	}
	return nil
}

// ScopedUserID returns the user whose sessions are visible, or zero for the
// full authorized workspace scope.
func ScopedUserID(ctx context.Context) int64 {
	id, _ := ctx.Value(scopeKey).(int64)
	return id
}

// WithClientIP attaches the transport-resolved client address for application
// audit workflows without exposing HTTP request types to those workflows.
func WithClientIP(ctx context.Context, address string) context.Context {
	return context.WithValue(ctx, clientIPKey, address)
}

// ClientIP returns the trusted client address attached by the HTTP adapter.
func ClientIP(ctx context.Context) string {
	address, _ := ctx.Value(clientIPKey).(string)
	return address
}

// WithTeamID attaches the authenticated workspace scope for workflow audit
// and application operations without exposing transport context types.
func WithTeamID(ctx context.Context, teamID int64) context.Context {
	return context.WithValue(ctx, teamIDKey, teamID)
}

func TeamID(ctx context.Context) int64 {
	id, _ := ctx.Value(teamIDKey).(int64)
	return id
}

// WithLocale attaches the resolved user-facing language for application
// notifications without carrying a transport request into workflows.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeKey, locale)
}

func Locale(ctx context.Context) string {
	locale, _ := ctx.Value(localeKey).(string)
	return locale
}
