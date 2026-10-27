import * as React from "react"
import { ChevronLeft, ChevronRight } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DisclosureSection } from "@/components/disclosure-section"
import { requestConfirmation } from "@/components/confirmation-dialog"
import { translate as t } from "@/i18n"
import type { CalendarData } from "@/dashboard/types"
import { apiJSON, ApiError } from "@/lib/json-api"
import { formatDateTime } from "@/lib/date-format"
import { loadMembers, memberLabel, type Member } from "@/lib/team-directory"
import {
  addDays, addMonths, clockOf, dayMillis, dayOf, daysInWindow, monthGridStart, monthStartOf, MONTH_GRID_DAYS,
  nextFullHour, weekStartOf, wallClockOf, type CalendarEvent,
} from "@/calendar/model"
import { EventForm } from "@/calendar/event-form"

type View = "week" | "month"

const capitalize = (text: string) => text.charAt(0).toUpperCase() + text.slice(1)

// A day string has no zone, so its name is formatted at UTC midnight: in a browser
// west of UTC the local midnight would be the day before.
const dayName = (locale: string, day: string, options: Intl.DateTimeFormatOptions) =>
  new Intl.DateTimeFormat(locale, { ...options, timeZone: "UTC" }).format(dayMillis(day))

/**
 * The team calendar. A week reads as an agenda, one block per day, so a phone reads
 * it top to bottom; a month reads as a grid, and the day picked in it lists its events
 * below. Shared events are visible to everyone; a personal event comes back only to its
 * author, its participants and managers, which the server decides.
 */
