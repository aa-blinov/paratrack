package web

import (
	"net/http"
	"net/url"
	"testing"
)

// The project's budget is set on the create form, typed the way people type money, and kept
// in cents (PLAN-09).
func TestProjectCreateTakesItsBudget(t *testing.T) {
	e := newAPIEnv(t)
	e.register("project-budget@x.test")
	resp := e.do("POST", "/projects/new", url.Values{"name": {"Бюджетный проект"}, "budget": {"5 000,50"}}, nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create the project: status %d", resp.StatusCode)
	}
	var budget *int
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT budget_cents FROM projects WHERE name = 'Бюджетный проект'`).Scan(&budget); err != nil {
		t.Fatal(err)
	}
	if budget == nil || *budget != 500_050 {
		t.Fatalf("the budget is %v, want 500050 cents", budget)
	}
}
