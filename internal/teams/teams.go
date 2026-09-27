// Package teams owns team, membership and invite operations.
package teams

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/translit"
	"net/url"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// Role enumerates the two roles currently supported inside a team.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin" // runs money and settings like the owner, can't delete the workspace
	RoleMember Role = "member"
)

// CanManage: invoices, payroll, rates, members, workspace settings.
func (r Role) CanManage() bool { return r == RoleOwner || r == RoleAdmin }

// Team is one workspace. Every user gets exactly one personal team
// on registration; they can create more later. A team with a single
// owner and no other members behaves like a personal workspace.
type Team struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	OwnerID   int64     `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Service groups DB-backed operations on teams. Same lifetime as
// db.DB — construct one per process and reuse.
type Service struct {
	d *db.DB
}

// NewService returns a Service bound to the given DB.
func NewService(d *db.DB) *Service { return &Service{d: d} }

// ErrNotFound / ErrDuplicate are returned by the typed lookups in this
// package; callers can match against them with errors.Is.
var (
	ErrNotFound   = errors.New("team not found")
	ErrDuplicate  = errors.New("team slug already taken")
	ErrValidation = errors.New("validation failed")
	ErrForbidden  = errors.New("forbidden")
)

// Slugify turns a free-form team name into a URL-safe slug. Whitespace
// becomes hyphens, the rest is lowercased, and any non-alphanumeric
// characters are dropped. Empty slugs fall back to "team".
func Slugify(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range translit.Latin(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "team"
	}
	return slug
}

// PersonalSlug returns the canonical slug for a user's personal team,
// derived from their numeric id so two "John"s don't collide.
func PersonalSlug(userID int64, name string) string {
	return fmt.Sprintf("personal-%d-%s", userID, Slugify(name))
}

// CreatePersonalInTx inserts the personal team + owner membership row
// for a freshly-registered user. Must run inside a transaction so the
// user and the team land atomically.
func (s *Service) CreatePersonalInTx(ctx context.Context, tx *db.Tx, ownerID int64, ownerName string) (int64, error) {
	slug := PersonalSlug(ownerID, ownerName)
	name := strings.TrimSpace(ownerName) + "'s workspace"
	now := db.FormatTime(time.Now().UTC())

	var teamID int64
	err := tx.QueryRowContext(ctx,
		`INSERT INTO teams (slug, name, owner_id, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		slug, name, ownerID, now,
	).Scan(&teamID)
	if err != nil {
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)`,
		teamID, ownerID, string(RoleOwner), now,
	); err != nil {
		return 0, err
	}
	return teamID, nil
}

// Create makes a brand-new team owned by ownerID. The slug is taken
// from the supplied name (Slugify) and an integer suffix is appended
// if it collides with an existing slug, so the caller's input keeps
// looking human in /settings/team.
func (s *Service) Create(ctx context.Context, ownerID int64, name string) (Team, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Team{}, fmt.Errorf("%w: name required", ErrValidation)
	}
	now := db.FormatTime(time.Now().UTC())
	base := Slugify(name)
	slug := base
	for i := 2; ; i++ {
		var id int64
		err := s.d.SQL().QueryRowContext(ctx,
			`INSERT INTO teams (slug, name, owner_id, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
			slug, name, ownerID, now,
		).Scan(&id)
		if err == nil {
			if _, err := s.d.SQL().ExecContext(ctx,
				`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)`,
				id, ownerID, string(RoleOwner), now,
			); err != nil {
				return Team{}, fmt.Errorf("add owner membership: %w", err)
			}
			return s.FindByID(ctx, id)
		}
		if !isUniqueViolation(err) {
			return Team{}, fmt.Errorf("insert team: %w", err)
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

// FindByID looks up a team by primary key.
func (s *Service) FindByID(ctx context.Context, id int64) (Team, error) {
	row := s.d.SQL().QueryRowContext(ctx,
		`SELECT id, slug, name, owner_id, created_at FROM teams WHERE id = ?`, id,
	)
	return scanTeam(row)
}

// FindBySlug looks up a team by its URL slug.
func (s *Service) FindBySlug(ctx context.Context, slug string) (Team, error) {
	row := s.d.SQL().QueryRowContext(ctx,
		`SELECT id, slug, name, owner_id, created_at FROM teams WHERE slug = ?`,
		strings.ToLower(strings.TrimSpace(slug)),
	)
	return scanTeam(row)
}

// ListForUser returns every team the user is a member of. Owner rows
// appear first; within each role bucket results are sorted by joined_at.
func (s *Service) ListForUser(ctx context.Context, userID int64) ([]Team, error) {
	rows, err := s.d.SQL().QueryContext(ctx, `
		SELECT t.id, t.slug, t.name, t.owner_id, t.created_at
		FROM teams t
		JOIN memberships m ON m.team_id = t.id
		WHERE m.user_id = ?
		ORDER BY (m.role = 'owner') DESC, m.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Rename changes the team's display name. Slug stays the same to keep
// URLs stable; only the human-facing name updates.
func (s *Service) Rename(ctx context.Context, teamID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name required", ErrValidation)
	}
	res, err := s.d.SQL().ExecContext(ctx,
		`UPDATE teams SET name = ? WHERE id = ?`, name, teamID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the team and — via ON DELETE CASCADE — every activity,
// session, tag, goal, membership and invite that belongs to it. Only
// the owner can do this; if the caller isn't the owner, ErrForbidden is
// returned. Teams with more than one member can't be deleted (the
// caller has to remove the others first) to avoid orphaning anyone.
func (s *Service) Delete(ctx context.Context, teamID, callerID int64) error {
	t, err := s.FindByID(ctx, teamID)
	if err != nil {
		return err
	}
	if t.OwnerID != callerID {
		return ErrForbidden
	}
	var n int
	if err := s.d.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memberships WHERE team_id = ?`, teamID,
	).Scan(&n); err != nil {
		return err
	}
	if n > 1 {
		return fmt.Errorf("%w: remove all members first", ErrValidation)
	}
	_, err = s.d.SQL().ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, teamID)
	return err
}

