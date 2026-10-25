package model

import (
	"errors"
	"time"
)

// Deal stages are fixed (platform-crm-calendar-mcp, D2). A deal that is lost needs
// a reason; a won deal keeps its close date and creates nothing else.
const (
	DealStageNew      = "new"
	DealStageWorking  = "working"
	DealStageProposal = "proposal"
	DealStageWon      = "won"
	DealStageLost     = "lost"
)

// DealStages lists the stages in the order a board shows them.
var DealStages = []string{DealStageNew, DealStageWorking, DealStageProposal, DealStageWon, DealStageLost}

// IsDealStage reports whether stage is one of the five fixed stages.
func IsDealStage(stage string) bool {
	for _, s := range DealStages {
		if s == stage {
			return true
		}
	}
	return false
}

// ActivityKinds lists what an activity can record.
var ActivityKinds = []string{"call", "meeting", "note"}

var (
	ErrInvalidDealStage        = errors.New("unknown deal stage")
	ErrLostReasonRequired      = errors.New("a lost deal needs a reason")
	ErrInvalidActivityKind     = errors.New("unknown activity kind")
	ErrEventEndBeforeStart     = errors.New("event ends before it starts")
	ErrCalendarTargetOutOfTeam = errors.New("linked project, client or participant is not in this workspace")
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
	ClientID    int64
	ContactID   int64
	OwnerID     int64
	AmountCents int64
	Currency    string
	Probability int
	CloseDate   string
	NextStepAt  string
	LostReason  string
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Overdue reports an open deal whose next step date has passed. Dates are
// compared as YYYY-MM-DD strings in the workspace day.
func (d CRMDeal) Overdue(today string) bool {
	if d.Stage == DealStageWon || d.Stage == DealStageLost || d.NextStepAt == "" {
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
