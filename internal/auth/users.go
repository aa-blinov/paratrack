// Package auth owns the user / session / password machinery behind
// the collaboration layer.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/teams"
	"golang.org/x/crypto/bcrypt"
)

// User is a registered paratrack account. Email is the login id, the
// password hash is bcrypt, and Name is what we show in the UI.
type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Service groups DB-backed operations on users. Construct one with
// NewService and reuse it across handlers — it's safe for concurrent
// use because the underlying *sql.DB serialises writes.
type Service struct {
	d *db.DB
}

// NewService returns a Service bound to the given DB.
func NewService(d *db.DB) *Service { return &Service{d: d} }

// ErrInvalidEmail is returned when the supplied email is malformed or
// empty. Other invalid cases (password too short, name empty) map to
// the generic ErrValidation.
var (
	ErrInvalidEmail = errors.New("invalid email")
	ErrValidation   = errors.New("validation failed")
	ErrNotFound     = errors.New("not found")
	ErrBadPassword  = errors.New("bad password")
)

// Validate runs static checks on email / password / name before any DB
// touch. Returns ErrInvalidEmail / ErrValidation with a wrapped reason.
func Validate(email, password, name string) error {
	if _, err := mail.ParseAddress(strings.TrimSpace(email)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEmail, err)
	}
	if len(password) < 8 {
		return fmt.Errorf("%w: password must be at least 8 characters", ErrValidation)
	}
	if len(password) > 72 {
		// bcrypt has a 72-byte input cap; longer passwords are silently
		// truncated, which is surprising. Refuse them up front.
		return fmt.Errorf("%w: password must be at most 72 characters", ErrValidation)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	return nil
}

// CreateUser inserts a user, hashes the password, and (in one tx) creates
// their personal team + owner row so the new account can immediately
// log in and have an empty workspace. Returns the new id and the
// personal team id (so the caller can set it as the current team).
func (s *Service) CreateUser(ctx context.Context, email, password, name string) (userID, teamID int64, err error) {
	if err := Validate(email, password, name); err != nil {
		return 0, 0, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, 0, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.d.SQL().BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := db.FormatTime(time.Now().UTC())
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		email, string(hash), name, now, now,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("insert user: %w", err)
	}
	userID, err = res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	teamSvc := teams.NewService(s.d)
	teamID, err = teamSvc.CreatePersonalInTx(ctx, tx, userID, name)
	if err != nil {
		return 0, 0, fmt.Errorf("create personal team: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return userID, teamID, nil
}

// FindByEmail returns the user with the given email (case-insensitive).
func (s *Service) FindByEmail(ctx context.Context, email string) (User, error) {
	row := s.d.SQL().QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at, updated_at
		 FROM users WHERE email = ? COLLATE NOCASE`, strings.ToLower(strings.TrimSpace(email)),
	)
	return scanUser(row)
}

// FindByID returns the user with the given id.
func (s *Service) FindByID(ctx context.Context, id int64) (User, error) {
	row := s.d.SQL().QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at, updated_at FROM users WHERE id = ?`, id,
	)
	return scanUser(row)
}

// VerifyPassword returns nil if the supplied password matches the
// user's stored hash, or ErrBadPassword otherwise.
func (s *Service) VerifyPassword(user User, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return ErrBadPassword
	}
	return nil
}

// UpdateName changes the user's display name. Empty names are rejected.
func (s *Service) UpdateName(ctx context.Context, userID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrValidation)
	}
	now := db.FormatTime(time.Now().UTC())
	res, err := s.d.SQL().ExecContext(ctx,
		`UPDATE users SET name = ?, updated_at = ? WHERE id = ?`, name, now, userID,
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

// UpdatePassword re-hashes the new password. Same length rules as
// registration (8-72 chars).
func (s *Service) UpdatePassword(ctx context.Context, userID int64, newPassword string) error {
	if len(newPassword) < 8 || len(newPassword) > 72 {
		return fmt.Errorf("%w: password must be 8-72 characters", ErrValidation)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := db.FormatTime(time.Now().UTC())
	_, err = s.d.SQL().ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		string(hash), now, userID,
	)
	return err
}

func scanUser(r interface{ Scan(...any) error }) (User, error) {
	var (
		u        User
		created  string
		updated  string
	)
	if err := r.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	if t, err := db.ScanTime(created); err == nil {
		u.CreatedAt = t
	}
	if t, err := db.ScanTime(updated); err == nil {
		u.UpdatedAt = t
	}
	return u, nil
}