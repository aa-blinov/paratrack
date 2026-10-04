package web

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestApplyPersonScopeUsesMemberAndRejectsUnknownSelection(t *testing.T) {
	base := context.Background()
	members := []model.TeamMember{
		{UserID: 5, Email: "five@example.test"},
		{UserID: 6, Name: "Six"},
		{UserID: 7},
	}

	scoped, selectedID, name := applyPersonScope(base, members, 5)
	if selectedID != 5 || name != "five@example.test" || requestctx.ScopedUserID(scoped) != 5 {
		t.Fatalf("valid selection = (%d, %q, scope %d)", selectedID, name, requestctx.ScopedUserID(scoped))
	}

	unchanged, selectedID, name := applyPersonScope(base, members, 99)
	if selectedID != 0 || name != "" || requestctx.ScopedUserID(unchanged) != 0 {
		t.Fatalf("unknown selection retained scope: (%d, %q, scope %d)", selectedID, name, requestctx.ScopedUserID(unchanged))
	}

	unchanged, selectedID, name = applyPersonScope(base, members, 7)
	if selectedID != 0 || name != "" || requestctx.ScopedUserID(unchanged) != 0 {
		t.Fatalf("member without display name retained scope: (%d, %q, scope %d)", selectedID, name, requestctx.ScopedUserID(unchanged))
	}
}

func TestParsePeriodAtUsesSuppliedRequestTime(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2026, 10, 1, 0, 0, 1, 0, location)
	request := httptest.NewRequest("GET", "/stats?period=today", nil)
	period := (&Server{}).parsePeriodAt(request, now)
	wantStart := time.Date(2026, 10, 1, 0, 0, 0, 0, location)
	if !period.Start.Equal(wantStart) || !period.End.Equal(now) {
		t.Fatalf("period = %s..%s, want %s..%s", period.Start, period.End, wantStart, now)
	}
}
