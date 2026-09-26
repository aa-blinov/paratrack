package db

import (
	"testing"
)

func TestEstimateRoundTrip(t *testing.T) {
	d, _ := Open(":memory:")
	t.Cleanup(func() { _ = d.Close() })
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	p, err := d.CreateProject(ctx, 1, "Budgeted", "", "#7c3aed")
	if err != nil {
		t.Fatal(err)
	}
	v := 480
	upd, err := d.UpdateProject(ctx, 1, p.ID, "", "", nil, &v)
	if err != nil {
		t.Fatal(err)
	}
	if upd.EstimateMinutes == nil || *upd.EstimateMinutes != 480 {
		t.Fatalf("estimate=%v", upd.EstimateMinutes)
	}
	got, _ := d.GetProject(ctx, p.ID)
	if got.EstimateMinutes == nil || *got.EstimateMinutes != 480 {
		t.Fatalf("reread=%v", got.EstimateMinutes)
	}
}
