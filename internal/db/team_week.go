package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/money"
)

// TrackedSecondsByPersonDay sums what each member tracked on each day of the query
// (PLAN-04), open timers included. A session counts for the part of each day it covers,
// so a session that runs past midnight is split between the two days.
func (d *DB) TrackedSecondsByPersonDay(ctx context.Context, query appmodel.PersonDayQuery) (map[int64][]int, error) {
	if query.TeamID <= 0 || len(query.Days) == 0 {
		return nil, ErrNotFound
	}
	first := query.Days[0]
	end := query.Days[len(query.Days)-1].AddDate(0, 0, 1)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT s.user_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		   FROM sessions s
		  WHERE s.team_id = ? AND s.start_at < ? AND (s.end_at IS NULL OR s.end_at > ?)`,
		query.TeamID, FormatTime(end), FormatTime(first))
	if err != nil {
		return nil, fmt.Errorf("read sessions for the team week: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]int)
	for rows.Next() {
		var (
			userID              sql.NullInt64
			startAt             string
			endAt, lastResumeAt sql.NullString
			accumulated, paused int
		)
		if err := rows.Scan(&userID, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		if !userID.Valid {
			continue
		}
		session, err := scheduleSession(startAt, endAt, lastResumeAt, accumulated, paused)
		if err != nil {
			return nil, err
		}
		if out[userID.Int64] == nil {
			out[userID.Int64] = make([]int, len(query.Days))
		}
		for i, day := range query.Days {
			total, err := money.AddInt(out[userID.Int64][i], session.TrackedSecondsInWindow(day, day.AddDate(0, 0, 1), query.Now))
			if err != nil {
				return nil, fmt.Errorf("sum team week: %w", err)
			}
			out[userID.Int64][i] = total
		}
	}
	return out, rows.Err()
}

// TrackedSecondsByProject sums what the workspace tracked on each project over the days of
// the query (PLAN-05): a session counts for the part of the window it covers, and sits on
// its own project or, failing that, on its activity's project.
func (d *DB) TrackedSecondsByProject(ctx context.Context, query appmodel.PersonDayQuery) (map[int64]int, error) {
	if query.TeamID <= 0 || len(query.Days) == 0 {
		return nil, ErrNotFound
	}
	from := query.Days[0]
	until := query.Days[len(query.Days)-1].AddDate(0, 0, 1)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT `+sessionProject+`, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		   FROM sessions s JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		  WHERE s.team_id = ? AND s.start_at < ? AND (s.end_at IS NULL OR s.end_at > ?)`,
		query.TeamID, FormatTime(until), FormatTime(from))
	if err != nil {
		return nil, fmt.Errorf("read sessions for the project plan: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]int)
	for rows.Next() {
		var (
			projectID           sql.NullInt64
			startAt             string
			endAt, lastResumeAt sql.NullString
			accumulated, paused int
		)
		if err := rows.Scan(&projectID, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		if !projectID.Valid {
			continue
		}
		session, err := scheduleSession(startAt, endAt, lastResumeAt, accumulated, paused)
		if err != nil {
			return nil, err
		}
		total, err := money.AddInt(out[projectID.Int64], session.TrackedSecondsInWindow(from, until, query.Now))
		if err != nil {
			return nil, fmt.Errorf("sum project tracked time: %w", err)
		}
		out[projectID.Int64] = total
	}
	return out, rows.Err()
}
