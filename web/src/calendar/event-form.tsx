import * as React from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { DateTimeField } from "@/components/date-field"
import { FieldIssue, FormIssue } from "@/components/form-issue"
import { StatusLine } from "@/components/status-line"
import { translate as t } from "@/i18n"
import { apiJSON, issueOf, type Issue } from "@/lib/json-api"
import { parseISODateTime } from "@/lib/date-format"
import { memberLabel, type Member } from "@/lib/team-directory"
import { addMinutes, wallClockOf, type CalendarEvent } from "@/calendar/model"

const eventFields: Record<string, string> = {
  title: "title",
  "event title": "title",
  starts_at: "starts_at",
  "start and end": "starts_at",
  ends_at: "ends_at",
  "location or notes": "location",
  location: "location",
  notes: "notes",
  links: "participants",
}

const sameIDs = (a: number[], b: number[]) => a.length === b.length && [...a].sort().join() === [...b].sort().join()

type Props = {
  lang: string
  csrf: string
  canManage: boolean
  members: Member[]
  /** Given, the form edits this event and sends only what changed. */
  event?: CalendarEvent
  /** Where a new event starts, as the workspace wall clock; it runs for an hour. */
  startAt?: string
  /** The providers the member has connected here; the push switch needs one (CRM-19). */
  syncProviders?: string[]
  /** The signed-in member: only the author of an event may switch its push. */
  userID?: number
  onSaved: () => void
  onCancel?: () => void
}

