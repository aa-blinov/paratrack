package model

import (
	"errors"
	"strings"
)

var (
	ErrActiveSessionExists     = errors.New("activity already has an active session")
	ErrSessionNotActive        = errors.New("session is not active")
	ErrNotFound                = errors.New("not found")
	ErrInviteExpired           = errors.New("invite expired")
	ErrGoalNotFound            = errors.New("goal not found")
	ErrSessionInvoiceLocked    = errors.New("session belongs to a sent or paid invoice")
	ErrInvoiceNotDraft         = errors.New("invoice is no longer a draft")
	ErrInvoiceNotPayable       = errors.New("invoice is not waiting for payment")
	ErrInvoiceTotal            = errors.New("invoice total cannot be negative")
	ErrPayrollChanged          = errors.New("pay run changed since the preview")
	ErrNoClientCard            = errors.New("the invoice's project has no client card")
	ErrPayrollNotDraft         = errors.New("pay run is no longer a draft")
	ErrAdjustmentNoBase        = errors.New("the member has no hours or salary on this run to take a percent of")
	ErrPaymentAmount           = errors.New("payment must be above zero and not more than what is still owed")
	ErrPaymentDate             = errors.New("payment day must be a date")
	ErrTokenInvalid            = errors.New("api token invalid")
	ErrNoBillableTime          = errors.New("no billable time in that period")
	ErrNoPayableTime           = errors.New("no paid members with tracked time in that period")
	ErrNoPayRates              = errors.New("no member has an hourly rate")
	ErrAlreadyExists           = errors.New("record already exists")
	ErrAlreadyBilled           = errors.New("some of this time was just billed on another invoice; reload and try again")
	ErrMixedCurrency           = errors.New("lines in different currencies")
	ErrInvoiceChanged          = errors.New("invoice changed while email was being prepared")
	ErrTagNotFound             = errors.New("tag not found")
	ErrStripeSessionMismatch   = errors.New("stripe checkout session does not match invoice")
	ErrStripeSessionPending    = errors.New("stripe checkout session is not persisted yet")
	ErrAmbiguousProject        = errors.New("project slug exists in more than one team")
	ErrForbidden               = errors.New("forbidden")
	ErrLastOwner               = errors.New("cannot remove the last team owner")
	ErrOwnerMustTransfer       = errors.New("transfer team ownership before removing its current owner")
	ErrTeamHasMembers          = errors.New("team still has multiple members")
	ErrSessionAlreadyPaused    = errors.New("session is already paused")
	ErrSessionNotPaused        = errors.New("session is not paused")
	ErrSessionReopenExpired    = errors.New("session can no longer be reopened")
	ErrSessionDurationOverflow = errors.New("session duration exceeds the representable range")
	ErrSessionPeriodInvalid    = errors.New("session end must be after its start")
	// ErrSessionEndInFuture refuses a session recorded afterwards that has not ended yet.
	ErrSessionEndInFuture     = errors.New("session end is later than now")
	ErrSessionLengthInvalid   = errors.New("session duration cannot be negative")
	ErrProjectRebindForbidden = errors.New("members may assign only unassigned activities to projects")
)

// AlreadyBilledInPeriodError says the period was billed already and names the
// invoices that hold its time. "Nothing to bill" alone reads as "wrong dates"
// and sends a person hunting through their period bounds instead of the list.
type AlreadyBilledInPeriodError struct {
	Invoices []string
}

func NewAlreadyBilledInPeriodError(invoices []string) *AlreadyBilledInPeriodError {
	return &AlreadyBilledInPeriodError{Invoices: invoices}
}

func (e *AlreadyBilledInPeriodError) Error() string {
	return "this period's time is already on invoices: " + strings.Join(e.Invoices, ", ")
}
