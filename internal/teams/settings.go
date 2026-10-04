package teams

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// SetStripeCredentials stores the workspace's Stripe API and webhook secrets.
func (s *Service) SetStripeCredentials(ctx context.Context, request appmodel.TeamStripeCredentialsRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrValidation
	}
	request.Key = strings.TrimSpace(request.Key)
	request.Secret = strings.TrimSpace(request.Secret)
	if err := s.settings.SetTeamStripe(ctx, request); err != nil {
		return fmt.Errorf("save workspace Stripe credentials: %w", err)
	}
	return nil
}

// UpdateBilling validates and stores the team's time-rounding and invoice
// numbering preferences shared by billable documents.
func (s *Service) UpdateBilling(ctx context.Context, request appmodel.TeamBillingRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	b := request.Rules
	switch b.RoundMinutes {
	case 0, 6, 15, 30, 60:
	default:
		return fmt.Errorf("%w: unsupported rounding interval", ErrInvalidBilling)
	}
	if b.RoundMode != "up" && b.RoundMode != "nearest" {
		return fmt.Errorf("%w: unsupported rounding mode", ErrInvalidBilling)
	}
	b.InvoicePrefix = strings.TrimSpace(b.InvoicePrefix)
	if !validInvoicePrefix(b.InvoicePrefix) {
		return fmt.Errorf("%w: invalid invoice prefix", ErrInvalidBilling)
	}
	request.Rules = b
	if err := s.settings.SetTeamBilling(ctx, request); err != nil {
		return fmt.Errorf("update team billing settings: %w", err)
	}
	return nil
}

// Settings loads the workspace preferences presented together on the team
// settings page. A partial result is never returned as a successful load.
func (s *Service) Settings(ctx context.Context, teamID int64) (model.TeamSettings, error) {
	if teamID <= 0 {
		return model.TeamSettings{}, ErrNotFound
	}
	var settings model.TeamSettings
	var err error
	if settings.Currency, err = s.settings.TeamCurrency(ctx, teamID); err != nil {
		return model.TeamSettings{}, fmt.Errorf("load team currency: %w", err)
	}
	if settings.Requisites, settings.VATNote, err = s.settings.TeamRequisites(ctx, teamID); err != nil {
		return model.TeamSettings{}, fmt.Errorf("load team requisites: %w", err)
	}
	if settings.Billing, err = s.settings.TeamBilling(ctx, teamID); err != nil {
		return model.TeamSettings{}, fmt.Errorf("load team billing settings: %w", err)
	}
	return settings, nil
}

func (s *Service) Currency(ctx context.Context, teamID int64) (string, error) {
	if teamID <= 0 {
		return "", ErrNotFound
	}
	currency, err := s.settings.TeamCurrency(ctx, teamID)
	if err != nil {
		return "", fmt.Errorf("load team currency: %w", err)
	}
	return currency, nil
}

func (s *Service) BillingRules(ctx context.Context, teamID int64) (model.BillingRules, error) {
	if teamID <= 0 {
		return model.BillingRules{}, ErrNotFound
	}
	rules, err := s.settings.TeamBilling(ctx, teamID)
	if err != nil {
		return model.BillingRules{}, fmt.Errorf("load team billing rules: %w", err)
	}
	return rules, nil
}

// UpdateCurrency accepts an ISO-style three-letter uppercase currency code.
// The HTTP adapter may present a smaller product-specific menu, while the
// application rule remains transport-independent.
func (s *Service) UpdateCurrency(ctx context.Context, request appmodel.TeamCurrencyRequest) error {
	currency := request.Currency
	if request.TeamID <= 0 || request.CallerID <= 0 || len(currency) != 3 || currency != strings.ToUpper(currency) {
		return fmt.Errorf("%w: currency must be a three-letter uppercase code", ErrInvalidSettings)
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return fmt.Errorf("%w: currency must be a three-letter uppercase code", ErrInvalidSettings)
		}
	}
	request.Currency = currency
	if err := s.settings.SetTeamCurrency(ctx, request); err != nil {
		return fmt.Errorf("update team currency: %w", err)
	}
	return nil
}

// UpdateRequisites stores issuer data printed on new invoices. Limits are
// enforced here so they do not depend on an HTTP form's own validation.
func (s *Service) UpdateRequisites(ctx context.Context, request appmodel.TeamRequisitesRequest) error {
	request.Requisites, request.VATNote = strings.TrimSpace(request.Requisites), strings.TrimSpace(request.VATNote)
	requisites, vatNote := request.Requisites, request.VATNote
	if request.TeamID <= 0 || request.CallerID <= 0 || len(requisites) > 2000 || len(vatNote) > 200 {
		return fmt.Errorf("%w: requisites are too long", ErrInvalidSettings)
	}
	if err := s.settings.SetTeamRequisites(ctx, request); err != nil {
		return fmt.Errorf("update team requisites: %w", err)
	}
	return nil
}

// UpdateLogo accepts only the bounded PNG/JPEG data URLs produced by the
// workspace logo upload and persists the empty string to remove a logo.
func (s *Service) UpdateLogo(ctx context.Context, request appmodel.TeamLogoRequest) error {
	dataURL := request.DataURL
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	if dataURL != "" {
		prefix, encoded, ok := strings.Cut(dataURL, ",")
		if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") {
			return fmt.Errorf("%w: unsupported logo format", ErrInvalidSettings)
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) == 0 || len(raw) > 200<<10 {
			return fmt.Errorf("%w: invalid logo data", ErrInvalidSettings)
		}
	}
	request.DataURL = dataURL
	if err := s.settings.SetTeamLogo(ctx, request); err != nil {
		return fmt.Errorf("update team logo: %w", err)
	}
	return nil
}

func validInvoicePrefix(prefix string) bool {
	if prefix == "" || len([]rune(prefix)) > 12 {
		return false
	}
	for _, char := range prefix {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '-' {
			return false
		}
	}
	return true
}

// Delete removes the team and — via ON DELETE CASCADE — every activity,
// session, tag, goal, membership and invite that belongs to it. Only
// the owner can do this; if the caller isn't the owner, ErrForbidden is
// returned. Teams with more than one member can't be deleted (the
// caller has to remove the others first) to avoid orphaning anyone.
