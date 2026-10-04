package web

import (
	"io"
	"log"
	"testing"
	"time"
)

func TestNewRequiresProcessLoggerAndClock(t *testing.T) {
	if _, err := New(Dependencies{}, "", Config{}, RuntimeDependencies{}); err != ErrMissingLogger {
		t.Fatalf("New error = %v, want %v", err, ErrMissingLogger)
	}
	config := Config{Logger: log.New(io.Discard, "", 0)}
	if _, err := New(Dependencies{}, "", config, RuntimeDependencies{}); err != ErrMissingClock {
		t.Fatalf("New error = %v, want %v", err, ErrMissingClock)
	}
	config.Now = time.Now
	if _, err := New(Dependencies{}, "", config, RuntimeDependencies{}); err == ErrMissingLogger || err == ErrMissingClock {
		t.Fatalf("New rejected complete runtime configuration: %v", err)
	}
}
