package providers

import (
	"errors"
	"testing"
)

func TestProviderConstructorsRequireProcessOwnedHTTPClient(t *testing.T) {
	if _, err := NewConfigured(nil, Config{}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("NewConfigured(nil) error = %v, want %v", err, ErrIncompleteDependencies)
	}
	if _, err := NewWithRetryWaits(nil, nil); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("NewWithRetryWaits(nil) error = %v, want %v", err, ErrIncompleteDependencies)
	}
}
