package model

import (
	"errors"
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
)

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
}

// CalendarEvent is a team event. Participants are listed by user ID.
type CalendarEvent struct {
	ID           int64
	TeamID       int64
	Title        string
	StartsAt     time.Time
	EndsAt       time.Time
	Location     string
	Notes        string
	ProjectID    int64
	ClientID     int64
	Shared       bool
	CreatedBy    int64
	Participants []int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
