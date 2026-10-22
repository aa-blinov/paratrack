package invoicing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// shareLinkLifetime is how long a client's link opens its invoice (FIN-13).
const shareLinkLifetime = 30 * 24 * time.Hour

// CreateShareLink makes a client's link to one invoice and returns the token and when
// the link stops opening. The token is random and is not stored: the store keeps its
// hash, so the token cannot be read back later (FIN-13).
func (s *Service) CreateShareLink(ctx context.Context, request appmodel.InvoiceShareRequest) (string, time.Time, error) {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return "", time.Time{}, ErrInvalidInvoice
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("make share token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	request.TokenHash = ShareTokenHash(token)
	request.ExpiresAt = request.Now.Add(shareLinkLifetime)
	if err := s.writer.CreateInvoiceShareLink(ctx, request); err != nil {
		return "", time.Time{}, fmt.Errorf("store share link: %w", err)
	}
	return token, request.ExpiresAt, nil
}

// ResolveShareLink opens a client's link: the workspace and invoice it names. An
// unknown or expired token is not found, and nothing else is read.
func (s *Service) ResolveShareLink(ctx context.Context, token string, now time.Time) (appmodel.InvoiceShareLookup, error) {
	if token == "" {
		return appmodel.InvoiceShareLookup{}, model.ErrNotFound
	}
	return s.reader.FindInvoiceShareLink(ctx, appmodel.InvoiceShareLookupQuery{TokenHash: ShareTokenHash(token), Now: now})
}

// ShareTokenHash is what a client's link keeps of its token.
func ShareTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
