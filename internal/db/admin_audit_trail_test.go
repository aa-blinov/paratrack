package db

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type trailRow struct {
	UserID int64
	Target string
	Meta   string
}

// trailOf reads the rows of one action in one team, oldest first.
func trailOf(t *testing.T, d *DB, teamID int64, action string) []trailRow {
	t.Helper()
	rows, err := d.TestSQL().QueryContext(t.Context(),
		`SELECT user_id, target, meta FROM audit_log WHERE team_id = ? AND action = ? ORDER BY id`, teamID, action)
	if err != nil {
		t.Fatalf("read %s rows: %v", action, err)
	}
	defer rows.Close()
	var out []trailRow
	for rows.Next() {
		var r trailRow
		if err := rows.Scan(&r.UserID, &r.Target, &r.Meta); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func addMember(t *testing.T, d *DB, teamID, userID int64, role string) {
	t.Helper()
	if _, err := d.TestSQL().ExecContext(t.Context(),
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)`,
		teamID, userID, role, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("add %s: %v", role, err)
	}
}

// An owner removing a member leaves one trail row in the same transaction, naming
// the member, and the author is the owner.
func TestRemovingAMemberWritesTheTrailWithTheRemover(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Удаление", "remove-member-trail")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "left-member")
	addMember(t, d, teamID, memberID, "member")

	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: memberID, CallerID: ownerID, LeftAt: time.Now().UTC()}); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	rows := trailOf(t, d, teamID, "member.remove")
	if len(rows) != 1 {
		t.Fatalf("member.remove rows = %+v, want one", rows)
	}
	if rows[0].UserID != ownerID || rows[0].Target != "left-member" || rows[0].Meta != fmt.Sprintf("user:%d", memberID) {
		t.Fatalf("member.remove row = %+v, want author %d, target left-member, meta user:%d", rows[0], ownerID, memberID)
	}
}

// The row keeps the client address the request carried and the time it was written,
// which is what someone reading the trail later needs to place the removal.
func TestTrailRowKeepsTheClientAddressAndTheTime(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Адрес", "trail-address")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "addressed-member")
	addMember(t, d, teamID, memberID, "member")
	ctx := requestctx.WithClientIP(t.Context(), "203.0.113.7")

	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: memberID, CallerID: ownerID, LeftAt: time.Now().UTC()}); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	entries, err := d.ListAudit(t.Context(), appmodel.AuditListQuery{TeamID: teamID, Action: "member.remove", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("member.remove entries = %+v, want one", entries)
	}
	if entries[0].IP != "203.0.113.7" {
		t.Fatalf("row ip = %q, want the request's client address", entries[0].IP)
	}
	if entries[0].CreatedAt.IsZero() {
		t.Fatal("row has no time")
	}
}

// An admin cannot remove the owner, and a refused removal leaves no row.
func TestRefusedMemberRemovalLeavesNoTrailRow(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Отказ", "refused-removal-trail")
	ownerID := teamOwner(t, d, teamID)
	adminID := seedProjectTestUser(t, d, "trail-admin")
	addMember(t, d, teamID, adminID, "admin")

	err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: ownerID, CallerID: adminID, LeftAt: time.Now().UTC()})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("admin removes owner = %v, want forbidden", err)
	}
	if rows := trailOf(t, d, teamID, "member.remove"); len(rows) != 0 {
		t.Fatalf("a refused removal wrote %+v", rows)
	}
}

// Leaving on one's own is a removal too: the trail shows the member as author and target.
func TestSelfLeaveWritesTheTrailWithTheMemberAsAuthor(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Уход", "self-leave-trail")
	memberID := seedProjectTestUser(t, d, "leaving-member")
	addMember(t, d, teamID, memberID, "member")

	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: memberID, CallerID: memberID, LeftAt: time.Now().UTC()}); err != nil {
		t.Fatalf("leave: %v", err)
	}
	rows := trailOf(t, d, teamID, "member.remove")
	if len(rows) != 1 || rows[0].UserID != memberID || rows[0].Target != "leaving-member" {
		t.Fatalf("self-leave rows = %+v, want one row with the member as author and target", rows)
	}
}

// Deleting a draft writes the invoice number into the trail, in the same transaction.
func TestDeletingADraftInvoiceWritesTheTrail(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Черновик", "draft-delete-trail")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-900", Client: "Client", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	if err := d.DeleteInvoice(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatalf("delete draft: %v", err)
	}
	rows := trailOf(t, d, teamID, "invoice.delete")
	if len(rows) != 1 || rows[0].UserID != ownerID || rows[0].Target != "INV-2026-900" || rows[0].Meta != fmt.Sprintf("invoice:%d", invoice.ID) {
		t.Fatalf("invoice.delete rows = %+v, want the draft's number", rows)
	}
}

// A sent invoice cannot be deleted, and the refused delete leaves no row.
func TestDeletingASentInvoiceLeavesNoTrailRow(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Отправлен", "sent-delete-trail")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-901", Client: "Client", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	err = d.DeleteInvoice(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID})
	if !errors.Is(err, ErrInvoiceNotDraft) {
		t.Fatalf("delete sent invoice = %v, want not-a-draft", err)
	}
	if rows := trailOf(t, d, teamID, "invoice.delete"); len(rows) != 0 {
		t.Fatalf("a refused delete wrote %+v", rows)
	}
}

// A restored workspace opens its trail with one row: the restore, by the owner who
// asked for it, with the numbers the file brought.
func TestRestoreOpensTheTrailWithOneRow(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "restore-trail")
	file, err := exportFile(ctx, d, appmodel.WorkspaceExportRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restored, err := restoreFile(ctx, d, ws.ownerID, file)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	trail, err := d.ListAudit(ctx, appmodel.AuditListQuery{TeamID: restored.TeamID, Limit: 100})
	if err != nil {
		t.Fatalf("read the restored trail: %v", err)
	}
	if len(trail) != 1 || trail[0].Action != "workspace.restore" || trail[0].UserID != ws.ownerID {
		t.Fatalf("the restored trail = %+v, want one workspace.restore row by the owner", trail)
	}
	var members, invoices int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE team_id = ?`, restored.TeamID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE team_id = ?`, restored.TeamID).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("участников: %d, счетов: %d", members, invoices); trail[0].Meta != want {
		t.Fatalf("restore meta = %q, want %q", trail[0].Meta, want)
	}
}

// Revoking a token through the store returns its name, which the audit row carries.
func TestRevokingATokenReturnsItsName(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Токены", "token-name-trail")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	_, token, err := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: ownerID, CallerID: ownerID, Name: "sync-laptop", Options: appmodel.TokenOptions{TeamID: teamID}})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	name, err := d.DeleteAPIToken(ctx, appmodel.APITokenDeleteRequest{UserID: ownerID, CallerID: ownerID, TokenID: token.ID})
	if err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if name != "sync-laptop" {
		t.Fatalf("revoked token name = %q, want sync-laptop", name)
	}
}
