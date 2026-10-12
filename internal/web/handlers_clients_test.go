package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

func TestClientsPage_Renders(t *testing.T) {
	e := newProjectTestEnv(t)

	w := e.do("GET", "/clients", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clients: code=%d body=%s", w.Code, w.Body.String())
	}
	page := reactData[map[string]any](t, w.Body.String())
	if page["ClientsReact"] != true {
		t.Fatalf("page is missing the ClientsReact discriminator: %v", page)
	}
	if _, ok := page["Currencies"].([]any); !ok {
		t.Errorf("currency menu missing from payload: %v", page["Currencies"])
	}
}

// The directory is where a duplicate customer used to be created by
// retyping the same name on two projects; the card must refuse it.
func TestClientsCreate_RejectsDuplicateName(t *testing.T) {
	e := newProjectTestEnv(t)

	form := url.Values{"name": {"Ромашка"}, "email": {"buh@romashka.ru"}}
	w := e.do("POST", "/clients", []byte(form.Encode()), "application/x-www-form-urlencoded")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create: code=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); !strings.Contains(got, "flash=clients.created") {
		t.Fatalf("expected a success flash, got %q", got)
	}

	dup := url.Values{"name": {"Ромашка"}}
	w = e.do("POST", "/clients", []byte(dup.Encode()), "application/x-www-form-urlencoded")
	if got := w.Header().Get("Location"); !strings.Contains(got, "clients.nameTaken") {
		t.Fatalf("expected the duplicate to be refused, got %q", got)
	}
}

func TestClientsCreate_RequiresName(t *testing.T) {
	e := newProjectTestEnv(t)

	form := url.Values{"name": {"   "}, "email": {"buh@romashka.ru"}}
	w := e.do("POST", "/clients", []byte(form.Encode()), "application/x-www-form-urlencoded")
	if got := w.Header().Get("Location"); !strings.Contains(got, "clients.nameRequired") {
		t.Fatalf("an empty name must be refused, got %q", got)
	}
	var count int
	if err := e.db.TestSQL().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM clients`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a refused client must not be stored, found %d rows", count)
	}
}

// Only managers file cards. A member gets 403 from the middleware, so the
// page must not offer the form in the first place.
func TestClientsCreate_RequiresManager(t *testing.T) {
	e := newProjectTestEnv(t)
	downgradeToMember(t, e)

	form := url.Values{"name": {"Ромашка"}}
	w := e.do("POST", "/clients", []byte(form.Encode()), "application/x-www-form-urlencoded")
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("a member must not be able to file a client, got %d", w.Code)
	}
	page := e.do("GET", "/clients", nil, "")
	if data := reactData[map[string]any](t, page.Body.String()); data["CanManage"] != false {
		t.Errorf("a member should not be offered the client form: %v", data["CanManage"])
	}
}

func TestProjectForm_LinksClientAndBudget(t *testing.T) {
	e := newProjectTestEnv(t)
	ctx := context.Background()

	clientID := seedClient(t, e, "Ромашка")
	project := createProject(t, e, "Сайт", "site")

	form := url.Values{
		"name": {"Сайт"}, "color": {"#7c8499"},
		"client_id": {fmt.Sprint(clientID)}, "budget": {"250000"},
	}
	w := e.do("POST", "/projects/"+project, []byte(form.Encode()), "application/x-www-form-urlencoded")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("update project: code=%d body=%s", w.Code, w.Body.String())
	}

	var clientLink int64
	var budget *int
	row := e.db.TestSQL().QueryRowContext(ctx,
		`SELECT client_id, budget_cents FROM projects WHERE slug = ?`, project)
	if err := row.Scan(&clientLink, &budget); err != nil {
		t.Fatal(err)
	}
	if clientLink != clientID {
		t.Errorf("project linked to client %d, want %d", clientLink, clientID)
	}
	if budget == nil || *budget != 250_000_00 {
		t.Fatalf("budget = %v, want %d cents", budget, 250_000_00)
	}

	// Clearing the field must remove the ceiling rather than store zero,
	// which would read as "this project may not be billed at all".
	form.Set("budget", "")
	w = e.do("POST", "/projects/"+project, []byte(form.Encode()), "application/x-www-form-urlencoded")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("clear budget: code=%d body=%s", w.Code, w.Body.String())
	}
	budget = nil
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT budget_cents FROM projects WHERE slug = ?`, project).Scan(&budget); err != nil {
		t.Fatal(err)
	}
	if budget != nil {
		t.Fatalf("cleared budget stored as %d instead of NULL", *budget)
	}
}

