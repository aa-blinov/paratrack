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
func (s *Service) CreateAPIToken(ctx context.Context, request appmodel.APITokenCreateRequest) (string, appmodel.APITokenSummary, error) {
	if request.UserID <= 0 || request.CallerID <= 0 || request.UserID != request.CallerID || request.Options.TeamID < 0 {
		return "", appmodel.APITokenSummary{}, fmt.Errorf("%w: invalid token owner or scope", ErrValidation)
	}
	if request.Options.ExpiresAt != nil && expiredAt(*request.Options.ExpiresAt, s.now().UTC()) {
		return "", appmodel.APITokenSummary{}, fmt.Errorf("%w: token expiry must be in the future", ErrValidation)
	}
	if request.Options.TeamID > 0 {
		_, member, err := s.memberships.FindMembershipForUser(ctx, appmodel.TeamMembershipQuery{TeamID: request.Options.TeamID, UserID: request.UserID})
		if err != nil {
			return "", appmodel.APITokenSummary{}, fmt.Errorf("check token team membership: %w", err)
		}
		if !member {
			return "", appmodel.APITokenSummary{}, ErrForbidden
		}
	}
	request.Name = strings.TrimSpace(request.Name)
	raw, token, err := s.tokens.CreateAPIToken(ctx, request)
	if err != nil {
		return "", appmodel.APITokenSummary{}, fmt.Errorf("create API token: %w", err)
	}
	s.recordAudit(ctx, request.Options.TeamID, request.UserID, "auth.api_token_create", fmt.Sprint(token.ID), token.Name)
	return raw, apiTokenSummary(token), nil
}

func (s *Service) ListAPITokens(ctx context.Context, request appmodel.APITokenListRequest) ([]appmodel.APITokenSummary, error) {
	if request.UserID <= 0 || request.CallerID <= 0 {
		return nil, ErrNotFound
	}
	if request.UserID != request.CallerID {
		return nil, ErrForbidden
	}
	tokens, err := s.tokens.ListAPITokens(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	results := make([]appmodel.APITokenSummary, 0, len(tokens))
	for _, token := range tokens {
		results = append(results, apiTokenSummary(token))
	}
	return results, nil
}

func apiTokenSummary(token model.APIToken) appmodel.APITokenSummary {
	return appmodel.APITokenSummary{
		ID: token.ID, UserID: token.UserID, Name: token.Name, Prefix: token.Prefix,
		CreatedAt: token.CreatedAt, LastUsedAt: token.LastUsedAt,
		TeamID: token.TeamID, ExpiresAt: token.ExpiresAt, ReadOnly: token.ReadOnly,
	}
}

func (s *Service) DeleteAPIToken(ctx context.Context, request appmodel.APITokenDeleteRequest) error {
	if request.UserID <= 0 || request.CallerID <= 0 || request.TokenID <= 0 {
		return ErrNotFound
	}
	if request.UserID != request.CallerID {
		return ErrForbidden
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

func (s *Service) APITokenByRaw(ctx context.Context, request appmodel.APITokenLookupRequest) (appmodel.APITokenIdentity, error) {
	request.Raw = strings.TrimSpace(request.Raw)
	if request.Raw == "" {
		return appmodel.APITokenIdentity{}, ErrTokenInvalid
	}
	token, err := s.tokens.APITokenByRaw(ctx, request)
	if errors.Is(err, model.ErrTokenInvalid) {
		return appmodel.APITokenIdentity{}, ErrTokenInvalid
	}
	if err != nil {
		return appmodel.APITokenIdentity{}, fmt.Errorf("resolve API token: %w", err)
	}
	return appmodel.APITokenIdentity{UserID: token.UserID, TeamID: token.TeamID, ReadOnly: token.ReadOnly}, nil
}
