package web

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// A member sees a client's name, its projects and the time on them. The contacts,
// the requisites, the internal notes and every money figure reach the people who
// bill only, on the pages and through the API alike (ADM-08, D-2).
func TestMemberSeesClientsWithoutContactsOrMoney(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("client-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("client-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"client-member@x.test": member}, "client-member@x.test")

	created := owner.do("POST", "/clients", url.Values{
		"name":    {"Ромашка"},
		"email":   {"buh-private@romashka.test"},
		"details": {"ИНН 7701000001 details-private"},
		"notes":   {"pays late notes-private"},
	}, nil)
	created.Body.Close()
	if created.StatusCode >= 400 {
		t.Fatalf("create client: status %d", created.StatusCode)
	}
	var clientID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM clients WHERE name = 'Ромашка'`).Scan(&clientID); err != nil {
		t.Fatal(err)
	}

	secrets := []string{"buh-private@romashka.test", "details-private", "notes-private"}
	moneyField := regexp.MustCompile(`"(BilledLabel|UnbilledLabel|BilledTotal|SpentLabel|BudgetLabel)":"[^"]`)

	ownerAPI := readBody(t, owner.do("GET", "/api/v1/clients", nil, contactsReadToken(t, owner)))
	memberAPI := readBody(t, member.do("GET", "/api/v1/clients", nil, contactsReadToken(t, member)))
	ownerCard := readBody(t, owner.do("GET", fmt.Sprintf("/clients/%d", clientID), nil, nil))
	memberPages := map[string]string{
		"api":  memberAPI,
		"list": readBody(t, member.do("GET", "/clients", nil, nil)),
		"card": readBody(t, member.do("GET", fmt.Sprintf("/clients/%d", clientID), nil, nil)),
	}

	for _, secret := range secrets {
		if !strings.Contains(ownerAPI, secret) {
			t.Errorf("owner API lost %q: the people who bill must still see it", secret)
		}
	}
	for name, body := range memberPages {
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Errorf("member sees %q on %s", secret, name)
			}
		}
		if moneyField.MatchString(body) {
			t.Errorf("member sees a money figure on %s", name)
		}
	}
	// The same pattern must catch a money figure when the owner has one, or the
	// check above would pass for the wrong reason.
	if !moneyField.MatchString(ownerCard) {
		t.Error("owner card shows no money figure: the money check cannot be trusted")
	}
	if !strings.Contains(memberAPI, "Ромашка") || !strings.Contains(memberPages["card"], "Ромашка") {
		t.Error("member does not see the client's name")
	}
}

// contactsReadToken mints a read-only token for the env's user and returns its
// bearer header.
func contactsReadToken(t *testing.T, env *apiEnv) map[string]string {
	t.Helper()
	page := readBody(t, env.do("POST", "/api/tokens", url.Values{"name": {"contacts-check"}, "read_only": {"1"}}, nil))
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	if raw == "" {
		t.Fatal("the token is not shown on the page")
	}
	return map[string]string{"Authorization": "Bearer " + mustUnescape(t, raw)}
}
