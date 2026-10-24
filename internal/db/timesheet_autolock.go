package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// Closing by age (timesheet-autolock). A workspace may set how many days back
// time stays open for edits. The cutoff is computed from the clock on every
// write and is not stored: nothing is written when the day passes, so there is
// no job to run and no approval is implied. Approval and pay are unchanged.

// TimesheetAutoLockCutoff returns the first day that is still open and whether
// the rule is on at all. It is the same date the write checks use.
func (d *DB) TimesheetAutoLockCutoff(ctx context.Context, teamID int64) (time.Time, bool, error) {
	days, loc, err := autoLockSettings(ctx, d.sql, teamID)
	if err != nil {
		return time.Time{}, false, err
	}
	if days == 0 {
		return time.Time{}, false, nil
	}
	return autoLockCutoff(d.currentTime(), days, loc), true, nil
}

// lockAutoClosed refuses a write to time that starts before the cutoff. It runs
// in the same transaction as the write, next to the approved-week check.
func lockAutoClosed(ctx context.Context, tx *Tx, teamID int64, at, now time.Time) error {
	days, loc, err := autoLockSettings(ctx, tx, teamID)
	if err != nil {
		return err
	}
	if days == 0 {
		return nil
	}
	cutoff := autoLockCutoff(now, days, loc)
	if at.Before(cutoff) {
		return &model.SessionAutoLockError{Before: cutoff}
	}
	return nil
}

// lockAutoClosedSessionStart checks a session by its stored start time.
func lockAutoClosedSessionStart(ctx context.Context, tx *Tx, teamID int64, startAt string, now time.Time) error {
	at, err := ParseTime(startAt)
	if err != nil {
		return err
	}
	return lockAutoClosed(ctx, tx, teamID, at, now)
}

// lockAutoClosedSessionByID checks a session that a write is about to change,
// by its id. A session that is gone is left to the write itself to report.
func lockAutoClosedSessionByID(ctx context.Context, tx *Tx, teamID, sessionID int64, now time.Time) error {
	var startAt string
	err := tx.QueryRowContext(ctx,
		`SELECT start_at FROM sessions WHERE id = ? AND team_id = ?`, sessionID, teamID).Scan(&startAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return lockAutoClosedSessionStart(ctx, tx, teamID, startAt, now)
}

// autoLockSettings reads the workspace's term and zone. A blank zone means UTC,
// the same fallback the rest of the workspace calendar uses.
func autoLockSettings(ctx context.Context, q queryRower, teamID int64) (int, *time.Location, error) {
	var days int
	var zone string
	if err := q.QueryRowContext(ctx,
		`SELECT autolock_days, timezone FROM teams WHERE id = ?`, teamID).Scan(&days, &zone); err != nil {
		return 0, nil, err
	}
	loc := time.UTC
	if zone != "" {
		parsed, err := time.LoadLocation(zone)
		if err != nil {
			return 0, nil, fmt.Errorf("load workspace time zone: %w", err)
		}
		loc = parsed
	}
	return days, loc, nil
}

// autoLockCutoff is the start of the local day that was days ago. time.Date
// normalises a day before the first of the month into the month before.
func autoLockCutoff(now time.Time, days int, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day()-days, 0, 0, 0, 0, loc)
}
