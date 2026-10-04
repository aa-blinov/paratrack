package app

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/netclients"
	"github.com/aa-blinov/paratrack/internal/push"
	pushproviders "github.com/aa-blinov/paratrack/internal/push/providers"
	"github.com/aa-blinov/paratrack/internal/webhooks"
	webhookhttp "github.com/aa-blinov/paratrack/internal/webhooks/httpdelivery"
)

// newDeliveryServices wires the shared webhook and Web Push delivery graph.
// Register the client before constructing adapters so the caller's resource
// rollback owns it even if a later constructor fails.
func newDeliveryServices(
	webhookStore webhooks.Store,
	pushStore push.Store,
	allowPrivateWebhook bool,
	logger *log.Logger,
	now func() time.Time,
	auditService *audit.Service,
	resources *applicationResources,
) (*webhooks.Service, *push.Service, error) {
	client := netclients.Webhook(allowPrivateWebhook)
	resources.clients = append(resources.clients, client)

	deliverer, err := webhookhttp.New(client)
	if err != nil {
		return nil, nil, fmt.Errorf("construct webhook HTTP adapter: %w", err)
	}
	webhookService, err := webhooks.New(webhookStore, deliverer, now, auditService, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("construct webhooks service: %w", err)
	}
	resources.webhooks = webhookService

	pushClient := netclients.PublicOutbound()
	resources.clients = append(resources.clients, pushClient)
	pushService, err := newPushServiceWithClient(pushStore, logger, auditService, resources, pushClient)
	if err != nil {
		return nil, nil, err
	}
	return webhookService, pushService, nil
}

func newPushService(
	store push.Store,
	logger *log.Logger,
	auditService *audit.Service,
	resources *applicationResources,
) (*push.Service, error) {
	client := netclients.PublicOutbound()
	resources.clients = append(resources.clients, client)
	return newPushServiceWithClient(store, logger, auditService, resources, client)
}

func newPushServiceWithClient(
	store push.Store,
	logger *log.Logger,
	auditService *audit.Service,
	resources *applicationResources,
	client *http.Client,
) (*push.Service, error) {
	sender, err := pushproviders.New(client)
	if err != nil {
		return nil, fmt.Errorf("construct Web Push adapter: %w", err)
	}
	pushService, err := push.New(store, sender, logger, auditService)
	if err != nil {
		return nil, fmt.Errorf("construct push service: %w", err)
	}
	resources.push = pushService
	return pushService, nil
}
