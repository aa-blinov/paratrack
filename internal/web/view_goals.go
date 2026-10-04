package web

import (
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// goalView is the per-row representation of a configured goal plus
// the progress actually achieved in its current window. Powers both the
// dashboard widget and the /goals management page.
type goalView struct {
	ID               int64
	ActivityName     string
	Color            string
	Period           string
	TargetMinutes    int
	TargetLabel      string // "2h", "1h 30m"
	AchievedMinutes  int
	AchievedLabel    string // "1h 32m"
	Percent          int    // 0..100+
	AchievedClass    string // "" | "met" | "exceeded"
	PeriodStartLabel string // "Mon Sep 22"
	PeriodEndLabel   string // "Sun Sep 28"
	PeriodRangeLabel string // short label e.g. "this week"
	Lang             string
}

func (v goalView) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }
