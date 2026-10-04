package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/money"
)

// durationToHuman is an alias of fmtDurL so the editable field
// matches the read-only cells.
func durationToHuman(lang i18n.Lang, sec int) string {
	if sec > 0 && sec < 60 {
		if lang == i18n.Ru {
			return fmt.Sprintf("%d\u00a0с", sec)
		}
		return fmt.Sprintf("%ds", sec)
	}
	return fmtDurL(lang, sec)
}

// fmtDuration is the English duration label (API, CSV, tests).
func fmtDuration(sec int) string { return fmtDurL(i18n.En, sec) }

// fmtDur is a duration in the request's language and the user's format.
func fmtDur(r *http.Request, sec int) string { return fmtDurF(resolveLang(r), durFmtOf(r), sec) }

// fmtDurF is fmtDurL in a chosen format: "hm" (default, 2 ч 30 мин),
// "decimal" (2,50 ч: hundredths, as billed) or "clock" (2:30).
func fmtDurF(lang i18n.Lang, f string, sec int) string {
	switch f {
	case "decimal":
		unit := " h"
		if lang == i18n.Ru {
			unit = "\u00a0ч"
		}
		return fmtHoursL(lang, money.HoursHundredths(max(sec, 0))) + unit
	case "clock":
		if sec < 0 {
			sec = 0
		}
		return fmt.Sprintf("%d:%02d", sec/3600, (sec/60)%60)
	}
	return fmtDurL(lang, sec)
}

// fmtDurL is the single duration label used everywhere: "1h 30m" /
// "1 ч 30 мин". timeparse.ParseDuration reads both back.
func fmtDurL(lang i18n.Lang, sec int) string {
	mu := "m"
	if lang == i18n.Ru {
		mu = "\u00a0мин"
	}
	if sec <= 0 {
		return "0" + mu
	}
	if sec < 60 {
		// Honest: a few seconds are not "1 min". Editable inputs use
		// durationToHuman, which keeps the real seconds.
		return "<1" + mu
	}
	return fmtMinutesL(lang, sec/60)
}

func fmtMinutesL(lang i18n.Lang, minutes int) string {
	hu, mu := "h", "m"
	if lang == i18n.Ru {
		hu, mu = "\u00a0ч", "\u00a0мин"
	}
	if minutes <= 0 {
		return "0" + mu
	}
	h := minutes / 60
	m := minutes % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d%s %d%s", h, hu, m, mu)
	case h > 0:
		return fmt.Sprintf("%d%s", h, hu)
	default:
		return fmt.Sprintf("%d%s", m, mu)
	}
}

var (
	ruWeekdays = [...]string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}
	ruMonths   = [...]string{"янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
)

// fmtWeekday is the short weekday: "Mon" / "Пн".
func fmtWeekday(lang i18n.Lang, t time.Time) string {
	if lang == i18n.Ru {
		return ruWeekdays[t.Weekday()]
	}
	return t.Format("Mon")
}

// fmtDay is day + month: "Sep 21" / "21 сен".
func fmtDay(lang i18n.Lang, t time.Time) string {
	if lang == i18n.Ru {
		return fmt.Sprintf("%d %s", t.Day(), ruMonths[t.Month()-1])
	}
	return t.Format("Jan 2")
}

// fmtDate is a full date: "Sep 21, 2026" / "21 сен 2026".
func fmtDate(lang i18n.Lang, t time.Time) string {
	if lang == i18n.Ru {
		return fmt.Sprintf("%s %d", fmtDay(lang, t), t.Year())
	}
	return t.Format("Jan 2, 2006")
}

// fmtHours renders billable hundredths of an hour as "3.50". Unitless so
// it reads the same in every language and in the PDF core font.
func fmtHours(hundredths int) string {
	return fmt.Sprintf("%d.%02d", hundredths/100, hundredths%100)
}

// fmtHoursL is fmtHours with the language's decimal mark: "3,50" in
// Russian, matching the money beside it on the same document.
func fmtHoursL(lang i18n.Lang, hundredths int) string {
	if lang == i18n.Ru {
		return fmt.Sprintf("%d,%02d", hundredths/100, hundredths%100)
	}
	return fmtHours(hundredths)
}

// fmtClock is a stopwatch reading, "1:02:05", for timers that tick.
func fmtClock(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}

// fmtWhen is a human timestamp relative to now: "сегодня 05:11",
// "вчера 09:00", "пт 25 сен, 09:00"; the year appears only when it differs.
func fmtWhen(lang i18n.Lang, t, now time.Time) string {
	hm := t.Format("15:04")
	switch {
	case sameDay(t, now):
		return i18n.T(lang, "when.today") + " " + hm
	case sameDay(t, now.AddDate(0, 0, -1)):
		return i18n.T(lang, "when.yesterday") + " " + hm
	}
	day := fmtWeekday(lang, t) + " " + fmtDay(lang, t)
	if lang != i18n.Ru {
		day = fmtWeekday(lang, t) + ", " + fmtDay(lang, t)
	}
	if t.Year() != now.Year() {
		day += " " + t.Format("2006")
	}
	return day + ", " + hm
}
