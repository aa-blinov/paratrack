package db

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A tag is trimmed and lower-cased, and a repeated tag counts once. Ten tags are the
// most a client carries; an eleventh, an empty tag, a tag with a comma and a tag longer
// than forty characters are refused.
func TestClientTagsNormalizeLowerCaseAndCapAtTen(t *testing.T) {
	got, err := model.NormalizeClientTags([]string{"  VIP ", "Постоянный", "vip", "Оптовый"})
	if err != nil || strings.Join(got, "|") != "vip|постоянный|оптовый" {
		t.Fatalf("normalize = %v, %v; want vip, постоянный, оптовый", got, err)
	}
	ten := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10"}
	if _, err := model.NormalizeClientTags(ten); err != nil {
		t.Fatalf("ten tags refused: %v", err)
	}
	if _, err := model.NormalizeClientTags(append(ten, "t11")); !errors.Is(err, model.ErrInvalidClientTag) {
		t.Fatalf("eleven tags = %v, want ErrInvalidClientTag", err)
	}
	for _, bad := range []string{"", "   ", "a,b", strings.Repeat("я", 41)} {
		if _, err := model.NormalizeClientTags([]string{bad}); !errors.Is(err, model.ErrInvalidClientTag) {
			t.Fatalf("tag %q accepted: %v", bad, err)
		}
	}
	if _, err := model.NormalizeClientTags([]string{strings.Repeat("я", 40)}); err != nil {
		t.Fatalf("a forty-letter tag refused: %v", err)
	}
}

