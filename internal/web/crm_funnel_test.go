package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A conversion is null while nothing was closed or created, and a rate otherwise (CRM-17).
func TestCRMFunnelViewConversionIsNullWithoutClosedDeals(t *testing.T) {
	empty := crmFunnelViewOf(model.CRMFunnel{}, i18n.Ru, "2026-10-01", "2026-10-31", "2026-10-25")
	if empty.Conversion != nil || empty.Leads.Conversion != nil {
		t.Fatalf("conversions with nothing closed = %v and %v, want null", empty.Conversion, empty.Leads.Conversion)
	}

	closed := crmFunnelViewOf(model.CRMFunnel{
		Won: model.CRMDealTotal{Count: 1}, Lost: model.CRMDealTotal{Count: 3},
		LeadsCreated: 4, LeadsConverted: 1,
	}, i18n.Ru, "2026-10-01", "2026-10-31", "2026-10-25")
	if closed.Conversion == nil || *closed.Conversion != 0.25 {
		t.Fatalf("win rate = %v, want 0.25", closed.Conversion)
	}
	if closed.Leads.Conversion == nil || *closed.Leads.Conversion != 0.25 {
		t.Fatalf("lead conversion = %v, want 0.25", closed.Leads.Conversion)
	}
}

// An empty source is named in the reader's language; any other source is shown as typed.
func TestCRMFunnelViewNamesAnEmptySource(t *testing.T) {
	leads := crmFunnelLeadsViewOf(model.CRMFunnel{
		LeadSources: []model.CRMLeadSource{{Source: "", Created: 2}, {Source: "сайт", Created: 1}},
	}, i18n.Ru)
	if got := leads.Sources[0]; got.Source != "" || got.Label != "не указано" || got.Count != 2 {
		t.Fatalf("empty source = %+v, want the label не указано", got)
	}
	if got := leads.Sources[1]; got.Label != "сайт" || got.Count != 1 {
		t.Fatalf("typed source = %+v", got)
	}
}

// A manager reads the amounts of the funnel over HTTP and through the tool; a member reads the same
// counts and no amount. The period is refused with the field that is wrong, as the response stats are.
func TestCRMFunnelAmountsOnlyForAManager(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("funnel-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("funnel-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"funnel-member@x.test": member}, "funnel-member@x.test")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "owner"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	if text, failed := callTool(t, owner, ownerAuth, "deal_create", map[string]any{"title": "Сайт", "amount_cents": 250000, "currency": "RUB"}); failed {
		t.Fatalf("deal_create: %s", text)
	}

	var ownerView crmFunnelView
	decodeBody(t, owner.do("GET", "/api/crm/report/funnel?from=2000-01-01&to=2100-01-01", nil, nil), &ownerView)
	if !ownerView.AmountsVisible || ownerView.Pipeline[0].Count != 1 || len(ownerView.Pipeline[0].Amounts) != 1 ||
		ownerView.Pipeline[0].Amounts[0].AmountCents != 250000 {
		t.Fatalf("manager's report = %+v, want one deal of 250000 in the first stage", ownerView.Pipeline[0])
	}

	resp := member.do("GET", "/api/crm/report/funnel?from=2000-01-01&to=2100-01-01", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a member reads the funnel: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"amounts"`) || strings.Contains(string(raw), `"amount_cents"`) {
		t.Fatalf("a member's funnel carries amounts: %s", raw)
	}
	var memberView crmFunnelView
	if err := json.Unmarshal(raw, &memberView); err != nil {
		t.Fatal(err)
	}
	if memberView.AmountsVisible || memberView.Pipeline[0].Count != 1 {
		t.Fatalf("member's report = %+v, want the count and no amounts", memberView)
	}

	text, failed := callTool(t, member, memberAuth, "funnel_report", map[string]any{"from": "2000-01-01", "to": "2100-01-01"})
	if failed {
		t.Fatalf("funnel_report for a member: %s", text)
	}
	if strings.Contains(text, "amount_cents") {
		t.Fatalf("a member's funnel_report carries amounts: %s", text)
	}
	var viaTool crmFunnelView
	if err := json.Unmarshal([]byte(text), &viaTool); err != nil || viaTool.AmountsVisible || viaTool.Won.Count != 0 {
		t.Fatalf("funnel_report = %q, %v", text, err)
	}
	text, failed = callTool(t, owner, ownerAuth, "funnel_report", map[string]any{"from": "2000-01-01", "to": "2100-01-01"})
	if failed || !strings.Contains(text, `"amount_cents":250000`) {
		t.Fatalf("a manager's funnel_report = %q, failed=%v, want the amount", text, failed)
	}

	assertFieldError(t, member.do("GET", "/api/crm/report/funnel", nil, nil), http.StatusBadRequest, "from")
	assertFieldError(t, member.do("GET", "/api/crm/report/funnel?from=2026-10-25&to=2026-10-01", nil, nil), http.StatusBadRequest, "to")
	if text, failed = callTool(t, member, memberAuth, "funnel_report", map[string]any{"from": "2026-10-25"}); !failed || !strings.Contains(text, "to") {
		t.Fatalf("funnel_report without to: failed=%v text=%q, want the field named", failed, text)
	}
}
