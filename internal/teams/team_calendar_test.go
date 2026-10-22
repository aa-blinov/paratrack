package teams

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The workspace calendar takes a zone the server knows and a week that starts on mon
// or sun; anything else is refused before the store is asked (ADM-07).
func TestSetCalendarRefusesWhatTheServerCannotUse(t *testing.T) {
	service := &Service{}
	for _, request := range []appmodel.TeamCalendarRequest{
		{TeamID: 1, CallerID: 1, TimeZone: "Mars/Base"},
		{TeamID: 1, CallerID: 1, WeekStart: "sat"},
	} {
		if err := service.SetCalendar(context.Background(), request); !errors.Is(err, ErrValidation) {
			t.Fatalf("SetCalendar(%+v) = %v, want a validation error", request, err)
		}
	}
	if err := service.SetCalendar(context.Background(), appmodel.TeamCalendarRequest{TeamID: 0, CallerID: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a calendar without a workspace = %v, want not found", err)
	}
}
