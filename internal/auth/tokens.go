package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var ErrTokenInvalid = appmodel.ErrAuthTokenInvalid

// CreateAPIToken issues a token for the requested user and verifies that a
// team-scoped credential cannot be minted for a workspace they do not belong
// to. The raw value is returned once for display by the adapter.
func (s *Service) CreateAPIToken(ctx context.Context, request appmodel.APITokenCreateRequest) (string, model.APIToken, error) {
	if request.UserID <= 0 || request.Options.TeamID < 0 {
		return "", model.APIToken{}, fmt.Errorf("%w: invalid token owner or scope", ErrValidation)
	}
	if request.Options.ExpiresAt != nil && expiredAt(*request.Options.ExpiresAt, s.now().UTC()) {
		return "", model.APIToken{}, fmt.Errorf("%w: token expiry must be in the future", ErrValidation)
	}
	if request.Options.TeamID > 0 {
		_, member, err := s.memberships.FindMembershipForUser(ctx, request.Options.TeamID, request.UserID)
		if err != nil {
			return "", model.APIToken{}, fmt.Errorf("check token team membership: %w", err)
		}
		if !member {
			return "", model.APIToken{}, ErrForbidden
		}
	}
	request.Name = strings.TrimSpace(request.Name)
	raw, token, err := s.tokens.CreateAPIToken(ctx, request)
	if err != nil {
		return "", model.APIToken{}, fmt.Errorf("create API token: %w", err)
	}
	s.recordAudit(ctx, request.Options.TeamID, request.UserID, "auth.api_token_create", fmt.Sprint(token.ID), token.Name)
	return raw, token, nil
}

func (s *Service) ListAPITokens(ctx context.Context, userID int64) ([]model.APIToken, error) {
	if userID <= 0 {
		return nil, ErrNotFound
	}
	tokens, err := s.tokens.ListAPITokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	return tokens, nil
}

func (s *Service) DeleteAPIToken(ctx context.Context, request appmodel.APITokenDeleteRequest) error {
	if request.UserID <= 0 || request.TokenID <= 0 {
		return ErrNotFound
	}
	if err := s.tokens.DeleteAPIToken(ctx, request); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("delete API token: %w", err)
	}
	s.recordAudit(ctx, requestctx.TeamID(ctx), request.UserID, "auth.api_token_delete", fmt.Sprint(request.TokenID), "")
	return nil
}

func (s *Service) APITokenByRaw(ctx context.Context, request appmodel.APITokenLookupRequest) (model.APIToken, error) {
	request.Raw = strings.TrimSpace(request.Raw)
	if request.Raw == "" {
		return model.APIToken{}, ErrTokenInvalid
	}
	token, err := s.tokens.APITokenByRaw(ctx, request)
	if errors.Is(err, model.ErrTokenInvalid) {
		return model.APIToken{}, ErrTokenInvalid
	}
	if err != nil {
		return model.APIToken{}, fmt.Errorf("resolve API token: %w", err)
	}
	return token, nil
}
