package web

import (
	"net/url"
	"testing"
	"time"
)

// The whole-month gate (FIN-25) lives with the payouts: the accountant closes
// one month from the page, the page lists what is fixed and who closed it, and
// a reopen lifts the gate again.
func TestPeriodCloseRoundTripOnThePayoutsPage(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("period-owner@x.test")

	// A day inside the previous calendar month: the current one cannot close.
	prev := time.Now().UTC().AddDate(0, -1, 1)
	day := prev.Format("2006-01-02")

	close := owner.do("POST", "/api/period/close", url.Values{"month": {day}}, nil)
	page := reactData[payrollPage](t, readBody(t, owner.do("GET", redirectOf(t, close), nil, nil)))
	if !page.FlashOK {
		t.Fatalf("the close refused: %q", page.Flash)
	}
	if len(page.ClosedMonths) != 1 || page.ClosedMonths[0].Month != day[:8]+"01" {
		t.Fatalf("the page lists %+v, want the previous month closed", page.ClosedMonths)
	}

	// Closing twice is not an argument: the first close stands, the list still
	// holds one row, and the answer reads like the first one.
	again := reactData[payrollPage](t, readBody(t, owner.do("GET", redirectOf(t, owner.do("POST", "/api/period/close", url.Values{"month": {day}}, nil)), nil, nil)))
	if !again.FlashOK {
		t.Fatalf("closing the closed month again refused: %q", again.Flash)
	}
	if len(again.ClosedMonths) != 1 {
		t.Fatalf("the page lists %+v, want the one close standing", again.ClosedMonths)
	}

	reopen := owner.do("POST", "/api/period/reopen", url.Values{"month": {day}}, nil)
	page = reactData[payrollPage](t, readBody(t, owner.do("GET", redirectOf(t, reopen), nil, nil)))
	if !page.FlashOK {
		t.Fatalf("the reopen refused: %q", page.Flash)
	}
	if len(page.ClosedMonths) != 0 {
		t.Fatalf("the page still lists %+v after the reopen", page.ClosedMonths)
	}
}

// The payouts page shows only the closed months: a fresh workspace lists none.
func TestPayoutsPageStartsWithNoClosedMonths(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("fresh-payroll@x.test")
	page := reactData[payrollPage](t, readBody(t, owner.do("GET", "/payroll", nil, nil)))
	// The section is always visible: the accountant needs the close where the
	// payouts are, not inside a settings page.
	if len(page.ClosedMonths) != 0 {
		t.Fatalf("a fresh workspace lists %+v", page.ClosedMonths)
	}
}
