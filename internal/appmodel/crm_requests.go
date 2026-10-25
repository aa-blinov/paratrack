package appmodel

import (
	"errors"
	"time"
)

// CRM and calendar requests (platform-crm-calendar-mcp). Mutations end in Request,
// reads in Query, so the architecture check can tell a write from a read.

// ErrInvalidField matches any FieldError. Transports show the field the user has to
// fix, so the message stays in their language.
var ErrInvalidField = errors.New("invalid field")

// ErrInvalidWorkspace is returned for a request without a positive workspace id.
var ErrInvalidWorkspace = errors.New("workspace id must be positive")

// FieldError names the input that was refused, such as "deal title".
type FieldError struct {
	Field string
}

func (e FieldError) Error() string {
	return "invalid field: " + e.Field
}

func (e FieldError) Is(target error) bool {
	return target == ErrInvalidField
}

type CRMContactCreateRequest struct {
	TeamID   int64
	CallerID int64
	ClientID int64
	Name     string
	Role     string
	Email    string
	Phone    string
}

type CRMDealCreateRequest struct {
	TeamID      int64
	CallerID    int64
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
}

// CRMDealUpdateRequest changes only the fields that are not nil.
type CRMDealUpdateRequest struct {
	TeamID      int64
	CallerID    int64
	DealID      int64
	Title       *string
	OwnerID     *int64
	ClientID    *int64
	ContactID   *int64
	AmountCents *int64
	Probability *int
	CloseDate   *string
	NextStepAt  *string
}

type CRMDealStageRequest struct {
	TeamID     int64
	CallerID   int64
	DealID     int64
	Stage      string
	LostReason string
}

type CRMDealListQuery struct {
	TeamID   int64
	CallerID int64
	Stage    string
	OwnerID  int64
	// Today is the workspace day as YYYY-MM-DD; Overdue keeps only open deals whose
	// next step is before it.
	Today   string
	Overdue bool
}

type CRMDealLookupQuery struct {
	TeamID   int64
	CallerID int64
	DealID   int64
}

type CRMContactListQuery struct {
	TeamID   int64
	CallerID int64
	ClientID int64
}

type CRMActivityCreateRequest struct {
	TeamID     int64
	CallerID   int64
	ClientID   int64
	DealID     int64
	Kind       string
	Body       string
	Result     string
	OccurredAt time.Time
}

type CRMActivityListQuery struct {
	TeamID   int64
	CallerID int64
	ClientID int64
	DealID   int64
}

type CalendarEventCreateRequest struct {
	TeamID         int64
	CallerID       int64
	Title          string
	StartsAt       time.Time
	EndsAt         time.Time
	Location       string
	Notes          string
	ProjectID      int64
	ClientID       int64
	Shared         bool
	ParticipantIDs []int64
}

// CalendarEventUpdateRequest changes only the fields that are not nil. Participants
// replace the list when ParticipantIDs is not nil.
type CalendarEventUpdateRequest struct {
	TeamID         int64
	CallerID       int64
	EventID        int64
	Title          *string
	StartsAt       *time.Time
	EndsAt         *time.Time
	Location       *string
	Notes          *string
	ProjectID      *int64
	ClientID       *int64
	Shared         *bool
	ParticipantIDs []int64
}

type CalendarEventDeleteRequest struct {
	TeamID   int64
	CallerID int64
	EventID  int64
}

type CalendarEventListQuery struct {
	TeamID   int64
	CallerID int64
	From     time.Time
	To       time.Time
}

// CRMStageListQuery reads the pipeline of a workspace.
type CRMStageListQuery struct {
	TeamID   int64
	CallerID int64
}

// CRMStageCreateRequest appends a stage to the pipeline. Manager is the caller's role
// as the transport read it; the persistence transaction checks the role again.
type CRMStageCreateRequest struct {
	TeamID   int64
	CallerID int64
	Manager  bool
	Name     string
	Kind     string
}

// CRMStageUpdateRequest changes the fields that are not nil. Key names the stage, and
// Position is 1-based; the other stages move to make room.
type CRMStageUpdateRequest struct {
	TeamID   int64
	CallerID int64
	Manager  bool
	Key      string
	Name     *string
	Kind     *string
	Position *int
}

// CRMStageMoveRequest moves a stage to a 1-based position; the stages in between shift.
type CRMStageMoveRequest struct {
	TeamID   int64
	CallerID int64
	Manager  bool
	Key      string
	Position int
}

type CRMStageDeleteRequest struct {
	TeamID   int64
	CallerID int64
	Manager  bool
	Key      string
}

// CRMPipeline is the stage count of a workspace's deals, in the workspace's stage
// order, and how many open deals have a next step that has already passed.
type CRMPipeline struct {
	Stages  []CRMPipelineStage
	Overdue int
}

type CRMPipelineStage struct {
	Stage string
	Name  string
	Kind  string
	Count int
}
