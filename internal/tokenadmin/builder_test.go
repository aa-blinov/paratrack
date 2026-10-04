package tokenadmin

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type tokenReaderStub struct {
	request appmodel.APITokenListRequest
	tokens  []appmodel.APITokenSummary
	err     error
}

func (stub *tokenReaderStub) ListAPITokens(_ context.Context, request appmodel.APITokenListRequest) ([]appmodel.APITokenSummary, error) {
	stub.request = request
	return stub.tokens, stub.err
}

type teamReaderStub struct {
	ids   []int64
	teams map[int64]model.Team
	err   error
}

func (stub *teamReaderStub) FindByIDs(_ context.Context, ids []int64) (map[int64]model.Team, error) {
	stub.ids = append([]int64(nil), ids...)
	return stub.teams, stub.err
}

func TestManagementBatchesDistinctTeamNames(t *testing.T) {
	created := time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)
	tokens := &tokenReaderStub{tokens: []appmodel.APITokenSummary{
		{ID: 1, TeamID: 7, CreatedAt: created}, {ID: 2, TeamID: 7}, {ID: 3}, {ID: 4, TeamID: 9},
	}}
	teams := &teamReaderStub{teams: map[int64]model.Team{7: {ID: 7, Name: "Studio"}, 9: {ID: 9, Name: "Client"}}}
	builder, err := New(Dependencies{Tokens: tokens, Teams: teams})
	if err != nil {
		t.Fatal(err)
	}
	request := appmodel.APITokenListRequest{UserID: 3, CallerID: 3}
	snapshot, err := builder.Management(context.Background(), request)
	if err != nil {
		t.Fatalf("Management: %v", err)
	}
	if tokens.request != request || !reflect.DeepEqual(teams.ids, []int64{7, 9}) || len(snapshot.Tokens) != 4 ||
		snapshot.TeamNames[7] != "Studio" || snapshot.TeamNames[9] != "Client" {
		t.Fatalf("token management snapshot = %+v, queried teams=%v", snapshot, teams.ids)
	}
}

func TestManagementSkipsTeamLookupWithoutScopedTokens(t *testing.T) {
	teams := &teamReaderStub{}
	builder, err := New(Dependencies{Tokens: &tokenReaderStub{}, Teams: teams})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Management(context.Background(), appmodel.APITokenListRequest{UserID: 1, CallerID: 1}); err != nil {
		t.Fatalf("Management: %v", err)
	}
	if teams.ids != nil {
		t.Fatalf("team lookup called for empty token list: %v", teams.ids)
	}
}

func TestManagementPropagatesTeamReadFailure(t *testing.T) {
	wantErr := errors.New("team store unavailable")
	builder, err := New(Dependencies{
		Tokens: &tokenReaderStub{tokens: []appmodel.APITokenSummary{{TeamID: 7}}},
		Teams:  &teamReaderStub{err: wantErr},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Management(context.Background(), appmodel.APITokenListRequest{UserID: 1, CallerID: 1}); !errors.Is(err, wantErr) {
		t.Fatalf("Management error = %v, want wrapped team read error", err)
	}
}
