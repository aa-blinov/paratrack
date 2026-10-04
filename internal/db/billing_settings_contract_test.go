package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestWorkspaceSettingsReturnDomainNotFound(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	if _, err := d.TeamBilling(ctx, 999); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("TeamBilling error = %v, want model.ErrNotFound", err)
	}
	if _, err := d.TeamModules(ctx, 999); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("TeamModules error = %v, want model.ErrNotFound", err)
	}
	if _, _, err := d.TeamRequisites(ctx, 999); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("TeamRequisites error = %v, want model.ErrNotFound", err)
	}
}
