import { lazy, StrictMode, Suspense, startTransition, useEffect, useMemo, useRef, useState } from "react"
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
import { ApplicationShell } from "@/shell/app-shell"
import { clockLabel, sessionElapsedSeconds } from "@/dashboard/time"
import type { AuditData, AuthPageData, DashboardData, ExportData, GoalsData, GraphData, HelpData, ImportData, IntegrationDetailData, IntegrationsData, InviteAcceptData, InvoiceDetailData, InvoicesData, MarketplaceData, NotificationsData, PayrollData, PayrollDetailData, PreferencesData, ProfileData, ProjectCreateData, ProjectDetailData, ProjectListData, ReactPageBootstrap, ReportRunData, ReportsData, ScheduleData, SectionsData, Session, StatsData, TagsData, TeamInvitesData, TeamMembersData, TeamSettingsData, TimesheetData, TokensData, WebhooksData } from "@/dashboard/types"

const root = document.getElementById("paratrack-react-root")
const payload = document.getElementById("react-page-data")
const ProjectList = lazy(() => import("@/projects/project-list").then(module => ({ default: module.ProjectList })))
const ProjectDetail = lazy(() => import("@/projects/project-detail").then(module => ({ default: module.ProjectDetail })))
const ProjectCreate = lazy(() => import("@/projects/project-create").then(module => ({ default: module.ProjectCreate })))
const GoalsPage = lazy(() => import("@/goals/goals-page").then(module => ({ default: module.GoalsPage })))
const TagsPage = lazy(() => import("@/tags/tags-page").then(module => ({ default: module.TagsPage })))
const GraphPage = lazy(() => import("@/graph/graph-page").then(module => ({ default: module.GraphPage })))
const TimesheetPage = lazy(() => import("@/timesheet/timesheet-page").then(module => ({ default: module.TimesheetPage })))
const Payroll = lazy(() => import("@/payroll/payroll-page").then(module => ({ default: module.PayrollPage })))
const PayrollDetail = lazy(() => import("@/payroll/payroll-page").then(module => ({ default: module.PayrollDetail })))
const SchedulePage = lazy(() => import("@/schedule/schedule-page").then(module => ({ default: module.SchedulePage })))
const InvoicesPage = lazy(() => import("@/invoices/invoices-page").then(module => ({ default: module.InvoicesPage })))
const InvoiceDetailPage = lazy(() => import("@/invoices/invoices-page").then(module => ({ default: module.InvoiceDetailPage })))
const InvoiceActPage = lazy(() => import("@/invoices/invoices-page").then(module => ({ default: module.InvoiceActPage })))
const StatsPage = lazy(() => import("@/stats/stats-page").then(module => ({ default: module.StatsPage })))
const ReportsPage = lazy(() => import("@/reports/reports-page").then(module => ({ default: module.ReportsPage })))
const ReportRunPage = lazy(() => import("@/reports/reports-page").then(module => ({ default: module.ReportRunPage })))
const ExportPage = lazy(() => import("@/export/export-page").then(module => ({ default: module.ExportPage })))
const IntegrationsPage = lazy(() => import("@/integrations/integrations-page").then(module => ({ default: module.IntegrationsPage })))
const IntegrationDetailPage = lazy(() => import("@/integrations/integrations-page").then(module => ({ default: module.IntegrationDetailPage })))
const MarketplacePage = lazy(() => import("@/integrations/integrations-page").then(module => ({ default: module.MarketplacePage })))
const TokensPage = lazy(() => import("@/settings/tokens-page").then(module => ({ default: module.TokensPage })))
const ProfilePage = lazy(() => import("@/settings/profile-page").then(module => ({ default: module.ProfilePage })))
const PreferencesPage = lazy(() => import("@/settings/preferences-page").then(module => ({ default: module.PreferencesPage })))
const NotificationsPage = lazy(() => import("@/settings/notifications-page").then(module => ({ default: module.NotificationsPage })))
const TeamSettingsPage = lazy(() => import("@/settings/team-settings-page").then(module => ({ default: module.TeamSettingsPage })))
const TeamMembersPage = lazy(() => import("@/settings/team-members-page").then(module => ({ default: module.TeamMembersPage })))
const TeamInvitesPage = lazy(() => import("@/settings/team-invites-page").then(module => ({ default: module.TeamInvitesPage })))
const SectionsPage = lazy(() => import("@/settings/sections-page").then(module => ({ default: module.SectionsPage })))
const WebhooksPage = lazy(() => import("@/settings/webhooks-page").then(module => ({ default: module.WebhooksPage })))
const AuditPage = lazy(() => import("@/settings/audit-page").then(module => ({ default: module.AuditPage })))
const HelpPage = lazy(() => import("@/help/help-page").then(module => ({ default: module.HelpPage })))
const ImportPage = lazy(() => import("@/import/import-page").then(module => ({ default: module.ImportPage })))
const InviteAcceptPage = lazy(() => import("@/settings/invite-accept-page").then(module => ({ default: module.InviteAcceptPage })))
const AuthPage = lazy(() => import("@/auth/auth-page").then(module => ({ default: module.AuthPage })))

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

  useEffect(() => {
    const pauseAll = () => { void mutate("/api/active/pause-all") }
    window.addEventListener("paratrack:pause-all", pauseAll)
    return () => window.removeEventListener("paratrack:pause-all", pauseAll)
  }, [data.CSRFToken])

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
            <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: session.Color }} /><strong className="ledger-activity min-w-16 truncate">{session.ActivityName}</strong>{projects.length > 0 ? <Select value={String(session.ProjectID || 0)} onValueChange={value => void mutate(`/api/activities/${session.ActivityID}/project`, { project_id: value })}><SelectTrigger aria-label={labels.project} className="ledger-project h-7 w-fit max-w-40 border-0 px-2 text-xs" data-value={session.ProjectID || 0}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="0">{labels.uncategorized}</SelectItem>{projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent></Select> : session.ProjectName && <Badge variant="outline">{session.ProjectName}</Badge>}</div><div className="mt-1 flex gap-3 text-xs text-muted-foreground"><span className={`status-pill ${session.Paused ? "is-paused" : "is-active"}`} data-state={session.Paused ? "paused" : "running"}>{session.Paused ? t(lang, "dash.statusPaused") : t(lang, "dash.statusActive")}</span><span>{session.StartLocal}</span><LiveClock session={session} /></div></div>
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
    {data.Widgets?.recent && recent.length > 0 && <Card><CardHeader className="flex-row items-center justify-between space-y-0"><CardTitle>{labels.recent}</CardTitle><a className="text-sm underline-offset-4 hover:underline" href="/stats">{labels.viewAll}</a></CardHeader><CardContent>
      <div className="hidden overflow-x-auto sm:block"><table className="w-full text-left text-sm"><thead><tr className="border-b text-muted-foreground"><th className="p-2">{t(lang, "dash.activity")}</th><th className="p-2">{t(lang, "dash.startLabel")}</th><th className="p-2">{t(lang, "stats.end")}</th><th className="p-2">{labels.duration}</th><th className="p-2">{labels.note}</th><th className="p-2">{t(lang, "stats.tags")}</th><th className="p-2" /></tr></thead><tbody>{recent.map(session => <tr key={session.ID} className="border-b last:border-0"><td className="p-2"><span className="mr-2 inline-block h-2 w-2 rounded-full" style={{ backgroundColor: session.Color }} /><span className="[overflow-wrap:anywhere]">{session.ActivityName}</span>{session.ProjectName && <div><Badge variant="outline" className="max-w-full whitespace-normal [overflow-wrap:anywhere]">{session.ProjectName}</Badge></div>}</td><td className="whitespace-nowrap p-2 font-mono">{session.StartLocal}</td><td className="whitespace-nowrap p-2 font-mono">{session.EndLocal}</td><td className="whitespace-nowrap p-2 font-mono">{session.Duration}</td><td className="p-2 [overflow-wrap:anywhere]">{session.Note}</td><td className="p-2">{session.Tags?.map(tag => <a key={tag.ID} className="mr-1 inline-block [overflow-wrap:anywhere]" href={`/stats?tag=${encodeURIComponent(tag.Name)}`}>#{tag.Name}</a>)}</td><td className="p-2"><Button variant="ghost" size="sm" onClick={() => void mutate("/api/start", { activity: session.ActivityName })}>{t(lang, "dash.again")}</Button></td></tr>)}</tbody></table></div>
      <div data-recent-sessions-mobile className="grid gap-2 sm:hidden">{recent.map(session => <article key={session.ID} className="grid min-w-0 gap-2 rounded-md border p-3 text-sm"><div className="flex min-w-0 items-start gap-2"><span aria-hidden="true" className="mt-1.5 size-2 shrink-0 rounded-full" style={{ backgroundColor: session.Color }} /><div className="min-w-0"><strong className="break-all">{session.ActivityName}</strong>{session.ProjectName && <div className="mt-1"><Badge variant="outline" className="h-auto max-w-full whitespace-normal break-all">{session.ProjectName}</Badge></div>}</div></div><div className="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground"><span>{session.StartLocal}</span><span aria-hidden="true">→</span><span>{session.EndLocal}</span><span className="font-mono text-foreground">{session.Duration}</span></div>{session.Note && <p className="break-words text-muted-foreground">{session.Note}</p>}{session.Tags?.length > 0 && <div className="flex flex-wrap gap-2">{session.Tags.map(tag => <a key={tag.ID} className="text-primary underline-offset-4 hover:underline" href={`/stats?tag=${encodeURIComponent(tag.Name)}`}>#{tag.Name}</a>)}</div>}<Button className="w-fit" variant="outline" size="sm" onClick={() => void mutate("/api/start", { activity: session.ActivityName })}>{t(lang, "dash.again")}</Button></article>)}</div>
    </CardContent></Card>}
    {data.Widgets?.unbilled && data.Mods?.invoices && data.Unbilled?.length > 0 && data.CanManage && <Card><CardHeader><CardTitle>{t(lang, "inv.unbilled")}</CardTitle></CardHeader><CardContent className="grid gap-2">{data.Unbilled.map(item => <div key={item.ProjectID} className="flex justify-between gap-3 text-sm"><a className="underline-offset-4 hover:underline" href={`/projects/${item.Slug}`}>{item.ProjectName}</a><span className="font-mono">{item.Hours}, {item.Amount}</span></div>)}</CardContent></Card>}
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

function PageScreen({ bootstrap, restoreFocus, routeKey }: { bootstrap: ReactPageBootstrap; restoreFocus: boolean; routeKey: string }) {
  const data = bootstrap.data
  return (
    <Suspense fallback={<div className="min-h-32 rounded-lg bg-muted" aria-hidden="true" />}>
      <div key={routeKey}>
          {"NewProject" in data && data.NewProject
            ? <ProjectCreate data={data as ProjectCreateData} />
            : "GoalsReact" in data && data.GoalsReact
            ? <GoalsPage data={data as GoalsData} />
            : "ReactTags" in data && data.ReactTags
            ? <TagsPage data={data as TagsData} />
            : "GraphReact" in data && data.GraphReact
            ? <GraphPage data={data as GraphData} />
            : "TimesheetReact" in data && data.TimesheetReact
            ? <TimesheetPage initial={data as TimesheetData} />
            : "PayrollReact" in data && data.PayrollReact
            ? "PayrollDetail" in data && data.PayrollDetail
              ? <PayrollDetail data={data as PayrollDetailData} />
              : <Payroll data={data as PayrollData} />
            : "ScheduleReact" in data && data.ScheduleReact
            ? <SchedulePage initial={data as ScheduleData} />
            : "InvoicesReact" in data && data.InvoicesReact
            ? <InvoicesPage data={data as InvoicesData} />
            : (("InvoiceReact" in data && data.InvoiceReact) || ("InvoiceActReact" in data && data.InvoiceActReact))
            ? "InvoiceActReact" in data && data.InvoiceActReact
              ? <InvoiceActPage data={data as InvoiceDetailData} />
              : <InvoiceDetailPage data={data as InvoiceDetailData} />
            : "ByProject" in data && "Sessions" in data && data.Active === "stats"
            ? <StatsPage data={data as StatsData} />
            : "ReportRunReact" in data && data.ReportRunReact
            ? <ReportRunPage data={data as ReportRunData} />
            : "ReportsReact" in data && data.ReportsReact
            ? <ReportsPage data={data as ReportsData} />
            : "ReportsEnabled" in data
            ? <ExportPage data={data as ExportData} />
            : "MarketReact" in data && data.MarketReact
            ? <MarketplacePage data={data as MarketplaceData} />
            : "IntegrationReact" in data && data.IntegrationReact
            ? <IntegrationDetailPage data={data as IntegrationDetailData} />
            : "IntegrationsReact" in data && data.IntegrationsReact
            ? <IntegrationsPage data={data as IntegrationsData} />
            : "TokensReact" in data && data.TokensReact
            ? <TokensPage data={data as TokensData} />
            : "ProfileReact" in data && data.ProfileReact
            ? <ProfilePage data={data as ProfileData} />
            : "PrefsReact" in data && data.PrefsReact
            ? <PreferencesPage data={data as PreferencesData} />
            : "NotificationsReact" in data && data.NotificationsReact
            ? <NotificationsPage data={data as NotificationsData} />
            : "TeamSettingsReact" in data && data.TeamSettingsReact
            ? <TeamSettingsPage data={data as TeamSettingsData} />
            : "MembersReact" in data && data.MembersReact
            ? <TeamMembersPage data={data as TeamMembersData} />
            : "InvitesReact" in data && data.InvitesReact
            ? <TeamInvitesPage data={data as TeamInvitesData} />
            : "SectionsReact" in data && data.SectionsReact
            ? <SectionsPage data={data as SectionsData} />
            : "WebhooksReact" in data && data.WebhooksReact
            ? <WebhooksPage data={data as WebhooksData} />
            : "AuditReact" in data && data.AuditReact
            ? <AuditPage data={data as AuditData} />
            : "HelpReact" in data && data.HelpReact
            ? <HelpPage data={data as HelpData} />
            : "ImportReact" in data && data.ImportReact
            ? <ImportPage data={data as ImportData} />
            : "InviteReact" in data && data.InviteReact
            ? <InviteAcceptPage data={data as InviteAcceptData} />
            : "AuthReact" in data && data.AuthReact
            ? <AuthPage data={data as AuthPageData} />
            : "Sessions" in data
            ? <ProjectDetail data={data as ProjectDetailData} />
            : "ShowArchived" in data
            ? <ProjectList data={data as ProjectListData} />
            : <DashboardApp initial={data as DashboardData} restoreFocus={restoreFocus} />}
      </div>
    </Suspense>
  )
}

function parseBootstrap(html: string): { bootstrap: ReactPageBootstrap; title: string } | null {
  const document = new DOMParser().parseFromString(html, "text/html")
  const payload = document.getElementById("react-page-data")
  if (!payload) return null
  try {
    const bootstrap = JSON.parse(payload.textContent || "") as ReactPageBootstrap
    if (!bootstrap.data || !bootstrap.shell) return null
    return { bootstrap, title: document.title }
  } catch {
    return null
  }
}

function AppRouter({ initial }: { initial: ReactPageBootstrap }) {
  const [route, setRoute] = useState(() => ({
    bootstrap: initial,
    key: location.pathname + location.search,
    restoreFocus: document.activeElement instanceof HTMLInputElement && document.activeElement.name === "activity",
    focusMain: false,
  }))
  const request = useRef<AbortController | null>(null)
  const scrollPositions = useRef(new Map<string, number>())

  useEffect(() => {
    history.replaceState({ ...history.state, paratrack: true }, "", location.href)
    const navigate = async (url: URL, mode: "push" | "pop", restoreFocus = false) => {
      request.current?.abort()
      const controller = new AbortController()
      request.current = controller
      if (mode === "push") {
        const currentKey = location.pathname + location.search
        scrollPositions.current.set(currentKey, window.scrollY)
      }
      try {
        const response = await fetch(url, { credentials: "same-origin", signal: controller.signal, headers: { Accept: "text/html" } })
        if (!response.ok || new URL(response.url).origin !== location.origin) throw new Error("Navigation response unavailable")
        const result = parseBootstrap(await response.text())
        if (!result) throw new Error("Navigation did not return a React page")
        const finalURL = new URL(response.url)
        if (url.hash) finalURL.hash = url.hash
        if (mode === "push") history.pushState({ paratrack: true }, "", finalURL.href)
        document.title = result.title
        startTransition(() => setRoute({ bootstrap: result.bootstrap, key: finalURL.pathname + finalURL.search, restoreFocus, focusMain: true }))
        requestAnimationFrame(() => {
          if (mode === "pop") window.scrollTo(0, scrollPositions.current.get(finalURL.pathname + finalURL.search) || 0)
          else if (finalURL.hash) document.getElementById(decodeURIComponent(finalURL.hash.slice(1)))?.scrollIntoView()
          else window.scrollTo(0, 0)
        })
      } catch (error) {
        if (controller.signal.aborted) return
        location.assign(url.href)
      }
    }
    const click = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
      const target = event.target
      if (!(target instanceof Element)) return
      const anchor = target.closest("a[href]")
      if (!(anchor instanceof HTMLAnchorElement)) return
      const inReactRoot = root?.contains(anchor) === true
      const inMobileSidebar = anchor.closest('[data-sidebar="sidebar"]') !== null
      if (!inReactRoot && !inMobileSidebar) return
      if (anchor.target && anchor.target !== "_self" || anchor.hasAttribute("download") || anchor.relList.contains("external")) return
      const url = new URL(anchor.href, location.href)
      if (url.origin !== location.origin || !["http:", "https:"].includes(url.protocol)) return
      if (url.pathname === location.pathname && url.search === location.search) return
      event.preventDefault()
      void navigate(url, "push")
    }
    const submit = (event: SubmitEvent) => {
      if (event.defaultPrevented) return
      const form = event.target
      if (!(form instanceof HTMLFormElement) || !root?.contains(form)) return
      const submitter = event.submitter instanceof HTMLButtonElement || event.submitter instanceof HTMLInputElement ? event.submitter : null
      const method = (submitter?.getAttribute("formmethod") || form.method || "get").toLowerCase()
      const target = submitter?.getAttribute("formtarget") || form.target
      if (method !== "get" || target && target !== "_self") return
      const url = new URL(submitter?.getAttribute("formaction") || form.action || location.href, location.href)
      if (url.origin !== location.origin || !["http:", "https:"].includes(url.protocol)) return
      if (url.pathname.startsWith("/api/")) return
      const params = new URLSearchParams()
      for (const [key, value] of new FormData(form, submitter || undefined)) {
        if (typeof value === "string") params.append(key, value)
        else return
      }
      url.search = params.toString()
      event.preventDefault()
      void navigate(url, "push")
    }
    const pop = () => { void navigate(new URL(location.href), "pop") }
    const customNavigate = (event: Event) => {
      const navigation = event as CustomEvent<{ url?: string; focusActivity?: boolean }>
      if (!navigation.detail?.url) return
      event.preventDefault()
      const url = new URL(navigation.detail.url, location.href)
      if (url.origin === location.origin) void navigate(url, "push", navigation.detail.focusActivity === true)
    }
    document.addEventListener("click", click)
    root?.addEventListener("submit", submit)
    window.addEventListener("popstate", pop)
    window.addEventListener("paratrack:navigate", customNavigate)
    return () => {
      request.current?.abort()
      document.removeEventListener("click", click)
      root?.removeEventListener("submit", submit)
      window.removeEventListener("popstate", pop)
      window.removeEventListener("paratrack:navigate", customNavigate)
    }
  }, [])

  useEffect(() => {
    if (route.focusMain && !route.restoreFocus) document.getElementById("main")?.focus({ preventScroll: true })
  }, [route.focusMain, route.key, route.restoreFocus])

  return <ApplicationShell shell={route.bootstrap.shell}><PageScreen bootstrap={route.bootstrap} restoreFocus={route.restoreFocus} routeKey={route.key} /></ApplicationShell>
}

if (root && payload) {
  try {
    const initial = JSON.parse(payload.textContent || "{}") as ReactPageBootstrap
    createRoot(root).render(<StrictMode><AppRouter initial={initial} /></StrictMode>)
  } catch (error) {
    console.error("Could not initialize dashboard", error)
  }
}
