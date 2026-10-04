import * as React from "react"
import { Flag, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import { durationLabel } from "@/dashboard/time"
import type { GoalView, GoalsData } from "@/dashboard/types"

type ProgressItem = { goal: { id: number; activity_id: number; period: string; target_minutes: number }; activity_name: string; achieved_minutes: number; percent_complete: number; period_start: string; period_end: string }

function dateLabel(date: string, lang: string) {
  return new Intl.DateTimeFormat(lang === "ru" ? "ru-RU" : "en-US", { month: "short", day: "numeric" }).format(new Date(date))
}

export function GoalsPage({ data }: { data: GoalsData }) {
  const [goals, setGoals] = React.useState(data.Goals)
  const [period, setPeriod] = React.useState("daily")
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")
  const lang = data.Lang

  async function request(url: string, method: string, body?: URLSearchParams) {
    const response = await fetch(url, { method, credentials: "same-origin", headers: { "X-CSRF-Token": data.CSRFToken, ...(body ? { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" } : {}) }, body })
    if (!response.ok) throw new Error((await response.text()).replace(/<[^>]*>/g, " ").trim() || response.statusText)
  }

  async function refresh() {
    const response = await fetch("/api/goals/progress", { credentials: "same-origin", headers: { Accept: "application/json" } })
    if (!response.ok) throw new Error(response.statusText)
    const payload = await response.json() as { progress: ProgressItem[] }
    const colors = new Map(data.Activities.map(activity => [activity.Name, activity.Color]))
    setGoals(payload.progress.map(item => ({
      ID: item.goal.id, ActivityName: item.activity_name, Color: colors.get(item.activity_name) || "#6b7280", Period: item.goal.period,
      TargetMinutes: item.goal.target_minutes, TargetLabel: durationLabel(item.goal.target_minutes * 60, lang),
      AchievedMinutes: item.achieved_minutes, AchievedLabel: durationLabel(item.achieved_minutes * 60, lang), Percent: item.percent_complete,
      PeriodStartLabel: dateLabel(item.period_start, lang), PeriodEndLabel: dateLabel(item.period_end, lang), PeriodRangeLabel: t(lang, `goals.${item.goal.period}`),
    })))
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formElement = event.currentTarget
    const form = new FormData(formElement)
    const body = new URLSearchParams({ activity: String(form.get("activity") || ""), period, minutes: String(form.get("minutes") || "") })
    setBusy(true); setError("")
    try { await request("/api/goals", "POST", body); await refresh(); formElement.reset() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
    finally { setBusy(false) }
  }

  async function remove(goal: GoalView) {
    if (!window.confirm(t(lang, "goals.confirmDelete"))) return
    setBusy(true); setError("")
    try { const query = new URLSearchParams({ activity: goal.ActivityName, period: goal.Period }); await request(`/api/goals?${query}`, "DELETE"); await refresh() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
    finally { setBusy(false) }
  }

  React.useEffect(() => {
    const timer = window.setInterval(() => void refresh().catch(() => {}), 60_000)
    return () => window.clearInterval(timer)
  }, [])

  return <main className="mx-auto w-full max-w-5xl space-y-5">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "nav.goals")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "goals.blurbFull")}{!data.CanManage && ` ${t(lang, "goals.managersSet")}`}</p></header>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    {data.CanManage && <Card><CardHeader><CardTitle>{t(lang, "goals.new")}</CardTitle></CardHeader><CardContent>
      <form onSubmit={submit} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem_10rem_auto] sm:items-end" aria-busy={busy}>
        <div className="space-y-2"><Label htmlFor="goal-activity">{t(lang, "dash.activity")}</Label><Input id="goal-activity" name="activity" list="goal-activities" placeholder={t(lang, "ph.activity")} required /><datalist id="goal-activities">{data.Activities.map(activity => <option key={activity.ID} value={activity.Name} />)}</datalist></div>
        <div className="space-y-2"><Label htmlFor="goal-period">{t(lang, "goals.period")}</Label><Select value={period} onValueChange={setPeriod}><SelectTrigger id="goal-period"><SelectValue /></SelectTrigger><SelectContent>{(["daily", "weekly", "monthly"] as const).map(item => <SelectItem key={item} value={item}>{t(lang, `goals.${item}`)}</SelectItem>)}</SelectContent></Select></div>
        <div className="space-y-2"><Label htmlFor="goal-minutes">{t(lang, "goals.minutes")}</Label><Input id="goal-minutes" name="minutes" type="number" min="1" max="10000" required placeholder="120" /></div>
        <Button type="submit" disabled={busy}><Flag aria-hidden="true" />{t(lang, "goals.set")}</Button>
      </form>
    </CardContent></Card>}
    {goals.length ? <Card><CardHeader><CardTitle>{t(lang, "goals.current")}</CardTitle></CardHeader><CardContent className="grid gap-5">
      {goals.map(goal => <div key={`${goal.ActivityName}:${goal.Period}`} className="grid gap-2">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-sm">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2"><strong className="inline-flex items-center gap-2"><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: goal.Color }} />{goal.ActivityName}</strong><span className="text-muted-foreground">{goal.PeriodRangeLabel} ({goal.PeriodStartLabel} – {goal.PeriodEndLabel})</span></div>
          <div className="flex items-center gap-2"><span className="whitespace-nowrap font-mono tabular-nums">{goal.AchievedLabel}<span className="text-muted-foreground"> / {goal.TargetLabel}</span></span>{data.CanManage && <Button variant="ghost" size="icon" disabled={busy} title={t(lang, "goals.delete")} aria-label={t(lang, "goals.delete")} onClick={() => void remove(goal)}><Trash2 aria-hidden="true" /></Button>}</div>
        </div>
        <Progress value={Math.min(goal.Percent, 100)} aria-label={`${goal.ActivityName}: ${goal.AchievedLabel} / ${goal.TargetLabel}`} />
      </div>)}
    </CardContent></Card> : <div className="py-8 text-center text-sm text-muted-foreground"><p className="font-medium">{t(lang, "goals.noGoals")}</p><p>{data.CanManage ? t(lang, "goals.noGoalsHint") : t(lang, "goals.noGoalsMember")}</p></div>}
  </main>
}