func scanTeam(r interface{ Scan(...any) error }) (Team, error) {
	var (
		t       Team
		created string
	)
	if err := r.Scan(&t.ID, &t.Slug, &t.Name, &t.OwnerID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, err
	}
	if ts, err := db.ScanTime(created); err == nil {
		t.CreatedAt = ts
	}
	return t, nil
}

// IsMember reports whether userID is a member of teamID. Cheap — one
// indexed lookup. Returns the role too so the caller can authorise.
func (s *Service) IsMember(ctx context.Context, teamID, userID int64) (Role, bool, error) {
	var role string
	err := s.d.SQL().QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ?`,
		teamID, userID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return Role(role), true, nil
}

// Members lists every member of the team, joined with user details so
// the UI can render "Alice (alice@example.com)" without an N+1 query.
// Owners are listed first, then by joined_at.
func (s *Service) Members(ctx context.Context, teamID int64) ([]Member, error) {
	rows, err := s.d.SQL().QueryContext(ctx, `
		SELECT u.id, u.email, u.name, m.role, m.joined_at
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.team_id = ?
		ORDER BY (m.role = 'owner') DESC, m.joined_at`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var (
			m      Member
			role   string
			joined string
		)
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &role, &joined); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		if t, err := db.ScanTime(joined); err == nil {
			m.JoinedAt = t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Member is one user as seen through the lens of a specific team.
type Member struct {
	UserID   int64     `json:"user_id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Role     Role      `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// SetRole changes a member's role. Only the owner decides who manages
// money and settings; the owner role itself isn't handed out here.
func (s *Service) SetRole(ctx context.Context, teamID, targetUserID, callerID int64, role Role) error {
	callerRole, ok, err := s.IsMember(ctx, teamID, callerID)
	if err != nil {
		return err
	}
	if !ok || callerRole != RoleOwner || callerID == targetUserID {
		return ErrForbidden
	}
	if role != RoleAdmin && role != RoleMember {
		return fmt.Errorf("%w: role must be admin or member", ErrValidation)
	}
	res, err := s.d.SQL().ExecContext(ctx,
		`UPDATE memberships SET role = ? WHERE team_id = ? AND user_id = ? AND role <> 'owner'`,
		string(role), teamID, targetUserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrForbidden
	}
	return nil
}

// TransferOwnership hands the workspace to another member. Only the owner
// can, the new owner must already be in the team, and the old owner stays
// on as an admin. A personal workspace isn't handed over.
func (s *Service) TransferOwnership(ctx context.Context, teamID, callerID, newOwnerID int64) error {
	t, err := s.FindByID(ctx, teamID)
	if err != nil {
		return err
	}
	if t.OwnerID != callerID || callerID == newOwnerID {
		return ErrForbidden
	}
	if strings.HasPrefix(t.Slug, fmt.Sprintf("personal-%d-", callerID)) {
		return fmt.Errorf("%w: a personal workspace can't be handed over", ErrValidation)
	}
	if _, ok, err := s.IsMember(ctx, teamID, newOwnerID); err != nil || !ok {
		return ErrNotFound
	}
	tx, err := s.d.SQL().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, st := range []struct {
		q    string
		args []any
	}{
		{`UPDATE memberships SET role = 'admin' WHERE team_id = ? AND user_id = ?`, []any{teamID, callerID}},
		{`UPDATE memberships SET role = 'owner' WHERE team_id = ? AND user_id = ?`, []any{teamID, newOwnerID}},
		{`UPDATE teams SET owner_id = ? WHERE id = ? AND owner_id = ?`, []any{newOwnerID, teamID, callerID}},
	} {
		res, err := tx.ExecContext(ctx, st.q, st.args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrForbidden // someone changed it meanwhile
		}
	}
	return tx.Commit()
}

// RemoveMember drops someone from the team. Owners can remove anyone
// (including another owner? — not yet, kept conservative); members can
// only remove themselves (leave). Returns ErrForbidden if the rules
// don't fit the caller's role.
func (s *Service) RemoveMember(ctx context.Context, teamID, targetUserID, callerID int64) error {
	callerRole, isCaller, err := s.IsMember(ctx, teamID, callerID)
	if err != nil {
		return err
	}
	if !isCaller {
		return ErrForbidden
	}
	if callerID == targetUserID {
		// Self-leave: members can always leave their own teams.
	} else if !callerRole.CanManage() {
		return ErrForbidden
	} else if targetRole, _, _ := s.IsMember(ctx, teamID, targetUserID); targetRole == RoleOwner && callerRole != RoleOwner {
		return ErrForbidden // an admin can't remove the owner
	}
	if callerID == targetUserID {
		// Last owner can't leave — they have to delete the team.
		t, err := s.FindByID(ctx, teamID)
		if err != nil {
			return err
		}
		if t.OwnerID == callerID {
			var ownerCount int
			if err := s.d.SQL().QueryRowContext(ctx,
				`SELECT COUNT(*) FROM memberships WHERE team_id = ? AND role = 'owner'`,
				teamID,
			).Scan(&ownerCount); err != nil {
				return err
			}
			if ownerCount <= 1 {
				return fmt.Errorf("%w: last owner must delete the team", ErrValidation)
			}
		}
	}
	// Remember the pay rate for the hours already worked (payroll reads it).
	if _, err := s.d.SQL().ExecContext(ctx,
		`DELETE FROM former_members WHERE team_id = ? AND user_id = ?`, teamID, targetUserID); err != nil {
		return err
	}
	if _, err := s.d.SQL().ExecContext(ctx,
		`INSERT INTO former_members (team_id, user_id, hourly_pay_cents, left_at)
		 SELECT team_id, user_id, hourly_pay_cents, ? FROM memberships WHERE team_id = ? AND user_id = ?`,
		db.FormatTime(time.Now().UTC()), teamID, targetUserID); err != nil {
		return err
	}
	// Their devices stop hearing about this workspace.
	if _, err := s.d.SQL().ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE team_id = ? AND user_id = ?`, teamID, targetUserID); err != nil {
		return err
	}
	_, err = s.d.SQL().ExecContext(ctx,
		`DELETE FROM memberships WHERE team_id = ? AND user_id = ?`,
		teamID, targetUserID,
	)
	return err
}

// NewInvite creates a single-use invite token. The token is the only
// secret — anyone with the URL can claim it until it expires (7 days).
// The token is base64-url-safe so it can sit in a query string without
// escaping.
func (s *Service) NewInvite(ctx context.Context, teamID, callerID int64) (Invite, error) {
	role, isCaller, err := s.IsMember(ctx, teamID, callerID)
	if err != nil {
		return Invite{}, err
	}
	if !isCaller || !role.CanManage() {
		return Invite{}, ErrForbidden
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Invite{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := time.Now().UTC()
	expires := now.Add(7 * 24 * time.Hour)
	_, err = s.d.SQL().ExecContext(ctx,
		`INSERT INTO invites (token, team_id, role, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		token, teamID, string(RoleMember), callerID,
		db.FormatTime(now), db.FormatTime(expires),
	)
	if err != nil {
		return Invite{}, err
	}
	return s.FindInvite(ctx, token)
}

// FindInvite looks up an invite by its token. Doesn't check expiry or
// already-accepted state — callers should consult Expired / Accepted
// fields on the result.
func (s *Service) FindInvite(ctx context.Context, token string) (Invite, error) {
	row := s.d.SQL().QueryRowContext(ctx,
		`SELECT token, team_id, role, created_by, created_at, expires_at, accepted_at, accepted_by
		 FROM invites WHERE token = ?`, token,
	)
	return scanInvite(row)
}

// InvitesForTeam lists every (live or dead) invite for a team.
func (s *Service) InvitesForTeam(ctx context.Context, teamID int64) ([]Invite, error) {
	rows, err := s.d.SQL().QueryContext(ctx,
		`SELECT token, team_id, role, created_by, created_at, expires_at, accepted_at, accepted_by
		 FROM invites WHERE team_id = ? ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvite deletes an invite. Only owners can revoke.
func (s *Service) RevokeInvite(ctx context.Context, teamID, callerID int64, token string) error {
	role, isCaller, err := s.IsMember(ctx, teamID, callerID)
	if err != nil {
		return err
	}
	if !isCaller || !role.CanManage() {
		return ErrForbidden
	}
	_, err = s.d.SQL().ExecContext(ctx,
		`DELETE FROM invites WHERE token = ? AND team_id = ?`, token, teamID,
	)
	return err
}

// AcceptInvite validates the token, marks it accepted, and adds the
// caller as a member of the team. Idempotent: if the user is already a
// member, the invite is just marked accepted. Returns the team they
// joined.
func (s *Service) AcceptInvite(ctx context.Context, token string, userID int64) (Team, error) {
	inv, err := s.FindInvite(ctx, token)
	if err != nil {
		return Team{}, err
	}
	if !inv.AcceptedAt.IsZero() {
		return Team{}, fmt.Errorf("%w: invite already used", ErrValidation)
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return Team{}, fmt.Errorf("%w: invite expired", ErrValidation)
	}
	tx, err := s.d.SQL().BeginTx(ctx, nil)
	if err != nil {
		return Team{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := db.FormatTime(time.Now().UTC())
	// Claim the link first: of two people opening it at once, one wins.
	res, err := tx.ExecContext(ctx,
		`UPDATE invites SET accepted_at = ?, accepted_by = ? WHERE token = ? AND accepted_at IS NULL`,
		now, userID, token,
	)
	if err != nil {
		return Team{}, fmt.Errorf("mark invite accepted: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		err = fmt.Errorf("%w: invite already used", ErrValidation)
		return Team{}, err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		inv.TeamID, userID, string(inv.Role), now,
	)
	if err != nil {
		return Team{}, fmt.Errorf("add membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Team{}, err
	}
	return s.FindByID(ctx, inv.TeamID)
}

// InviteURL composes the human-friendly share link for an invite token.
func InviteURL(base, token string) string {
	if base == "" {
		return "/invites/" + token
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "/invites/" + token
	}
	u.Path = "/invites/" + token
	return u.String()
}

// Invite is a row from invites.
type Invite struct {
	Token      string    `json:"token"`
	TeamID     int64     `json:"team_id"`
	Role       Role      `json:"role"`
	CreatedBy  int64     `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	AcceptedAt time.Time `json:"accepted_at"`
	AcceptedBy int64     `json:"accepted_by,omitempty"`
}

// Expired reports whether the invite has passed its expiry time.
func (i Invite) Expired() bool {
	return !i.ExpiresAt.IsZero() && time.Now().UTC().After(i.ExpiresAt)
}

// Used reports whether the invite has already been claimed.
func (i Invite) Used() bool { return !i.AcceptedAt.IsZero() }

// Live is the inverse of (Expired || Used).
func (i Invite) Live() bool { return !i.Expired() && !i.Used() }

func scanInvite(r interface{ Scan(...any) error }) (Invite, error) {
	var (
		i          Invite
		role       string
		created    string
		expires    string
		accepted   sql.NullString
		acceptedBy sql.NullInt64
	)
	if err := r.Scan(&i.Token, &i.TeamID, &role, &i.CreatedBy, &created, &expires, &accepted, &acceptedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Invite{}, ErrNotFound
		}
		return Invite{}, err
	}
	i.Role = Role(role)
	if t, err := db.ScanTime(created); err == nil {
		i.CreatedAt = t
	}
	if t, err := db.ScanTime(expires); err == nil {
		i.ExpiresAt = t
	}
	if accepted.Valid {
		if t, err := db.ScanTime(accepted.String); err == nil {
			i.AcceptedAt = t
		}
	}
	if acceptedBy.Valid {
		i.AcceptedBy = acceptedBy.Int64
	}
	return i, nil
}

// isUniqueViolation: see db.IsUniqueViolation.
func isUniqueViolation(err error) bool { return db.IsUniqueViolation(err) }
