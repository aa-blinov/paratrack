import { lazy, StrictMode, Suspense, useEffect, useMemo, useRef, useState } from "react"
import { createRoot } from "react-dom/client"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import { clockLabel, sessionElapsedSeconds } from "@/dashboard/time"
import type { DashboardData, GoalsData, GraphData, ProjectCreateData, ProjectDetailData, ProjectListData, ReactPageBootstrap, Session, TagsData, TimesheetData } from "@/dashboard/types"

const root = document.getElementById("paratrack-react-root")
const payload = document.getElementById("react-page-data")
const ProjectList = lazy(() => import("@/projects/project-list").then(module => ({ default: module.ProjectList })))
const ProjectDetail = lazy(() => import("@/projects/project-detail").then(module => ({ default: module.ProjectDetail })))
const ProjectCreate = lazy(() => import("@/projects/project-create").then(module => ({ default: module.ProjectCreate })))
const GoalsPage = lazy(() => import("@/goals/goals-page").then(module => ({ default: module.GoalsPage })))
const TagsPage = lazy(() => import("@/tags/tags-page").then(module => ({ default: module.TagsPage })))
const GraphPage = lazy(() => import("@/graph/graph-page").then(module => ({ default: module.GraphPage })))
const TimesheetPage = lazy(() => import("@/timesheet/timesheet-page").then(module => ({ default: module.TimesheetPage })))

async function messageFrom(response: Response): Promise<string> {
  const body = await response.text()
  const doc = new DOMParser().parseFromString(body, "text/html")
  return doc.body.textContent?.trim() || response.statusText || "Request failed"
}

