package db

import (
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
)

func TestEstimateRoundTrip(t *testing.T) {
	d, _ := OpenTest(t)
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: "Budgeted", Slug: "", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	v := 480
	upd, err := d.UpdateProjectWithOptions(ctx, appmodel.ProjectUpdateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, Update: appmodel.ProjectUpdate{Name: "", Color: "", Archived: nil, EstimateMinutes: &v}})
	if err != nil {
		t.Fatal(err)
	}
	if upd.EstimateMinutes == nil || *upd.EstimateMinutes != 480 {
		t.Fatalf("estimate=%v", upd.EstimateMinutes)
	}
	got, _ := d.GetProjectByID(ctx, p.ID)
	if got.EstimateMinutes == nil || *got.EstimateMinutes != 480 {
		t.Fatalf("reread=%v", got.EstimateMinutes)
	}
}
