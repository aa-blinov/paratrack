package appmodel

import "time"

// Calendar sync contracts (CRM-19). The workflow, the persistence adapter and the
// transports share these shapes; the tokens and the OAuth state never leave the
// persistence adapter in any other form than a sealed value or a hash.

// Origin of a link: a provider event pulled into the team, or a team event pushed to the provider.
const (
	CalendarOriginProvider = "provider"
	CalendarOriginTeam     = "team"
)

// Tombstone kinds: "delete" removes the provider copy of a pushed event on the next
// sync; "ignore" keeps a pulled event that the member deleted locally from coming back.
const (
	CalendarTombstoneDelete = "delete"
	CalendarTombstoneIgnore = "ignore"
)

// CalendarConnectRequest starts a member's connection to a provider calendar in the
// current workspace. SessionToken is the browser session the consent starts in;
// the callback must come back in that same session.
type CalendarConnectRequest struct {
	TeamID       int64
	UserID       int64
	Provider     string
	SessionToken string
	RedirectURI  string
}

// CalendarConnectCallbackRequest completes a connection with the provider's code.
type CalendarConnectCallbackRequest struct {
	TeamID       int64
	UserID       int64
	Provider     string
	State        string
	Code         string
	SessionToken string
	RedirectURI  string
}

// CalendarConnectionDeleteRequest disconnects one member's connection in a workspace.
type CalendarConnectionDeleteRequest struct {
	TeamID   int64
	UserID   int64
	Provider string
}

// CalendarConnection is a member's link to one provider calendar in one workspace,
// without its tokens.
type CalendarConnection struct {
	TeamID       int64
	UserID       int64
	Provider     string
	ConnectedAt  time.Time
	LastSyncedAt time.Time
	LastError    string
}

// CalendarTokens are a connection's credentials. The persistence adapter stores them sealed.
type CalendarTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// CalendarCredentials is a connection with its tokens, read only by the sync.
type CalendarCredentials struct {
	Connection CalendarConnection
	Tokens     CalendarTokens
}

// CalendarConnectionSaveRequest stores a connection's tokens. An empty refresh token keeps the stored one.
type CalendarConnectionSaveRequest struct {
	TeamID   int64
	UserID   int64
	Provider string
	Tokens   CalendarTokens
	Now      time.Time
}

// CalendarSyncRecordRequest notes the outcome of one sync attempt. Error is empty on success.
type CalendarSyncRecordRequest struct {
	TeamID   int64
	UserID   int64
	Provider string
	At       time.Time
	Error    string
}

// CalendarOAuthStateSaveRequest records a pending consent. The adapter stores a hash of the
// state and of the session, and the verifier sealed.
type CalendarOAuthStateSaveRequest struct {
	TeamID       int64
	UserID       int64
	Provider     string
	State        string
	Verifier     string
	SessionToken string
	ExpiresAt    time.Time
}

// CalendarOAuthStateTakeRequest consumes a pending consent. It matches only when the state
// and the session both match, and the row is removed in the same statement.
type CalendarOAuthStateTakeRequest struct {
	State        string
	SessionToken string
}

// CalendarOAuthState is a consumed consent.
type CalendarOAuthState struct {
	TeamID    int64
	UserID    int64
	Provider  string
	Verifier  string
	ExpiresAt time.Time
}

// CalendarEventFields are the values both sides share for one event.
type CalendarEventFields struct {
	Title    string
	Location string
	Notes    string
	StartsAt time.Time
	EndsAt   time.Time
}

// CalendarLink joins a team event to a provider event for one connection.
type CalendarLink struct {
	EventID    int64
	ExternalID string
	Origin     string
	Etag       string
	SyncedAt   time.Time
	Event      CalendarEventFields
	UpdatedAt  time.Time
}

// CalendarLinksQuery reads the links of one connection.
type CalendarLinksQuery struct {
	TeamID   int64
	UserID   int64
	Provider string
}

// CalendarPushQuery reads the member's own events that have push switched on and
// overlap [From, To). Each carries its link for the provider, if it has one.
type CalendarPushQuery struct {
	TeamID   int64
	UserID   int64
	Provider string
	From     time.Time
	To       time.Time
}

// CalendarPushCandidate is one event the sync may push.
type CalendarPushCandidate struct {
	EventID   int64
	Event     CalendarEventFields
	UpdatedAt time.Time
	Linked    bool
	Link      CalendarLink
}

// CalendarTombstone is a provider event the team must delete or leave alone.
type CalendarTombstone struct {
	ExternalID string
	Kind       string
}

// CalendarTombstoneRequest reads or clears the tombstones of one connection.
type CalendarTombstoneRequest struct {
	TeamID     int64
	UserID     int64
	Provider   string
	ExternalID string
}

// CalendarPulledEventRequest is a provider event brought into the team as the member's personal event.
type CalendarPulledEventRequest struct {
	TeamID     int64
	UserID     int64
	Provider   string
	EventID    int64
	ExternalID string
	Etag       string
	Event      CalendarEventFields
	SyncedAt   time.Time
}

// CalendarTeamLinkRequest records that a team event now exists at the provider.
type CalendarTeamLinkRequest struct {
	TeamID     int64
	UserID     int64
	Provider   string
	EventID    int64
	ExternalID string
	Etag       string
	SyncedAt   time.Time
}

// CalendarSyncRemovalRequest deletes a pulled event that the provider no longer holds.
type CalendarSyncRemovalRequest struct {
	TeamID   int64
	UserID   int64
	Provider string
	EventID  int64
}