function DashboardApp({ initial, restoreFocus }: { initial: DashboardData; restoreFocus: boolean }) {
  const [data, setData] = useState(initial)
  const [activity, setActivity] = useState("")
  const [project, setProject] = useState(String(initial.DefaultProject || ""))
  const [note, setNote] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [showNext, setShowNext] = useState(true)
  const [backfillFailure, setBackfillFailure] = useState<{ field: string; message: string } | null>(null)
  const [rebindWarning, setRebindWarning] = useState(false)
  const lang = data.Lang || "en"

  useEffect(() => {
    if (restoreFocus) document.getElementById("activity")?.focus()
  }, [restoreFocus])

  async function refresh() {
    const response = await fetch("/api/dashboard", { headers: { Accept: "application/json" }, credentials: "same-origin" })
    if (!response.ok) throw new Error(await response.text())
    const body = await response.json() as { data: DashboardData }
    setData(body.data)
  }

  async function mutate(url: string, fields: Record<string, string> = {}) {
    setBusy(true)
    setError("")
    if (url === "/api/sessions/backfill") setBackfillFailure(null)
    try {
      const body = new URLSearchParams({ csrf_token: data.CSRFToken, ...fields })
      const response = await fetch(url, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": data.CSRFToken, "HX-Request": "true" },
        body,
      })
      if (!response.ok) throw new Error(await messageFrom(response))
      if (url === "/api/sessions/backfill" && response.headers.get("X-Backfill-Saved") !== "true") {
        const message = await messageFrom(response)
        setBackfillFailure({ field: response.headers.get("X-Backfill-Field") || "form", message })
        throw new Error(message)
      }
      await refresh()
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      return false
    } finally { setBusy(false) }
  }

  async function start(event?: React.FormEvent) {
    event?.preventDefault()
    if (!activity.trim()) return
    if (await mutate("/api/start", { activity: activity.trim(), project_id: project, note })) {
      setActivity("")
      setNote("")
    }
  }

  const labels = useMemo(() => ({
    title: t(lang, "nav.dashboard"), what: t(lang, "ledger.what"), start: t(lang, "dash.start"),
    active: t(lang, "dash.activeSessions"), today: t(lang, "dash.today"), tracked: t(lang, "ledger.tracked"),
    running: t(lang, "ledger.running"), paused: t(lang, "ledger.paused"), top: t(lang, "ledger.top"),
    project: t(lang, "dash.projectLabel"), uncategorized: t(lang, "dash.uncategorized"), note: t(lang, "dash.note"),
    status: t(lang, "dash.status"), started: t(lang, "dash.started"), duration: t(lang, "dash.duration"),
    pause: t(lang, "dash.pause"), resume: t(lang, "dash.resume"), stop: t(lang, "dash.stop"), focus: t(lang, "dash.focus"),
    recent: t(lang, "dash.recent"), viewAll: t(lang, "dash.viewAll"), noActive: t(lang, "dash.noActive"),
    noActiveHint: t(lang, "dash.noActiveHint"), goals: t(lang, "nav.goals"), manage: t(lang, "dash.manage"),
  }), [lang])

  const activities = data.Activities ?? []
  const projects = data.Projects ?? []
  const active = data.ActiveSessions ?? []
  const recent = data.Recent ?? []
  const top = data.TopToday || t(lang, "ledger.none")

  function sessionActions(session: Session) {
    return <div className="flex justify-end gap-1">
      <Button variant="ghost" size="sm" disabled={busy} title={t(lang, "dash.focusTitle")} onClick={() => void mutate(`/api/focus/${encodeURIComponent(session.ActivityName)}`)}>{labels.focus}</Button>
      <Button variant="ghost" size="sm" disabled={busy} onClick={() => void mutate(`/api/sessions/${session.ID}/${session.Paused ? "resume" : "pause"}`)}>{session.Paused ? labels.resume : labels.pause}</Button>
      <Button variant="destructive" size="sm" disabled={busy} onClick={() => void mutate(`/api/sessions/${session.ID}/stop`)}>{labels.stop}</Button>
    </div>
  }

  return <div className="mx-auto grid w-full max-w-6xl gap-4 p-4 pb-24 sm:p-6" aria-busy={busy}>
    <h1 className="text-2xl font-semibold tracking-tight">{labels.title}</h1>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    {!data.HasSession && <section className="rounded-md border p-4"><p className="font-medium">{t(lang, "onb.try")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "onb.tryHint")}</p><div className="mt-3 flex flex-wrap items-center gap-2"><span className="text-sm text-muted-foreground">{t(lang, "onb.examples")}</span>{t(lang, "onb.exampleList").split(",").map(item => item.trim()).filter(Boolean).map(item => <Button key={item} variant="outline" size="sm" disabled={busy} onClick={() => { setActivity(item); void mutate("/api/start", { activity: item, project_id: project }) }}>{item}</Button>)}</div></section>}
    <Card>
      <CardContent className="grid gap-5 p-5">
        <form onSubmit={start} className="grid gap-3">
          <Label htmlFor="activity">{labels.what}</Label>
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_12rem_auto]">
            <Input id="activity" name="activity" list="known-activities" autoComplete="off" value={activity} onChange={e => { setActivity(e.target.value); const known = activities.find(item => item.Name.toLocaleLowerCase() === e.target.value.trim().toLocaleLowerCase()); setRebindWarning(Boolean(known && String(known.ProjectID) !== (project || "0"))) }} placeholder={t(lang, "dash.activityHint")} required />
            <datalist id="known-activities">{activities.map(item => <option key={item.ID} value={item.Name} data-project={item.ProjectID} />)}</datalist>
            <Select value={project || "none"} onValueChange={value => { setProject(value === "none" ? "" : value); const known = activities.find(item => item.Name.toLocaleLowerCase() === activity.trim().toLocaleLowerCase()); setRebindWarning(Boolean(known && String(known.ProjectID) !== (value === "none" ? "0" : value))) }}>
              <SelectTrigger id="project_id" aria-label={labels.project} data-value={project || "0"}><SelectValue placeholder={labels.project} /></SelectTrigger>
              <SelectContent><SelectItem value="none">{labels.uncategorized}</SelectItem>{projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent>
            </Select>
            <Button type="submit" disabled={busy}>{labels.start}</Button>
          </div>
          <Input aria-label={labels.note} name="note" value={note} onChange={e => setNote(e.target.value)} placeholder={t(lang, "dash.noteHint")} />
          <p id="ledger-project-rebind" className="text-xs text-muted-foreground" hidden={!rebindWarning}>{t(lang, "dash.projectHistoryHint")}</p>
        </form>
        <Separator />
        <section id="active-list" aria-labelledby="active-heading" className="grid gap-3">
          <CardTitle id="active-heading" className="text-sm font-medium">{labels.active}</CardTitle>
          {active.length ? <div className="grid gap-3">{active.length > 1 && <div className="flex justify-end gap-2"><Button variant="ghost" size="sm" disabled={busy || data.RunningCount === 0} onClick={() => void mutate("/api/active/pause-all")}>{t(lang, "dash.pauseAll")}</Button><Button variant="destructive" size="sm" disabled={busy} onClick={() => window.confirm(t(lang, "dash.stopAllConfirm", active.length)) && void mutate("/api/active/stop-all")}>{t(lang, "dash.stopAll")}</Button></div>}{active.map(session => <div key={session.ID} data-session-id={session.ID} className="grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
            <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><span className="h-2 w-2 rounded-full" style={{ backgroundColor: session.Color }} /><strong className="ledger-activity truncate">{session.ActivityName}</strong>{projects.length > 0 ? <Select value={String(session.ProjectID || 0)} onValueChange={value => void mutate(`/api/activities/${session.ActivityID}/project`, { project_id: value })}><SelectTrigger aria-label={labels.project} className="ledger-project h-7 w-fit max-w-40 border-0 px-2 text-xs" data-value={session.ProjectID || 0}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="0">{labels.uncategorized}</SelectItem>{projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent></Select> : session.ProjectName && <Badge variant="outline">{session.ProjectName}</Badge>}</div><div className="mt-1 flex gap-3 text-xs text-muted-foreground"><span className={`status-pill ${session.Paused ? "is-paused" : "is-active"}`} data-state={session.Paused ? "paused" : "running"}>{session.Paused ? t(lang, "dash.statusPaused") : t(lang, "dash.statusActive")}</span><span>{session.StartLocal}</span><LiveClock session={session} /></div></div>
            {sessionActions(session)}
          </div>)}</div> : <div className="py-5 text-center text-sm text-muted-foreground"><p>{labels.noActive}</p><p>{labels.noActiveHint}</p></div>}
        </section>
        <Separator />
        <section className="grid grid-cols-2 gap-4 sm:grid-cols-4" aria-label={labels.today}>
          <Metric label={labels.tracked} value={data.TodayTotal} />
          <Metric label={labels.running} value={String(data.RunningCount)} />
          <Metric label={labels.paused} value={String(data.PausedCount)} />
          <Metric label={labels.top} value={top} />
        </section>
      </CardContent>
    </Card>
    {!data.HasProject && data.HasSession && showNext && <nav aria-label={t(lang, "onb.next")} className="flex flex-wrap items-center gap-4 rounded-md border px-4 py-3 text-sm"><strong>{t(lang, "onb.next")}</strong><a href="/stats" className="underline-offset-4 hover:underline">{t(lang, "onb.nextStats")}</a>{data.Mods?.invoices && <a href="/projects/new" className="underline-offset-4 hover:underline">{t(lang, "onb.nextProject")}</a>}<Button type="button" size="sm" variant="ghost" className="ml-auto" onClick={() => setShowNext(false)}>{t(lang, "onb.hide")}</Button></nav>}
    {data.Widgets?.goals && data.Mods?.goals && data.Goals?.length > 0 && <Card><CardHeader className="flex-row items-center justify-between space-y-0"><CardTitle>{labels.goals}</CardTitle><a className="text-sm underline-offset-4 hover:underline" href="/goals">{labels.manage}</a></CardHeader><CardContent className="grid gap-4">{data.Goals.map(goal => <div key={goal.ID} className="grid gap-2"><div className="flex justify-between gap-2 text-sm"><span className="truncate">{goal.ActivityName} <span className="text-muted-foreground">{goal.PeriodRangeLabel}</span></span><span className="whitespace-nowrap font-mono">{goal.AchievedLabel} / {goal.TargetLabel}</span></div><Progress value={Math.min(goal.Percent, 100)} aria-label={goal.ActivityName} /></div>)}</CardContent></Card>}
    {data.Widgets?.recent && recent.length > 0 && <Card><CardHeader className="flex-row items-center justify-between space-y-0"><CardTitle>{labels.recent}</CardTitle><a className="text-sm underline-offset-4 hover:underline" href="/stats">{labels.viewAll}</a></CardHeader><CardContent className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr className="border-b text-muted-foreground"><th className="p-2">{t(lang, "dash.activity")}</th><th className="p-2">{t(lang, "dash.startLabel")}</th><th className="p-2">{t(lang, "stats.end")}</th><th className="p-2">{labels.duration}</th><th className="p-2">{labels.note}</th><th className="p-2">{t(lang, "stats.tags")}</th><th className="p-2" /></tr></thead><tbody>{recent.map(session => <tr key={session.ID} className="border-b last:border-0"><td className="p-2"><span className="mr-2 inline-block h-2 w-2 rounded-full" style={{ backgroundColor: session.Color }} />{session.ActivityName}{session.ProjectName && <div><Badge variant="outline">{session.ProjectName}</Badge></div>}</td><td className="whitespace-nowrap p-2 font-mono">{session.StartLocal}</td><td className="whitespace-nowrap p-2 font-mono">{session.EndLocal}</td><td className="whitespace-nowrap p-2 font-mono">{session.Duration}</td><td className="p-2">{session.Note}</td><td className="p-2">{session.Tags?.map(tag => <a key={tag.ID} className="mr-1 inline-block" href={`/stats?tag=${encodeURIComponent(tag.Name)}`}>#{tag.Name}</a>)}</td><td className="p-2"><Button variant="ghost" size="sm" onClick={() => void mutate("/api/start", { activity: session.ActivityName })}>{t(lang, "dash.again")}</Button></td></tr>)}</tbody></table></CardContent></Card>}
    {data.Widgets?.unbilled && data.Mods?.invoices && data.Unbilled?.length > 0 && data.CanManage && <Card><CardHeader><CardTitle>{t(lang, "inv.unbilled")}</CardTitle></CardHeader><CardContent className="grid gap-2">{data.Unbilled.map(item => <div key={item.ProjectID} className="flex justify-between gap-3 text-sm"><a className="underline-offset-4 hover:underline" href={`/projects/${item.Slug}`}>{item.ProjectName}</a><span className="font-mono">{item.Hours} · {item.Amount}</span></div>)}</CardContent></Card>}
    {data.Widgets?.backfill && <Backfill data={data} failure={backfillFailure} onSubmit={fields => mutate("/api/sessions/backfill", fields)} busy={busy} />}
  </div>
}

function Backfill({ data, failure, onSubmit, busy }: { data: DashboardData; failure: { field: string; message: string } | null; onSubmit: (fields: Record<string, string>) => Promise<boolean>; busy: boolean }) {
  const [open, setOpen] = useState(false)
  const [fields, setFields] = useState(() => ({ activity: "", start: "", end: "", project_id: data.DefaultProject ? String(data.DefaultProject) : "", note: "" }))
  const [dismissed, setDismissed] = useState<string[]>([])
  const startRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    setDismissed([])
    if (failure?.field === "start") startRef.current?.focus()
  }, [failure])
  const lang = data.Lang
  const set = (key: keyof typeof fields, value: string) => { setFields(previous => ({ ...previous, [key]: value })); setDismissed(previous => [...previous, key]) }
  const fieldError = (key: string) => failure?.field === key && !dismissed.includes(key) ? failure.message : ""
  return <Card><details id="backfill" open={open} onToggle={event => setOpen(event.currentTarget.open)}><summary className="cursor-pointer list-none p-5 font-medium">{t(lang, "dash.backfillTitle")}</summary><CardContent className="grid gap-3">
    <p className="text-sm text-muted-foreground">{t(lang, "dash.backfillBlurb")}</p>
    <form className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" onSubmit={async event => { event.preventDefault(); if (await onSubmit(fields)) setFields({ activity: "", start: "", end: "", project_id: data.DefaultProject ? String(data.DefaultProject) : "", note: "" }) }}>
      <div><Input id="b-activity" aria-label={t(lang, "dash.activity")} list="known-activities" placeholder={t(lang, "ph.activity")} value={fields.activity} onChange={event => set("activity", event.target.value)} required />{fieldError("activity") && <span className="text-xs text-destructive">{fieldError("activity")}</span>}</div>
      <div><Input id="b-start" ref={startRef} aria-label={t(lang, "dash.startLabel")} aria-invalid={fieldError("start") ? true : undefined} aria-describedby="b-start-error" placeholder={t(lang, "ph.start")} value={fields.start} onChange={event => set("start", event.target.value)} required /><span id="b-start-error" role="alert" className="text-xs text-destructive">{fieldError("start")}</span></div>
      <div><Input id="b-end" aria-label={t(lang, "stats.end")} aria-invalid={fieldError("end") ? true : undefined} placeholder={t(lang, "ph.end")} value={fields.end} onChange={event => set("end", event.target.value)} required /><span id="b-end-error" role="alert" className="text-xs text-destructive">{fieldError("end")}</span></div>
      <Select value={fields.project_id || "none"} onValueChange={value => set("project_id", value === "none" ? "" : value)}><SelectTrigger id="b-project" aria-label={t(lang, "dash.projectLabel")} data-value={fields.project_id || "0"}><SelectValue placeholder={t(lang, "dash.projectLabel")} /></SelectTrigger><SelectContent><SelectItem value="none">{t(lang, "dash.uncategorized")}</SelectItem>{data.Projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent></Select>
      <Input id="b-note" name="note" aria-label={t(lang, "dash.note")} placeholder={t(lang, "dash.noteShort")} value={fields.note} onChange={event => set("note", event.target.value)} />
      <Button type="submit" disabled={busy}>{t(lang, "dash.addSession")}</Button>
    </form>
  </CardContent></details></Card>
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><div className="text-xs text-muted-foreground">{label}</div><div className="truncate font-mono text-base tabular-nums">{value}</div></div>
}

function LiveClock({ session }: { session: Session }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  return <time className="font-mono tabular-nums">{clockLabel(sessionElapsedSeconds(session, now))}</time>
}

if (root && payload) {
  try {
    const initial = JSON.parse(payload.textContent || "{}") as ReactPageBootstrap
    const restoreFocus = document.activeElement instanceof HTMLInputElement && document.activeElement.name === "activity"
    createRoot(root).render(
      <StrictMode>
        <Suspense fallback={<div className="min-h-32 animate-pulse rounded-lg bg-muted" aria-hidden="true" />}>
          {"NewProject" in initial.data && initial.data.NewProject
            ? <ProjectCreate data={initial.data as ProjectCreateData} />
            : "GoalsReact" in initial.data && initial.data.GoalsReact
            ? <GoalsPage data={initial.data as GoalsData} />
            : "ReactTags" in initial.data && initial.data.ReactTags
            ? <TagsPage data={initial.data as TagsData} />
            : "GraphReact" in initial.data && initial.data.GraphReact
            ? <GraphPage data={initial.data as GraphData} />
            : "TimesheetReact" in initial.data && initial.data.TimesheetReact
            ? <TimesheetPage initial={initial.data as TimesheetData} />
            : "Sessions" in initial.data
            ? <ProjectDetail data={initial.data as ProjectDetailData} />
            : "ShowArchived" in initial.data
            ? <ProjectList data={initial.data as ProjectListData} />
            : <DashboardApp initial={initial.data as DashboardData} restoreFocus={restoreFocus} />}
        </Suspense>
      </StrictMode>
    )
  } catch (error) {
    console.error("Could not initialize dashboard", error)
  }
}
