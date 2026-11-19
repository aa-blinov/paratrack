package web

import (
	"net/url"
	"strconv"
	"testing"
)

// The salaries (FIN-26) ride on the same pay form the rates use: the manager
// switches a member's kind to a monthly salary with an amount, the members
// page reads both back, and the line appears in the pay run as «оклад».
func TestMemberPayKindRoundTripOnTheMembersPage(t *testing.T) {
	e := newAPIEnv(t)
	e.register("member-pay-kind@x.test")

	page := reactData[settingsPageData](t, readBody(t, e.do("GET", "/settings/members", nil, nil)))
	if len(page.Members) == 0 {
		t.Fatal("the members page lists nobody")
	}
	id := page.Members[0].UserID
	if page.Pay[id].Kind != "" && page.Pay[id].Kind != "hourly" {
		t.Fatalf("fresh kind = %q, want hourly or blank", page.Pay[id].Kind)
	}

	saved := e.do("POST", "/api/member/pay", url.Values{
		"user_id":          {strconv.FormatInt(id, 10)},
		"pay_kind":         {"monthly"},
		"monthly_pay":      {"20000.00"},
		"capacity_minutes": {"480"},
	}, nil)
	readBody(t, saved)
	page = reactData[settingsPageData](t, readBody(t, e.do("GET", "/settings/members", nil, nil)))
	pay := page.Pay[id]
	if pay.Kind != "monthly" || pay.Monthly != "20000.00" && pay.Monthly != "20000,00" {
		t.Fatalf("pay = %+v, want the monthly salary stored and shown", pay)
	}

	// An unknown kind does not reach the row.
	bad := e.do("POST", "/api/member/pay", url.Values{
		"user_id":  {strconv.FormatInt(id, 10)},
		"pay_kind": {"weekly"},
	}, nil)
	readBody(t, bad)
	page = reactData[settingsPageData](t, readBody(t, e.do("GET", "/settings/members", nil, nil)))
	if page.Pay[id].Kind != "monthly" {
		t.Fatalf("kind after the refused save = %q, want monthly kept", page.Pay[id].Kind)
	}

	// Back to hourly keeps the salary for the next switch.
	back := e.do("POST", "/api/member/pay", url.Values{
		"user_id":    {strconv.FormatInt(id, 10)},
		"pay_kind":   {"hourly"},
		"hourly_pay": {"500"},
	}, nil)
	readBody(t, back)
	page = reactData[settingsPageData](t, readBody(t, e.do("GET", "/settings/members", nil, nil)))
	pay = page.Pay[id]
	if pay.Kind != "hourly" || pay.Monthly == "" {
		t.Fatalf("pay after switching back = %+v, want hourly with the salary kept", pay)
	}
}
