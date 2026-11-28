package providers

import (
	"errors"
	"testing"
)

func TestProviderConstructorsRequireProcessOwnedHTTPClient(t *testing.T) {
	if _, err := NewConfigured(nil, Config{}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("NewConfigured(nil) error = %v, want %v", err, ErrIncompleteDependencies)
	}
	if _, err := NewWithConfigAndRetryWaits(nil, nil, Config{}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("NewWithConfigAndRetryWaits(nil) error = %v, want %v", err, ErrIncompleteDependencies)
	}
}
