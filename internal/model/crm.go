package model

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Deal stage keys of the default pipeline. A workspace starts with these five and may
// change its stages later; deals store the key, so a key never changes once it is set.
const (
	DealStageNew      = "new"
	DealStageWorking  = "working"
	DealStageProposal = "proposal"
	DealStageWon      = "won"
	DealStageLost     = "lost"
)

// Stage kinds say what a stage means to the rules, whatever its name is. An open
// stage is worked and can be overdue, a won stage is a sale, and a lost stage needs
// a reason.
const (
	StageKindOpen = "open"
	StageKindWon  = "won"
	StageKindLost = "lost"
)

// CRMStage is one step of a workspace's pipeline. The key is what deals store and what
// the API and the agents address; the name is what people read and may change.
type CRMStage struct {
	TeamID   int64
	Key      string
	Name     string
	Position int
	Kind     string
}

// DefaultDealStages is the pipeline of a workspace that has not changed it. Its keys
// are the ones existing deals already store. The names are empty: a screen shows the
// built-in name of the key in the reader's language until someone names the stage.
var DefaultDealStages = []CRMStage{
	{Key: DealStageNew, Kind: StageKindOpen},
	{Key: DealStageWorking, Kind: StageKindOpen},
	{Key: DealStageProposal, Kind: StageKindOpen},
	{Key: DealStageWon, Kind: StageKindWon},
	{Key: DealStageLost, Kind: StageKindLost},
}

// FindDealStage returns the stage with key from a workspace's pipeline.
func FindDealStage(stages []CRMStage, key string) (CRMStage, bool) {
	for _, stage := range stages {
		if stage.Key == key {
			return stage, true
		}
	}
	return CRMStage{}, false
}

// ValidStageKind reports whether kind is one of the three stage kinds.
func ValidStageKind(kind string) bool {
	return kind == StageKindOpen || kind == StageKindWon || kind == StageKindLost
}

// DealStageKey derives a stage key from its name: letters and digits in lower case,
// and every other run of characters becomes one dash. Keeping the letters of any
// script keeps the key readable for a Russian workspace. Uniqueness within a
// workspace is the persistence layer's job.
func DealStageKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	key := strings.TrimSuffix(b.String(), "-")
	if key == "" {
		return "stage"
	}
	return key
}

// ActivityKinds lists what an activity can record.
var ActivityKinds = []string{"call", "meeting", "note"}

var (
	ErrInvalidDealStage        = errors.New("unknown deal stage")
	ErrLostReasonRequired      = errors.New("a lost deal needs a reason")
	ErrInvalidActivityKind     = errors.New("unknown activity kind")
	ErrEventEndBeforeStart     = errors.New("event ends before it starts")
	ErrCalendarTargetOutOfTeam = errors.New("linked project, client or participant is not in this workspace")
	ErrProjectManagerRequired  = errors.New("a project from a deal is created by a workspace manager")
	ErrIntakeKeyInvalid        = errors.New("intake key is missing, wrong or revoked")
)

// Lead statuses. A lead is worked from new through contacted and qualified, and ends
// lost or converted. A converted lead already has its client and deal, so its status
// does not change again.
const (
	LeadStatusNew       = "new"
	LeadStatusContacted = "contacted"
	LeadStatusQualified = "qualified"
	LeadStatusLost      = "lost"
	LeadStatusConverted = "converted"
)

// LeadStatuses lists the statuses in the order the screen shows them.
var LeadStatuses = []string{LeadStatusNew, LeadStatusContacted, LeadStatusQualified, LeadStatusLost, LeadStatusConverted}

// ValidLeadStatus reports whether status is one of the lead statuses.
func ValidLeadStatus(status string) bool {
	return slices.Contains(LeadStatuses, status)
}

// CRMContact is a person at a client, with a role.
type CRMContact struct {
	ID        int64
	TeamID    int64
	ClientID  int64
	Name      string
	Role      string
	Email     string
	Phone     string
	CreatedBy int64
	CreatedAt time.Time
}