// Only a manager replaces a client's tags, and the database checks that inside the write.
// The set is replaced as a whole, an empty list clears it, and a client of another
// workspace cannot be tagged.
func TestClientTagsAreReplacedByAManagerOnly(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "client-tags-set")
	other := seedRichWorkspace(t, d, "client-tags-other")

	if _, err := d.SetClientTags(ctx, appmodel.ClientTagsRequest{TeamID: ws.teamID, ClientID: ws.clientID, CallerID: ws.memberID, Tags: []string{"vip"}}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member sets tags = %v, want ErrForbidden", err)
	}
	if _, err := d.SetClientTags(ctx, appmodel.ClientTagsRequest{TeamID: ws.teamID, ClientID: ws.clientID, CallerID: ws.ownerID, Tags: []string{" VIP ", "оптовый", "vip"}}); err != nil {
		t.Fatalf("manager sets tags: %v", err)
	}
	got, err := d.ClientTags(ctx, appmodel.ClientScopeQuery{TeamID: ws.teamID, ClientID: ws.clientID})
	if err != nil || !slices.Equal(got, []string{"vip", "оптовый"}) {
		t.Fatalf("stored tags = %v, %v; want vip and оптовый", got, err)
	}

	if _, err := d.SetClientTags(ctx, appmodel.ClientTagsRequest{TeamID: ws.teamID, ClientID: ws.clientID, CallerID: ws.ownerID, Tags: []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10", "t11"}}); !errors.Is(err, model.ErrInvalidClientTag) {
		t.Fatalf("eleven tags stored: %v", err)
	}
	if _, err := d.SetClientTags(ctx, appmodel.ClientTagsRequest{TeamID: other.teamID, ClientID: ws.clientID, CallerID: other.ownerID, Tags: []string{"чужой"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another workspace tags the client: %v, want ErrNotFound", err)
	}
	if got, _ := d.ClientTags(ctx, appmodel.ClientScopeQuery{TeamID: ws.teamID, ClientID: ws.clientID}); !slices.Equal(got, []string{"vip", "оптовый"}) {
		t.Fatalf("refused writes changed the tags to %v", got)
	}

	if _, err := d.SetClientTags(ctx, appmodel.ClientTagsRequest{TeamID: ws.teamID, ClientID: ws.clientID, CallerID: ws.ownerID, Tags: nil}); err != nil {
		t.Fatalf("clear tags: %v", err)
	}
	if got, _ := d.ClientTags(ctx, appmodel.ClientScopeQuery{TeamID: ws.teamID, ClientID: ws.clientID}); len(got) != 0 {
		t.Fatalf("cleared client still has tags %v", got)
	}
}

// The global search finds a client by a part of its name, by its tag, and for managers by
// its billing email, which a member does not match. A contact is found by name, role, email
// and phone, and a phone matches with spaces, dashes and brackets left out of the text or
// the number. Another workspace's clients and contacts never come back, a text made of
// separators matches no phone, and contacts stay out when the CRM section is off.
func TestClientSearchFindsClientsAndContactsWithinTheWorkspace(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "client-search")
	other := seedRichWorkspace(t, d, "client-search-other")

	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE clients SET email = 'buh@romashka.test' WHERE id = ?`, ws.clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetClientTags(ctx, appmodel.ClientTagsRequest{TeamID: ws.teamID, ClientID: ws.clientID, CallerID: ws.ownerID, Tags: []string{"Постоянный"}}); err != nil {
		t.Fatal(err)
	}
	contact, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID,
		Name: "Анна Смирнова", Role: "Директор", Email: "anna@romashka.test", Phone: "+7 (916) 123-45-67",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The other workspace has a client of the same name and a contact with the same phone.
	if _, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: other.teamID, CallerID: other.memberID, ClientID: other.clientID, Name: "Чужая", Phone: "+79161234567",
	}); err != nil {
		t.Fatal(err)
	}

	search := func(query appmodel.SearchQuery) appmodel.SearchResults {
		t.Helper()
		query.Limit = 8
		out, err := d.SearchWorkspace(ctx, query)
		if err != nil {
			t.Fatalf("search %q: %v", query.Text, err)
		}
		return out
	}
	member := func(text string) appmodel.SearchResults {
		return search(appmodel.SearchQuery{TeamID: ws.teamID, UserID: ws.memberID, Text: text, CRM: true})
	}

	if got := member("ромаш"); len(got.Clients) != 1 || got.Clients[0].ID != ws.clientID || got.Clients[0].Detail != "" {
		t.Fatalf("member's search by name = %+v, want the one client with no email", got.Clients)
	}
	if got := member("buh@romashka"); len(got.Clients) != 0 {
		t.Fatalf("member matched a billing email: %+v", got.Clients)
	}
	owner := search(appmodel.SearchQuery{TeamID: ws.teamID, UserID: ws.ownerID, Text: "buh@romashka", CRM: true})
	if len(owner.Clients) != 1 || owner.Clients[0].Detail != "buh@romashka.test" {
		t.Fatalf("manager's search by email = %+v, want the client with its email", owner.Clients)
	}
	if got := member("постоянн"); len(got.Clients) != 1 || got.Clients[0].ID != ws.clientID {
		t.Fatalf("search by tag = %+v, want the tagged client", got.Clients)
	}
	if got := search(appmodel.SearchQuery{TeamID: ws.teamID, UserID: ws.memberID, Text: "ромаш", Tag: "Постоянный", CRM: true}); len(got.Clients) != 1 {
		t.Fatalf("tag filter kept %d clients, want the tagged one", len(got.Clients))
	}
	if got := search(appmodel.SearchQuery{TeamID: ws.teamID, UserID: ws.memberID, Text: "ромаш", Tag: "оптовый", CRM: true}); len(got.Clients) != 0 {
		t.Fatalf("tag filter for another tag kept %+v", got.Clients)
	}

	for _, text := range []string{"916 123-45", "(916) 123", "+7 916", "91612345", "Анна", "директор", "anna@romashka"} {
		got := member(text)
		if len(got.Contacts) != 1 || got.Contacts[0].ID != contact.ID || got.Contacts[0].ClientID != ws.clientID {
			t.Fatalf("contact search %q = %+v, want the one contact of the client", text, got.Contacts)
		}
	}
	if got := member("- -"); len(got.Contacts) != 0 {
		t.Fatalf("a text of separators matched %d contacts", len(got.Contacts))
	}
	if got := search(appmodel.SearchQuery{TeamID: ws.teamID, UserID: ws.memberID, Text: "916 123-45", CRM: false}); len(got.Contacts) != 0 {
		t.Fatal("contacts came back with the CRM section off")
	}
}
