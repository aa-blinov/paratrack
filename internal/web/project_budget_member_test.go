package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The budget of a project, the money spent on it and what is left are the figures of
// the people who bill (D-2). A won deal's amount becomes the budget (CRM-13), so the
// project page must not carry it to a member either.
func TestMemberOpensProjectWithoutBudgetOrSpend(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("budget-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("budget-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"budget-member@x.test": member}, "budget-member@x.test")

	created := owner.do("POST", "/projects/new", url.Values{"name": {"Сайт"}, "color": {"#7c8499"}, "budget": {"250000"}}, nil)
	created.Body.Close()
	if created.StatusCode >= 400 {
		t.Fatalf("create project: status %d", created.StatusCode)
	}
	var slug string
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT slug FROM projects WHERE name = 'Сайт'`).Scan(&slug); err != nil {
		t.Fatal(err)
	}

	moneyField := regexp.MustCompile(`"(BudgetLabel|SpentLabel|RemainingLabel|UnbilledLabel|OverByLabel)":"[^"]`)
	ownerPage := readBody(t, owner.do("GET", "/projects/"+slug, nil, nil))
	memberPage := readBody(t, member.do("GET", "/projects/"+slug, nil, nil))

	// The owner's page must show the figure, or the member check passes for the wrong reason.
	if !moneyField.MatchString(ownerPage) {
		t.Fatal("owner page shows no budget figure: the money check cannot be trusted")
	}
	if moneyField.MatchString(memberPage) {
		t.Error("member sees a budget figure on the project page")
	}
	if !strings.Contains(memberPage, "Сайт") {
		t.Error("member does not see the project")
	}
}