// CRMDeal is one sale moving through the pipeline.
type CRMDeal struct {
	ID          int64
	TeamID      int64
	Title       string
	Stage       string
	StageKind   string
	ClientID    int64
	ContactID   int64
	OwnerID     int64
	AmountCents int64
	Currency    string
	Probability int
	CloseDate   string
	NextStepAt  string
	LostReason  string
	ProjectID   int64
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Overdue reports a deal in an open stage whose next step date has passed. The stage
// kind is read from the workspace's stage row into StageKind. Dates are compared as
// YYYY-MM-DD strings in the workspace day.
func (d CRMDeal) Overdue(today string) bool {
	if d.StageKind != StageKindOpen || d.NextStepAt == "" {
		return false
	}
	return d.NextStepAt < today
}

// CRMActivity is one entry in the timeline of a client or a deal.
type CRMActivity struct {
	ID         int64
	TeamID     int64
	ClientID   int64
	DealID     int64
	Kind       string
	Body       string
	Result     string
	OccurredAt time.Time
	AuthorID   int64
	CreatedAt  time.Time
	// LeadID is set for an activity on a lead; the client and the deal are then 0.
	LeadID int64
}

// CRMLead is an enquiry that is not a client or a deal yet. Owner 0 means nobody has taken
// it, which is how an enquiry from the website starts. Once converted, the lead keeps the
// ids of the client and the deal it made.
type CRMLead struct {
	ID                int64
	TeamID            int64
	Company           string
	ContactName       string
	Phone             string
	Email             string
	Source            string
	Message           string
	OwnerID           int64
	Status            string
	LostReason        string
	NextStepAt        string
	ConvertedDealID   int64
	ConvertedClientID int64
	CreatedBy         int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	// FirstResponseAt is the time of the lead's first activity; zero until it has one.
	FirstResponseAt time.Time
}

// LeadResponseTargetMinutes is the first-response target for a lead: an answer within two
// hours. The owner set it on 2026-10-25 (CRM-16, docs/backlog/crm.md). It is a value, not a
// workspace setting, until the owner asks for one.
const LeadResponseTargetMinutes = 120

// ResponseTime is the time from a lead's creation to its first activity, and zero while it
// has none. An activity logged as happening before the lead existed answers at once, so the
// time is never negative.
func (l CRMLead) ResponseTime() time.Duration {
	if l.FirstResponseAt.IsZero() {
		return 0
	}
	return max(0, l.FirstResponseAt.Sub(l.CreatedAt))
}

// CRMLeadResponseStats is how fast a period's leads were answered. MedianMinutes is zero
// when none of them has an activity yet, so Answered tells that case apart.
type CRMLeadResponseStats struct {
	Created       int64
	Answered      int64
	WithinTarget  int64
	MedianMinutes int64
}

// CRMAmount is a sum of deal amounts in one currency. Sums in different currencies are never
// added together, so a total keeps one entry per currency.
type CRMAmount struct {
	Currency    string
	AmountCents int64
}

// CRMDealTotal counts deals and, for a manager, sums their amounts per currency. A member's total
// has no Amounts.
type CRMDealTotal struct {
	Count   int64
	Amounts []CRMAmount
}

// CRMFunnelStage is one stage of the pipeline now, in the workspace's order.
type CRMFunnelStage struct {
	Key   string
	Name  string
	Kind  string
	Total CRMDealTotal
}

// CRMLeadSource counts the leads a period created from one source. An empty source is its own row.
type CRMLeadSource struct {
	Source  string
	Created int64
}

// CRMFunnel is the funnel report of one period (CRM-17). The pipeline is the state now; the won,
// lost and overdue figures and the leads created in the period are for the period as the report
// reads them. ShowAmounts is set for a manager; without it no total carries an amount.
type CRMFunnel struct {
	ShowAmounts    bool
	Pipeline       []CRMFunnelStage
	Won            CRMDealTotal
	Lost           CRMDealTotal
	Overdue        CRMDealTotal
	LeadStatuses   map[string]int64
	LeadsCreated   int64
	LeadsConverted int64
	LeadSources    []CRMLeadSource
	Response       CRMLeadResponseStats
}

// CRMIntakeKey is the workspace's key for the website's enquiry route. Only its hash is
// stored; the zero CreatedAt means the workspace has no key.
type CRMIntakeKey struct {
	TeamID    int64
	CreatedBy int64
	CreatedAt time.Time
}

// CalendarEvent is a team event. Participants are listed by user ID.
type CalendarEvent struct {
	ID        int64
	TeamID    int64
	Title     string
	StartsAt  time.Time
	EndsAt    time.Time
	Location  string
	Notes     string
	ProjectID int64
	ClientID  int64
	Shared    bool
	// SyncPush asks the author's sync to keep a copy of the event in the author's
	// connected provider calendar (CRM-19).
	SyncPush bool
	// Imported marks an event pulled from a provider; the sync owns it and never pushes it back.
	Imported     bool
	CreatedBy    int64
	Participants []int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Calendar sync failures (CRM-19). A missing connection is a refusal the member can
// fix by connecting; a disabled provider is a process configuration fact.
var (
	ErrCalendarNotConnected    = errors.New("calendar sync is not connected for this workspace")
	ErrCalendarSyncDisabled    = errors.New("calendar sync is not configured on this server")
	ErrCalendarStateInvalid    = errors.New("the calendar connection request is no longer valid")
	ErrCalendarProviderFailure = errors.New("the calendar provider refused the connection")
)