export function EventForm({ lang, csrf, canManage, members, event, startAt, syncProviders = [], userID = 0, onSaved, onCancel }: Props) {
  const prefix = event ? `event-${event.id}` : "event-new"
  const [title, setTitle] = React.useState(event?.title ?? "")
  const [start, setStart] = React.useState(event ? wallClockOf(event.starts_at) : startAt ?? "")
  const [end, setEnd] = React.useState(event ? wallClockOf(event.ends_at) : startAt ? addMinutes(startAt, 60) : "")
  const [location, setLocation] = React.useState(event?.location ?? "")
  const [notes, setNotes] = React.useState(event?.notes ?? "")
  const [shared, setShared] = React.useState(event?.shared ?? false)
  const [syncPush, setSyncPush] = React.useState(event?.sync_push ?? false)
  // An imported event belongs to the provider, so pushing it back would copy the provider's own event.
  const canPush = syncProviders.length > 0 && !event?.imported && (!event || event.created_by === userID)
  const [participants, setParticipants] = React.useState<number[]>(event?.participant_ids ?? [])
  const [broken, setBroken] = React.useState<Record<string, boolean>>({})
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [saved, setSaved] = React.useState(false)
  // The date fields keep their own text; a saved create starts them afresh by key.
  const [epoch, setEpoch] = React.useState(0)

  const invalid = (key: string) => issue?.key === key || undefined
  const described = (key: string) => issue?.key === key ? `${prefix}-${key}-issue` : undefined

  function toggle(id: number, on: boolean) {
    setParticipants(current => on ? [...current.filter(item => item !== id), id] : current.filter(item => item !== id))
  }

  async function submit(event_: React.FormEvent<HTMLFormElement>) {
    event_.preventDefault()
    setSaved(false)
    const startValid = parseISODateTime(start) !== null
    const endValid = parseISODateTime(end) !== null
    const brokenKey = Object.keys(broken).find(key => broken[key])
    if (brokenKey || !startValid || !endValid) {
      setIssue({ key: brokenKey || (!startValid ? "starts_at" : "ends_at"), message: t(lang, "err.invalidInput") })
      return
    }

    const values: Record<string, unknown> = {
      title: title.trim(),
      starts_at: start,
      ends_at: end,
      location: location.trim(),
      notes: notes.trim(),
      shared,
    }
    // Participants come from the members list, which only a manager can read. A
    // member's save leaves the list out, so the server keeps the one already set.
    const body: Record<string, unknown> = {}
    if (!event) {
      Object.assign(body, values)
      if (canManage) body.participant_ids = participants
      if (canPush && syncPush) body.sync_push = true
    } else {
      if (values.title !== event.title) body.title = values.title
      if (start !== wallClockOf(event.starts_at)) body.starts_at = values.starts_at
      if (end !== wallClockOf(event.ends_at)) body.ends_at = values.ends_at
      if (values.location !== event.location) body.location = values.location
      if (values.notes !== event.notes) body.notes = values.notes
      if (values.shared !== event.shared) body.shared = values.shared
      if (canManage && !sameIDs(participants, event.participant_ids)) body.participant_ids = participants
    }
    if (event && canPush && syncPush !== event.sync_push) body.sync_push = syncPush

    setBusy(true)
    setIssue(null)
    try {
      if (event) {
        await apiJSON(`/api/calendar/events/${event.id}`, "POST", csrf, body)
        setSaved(true)
      } else {
        await apiJSON("/api/calendar/events", "POST", csrf, body)
        setTitle("")
        setLocation("")
        setNotes("")
        setShared(false)
        setSyncPush(false)
        setParticipants([])
        setBroken({})
        setEpoch(value => value + 1)
      }
      onSaved()
    } catch (cause) {
      setIssue(issueOf(cause, eventFields, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <form onSubmit={submit} className="grid gap-4" aria-busy={busy}>
    <FormIssue issue={issue} />
    <div className="grid gap-2">
      <Label htmlFor={`${prefix}-title`}>{t(lang, "cal.eventTitle")}</Label>
      <Input id={`${prefix}-title`} value={title} onChange={event_ => setTitle(event_.target.value)} required maxLength={200} aria-invalid={invalid("title")} aria-describedby={described("title")} style={{ fontSize: 16 }} />
      <FieldIssue issue={issue} name="title" id={`${prefix}-title-issue`} />
    </div>
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-start`}>{t(lang, "cal.startsAt")}</Label>
        <DateTimeField key={`start-${epoch}`} id={`${prefix}-start`} ariaLabel={t(lang, "cal.startsAt")} required defaultValue={start} style={{ fontSize: 16 }} onValueChange={setStart} onBrokenChange={value => setBroken(previous => ({ ...previous, starts_at: value }))} />
        <FieldIssue issue={issue} name="starts_at" id={`${prefix}-starts_at-issue`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-end`}>{t(lang, "cal.endsAt")}</Label>
        <DateTimeField key={`end-${epoch}`} id={`${prefix}-end`} ariaLabel={t(lang, "cal.endsAt")} required defaultValue={end} style={{ fontSize: 16 }} onValueChange={setEnd} onBrokenChange={value => setBroken(previous => ({ ...previous, ends_at: value }))} />
        <FieldIssue issue={issue} name="ends_at" id={`${prefix}-ends_at-issue`} />
      </div>
    </div>
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-location`}>{t(lang, "cal.location")}</Label>
        <Input id={`${prefix}-location`} value={location} onChange={event_ => setLocation(event_.target.value)} maxLength={300} aria-invalid={invalid("location")} aria-describedby={described("location")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="location" id={`${prefix}-location-issue`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-notes`}>{t(lang, "cal.notes")}</Label>
        <Textarea id={`${prefix}-notes`} rows={3} value={notes} onChange={event_ => setNotes(event_.target.value)} maxLength={4000} aria-invalid={invalid("notes")} aria-describedby={described("notes")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="notes" id={`${prefix}-notes-issue`} />
      </div>
    </div>
    <label className="flex cursor-pointer items-start gap-3 text-sm">
      <Checkbox checked={shared} onCheckedChange={value => setShared(value === true)} className="mt-0.5" />
      <span className="grid gap-1">
        <span className="font-medium">{t(lang, "cal.sharedToggle")}</span>
        <span className="text-muted-foreground">{t(lang, "cal.sharedHint")}</span>
      </span>
    </label>
    {canPush && <label className="flex cursor-pointer items-start gap-3 text-sm">
      <Checkbox checked={syncPush} onCheckedChange={value => setSyncPush(value === true)} className="mt-0.5" />
      <span className="grid gap-1">
        <span className="font-medium">{t(lang, "cal.syncPush")}</span>
        <span className="text-muted-foreground">{t(lang, "cal.syncPushHint")}</span>
      </span>
    </label>}
    {event?.imported && <p className="text-sm text-muted-foreground">{t(lang, "cal.imported")}</p>}
    {canManage && members.length > 0 && <fieldset className="grid gap-2">
      <legend className="text-sm font-medium">{t(lang, "cal.participants")}</legend>
      <div className="grid gap-2 sm:grid-cols-2">
        {members.map(member => <label key={member.id} className="flex cursor-pointer items-center gap-2 text-sm">
          <Checkbox checked={participants.includes(member.id)} onCheckedChange={value => toggle(member.id, value === true)} />
          {memberLabel(member)}
        </label>)}
      </div>
      <FieldIssue issue={issue} name="participants" id={`${prefix}-participants-issue`} />
    </fieldset>}
    <div className="flex flex-wrap items-center gap-3">
      <Button type="submit" disabled={busy}>{event ? t(lang, "profile.save") : t(lang, "cal.createEvent")}</Button>
      {onCancel && <Button type="button" variant="outline" onClick={onCancel}>{t(lang, "projects.cancel")}</Button>}
      <StatusLine text={saved ? t(lang, "crm.saved") : ""} />
    </div>
  </form>
}
