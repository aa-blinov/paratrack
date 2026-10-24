package model

import (
	"errors"
	"time"
)

// A manager approves a finished week of the timesheet. While a week is approved
// its time is read-only for everyone in the workspace, until a manager reopens it.
var (
	ErrWeekApproved        = errors.New("week is approved and locked")
	ErrWeekNotFinished     = errors.New("week is not finished yet")
	ErrWeekHasRunningTimer = errors.New("a timer is still running in this week")
	ErrWeekPaid            = errors.New("a pay run already covers this week")
	ErrWeekNotApproved     = errors.New("time is not in an approved week")
	ErrDateClosed          = errors.New("time before the closing date is locked")
)

// SessionApprovalLockError says which approved week refused a write. Like the
// invoice lock, it wraps its sentinel so callers can match either form.
type SessionApprovalLockError struct {
	From time.Time
	To   time.Time
}

func (e *SessionApprovalLockError) Error() string { return ErrWeekApproved.Error() }

func (e *SessionApprovalLockError) Unwrap() error { return ErrWeekApproved }

// SessionAutoLockError says that a write starts before the workspace's closing
// date (timesheet-autolock). Before is the first day that is still open.
type SessionAutoLockError struct {
	Before time.Time
}

func (e *SessionAutoLockError) Error() string { return ErrDateClosed.Error() }

func (e *SessionAutoLockError) Unwrap() error { return ErrDateClosed }

// PayrollNotApprovedError reports the closed time a pay run would count before
// its week was approved: how many sessions, and the first and last start.
type PayrollNotApprovedError struct {
	Count int
	First time.Time
	Last  time.Time
}

func (e *PayrollNotApprovedError) Error() string { return ErrWeekNotApproved.Error() }

func (e *PayrollNotApprovedError) Unwrap() error { return ErrWeekNotApproved }

// UnapprovedWeeksError lists the weeks of an invoice period that still carry
// billable time outside every approved week. The invoice waits for the manager
// to confirm it (D-3), so nothing is written. Each week is named by its Monday, UTC.
type UnapprovedWeeksError struct {
	Weeks []time.Time
}

func (e *UnapprovedWeeksError) Error() string { return ErrWeekNotApproved.Error() }

func (e *UnapprovedWeeksError) Unwrap() error { return ErrWeekNotApproved }
