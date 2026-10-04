package postcommit

import (
	"context"
	"testing"
	"time"
)

type contextKey struct{}

func TestNewContextDetachesCancellationAndKeepsValues(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "trace"))
	defer cancelParent()

	ctx, cancel := NewContext(parent)
	defer cancel()
	cancelParent()
	if err := ctx.Err(); err != nil {
		t.Fatalf("detached context error = %v, want nil", err)
	}
	if got := ctx.Value(contextKey{}); got != "trace" {
		t.Fatalf("detached context value = %v, want trace", got)
	}
}

func TestNewContextWithTimeoutDetachesCancellationAndBoundsLifetime(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := NewContextWithTimeout(parent, time.Second)
	defer cancel()
	cancelParent()
	if err := ctx.Err(); err != nil {
		t.Fatalf("detached context error = %v, want nil", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("detached context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > time.Second {
		t.Fatalf("detached context remaining lifetime = %v, want (0, 1s]", remaining)
	}
}