// A project with no card must keep working: it still names its recipient
// through the snapshot columns that predate client cards.
func TestProjectPage_WithoutClientStillRenders(t *testing.T) {
	e := newProjectTestEnv(t)
	project := createProject(t, e, "Сайт", "site")

	w := e.do("GET", "/projects/"+project, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET project: code=%d body=%s", w.Code, w.Body.String())
	}
	data := reactData[map[string]any](t, w.Body.String())
	if data["Client"] != nil {
		t.Errorf("a project with no card must report no client, got %v", data["Client"])
	}
	budget, ok := data["Budget"].(map[string]any)
	if !ok {
		t.Fatalf("budget block missing from payload: %v", data["Budget"])
	}
	if budget["HasBudget"] != false {
		t.Errorf("a project without a ceiling must not claim one: %v", budget)
	}
}

// --- helpers ---------------------------------------------------------

func teamOwnerID(t *testing.T, e *projectTestEnv) int64 {
	t.Helper()
	var owner int64
	if err := e.db.TestSQL().QueryRowContext(context.Background(),
		`SELECT owner_id FROM teams WHERE id = ?`, e.teamID).Scan(&owner); err != nil {
		t.Fatalf("read team owner: %v", err)
	}
	return owner
}

func seedClient(t *testing.T, e *projectTestEnv, name string) int64 {
	t.Helper()
	card, err := e.db.CreateClient(context.Background(), appmodel.ClientCreateRequest{
		TeamID: e.teamID, CallerID: teamOwnerID(t, e), Name: name,
	})
	if err != nil {
		t.Fatalf("seed client: %v", err)
	}
	return card.ID
}

func createProject(t *testing.T, e *projectTestEnv, name, slug string) string {
	t.Helper()
	p, err := e.db.CreateProjectWithBilling(context.Background(), appmodel.ProjectCreateRequest{
		TeamID: e.teamID, CallerID: teamOwnerID(t, e), Name: name, Slug: slug, Color: "#7c8499",
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return p.Slug
}

func downgradeToMember(t *testing.T, e *projectTestEnv) {
	t.Helper()
	if _, err := e.db.TestSQL().ExecContext(context.Background(),
		`UPDATE memberships SET role = 'member' WHERE team_id = ?`, e.teamID); err != nil {
		t.Fatalf("downgrade membership: %v", err)
	}
}

// A refusal must arrive as a translated sentence marked as a failure. The
// flash fallback strips the "e:" prefix, reports the rest verbatim and calls
// it a success, so an unlisted code shows up as raw text in the success
// colour — which is how a rejected duplicate could read as a confirmation.
func TestClientFlashCodes_TranslateAndKeepPolarity(t *testing.T) {
	cases := []struct {
		code   string
		wantOK bool
	}{
		{"clients.created", true},
		{"clients.updated", true},
		{"e:clients.nameTaken", false},
		{"e:clients.nameRequired", false},
		{"e:clients.invalidEmail", false},
		{"e:clients.saveFailed", false},
	}
	for _, c := range cases {
		msg, ok := decodeFlash(c.code, i18n.Ru)
		if ok != c.wantOK {
			t.Errorf("%s: polarity = %v, want %v", c.code, ok, c.wantOK)
		}
		if msg == c.code || strings.HasPrefix(msg, "e:") || strings.Contains(msg, ".") {
			t.Errorf("%s was not translated, got %q", c.code, msg)
		}
	}
}
