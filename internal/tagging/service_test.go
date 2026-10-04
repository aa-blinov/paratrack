package tagging

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestMutationsRejectUnscopedWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.CreateForMember(context.Background(), appmodel.TagCreateRequest{TeamID: 0, CallerID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Create with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
	if err := service.AttachForMember(context.Background(), appmodel.SessionTagRequest{TeamID: 0, CallerID: 1, SessionID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Attach with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
	if err := service.DetachForMember(context.Background(), appmodel.SessionTagRequest{TeamID: 0, CallerID: 1, SessionID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Detach with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}
