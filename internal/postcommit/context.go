// Package postcommit creates bounded contexts for effects that follow a
// committed state change and must outlive cancellation of the initiating
// request long enough to be recorded or queued.
package postcommit

import (
	"context"
	"time"
)

const timeout = 10 * time.Second

// NewContext preserves request values while removing caller cancellation and
// its deadline. The new timeout keeps post-commit work bounded. ctx must be
// non-nil, following the standard context contract.
func NewContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return NewContextWithTimeout(ctx, timeout)
}

// NewContextWithTimeout creates a detached context with a caller-selected
// upper bound for recording an outcome after its initiating operation ends.
func NewContextWithTimeout(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), limit)
}
