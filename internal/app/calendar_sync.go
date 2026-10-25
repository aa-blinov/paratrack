package app

import (
	"fmt"
	"net/http"

	"github.com/aa-blinov/paratrack/internal/calendarport"
	"github.com/aa-blinov/paratrack/internal/calendarsync"
	"github.com/aa-blinov/paratrack/internal/netclients"
)

// calendarSyncAdapters builds the adapter of every provider whose OAuth client is
// configured. It returns the HTTP client the adapters share, or nil when none is
// configured, so the sync stays off without an outbound client to close.
func calendarSyncAdapters(config Config) (map[string]calendarport.Adapter, *http.Client, error) {
	adapters := make(map[string]calendarport.Adapter, 2)
	if config.CalendarGoogle == (CalendarOAuthClient{}) && config.CalendarMicrosoft == (CalendarOAuthClient{}) {
		return adapters, nil, nil
	}
	client := netclients.External()
	base := calendarsync.Config{HTTPClient: client, Now: config.Now}
	if config.CalendarGoogle != (CalendarOAuthClient{}) {
		cfg := base
		cfg.Credentials = calendarOAuthCredentials(config.CalendarGoogle)
		google, err := calendarsync.NewGoogle(cfg)
		if err != nil {
			client.CloseIdleConnections()
			return nil, nil, fmt.Errorf("google calendar: %w", err)
		}
		adapters[calendarport.ProviderGoogle] = google
	}
	if config.CalendarMicrosoft != (CalendarOAuthClient{}) {
		cfg := base
		cfg.Credentials = calendarOAuthCredentials(config.CalendarMicrosoft)
		microsoft, err := calendarsync.NewMicrosoft(cfg)
		if err != nil {
			client.CloseIdleConnections()
			return nil, nil, fmt.Errorf("microsoft calendar: %w", err)
		}
		adapters[calendarport.ProviderMicrosoft] = microsoft
	}
	return adapters, client, nil
}

func calendarOAuthCredentials(client CalendarOAuthClient) calendarport.Credentials {
	return calendarport.Credentials{ClientID: client.ClientID, ClientSecret: client.ClientSecret}
}
