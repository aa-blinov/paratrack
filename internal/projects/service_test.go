package projects

import (
	"context"
	"errors"
	"testing"
)

func TestListRejectsUnscopedWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.List(context.Background(), 0, false); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("List with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}