export function CalendarPage({ data, viewerID }: { data: CalendarData; viewerID: number }) {
  const lang = data.Lang
  const locale = lang === "en" ? "en-US" : "ru-RU"
  const sunday = data.WeekStartsSunday
  const [view, setView] = React.useState<View>("week")
  const [weekStart, setWeekStart] = React.useState(() => weekStartOf(data.Today, sunday))
  const [monthStart, setMonthStart] = React.useState(() => monthStartOf(data.Today))
  const [selectedDay, setSelectedDay] = React.useState(data.Today)
  const [events, setEvents] = React.useState<CalendarEvent[]>([])
  const [members, setMembers] = React.useState<Member[]>([])
  const [selectedID, setSelectedID] = React.useState<number | null>(null)
  const [editingID, setEditingID] = React.useState<number | null>(null)
  const [defaultStart] = React.useState(() => nextFullHour(data.NowLocal))
  const [error, setError] = React.useState("")

  // What the page reads: the week, or the six weeks of the month grid. Both ends are inclusive.
  const first = view === "week" ? weekStart : monthGridStart(monthStart, sunday)
  const last = addDays(first, view === "week" ? 6 : MONTH_GRID_DAYS - 1)

  const load = React.useCallback(async (from: string, to: string) => {
    const query = new URLSearchParams({ from, to })
    try {
      const body = await apiJSON<{ events: CalendarEvent[] }>(`/api/calendar/events?${query}`, "GET", "")
      setEvents(body.events)
      setError("")
    } catch (cause) {
      setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))
    }
  }, [lang])

  React.useEffect(() => { void load(first, last) }, [load, first, last])

  React.useEffect(() => {
    if (data.CanManage) loadMembers().then(setMembers).catch(() => setError(t(lang, "err.generic")))
  }, [data.CanManage, lang])

  function resetPicks() {
    setSelectedID(null)
    setEditingID(null)
  }

  function moveWeek(day: string) {
    setWeekStart(day)
    resetPicks()
  }

  function moveMonth(month: string) {
    setMonthStart(month)
    setSelectedDay(monthStartOf(data.Today) === month ? data.Today : month)
    resetPicks()
  }

  // The week and the month share the selected day: the month opens on the week's first day,
  // and the week opens on the day picked in the month.
  function switchView(next: View) {
    if (next === view) return
    if (next === "month") {
      setMonthStart(monthStartOf(weekStart))
      setSelectedDay(data.Today >= weekStart && data.Today < addDays(weekStart, 7) ? data.Today : weekStart)
    } else {
      setWeekStart(weekStartOf(selectedDay, sunday))
    }
    setView(next)
    resetPicks()
  }

  // A timed event is listed on the day it starts, and an all-day event on each day it covers.
  // An event that started before the window and runs into it is listed on the window's first day.
  const byDay = new Map<string, CalendarEvent[]>()
  const place = (day: string, event: CalendarEvent) => byDay.set(day, [...(byDay.get(day) ?? []), event])
  for (const event of events) {
    const startDay = dayOf(event.starts_at)
    if (!event.all_day) {
      place(startDay < first ? first : startDay, event)
      continue
    }
    for (const day of daysInWindow(event, first, last)) place(day, event)
  }
  for (const list of byDay.values()) list.sort((a, b) => a.starts_at.localeCompare(b.starts_at))

  function canChange(event: CalendarEvent) {
    return data.CanManage || event.created_by === viewerID
  }

  // Named in full, so the range reads «2 ноября по 3 ноября» and never «2 нояб. по 3 нояб.».
  const shortDay = (day: string) => dayName(locale, day, { day: "numeric", month: "long" })

  function timeLabel(event: CalendarEvent) {
    if (event.all_day) {
      const firstDay = dayOf(event.starts_at)
      const lastDay = addDays(dayOf(event.ends_at), -1)
      return firstDay === lastDay ? t(lang, "cal.allDay") : t(lang, "period.span", shortDay(firstDay), shortDay(lastDay))
    }
    const sameDay = dayOf(event.starts_at) === dayOf(event.ends_at)
    const show = (iso: string) => sameDay ? clockOf(iso) : formatDateTime(wallClockOf(iso), lang)
    return t(lang, "cal.range", show(event.starts_at), show(event.ends_at))
  }

  async function remove(event: CalendarEvent) {
    if (!await requestConfirmation(t(lang, "cal.deleteConfirm"), { title: t(lang, "cal.deleteTitle"), action: t(lang, "nav.delete") })) return
    try {
      await apiJSON(`/api/calendar/events/${event.id}/delete`, "POST", data.CSRFToken)
      resetPicks()
      await load(first, last)
    } catch (cause) {
      setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))
    }
  }

  // The events of a day, each opening into its notes and the actions its author or a manager has.
  function renderEvents(list: CalendarEvent[]) {
    return <ul className="grid divide-y rounded-lg border">
      {list.map(event => {
        const open = selectedID === event.id
        return <li key={event.id} className="px-3">
          <button type="button" aria-expanded={open} onClick={() => { setSelectedID(open ? null : event.id); setEditingID(null) }} className="grid w-full gap-1 py-3 text-left">
            <span className="flex flex-wrap items-center gap-2"><time className="font-mono text-sm tabular-nums">{timeLabel(event)}</time><Badge variant={event.shared ? "secondary" : "outline"}>{event.shared ? t(lang, "cal.shared") : t(lang, "cal.personal")}</Badge></span>
            <span className="font-medium [overflow-wrap:anywhere]">{event.title}</span>
            {event.location && <span className="text-sm text-muted-foreground [overflow-wrap:anywhere]">{event.location}</span>}
          </button>
          {open && <div className="pb-4">
            {editingID === event.id
              ? <EventForm key={`edit-${event.id}`} lang={lang} csrf={data.CSRFToken} canManage={data.CanManage} members={members} event={event} syncProviders={data.SyncProviders} userID={data.UserID} onSaved={() => { setEditingID(null); void load(first, last) }} onCancel={() => setEditingID(null)} />
              : <div className="grid gap-3">
                {event.notes && <p className="whitespace-pre-line text-sm [overflow-wrap:anywhere]">{event.notes}</p>}
                {data.CanManage && event.participant_ids.length > 0 && <p className="text-sm"><span className="text-muted-foreground">{t(lang, "cal.participants")}: </span>{event.participant_ids.map(id => members.find(member => member.id === id)).filter(Boolean).map(member => memberLabel(member!)).join(", ")}</p>}
                {canChange(event) && <div className="flex flex-wrap gap-2">
                  <Button type="button" variant="outline" size="sm" onClick={() => setEditingID(event.id)}>{t(lang, "projects.edit")}</Button>
                  <Button type="button" variant="ghost" size="sm" onClick={() => void remove(event)}>{t(lang, "nav.delete")}</Button>
                </div>}
              </div>}
          </div>}
        </li>
      })}
    </ul>
  }

  const title = view === "week"
    ? capitalize(`${dayName(locale, weekStart, { month: "long" })} ${weekStart.slice(0, 4)}`)
    : capitalize(`${dayName(locale, monthStart, { month: "long" })} ${monthStart.slice(0, 4)}`)
  const days = Array.from({ length: 7 }, (_, index) => addDays(weekStart, index))
  const gridDays = Array.from({ length: MONTH_GRID_DAYS }, (_, index) => addDays(first, index))
  const pickedList = byDay.get(selectedDay) ?? []
  const pickedTitle = capitalize(dayName(locale, selectedDay, { weekday: "long", day: "numeric", month: "long" }))

  return <main className="mx-auto w-full max-w-3xl space-y-5">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "nav.calendar")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "cal.blurb")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "cal.zoneLabel")}: {data.Zone}</p></header>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    <DisclosureSection title={t(lang, "cal.newEvent")}>
      <EventForm lang={lang} csrf={data.CSRFToken} canManage={data.CanManage} members={members} startAt={defaultStart} syncProviders={data.SyncProviders} userID={data.UserID} onSaved={() => void load(first, last)} />
    </DisclosureSection>
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h2 className="text-lg font-semibold">{title}</h2>
      <div className="flex flex-wrap items-center gap-1">
        <div role="group" aria-label={t(lang, "cal.viewLabel")} className="mr-1 flex rounded-md border p-0.5">
          <Button type="button" variant={view === "week" ? "secondary" : "ghost"} size="sm" aria-pressed={view === "week"} onClick={() => switchView("week")}>{t(lang, "cal.viewWeek")}</Button>
          <Button type="button" variant={view === "month" ? "secondary" : "ghost"} size="sm" aria-pressed={view === "month"} onClick={() => switchView("month")}>{t(lang, "cal.viewMonth")}</Button>
        </div>
        {view === "week" ? <>
          <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "cal.prevWeek")} onClick={() => moveWeek(addDays(weekStart, -7))}><ChevronLeft aria-hidden="true" /></Button>
          <Button type="button" variant="outline" size="sm" onClick={() => moveWeek(weekStartOf(data.Today, sunday))}>{t(lang, "cal.thisWeek")}</Button>
          <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "cal.nextWeek")} onClick={() => moveWeek(addDays(weekStart, 7))}><ChevronRight aria-hidden="true" /></Button>
        </> : <>
          <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "cal.prevMonth")} onClick={() => moveMonth(addMonths(monthStart, -1))}><ChevronLeft aria-hidden="true" /></Button>
          <Button type="button" variant="outline" size="sm" onClick={() => moveMonth(monthStartOf(data.Today))}>{t(lang, "cal.thisMonth")}</Button>
          <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "cal.nextMonth")} onClick={() => moveMonth(addMonths(monthStart, 1))}><ChevronRight aria-hidden="true" /></Button>
        </>}
      </div>
    </div>
    {view === "week"
      ? <div className="grid gap-5">
        {days.map(day => {
          const list = byDay.get(day) ?? []
          const dayTitle = capitalize(dayName(locale, day, { weekday: "long", day: "numeric", month: "long" }))
          return <section key={day} aria-labelledby={`day-${day}`} className="grid gap-2">
            <h3 id={`day-${day}`} className="flex items-center gap-2 text-sm font-semibold">{dayTitle}{day === data.Today && <Badge variant="secondary">{t(lang, "dash.today")}</Badge>}</h3>
            {list.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "cal.noEvents")}</p> : renderEvents(list)}
          </section>
        })}
      </div>
      : <>
        <div className="grid grid-cols-7 gap-px overflow-hidden rounded-lg border bg-border">
          {gridDays.slice(0, 7).map(day => <div key={`head-${day}`} className="bg-muted/40 px-1 py-1 text-center text-xs font-medium text-muted-foreground">{capitalize(dayName(locale, day, { weekday: "short" }))}</div>)}
          {gridDays.map(day => {
            const list = byDay.get(day) ?? []
            const picked = day === selectedDay
            const inMonth = day.slice(0, 7) === monthStart.slice(0, 7)
            return <button key={day} type="button" aria-pressed={picked} aria-label={`${capitalize(dayName(locale, day, { weekday: "long", day: "numeric", month: "long" }))}, ${list.length}`} onClick={() => { setSelectedDay(day); resetPicks() }} className={`grid min-h-16 content-start gap-1 bg-background p-1 text-left sm:min-h-24 ${inMonth ? "" : "text-muted-foreground/60"} ${picked ? "ring-2 ring-inset ring-ring" : ""}`}>
              <span className={`w-fit rounded-full px-1.5 text-xs font-medium ${day === data.Today ? "bg-primary text-primary-foreground" : ""}`}>{Number(day.slice(8))}</span>
              {list.slice(0, 2).map(event => <span key={event.id} className="truncate rounded bg-muted px-1 text-[11px] leading-4">{event.title}</span>)}
              {list.length > 2 && <span className="text-[11px] text-muted-foreground">{t(lang, "cal.moreEvents", String(list.length - 2))}</span>}
            </button>
          })}
        </div>
        <section aria-labelledby="picked-day" className="grid gap-2">
          <h3 id="picked-day" className="flex items-center gap-2 text-sm font-semibold">{pickedTitle}{selectedDay === data.Today && <Badge variant="secondary">{t(lang, "dash.today")}</Badge>}</h3>
          {pickedList.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "cal.noEvents")}</p> : renderEvents(pickedList)}
        </section>
      </>}
  </main>
}
