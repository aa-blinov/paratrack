package web

import (
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

func attachSessionTags(rows []sessionView, tagsByID map[int64][]model.Tag) {
	for i := range rows {
		for _, t := range tagsByID[rows[i].ID] {
			rows[i].Tags = append(rows[i].Tags, tagChip{ID: t.ID, Name: t.Name})
		}
	}
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
