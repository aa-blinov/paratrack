package tracking

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestUnscopedWorkspaceIsRejected(t *testing.T) {
	service := &Service{}
	if _, err := service.ActiveSessions(context.Background(), 0); !errors.Is(err, ErrInvalidStart) {
		t.Fatalf("ActiveSessions with no workspace error = %v, want %v", err, ErrInvalidStart)
	}
	if _, err := service.ResolveActivityForMember(context.Background(), appmodel.ActivityResolveRequest{Name: "writing"}); !errors.Is(err, ErrInvalidStart) {
		t.Fatalf("ResolveActivity with no workspace error = %v, want %v", err, ErrInvalidStart)
	}
}
