import { useEffect, useState } from "react"
import { Pause, Play, Square } from "lucide-react"
import { Button } from "@/components/ui/button"
import { translate as t } from "@/i18n"
import { scheduleBadge, subscribeOffline } from "./controller"
import { ColorMark } from "@/components/color-mark"
import { clockLabel, sessionElapsedSeconds } from "@/dashboard/time"

type RunningItem = {
  ID: number
  ActivityName: string
  ProjectName: string
  Color: string
  ProjectColor: string
  Paused: boolean
  Clock: string
  ResumeISO: string
  AccumulatedSeconds: number
}

const csrf = () => {
  const match = document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/)
  return match ? decodeURIComponent(match[1]) : ""
}

// The phone's "now tracking" bar, above the tab bar on every page but the
// dashboard. It shows the running timer and lets its pause and stop be pressed;
// the others are counted and lead to the dashboard, which lists them all.
export function MiniBar({ lang }: { lang: string }) {
  const [items, setItems] = useState<RunningItem[]>([])
  const [, setTick] = useState(0)
  useEffect(() => subscribeOffline(() => setTick(value => value + 1)), [])
  // The clock ticks here between loads; it used to stand still for 30 s.
  const ticking = items.some(item => !item.Paused)
  useEffect(() => {
    if (!ticking) return
    const id = window.setInterval(() => setTick(value => value + 1), 1000)
    return () => window.clearInterval(id)
  }, [ticking])

  async function load() {
    try {
      const response = await fetch("/api/active", { credentials: "same-origin", headers: { Accept: "application/json" } })
      if (!response.ok) return
      const payload = await response.json() as { Items?: RunningItem[] }
      // Running timers first: the bar shows the one that is actually ticking.
      setItems([...(payload.Items ?? [])].sort((a, b) => Number(a.Paused) - Number(b.Paused)))
    } catch {
      // offline: keep what was shown
    }
  }

  useEffect(() => {
    void load()
    // A hidden tab has no bar to keep fresh; it reloads when shown again.
    const timer = window.setInterval(() => { if (!document.hidden) void load() }, 30000)
    const shown = () => { if (!document.hidden) void load() }
    document.addEventListener("visibilitychange", shown)
    const synced = () => { void load() }
    window.addEventListener("paratrack:synced", synced)
    return () => { window.clearInterval(timer); window.removeEventListener("paratrack:synced", synced); document.removeEventListener("visibilitychange", shown) }
  }, [])

  async function act(id: number, action: "pause" | "resume" | "stop") {
    const response = await fetch(`/api/sessions/${id}/${action}`, {
      method: "POST", credentials: "same-origin", headers: { "X-CSRF-Token": csrf() },
    })
    if (response.ok || response.headers.get("X-Offline-Queued")) {
      scheduleBadge()
      await load()
    }
  }

  const current = items[0]
  if (!current) return null
  const others = items.length - 1
  const name = current.ActivityName

  return <aside data-minibar aria-label={t(lang, "dash.activeSessions")} className="fixed inset-x-0 bottom-20 z-20 border-t border-border bg-background px-3 py-2 lg:hidden">
    <div className="flex items-center gap-2">
      <a href="/" className="flex min-h-12 min-w-0 flex-1 items-center gap-2 self-stretch">
        <ColorMark color={current.Color || current.ProjectColor} />
        <span className="min-w-0 flex-1 truncate font-medium">{name}</span>
        {/* На 360px проект отнимал у названия активности половину места: «Поддержк…». */}
        {current.ProjectName && <span className="hidden min-w-0 max-w-[35%] truncate text-xs text-muted-foreground sm:inline">{current.ProjectName}</span>}
        <span className="shrink-0 font-mono text-sm tabular-nums">{clockLabel(sessionElapsedSeconds(current, Date.now()))}</span>
      </a>
      {others > 0 && <a href="/" data-minibar-more className="inline-flex h-12 min-w-12 shrink-0 items-center justify-center rounded-full px-2 font-mono text-sm tabular-nums text-muted-foreground hover:bg-muted" aria-label={t(lang, "minibar.more", others)}>+{others}</a>}
      {current.Paused
        ? <Button type="button" size="icon" variant="ghost" className="size-12" title={t(lang, "minibar.resume", name)} aria-label={t(lang, "minibar.resume", name)} onClick={() => void act(current.ID, "resume")}><Play aria-hidden="true" /></Button>
        : <Button type="button" size="icon" variant="ghost" className="size-12" title={t(lang, "minibar.pause", name)} aria-label={t(lang, "minibar.pause", name)} onClick={() => void act(current.ID, "pause")}><Pause aria-hidden="true" /></Button>}
      <Button type="button" size="icon" variant="ghost" className="size-12" title={t(lang, "minibar.stop", name)} aria-label={t(lang, "minibar.stop", name)} onClick={() => void act(current.ID, "stop")}><Square aria-hidden="true" /></Button>
    </div>
  </aside>
}
