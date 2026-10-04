package app

import (
	"errors"
	"testing"
)

func TestNewServicesRejectsNilDatabase(t *testing.T) {
	services, err := NewServices(nil, Config{})
	if services != nil {
		t.Fatal("NewServices(nil) returned services")
	}
	if !errors.Is(err, ErrNilDatabase) {
		t.Fatalf("NewServices(nil) error = %v, want %v", err, ErrNilDatabase)
	}
}
