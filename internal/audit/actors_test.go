package audit

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// The filter's people come from the journal itself, and the names come from the
// accounts, so both answers stay inside one workspace and skip ids with no account.
func TestActorsAndLabelsStayInsideTheWorkspace(t *testing.T) {
	store := &storeStub{actors: []int64{3, 9}, names: map[int64]string{3: "Мария", 9: "former@example.com"}}
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := service.Actors(ctx, 0); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("actors without a workspace = %v, want invalid scope", err)
	}
	ids, err := service.Actors(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if store.listTeamID != 7 || !reflect.DeepEqual(ids, []int64{3, 9}) {
		t.Fatalf("actors = %v for team %d, want [3 9] for team 7", ids, store.listTeamID)
	}

	labels, err := service.Labels(ctx, []int64{3, 9, 42})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]string{3: "Мария", 9: "former@example.com"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}
