package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aa-blinov/paratrack/internal/db"
)

// openTestDB creates a temp DB, applies the schema, and returns it
// along with a cleanup. The DB lives in a private file so concurrent
// tests don't share WAL state.
func openTestDB(t *testing.T) *dbpkg.DB {
	t.Helper()
	d, err := dbpkg.Open(t.TempDir() + "/auth.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestValidate(t *testing.T) {
	for _, ok := range []struct{ email, pw, name string }{
		{"alice@example.com", "longenough", "Alice"},
		{"bob@host", "longenough", "Bob"},
	} {
		if err := Validate(ok.email, ok.pw, ok.name); err != nil {
			t.Errorf("Validate(%q,…) should pass: %v", ok.email, err)
		}
	}
	for _, bad := range []struct {
		email, pw, name, why string
	}{
		{"", "longenough", "X", "empty email"},
		{"no-at-sign", "longenough", "X", "malformed email"},
		{"x@y.z", "short", "X", "short password"},
		{"x@y.z", strings.Repeat("a", 73), "X", "long password"},
		{"x@y.z", "longenough", "  ", "empty name"},
	} {
		if err := Validate(bad.email, bad.pw, bad.name); err == nil {
			t.Errorf("%s: Validate should fail", bad.why)
		}
	}
}

func TestCreateUser_HashesPasswordAndCreatesPersonalTeam(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	uid, teamID, err := svc.CreateUser(ctx, "alice@example.com", "longenough", "Alice")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if uid == 0 || teamID == 0 {
		t.Fatalf("ids should be set: uid=%d team=%d", uid, teamID)
	}

	u, err := svc.FindByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("FindByEmail (mixed case): %v", err)
	}
	if u.ID != uid {
		t.Errorf("FindByEmail returned %d, want %d", u.ID, uid)
	}
	if u.PasswordHash == "" || u.PasswordHash == "longenough" {
		t.Errorf("password should be hashed, got %q", u.PasswordHash)
	}

	if err := svc.VerifyPassword(u, "longenough"); err != nil {
		t.Errorf("VerifyPassword should accept correct password: %v", err)
	}
	if err := svc.VerifyPassword(u, "wrong-password"); !errors.Is(err, ErrBadPassword) {
		t.Errorf("VerifyPassword wrong-password: want ErrBadPassword, got %v", err)
	}
}

func TestCreateUser_DuplicateEmailRejected(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	if _, _, err := svc.CreateUser(ctx, "dup@example.com", "longenough", "Dup"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	// SQLite returns a generic error for unique-constraint failures.
	if _, _, err := svc.CreateUser(ctx, "dup@example.com", "longenough", "Dup"); err == nil {
		t.Fatalf("second CreateUser should fail")
	}
}

func TestSessions(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	uid, _, err := svc.CreateUser(ctx, "bob@example.com", "longenough", "Bob")
	if err != nil {
		t.Fatal(err)
	}

	sess, err := svc.NewSession(ctx, uid)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(sess.Token) < 20 {
		t.Errorf("token should be reasonably long, got %d chars", len(sess.Token))
	}
	if time.Until(sess.ExpiresAt) < 24*time.Hour {
		t.Errorf("session expiry too close: %v", sess.ExpiresAt)
	}

	got, user, err := svc.FindByToken(ctx, sess.Token)
	if err != nil {
		t.Fatalf("FindByToken: %v", err)
	}
	if got.UserID != uid || user.ID != uid {
		t.Errorf("session/user id mismatch")
	}

	if _, _, err := svc.FindByToken(ctx, "no-such-token"); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("FindByToken with junk: want ErrSessionInvalid, got %v", err)
	}

	if err := svc.DeleteByToken(ctx, sess.Token); err != nil {
		t.Fatalf("DeleteByToken: %v", err)
	}
	if _, _, err := svc.FindByToken(ctx, sess.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("after delete: want ErrSessionInvalid, got %v", err)
	}
}

func TestSessionExpiredIsRejected(t *testing.T) {
	d := openTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	uid, _, err := svc.CreateUser(ctx, "carol@example.com", "longenough", "Carol")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.NewSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	// Force the session to be expired by rewriting expires_at directly.
	if _, err := d.SQL().ExecContext(ctx,
		`UPDATE auth_sessions SET expires_at = ? WHERE token = ?`,
		dbpkg.FormatTime(time.Now().UTC().Add(-time.Hour)), sess.Token,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.FindByToken(ctx, sess.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("expired session: want ErrSessionInvalid, got %v", err)
	}
}