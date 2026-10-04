package db

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

func TestOpenConfiguredContextRejectsNilContext(t *testing.T) {
	var ctx context.Context
	_, err := OpenConfiguredContext(ctx, Config{URL: "postgres://invalid"})
	if !errors.Is(err, ErrNilDatabaseContext) {
		t.Fatalf("OpenConfiguredContext error = %v, want %v", err, ErrNilDatabaseContext)
	}
}

func TestOpenConfiguredContextRequiresSecretKeyWhenConfigured(t *testing.T) {
	_, err := OpenConfiguredContext(context.Background(), Config{RequireSecretKey: true})
	if err == nil || !strings.Contains(err.Error(), "PARATRACK_SECRET_KEY is required") {
		t.Fatalf("OpenConfiguredContext error = %v, want missing secret-key error", err)
	}
}

func TestOpenConfiguredContextRequiresLoggerAndClock(t *testing.T) {
	config := Config{
		URL:    "postgres://invalid",
		Logger: log.New(io.Discard, "", 0),
		Now:    time.Now,
	}
	config.Logger = nil
	if _, err := OpenConfiguredContext(context.Background(), config); !errors.Is(err, ErrMissingLogger) {
		t.Fatalf("OpenConfiguredContext error = %v, want %v", err, ErrMissingLogger)
	}
	config.Logger = log.New(io.Discard, "", 0)
	config.Now = nil
	if _, err := OpenConfiguredContext(context.Background(), config); !errors.Is(err, ErrMissingClock) {
		t.Fatalf("OpenConfiguredContext error = %v, want %v", err, ErrMissingClock)
	}
}
