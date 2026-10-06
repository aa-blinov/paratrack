import { requestConfirmation } from "@/components/confirmation-dialog"
import * as React from "react"
import { BarChart3, Plus, Trash2, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DisclosureSection } from "@/components/disclosure-section"
import { translate as t } from "@/i18n"
import type { Session, StatsData } from "@/dashboard/types"

const periods = ["today", "yesterday", "week", "last_week", "month", "last_month"]

// periodName maps a ?period= value onto its switcher label. "today" is the
// dashboard's word for the same window, so the two screens say the same thing.
function periodName(lang: string, period: string) {
  return t(lang, period === "today" ? "dash.today" : `stats.${period === "last_week" ? "lastWeek" : period === "last_month" ? "lastMonth" : period}`)
}

function queryFor(data: StatsData, period = data.Period.Label, project = data.ProjectFilter, tag = data.TagFilter, person = data.PersonFilter) {
  const query = new URLSearchParams({ period })
  if (project) query.set("project", project)
  if (tag) query.set("tag", tag)
  if (person) query.set("person", String(person))
  return `/stats?${query}`
}

async function send(url: string, method: string, body: URLSearchParams | undefined, csrf: string, htmx = false) {
  const response = await fetch(url, { method, credentials: "same-origin", body, headers: { "X-CSRF-Token": csrf, ...(body ? { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" } : {}), ...(htmx ? { "HX-Request": "true" } : {}) } })
  if (!response.ok) {
    const text = await response.text()
    const message = new DOMParser().parseFromString(text, "text/html").body.textContent?.trim()
    throw new Error(message || response.statusText)
  }
}

function EditableSession({ data, session, onRemove }: { data: StatsData; session: Session; onRemove: (id: number) => void }) {
  const [start, setStart] = React.useState(session.StartInput || "")
  const [end, setEnd] = React.useState(session.EndInput || "")
  const [duration, setDuration] = React.useState(session.DurationInput)
  const [note, setNote] = React.useState(session.Note)
  const [tag, setTag] = React.useState("")
  const [error, setError] = React.useState("")
  const busy = React.useRef(false)

  async function save(patch: Partial<{ start_at: string; end_at: string; duration: string; note: string }> = {}) {
    if (busy.current) return
    const body = new URLSearchParams({ start_at: start, end_at: end, duration, note, ...patch })
    busy.current = true
    setError("")
    try { await send(`/api/sessions/${session.ID}`, "PATCH", body, data.CSRFToken, true); window.location.reload() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(data.Lang, "err.generic")) }
    finally { busy.current = false }
  }

  async function addTag(event?: React.FormEvent) {
    event?.preventDefault()
    const name = tag.trim()
    if (!name) return
    try { await send(`/api/sessions/${session.ID}/tags`, "POST", new URLSearchParams({ name }), data.CSRFToken, true); window.location.reload() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(data.Lang, "err.generic")) }
  }

  async function removeTag(name: string) {
    try { await send(`/api/sessions/${session.ID}/tags?name=${encodeURIComponent(name)}`, "DELETE", undefined, data.CSRFToken, true); window.location.reload() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(data.Lang, "err.generic")) }
  }

  const fields = <>
    <div className="grid min-w-0 gap-1.5"><Label htmlFor={`session-${session.ID}-0`} className="text-xs">{t(data.Lang, "dash.startLabel")}</Label><Input id={`session-${session.ID}-0`} type="datetime-local" style={{ fontSize: 16 }} aria-label={`${t(data.Lang, "dash.startLabel")} — ${session.ActivityName}`} value={start} onChange={event => setStart(event.target.value)} onBlur={() => { if (start !== session.StartInput) void save() }} /></div>
    <div className="grid min-w-0 gap-1.5"><Label htmlFor={`session-${session.ID}-1`} className="text-xs">{t(data.Lang, "stats.end")}</Label><Input id={`session-${session.ID}-1`} type="datetime-local" style={{ fontSize: 16 }} aria-label={`${t(data.Lang, "stats.end")} — ${session.ActivityName}`} value={end} onChange={event => setEnd(event.target.value)} onBlur={() => { if (end !== session.EndInput) void save() }} /></div>
    <div className="grid min-w-0 gap-1.5"><Label htmlFor={`session-${session.ID}-2`} className="text-xs">{t(data.Lang, "dash.duration")}</Label><Input id={`session-${session.ID}-2`} aria-label={`${t(data.Lang, "dash.duration")} — ${session.ActivityName}`} title={t(data.Lang, "stats.durationTitle")} placeholder={t(data.Lang, "ph.duration")} value={duration} onChange={event => setDuration(event.target.value)} onBlur={() => { if (duration !== session.DurationInput) void save() }} /></div>
    <div className="grid min-w-0 gap-1.5"><Label htmlFor={`session-${session.ID}-3`} className="text-xs">{t(data.Lang, "stats.note")}</Label><Input id={`session-${session.ID}-3`} aria-label={`${t(data.Lang, "stats.note")} — ${session.ActivityName}`} placeholder={t(data.Lang, "dash.noteShort")} value={note} onChange={event => setNote(event.target.value)} onBlur={() => { if (note !== session.Note) void save() }} /></div>
  </>
  return <>
    <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">{fields}</div>
    <div className="mt-2 flex flex-wrap items-center gap-2">
      {session.Tags?.map(item => <Badge key={item.ID} variant="outline" className="gap-1">#{item.Name}<Button variant="ghost" size="icon-xs" className="size-5" type="button" aria-label={`${t(data.Lang, "stats.removeTag")} ${item.Name}`} onClick={() => void removeTag(item.Name)}><X aria-hidden="true" className="size-3" /></Button></Badge>)}
      <form className="flex max-w-xs gap-1" onSubmit={addTag}><Input list="known-tags-all" aria-label={`${t(data.Lang, "stats.addTag")} — ${session.ActivityName}`} placeholder={t(data.Lang, "stats.addTag")} value={tag} onChange={event => setTag(event.target.value)} /><Button type="submit" size="icon" variant="outline" aria-label={t(data.Lang, "stats.attachTag")}><Plus aria-hidden="true" /></Button></form>
      <Button type="button" size="icon" variant="ghost" className="ml-auto text-destructive" title={t(data.Lang, "stats.deleteSession")} aria-label={t(data.Lang, "stats.deleteSession")} onClick={async () => await requestConfirmation(t(data.Lang, "stats.confirmDelete")) && onRemove(session.ID)}><Trash2 aria-hidden="true" /></Button>
    </div>
    {error && <p role="alert" className="mt-2 text-xs text-destructive">{error}</p>}
  </>
}

export function StatsPage({ data }: { data: StatsData }) {
  const [error, setError] = React.useState("")
  const lang = data.Lang || "en"
  async function removeSession(id: number) {
    try { await send(`/api/sessions/${id}`, "DELETE", undefined, data.CSRFToken, true); window.location.reload() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
  }
  const graphQuery = new URLSearchParams({ period: data.Period.Label })
  if (data.ProjectFilter) graphQuery.set("project", data.ProjectFilter)
  if (data.TagFilter) graphQuery.set("tag", data.TagFilter)
  if (data.PersonFilter) graphQuery.set("person", String(data.PersonFilter))
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "stats.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "stats.blurb")}</p></header>
    {data.Mods?.graph !== false && <a className="inline-flex items-center gap-2 text-sm underline underline-offset-4" href={`/graph?${graphQuery}`}><BarChart3 aria-hidden="true" className="size-4" />{data.ProjectFilter || data.TagFilter || data.PersonFilter ? t(lang, "stats.viewGraph") : t(lang, "stats.viewGraphAll")}</a>}
    <nav aria-label={t(lang, "stats.title")} className="flex flex-wrap gap-1 rounded-md border p-1">{periods.map(period => <a key={period} aria-current={data.Period.Label === period ? "page" : undefined} href={queryFor(data, period)} className={`rounded px-3 py-2 text-sm ${data.Period.Label === period ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted"}`}>{periodName(lang, period)}</a>)}</nav>
    {!!data.People?.length && <div className="flex flex-wrap items-center gap-2"><Label htmlFor="stats-person">{t(lang, "stats.person")}</Label><Select value={String(data.PersonFilter || "all")} onValueChange={value => { window.location.href = queryFor(data, data.Period.Label, data.ProjectFilter, data.TagFilter, value === "all" ? 0 : Number(value)) }}><SelectTrigger id="stats-person" className="w-full max-w-xs"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">{t(lang, "stats.everyone")}</SelectItem>{data.People.map(person => <SelectItem key={person.ID} value={String(person.ID)}>{person.Name}</SelectItem>)}</SelectContent></Select></div>}
    {(data.TagFilter || data.ProjectFilter) && <Card><CardContent className="flex flex-wrap items-center gap-2 p-3"><span className="text-xs uppercase text-muted-foreground">{t(lang, "stats.filteredBy")}</span>{data.ProjectFilter && <Badge variant="secondary">{data.ProjectFilter}</Badge>}{data.TagFilter && <Badge variant="secondary">#{data.TagFilter}</Badge>}<Button asChild size="sm" variant="ghost" className="ml-auto"><a href={queryFor(data, data.Period.Label, "", "")}>{t(lang, "stats.clearFilters")}</a></Button></CardContent></Card>}

    <Card><CardHeader><CardTitle>{t(lang, "stats.breakdown")}</CardTitle></CardHeader><CardContent className="grid gap-3">{data.Projects?.length > 0 && <div className="flex flex-wrap gap-1"><a href={queryFor(data, data.Period.Label, "")} className={`rounded-md px-2 py-1 text-xs ${!data.ProjectFilter ? "bg-primary text-primary-foreground" : "hover:bg-muted"}`}>{t(lang, "stats.all")}</a>{data.Projects.map(project => <a key={project.ID} title={project.Name} href={queryFor(data, data.Period.Label, project.Slug)} className={`inline-flex max-w-full items-center gap-1.5 rounded-md px-2 py-1 text-xs ${data.ProjectFilter === project.Slug ? "bg-primary text-primary-foreground" : "hover:bg-muted"}`}><span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: project.Color }} /><span className="truncate">{project.Name}</span></a>)}</div>}
      {data.ByProject?.length ? <><div className="grid gap-3">{data.ByProject.map(project => <section key={`${project.ProjectID}:${project.ProjectName}`} className="rounded-md border"><div className="flex items-center justify-between gap-3 p-3 text-sm font-semibold"><div className="flex min-w-0 items-center gap-2"><span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: project.Color }} />{project.Slug ? <a className="break-words hover:underline" href={`/projects/${project.Slug}`}>{project.ProjectName}</a> : <span>{project.ProjectName}</span>}</div><span className="whitespace-nowrap font-mono">{project.Duration}, {project.Share.toFixed(1)}%</span></div>{project.Activities.map(activity => <div key={`${project.ProjectID}:${activity.ActivityName}`} className="flex items-center justify-between gap-3 border-t px-3 py-2 text-sm"><span className="flex min-w-0 items-center gap-2"><span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: activity.Color }} /><span className="break-words">{activity.ActivityName}</span></span><span className="whitespace-nowrap font-mono text-muted-foreground">{activity.Duration}, {activity.Share.toFixed(1)}%</span></div>)}</section>)}</div><div className="flex justify-between border-t-2 pt-3 font-semibold"><span>{t(lang, "stats.total")}</span><span className="font-mono">{data.Total}</span></div></> : <p className="py-6 text-center text-sm text-muted-foreground">{t(lang, "stats.noSessionsPeriod")}</p>}
    </CardContent></Card>
    <DisclosureSection title={`${t(lang, "reports.title")} (${data.SavedReports?.length ?? 0})`} description={t(lang, "stats.savedHint")}><div className="grid gap-4"><form method="post" action="/api/reports/save" className="flex w-full min-w-0 gap-2 sm:w-auto"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="period" value={data.Period.Label} /><input type="hidden" name="project" value={data.ProjectFilter} /><input type="hidden" name="tag" value={data.TagFilter} /><Label htmlFor="report-save-name" className="sr-only">{t(lang, "reports.namePh")}</Label><Input id="report-save-name" name="name" required maxLength={40} placeholder={t(lang, "reports.namePh")} /><Button type="submit" size="sm"><Plus aria-hidden="true" />{t(lang, "reports.save")}</Button></form></div><div className="mt-4">{data.SavedReports?.length ? <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{data.SavedReports.map(report => <div key={report.ID} className="flex min-w-0 items-center rounded-md border"><a className="min-w-0 flex-1 break-words p-2 text-sm hover:underline" href={queryFor(data, report.Period, report.ProjectSlug, report.Tag, 0)}>{report.Name}</a>{(data.CanManage || report.CreatedBy === data.MeID) && <form method="post" action={`/api/reports/${report.ID}/delete`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="ghost" size="icon" aria-label={t(lang, "reports.deleteNamed", report.Name)}><X aria-hidden="true" /></Button></form>}</div>)}</div> : <p className="text-sm text-muted-foreground">{t(lang, "reports.emptyHint")}</p>}</div></DisclosureSection>
    <Card><CardHeader><CardTitle>{t(lang, "stats.sessions")} ({data.SessionCount})</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "stats.editHint")}</p></CardHeader><CardContent className="grid gap-4 p-3 sm:p-6">{error && <p role="alert" className="text-sm text-destructive">{error}</p>}<datalist id="known-tags-all">{data.AllTagNames?.map(name => <option key={name} value={name} />)}</datalist>
      {data.Sessions?.length ? <div className="grid gap-3">{data.Sessions.map(session => <article key={session.ID} className="min-w-0 rounded-md border p-2 sm:p-3"><div className="mb-3 flex flex-wrap items-start justify-between gap-2"><div className="flex min-w-0 items-start gap-2"><span className="mt-1 size-2 shrink-0 rounded-full" style={{ backgroundColor: session.Color }} /><div className="min-w-0"><strong className="break-words">{session.ActivityName}</strong>{session.PersonName && <p className="text-xs text-muted-foreground">{session.PersonName}</p>}</div></div>{session.ProjectName && <Badge variant="outline">{session.ProjectName}</Badge>}</div><EditableSession data={data} session={session} onRemove={id => void removeSession(id)} /></article>)}</div> : <div className="py-6 text-center"><p className="font-medium text-muted-foreground">{t(lang, "stats.noSessions")}</p><p className="text-sm text-muted-foreground">{t(lang, "stats.noSessionsHint")}</p>{data.Elsewhere?.length ? <p data-elsewhere className="mt-2 text-sm text-muted-foreground">{t(lang, "stats.elsewhere")} {data.Elsewhere.map(option => <span key={option.Label} className="whitespace-nowrap">{" "}<a className="underline underline-offset-4" href={queryFor(data, option.Label)}>{periodName(lang, option.Label)} — {option.Total}</a></span>)}</p> : null}<Button asChild size="sm" className="mt-3"><a href="/">{t(lang, "stats.toDashboard")}</a></Button></div>}
      {data.SessionsCut && <p className="text-sm text-muted-foreground">{t(lang, "stats.logCut", data.Sessions.length, data.SessionCount)} {data.Sessions.length < 500 && <a className="underline" href={data.ShowAllURL}>{t(lang, "stats.logAll")}</a>} <a className="underline" href="/export">{t(lang, "nav.csvExport")}</a></p>}
    </CardContent></Card>

  </main>
}
