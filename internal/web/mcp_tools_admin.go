package web

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpAdminTools are the reads of the workspace's administration: webhooks, integrations,
// the caller's API tokens, the workspace settings and the invitations. Each reads what
// the page of that name reads, and none of them returns a secret: no signing secret, no
// token or key, no invitation link. Nothing here writes, so a read-only token can call all of them.
var mcpAdminTools = []mcpTool{
	{
		Name: "webhooks_list",
		Description: "List the workspace's webhook endpoints: url, events, whether each is active, and its last delivery. " +
			"The signing secret is never returned. Only the owner can, as the webhooks page is the owner's.",
		Schema: objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			if !RoleFrom(c.ctx).IsOwner() {
				return nil, model.ErrForbidden
			}
			snapshot, err := s.services.Webhooks.Management(c.ctx, appmodel.WebhookManagementQuery{TeamID: c.teamID, DeliveriesPerEndpoint: 1})
			if err != nil {
				return nil, err
			}
			out := make([]mcpWebhookView, 0, len(snapshot.Endpoints))
			for _, h := range snapshot.Endpoints {
				view := mcpWebhookView{
					ID: h.ID, URL: h.URL, Active: h.Active,
					Events: strings.FieldsFunc(h.Events, func(r rune) bool { return r == ',' }),
				}
				if deliveries := snapshot.Deliveries[h.ID]; len(deliveries) > 0 {
					d := deliveries[0]
					view.LastDelivery = &mcpWebhookDeliveryView{CreatedAt: d.CreatedAt, Event: d.Event, Status: d.Status, Error: d.Error}
				}
				out = append(out, view)
			}
			return struct {
				Webhooks []mcpWebhookView `json:"webhooks"`
			}{out}, nil
		},
	},
	{
		Name: "integrations_list",
		Description: "List the workspace's integrations: provider, name, state (ok, or failed with the last sync error), and when it was connected. " +
			"Tokens and keys are never returned.",
		Schema: objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			snapshot, err := s.services.Integrations.Queries.Management(c.ctx, c.teamID)
			if err != nil {
				return nil, err
			}
			out := make([]mcpIntegrationView, 0, len(snapshot.Items))
			for _, item := range snapshot.Items {
				it := item.Integration
				view := mcpIntegrationView{ID: it.ID, Provider: it.Provider, Name: it.Name, State: "ok", LastSyncError: it.LastSyncError, CreatedAt: it.CreatedAt}
				if it.LastSyncError != "" {
					view.State = "failed"
				}
				out = append(out, view)
			}
			return struct {
				Integrations []mcpIntegrationView `json:"integrations"`
			}{out}, nil
		},
	},
	{
		Name: "api_tokens_list",
		Description: "List your own API tokens: id, name, prefix, whether it is read-only, and when it was created and last used. " +
			"The token value is never returned, and another member's tokens are never listed.",
		Schema: objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			snapshot, err := s.services.TokenAdmin.Management(c.ctx, appmodel.APITokenListRequest{UserID: c.userID, CallerID: c.userID})
			if err != nil {
				return nil, err
			}
			out := make([]mcpAPITokenView, 0, len(snapshot.Tokens))
			for _, t := range snapshot.Tokens {
				out = append(out, mcpAPITokenView{ID: t.ID, Name: t.Name, Prefix: t.Prefix, ReadOnly: t.ReadOnly, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt})
			}
			return struct {
				Tokens []mcpAPITokenView `json:"tokens"`
			}{out}, nil
		},
	},
	{
		Name: "workspace_settings_get",
		Description: "Read the workspace settings: name, timezone, week start, auto-lock days, currency, billing rounding, invoice prefix, " +
			"VAT rate and note, and the requisites. Only a manager can, as the team settings page is. Stripe keys and webhook " +
			"secrets are never returned; stripe_key_set says only whether a key is stored.",
		Schema: objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			team, _ := TeamFrom(c.ctx)
			settings, err := s.services.Teams.Settings.Settings(c.ctx, c.teamID)
			if err != nil {
				return nil, err
			}
			return mcpWorkspaceSettingsView{
				Name: team.Name, TimeZone: team.TimeZone, WeekStart: team.WeekStart, AutoLockDays: team.AutoLockDays,
				Currency: settings.Currency, RoundMinutes: settings.Billing.RoundMinutes, RoundMode: settings.Billing.RoundMode,
				InvoicePrefix: settings.Billing.InvoicePrefix, VATRate: settings.VATRate, VATNote: settings.VATNote,
				Requisites: settings.Requisites, RequisitesSet: settings.Requisites != "", StripeKeySet: settings.StripeKeySet,
			}, nil
		},
	},
	{
		Name: "invitations_list",
		Description: "List the workspace's invitations with their state (open, accepted or expired), role, and creation and expiry times. " +
			"The invitation link is a secret and is never returned; a revoked invitation is removed, so it is not listed. Only a manager can.",
		Schema: objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			invites, err := s.services.Teams.Invitations.InvitesForTeam(c.ctx, c.teamID)
			if err != nil {
				return nil, err
			}
			out := make([]mcpInvitationView, 0, len(invites))
			for _, inv := range invites {
				state := "open"
				switch {
				case inv.Used():
					state = "accepted"
				case inv.ExpiredAt(c.now):
					state = "expired"
				}
				out = append(out, mcpInvitationView{Role: string(inv.Role), State: state, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt})
			}
			return struct {
				Invitations []mcpInvitationView `json:"invitations"`
			}{out}, nil
		},
	},
}

type mcpWebhookView struct {
	ID           int64                   `json:"id"`
	URL          string                  `json:"url"`
	Events       []string                `json:"events"`
	Active       bool                    `json:"active"`
	LastDelivery *mcpWebhookDeliveryView `json:"last_delivery"`
}

type mcpWebhookDeliveryView struct {
	CreatedAt time.Time `json:"created_at"`
	Event     string    `json:"event"`
	Status    int       `json:"status"`
	Error     string    `json:"error"`
}

type mcpIntegrationView struct {
	ID            int64     `json:"id"`
	Provider      string    `json:"provider"`
	Name          string    `json:"name"`
	State         string    `json:"state"`
	LastSyncError string    `json:"last_sync_error"`
	CreatedAt     time.Time `json:"created_at"`
}

type mcpAPITokenView struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	ReadOnly   bool       `json:"read_only"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type mcpWorkspaceSettingsView struct {
	Name          string `json:"name"`
	TimeZone      string `json:"timezone"`
	WeekStart     string `json:"week_start"`
	AutoLockDays  int    `json:"auto_lock_days"`
	Currency      string `json:"currency"`
	RoundMinutes  int    `json:"round_minutes"`
	RoundMode     string `json:"round_mode"`
	InvoicePrefix string `json:"invoice_prefix"`
	VATRate       int    `json:"vat_rate"`
	VATNote       string `json:"vat_note"`
	Requisites    string `json:"requisites"`
	RequisitesSet bool   `json:"requisites_set"`
	StripeKeySet  bool   `json:"stripe_key_set"`
}

type mcpInvitationView struct {
	Role      string    `json:"role"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
