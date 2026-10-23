package web

import (
	"net/url"
	"testing"
)

// A by-person line loses the member's name only when the name is the label's last segment
// and names a member. An activity that happens to be called like a member stays (FIN-15).
func TestWithoutExecutorDropsOnlyAMembersName(t *testing.T) {
	names := map[string]bool{"Анна": true, "anna@x.test": true}
	cases := map[string]string{
		"EORA · дизайн · Анна":        "EORA · дизайн",
		"EORA · дизайн · anna@x.test": "EORA · дизайн",
		"EORA · дизайн · Сайт":        "EORA · дизайн · Сайт",
		"EORA · дизайн":               "EORA · дизайн",
	}
	for label, want := range cases {
		if got := withoutExecutor(label, names); got != want {
			t.Errorf("withoutExecutor(%q) = %q, want %q", label, got, want)
		}
	}
	if got := withoutExecutor("EORA · дизайн · Анна", nil); got != "EORA · дизайн · Анна" {
		t.Errorf("nothing hidden, but the name went: %q", got)
	}
}

// The requisites card's switch is saved with the requisites (FIN-15).
func TestRequisitesSwitchHidesExecutorNames(t *testing.T) {
	e := newAPIEnv(t)
	e.register("hide-executor@x.test")
	readBody(t, e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИП Иванова"}, "hide_executor": {"1"}}, nil))
	var hide int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT hide_executor_names FROM teams ORDER BY id DESC LIMIT 1`).Scan(&hide); err != nil {
		t.Fatal(err)
	}
	if hide != 1 {
		t.Fatalf("the switch was not saved: hide_executor_names = %d", hide)
	}
	readBody(t, e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИП Иванова"}}, nil))
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT hide_executor_names FROM teams ORDER BY id DESC LIMIT 1`).Scan(&hide); err != nil {
		t.Fatal(err)
	}
	if hide != 0 {
		t.Fatalf("an unticked switch kept hiding the names: %d", hide)
	}
}
