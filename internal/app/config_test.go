package app

import (
	"io"
	"log"
	"testing"
	"time"
)

func TestValidateConfigRequiresProcessLoggerAndClock(t *testing.T) {
	config := Config{Now: time.Now}
	if err := validateConfig(config); err != ErrMissingLogger {
		t.Fatalf("validateConfig error = %v, want %v", err, ErrMissingLogger)
	}
	config.Logger = log.New(io.Discard, "", 0)
	config.Now = nil
	if err := validateConfig(config); err != ErrMissingClock {
		t.Fatalf("validateConfig error = %v, want %v", err, ErrMissingClock)
	}
	config.Now = time.Now
	if err := validateConfig(config); err != nil {
		t.Fatalf("validateConfig with complete process dependencies: %v", err)
	}
}

func TestValidateCLIConfigRequiresOnlyProcessLogger(t *testing.T) {
	if err := validateCLIConfig(CLIConfig{}); err != ErrMissingLogger {
		t.Fatalf("validateCLIConfig without logger = %v, want %v", err, ErrMissingLogger)
	}
	config := CLIConfig{Logger: log.New(io.Discard, "", 0)}
	if err := validateCLIConfig(config); err != nil {
		t.Fatalf("validateCLIConfig with logger: %v", err)
	}
}
