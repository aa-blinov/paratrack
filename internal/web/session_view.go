package web

import (
	"context"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// -- view-model helpers ----------------------------------------------

func toSessionView(s model.Session, a model.Activity, periodStart, periodEnd time.Time, now time.Time, lang i18n.Lang, durFmt ...string) sessionView {
	df := ""
	if len(durFmt) > 0 {
		df = durFmt[0]
	}
	v := sessionView{
		ID:                 s.ID,
		ActivityID:         s.ActivityID,
		ActivityName:       a.Name,
		Color:              colorFor(a.Name),
		ProjectID:          a.ProjectID,
		StartISO:           s.StartAt.UTC().Format(time.RFC3339Nano),
		ResumeISO:          s.StartAt.UTC().Format(time.RFC3339Nano),
		StartLocal:         fmtWhen(lang, s.StartAt.In(now.Location()), now.In(now.Location())),
		Clock:              fmtClock(s.DurationSeconds(now)),
		AccumulatedSeconds: s.AccumulatedSeconds,
		Paused:             s.Paused,
		Lang:               string(lang),
	}
	if s.LastResumeAt != nil {
		v.ResumeISO = s.LastResumeAt.UTC().Format(time.RFC3339Nano)
	}
	if s.EndAt != nil {
		// Same day as the start: the time alone reads cleaner.
		if sameDay(s.EndAt.In(now.Location()), s.StartAt.In(now.Location())) {
			v.EndLocal = s.EndAt.In(now.Location()).Format("15:04")
		} else {
			v.EndLocal = fmtWhen(lang, s.EndAt.In(now.Location()), now.In(now.Location()))
		}
		v.EndInput = toLocalInput(s.EndAt.In(now.Location()))
	}
	v.StartInput = toLocalInput(s.StartAt.In(now.Location()))
	if s.Note != nil {
		v.Note = *s.Note
	}
	// Compute the FULL duration first (for the editable input); the
	// clipped `Duration` (shown in the table cell) is derived after.
	if s.EndAt != nil {
		fullSecs := s.DurationSeconds(now)
		v.DurationInput = durationToHuman(lang, fullSecs)
		v.DurationSecs = s.TrackedSecondsInWindow(periodStart, periodEnd, now)
		v.Duration = fmtDurF(lang, df, v.DurationSecs)
	} else if s.LastResumeAt != nil && !s.Paused {
		secs := s.DurationSeconds(now)
		v.DurationSecs = s.TrackedSecondsInWindow(periodStart, periodEnd, now)
		v.Duration = fmtDurF(lang, df, v.DurationSecs)
		v.DurationInput = durationToHuman(lang, secs)
	} else if s.Paused {
		v.DurationSecs = s.TrackedSecondsInWindow(periodStart, periodEnd, now)
		v.Duration = fmtDurF(lang, df, v.DurationSecs)
		v.DurationInput = durationToHuman(lang, s.AccumulatedSeconds)
	} else {
		v.Duration = fmtDurF(lang, df, 0)
		v.DurationInput = v.Duration
	}
	return v
}

// hydrateSessionTags does a single batched lookup and attaches the
// resulting tag chips to each row. Safe to call with an empty slice.
type sessionTagReader interface {
	TagsForSessions(context.Context, int64, []int64) (map[int64][]model.Tag, error)
}

type viewLogger interface {
	Printf(string, ...any)
}

func hydrateSessionTags(ctx context.Context, reader sessionTagReader, teamID int64, rows []sessionView, logger viewLogger) {
	if len(rows) == 0 {
		return
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	tagsByID, err := reader.TagsForSessions(ctx, teamID, ids)
	if err != nil {
		if logger != nil {
			logger.Printf("web: attach tags to %d session rows for team %d: %v", len(rows), teamID, err)
		}
		return // non-fatal — just skip rendering tags
	}
	attachSessionTags(rows, tagsByID)
}

func attachSessionTags(rows []sessionView, tagsByID map[int64][]model.Tag) {
	for i := range rows {
		for _, t := range tagsByID[rows[i].ID] {
			rows[i].Tags = append(rows[i].Tags, tagChip{ID: t.ID, Name: t.Name})
		}
	}
}

// hydrateSessionProjects looks up the project for each session's
// activity (a session inherits its activity's project) and writes
// the chip onto the row. Sessions whose activity has no project are
// left blank — they show as "Uncategorized" in the badge if the
// template chooses to render that.
type projectSummaryReader interface {
	Summaries(context.Context, int64, []int64) (map[int64]model.ProjectSummary, error)
}

func hydrateSessionProjects(ctx context.Context, reader projectSummaryReader, teamID int64, rows []sessionView, logger viewLogger) {
	if len(rows) == 0 {
		return
	}
	// One query: every distinct (project_id) across the rows.
	seen := map[int64]struct{}{}
	pids := []int64{}
	for _, r := range rows {
		if r.ProjectID == 0 {
			continue
		}
		if _, ok := seen[r.ProjectID]; ok {
			continue
		}
		seen[r.ProjectID] = struct{}{}
		pids = append(pids, r.ProjectID)
	}
	if len(pids) == 0 {
		return
	}
	byID, err := reader.Summaries(ctx, teamID, pids)
	if err != nil {
		if logger != nil {
			logger.Printf("web: attach project summaries to %d session rows for team %d: %v", len(rows), teamID, err)
		}
		return
	}
	attachSessionProjects(rows, byID)
}

func attachSessionProjects(rows []sessionView, byID map[int64]model.ProjectSummary) {
	for i, r := range rows {
		if p, ok := byID[r.ProjectID]; ok {
			rows[i].ProjectName = p.Name
			rows[i].ProjectColor = p.Color
			rows[i].ProjectSlug = p.Slug
		}
	}
}

func toLocalInput(t time.Time) string {
	// datetime-local wants "2006-01-02T15:04" with no zone and no seconds.
	return t.Format("2006-01-02T15:04") // callers pass it in the user's zone
}
