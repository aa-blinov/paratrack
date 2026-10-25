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
import { addDays, clockOf, dayMillis, dayOf, nextFullHour, weekStartOf, wallClockOf, type CalendarEvent } from "@/calendar/model"
import { EventForm } from "@/calendar/event-form"

const capitalize = (text: string) => text.charAt(0).toUpperCase() + text.slice(1)

// A day string has no zone, so its name is formatted at UTC midnight: in a browser
// west of UTC the local midnight would be the day before.
const dayName = (locale: string, day: string, options: Intl.DateTimeFormatOptions) =>
  new Intl.DateTimeFormat(locale, { ...options, timeZone: "UTC" }).format(dayMillis(day))

/**
 * The team week as an agenda: one block per day, so a phone reads it top to
 * bottom. Shared events are visible to everyone; a personal event comes back
 * only to its author, its participants and managers, which the server decides.
 */
export function CalendarPage({ data, viewerID }: { data: CalendarData; viewerID: number }) {
  const lang = data.Lang
  const locale = lang === "en" ? "en-US" : "ru-RU"
  const [weekStart, setWeekStart] = React.useState(() => weekStartOf(data.Today, data.WeekStartsSunday))
  const [events, setEvents] = React.useState<CalendarEvent[]>([])
  const [members, setMembers] = React.useState<Member[]>([])
  const [selectedID, setSelectedID] = React.useState<number | null>(null)
  const [editingID, setEditingID] = React.useState<number | null>(null)
  const [defaultStart] = React.useState(() => nextFullHour(data.NowLocal))
  const [error, setError] = React.useState("")

  // The window is inclusive on both ends, so a week is its first day plus six.
  const load = React.useCallback(async (from: string) => {
    const query = new URLSearchParams({ from, to: addDays(from, 6) })
    try {
      const body = await apiJSON<{ events: CalendarEvent[] }>(`/api/calendar/events?${query}`, "GET", "")
      setEvents(body.events)
      setError("")
    } catch (cause) {
      setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))
    }
  }, [lang])

  React.useEffect(() => { void load(weekStart) }, [load, weekStart])

  React.useEffect(() => {
    if (data.CanManage) loadMembers().then(setMembers).catch(() => setError(t(lang, "err.generic")))
  }, [data.CanManage, lang])

  function moveWeek(day: string) {
    setWeekStart(day)
    setSelectedID(null)
    setEditingID(null)
  }

  // An event that started before this week and runs into it is shown on its first day.
  const byDay = new Map<string, CalendarEvent[]>()
  for (const event of events) {
    const day = dayOf(event.starts_at)
    const key = day < weekStart ? weekStart : day
    byDay.set(key, [...(byDay.get(key) ?? []), event])
  }
  for (const list of byDay.values()) list.sort((a, b) => a.starts_at.localeCompare(b.starts_at))

  function canChange(event: CalendarEvent) {
    return data.CanManage || event.created_by === viewerID
  }

  function timeLabel(event: CalendarEvent) {
    const sameDay = dayOf(event.starts_at) === dayOf(event.ends_at)
    const show = (iso: string) => sameDay ? clockOf(iso) : formatDateTime(wallClockOf(iso), lang)
    return t(lang, "cal.range", show(event.starts_at), show(event.ends_at))
  }

  async function remove(event: CalendarEvent) {
    if (!await requestConfirmation(t(lang, "cal.deleteConfirm"), { title: t(lang, "cal.deleteTitle"), action: t(lang, "nav.delete") })) return
    try {
      await apiJSON(`/api/calendar/events/${event.id}/delete`, "POST", data.CSRFToken)
      setSelectedID(null)
      setEditingID(null)
      await load(weekStart)
    } catch (cause) {
      setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))
    }
  }

  const monthTitle = capitalize(`${dayName(locale, weekStart, { month: "long" })} ${weekStart.slice(0, 4)}`)
  const days = Array.from({ length: 7 }, (_, index) => addDays(weekStart, index))

  return <main className="mx-auto w-full max-w-3xl space-y-5">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "nav.calendar")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "cal.blurb")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "cal.zoneLabel")}: {data.Zone}</p></header>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    <DisclosureSection title={t(lang, "cal.newEvent")}>
      <EventForm lang={lang} csrf={data.CSRFToken} canManage={data.CanManage} members={members} startAt={defaultStart} syncProviders={data.SyncProviders} userID={data.UserID} onSaved={() => void load(weekStart)} />
    </DisclosureSection>
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h2 className="text-lg font-semibold">{monthTitle}</h2>
      <div className="flex items-center gap-1">
        <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "cal.prevWeek")} onClick={() => moveWeek(addDays(weekStart, -7))}><ChevronLeft aria-hidden="true" /></Button>
        <Button type="button" variant="outline" size="sm" onClick={() => moveWeek(weekStartOf(data.Today, data.WeekStartsSunday))}>{t(lang, "cal.thisWeek")}</Button>
        <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "cal.nextWeek")} onClick={() => moveWeek(addDays(weekStart, 7))}><ChevronRight aria-hidden="true" /></Button>
      </div>
    </div>
    <div className="grid gap-5">
      {days.map(day => {
        const key = day
        const list = byDay.get(key) ?? []
        const title = capitalize(dayName(locale, day, { weekday: "long", day: "numeric", month: "long" }))
        return <section key={key} aria-labelledby={`day-${key}`} className="grid gap-2">
          <h3 id={`day-${key}`} className="flex items-center gap-2 text-sm font-semibold">{title}{key === data.Today &&<Badge variant="secondary">{t(lang, "dash.today")}</Badge>}</h3>
          {list.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "cal.noEvents")}</p> : <ul className="grid divide-y rounded-lg border">
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
                    ? <EventForm key={`edit-${event.id}`} lang={lang} csrf={data.CSRFToken} canManage={data.CanManage} members={members} event={event} syncProviders={data.SyncProviders} userID={data.UserID} onSaved={() => { setEditingID(null); void load(weekStart) }} onCancel={() => setEditingID(null)} />
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
          </ul>}
        </section>
      })}
    </div>
  </main>
}
