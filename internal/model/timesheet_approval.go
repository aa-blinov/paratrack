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
)

// SessionApprovalLockError says which approved week refused a write. Like the
// invoice lock, it wraps its sentinel so callers can match either form.
type SessionApprovalLockError struct {
	From time.Time
	To   time.Time
}

func (e *SessionApprovalLockError) Error() string { return ErrWeekApproved.Error() }

func (e *SessionApprovalLockError) Unwrap() error { return ErrWeekApproved }

// PayrollNotApprovedError reports the closed time a pay run would count before
// its week was approved: how many sessions, and the first and last start.
type PayrollNotApprovedError struct {
	Count int
	First time.Time
	Last  time.Time
}

func (e *PayrollNotApprovedError) Error() string { return ErrWeekNotApproved.Error() }

func (e *PayrollNotApprovedError) Unwrap() error { return ErrWeekNotApproved }
