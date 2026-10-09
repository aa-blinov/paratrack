import * as React from "react"
import { Bell, BellOff, Send } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { NotificationsData } from "@/dashboard/types"

type TopicsAnswer = { muted?: string[]; outcome?: string }

function vapidBytes(key: string): Uint8Array {
  const padded = key + "=".repeat((4 - key.length % 4) % 4)
  const binary = atob(padded.replace(/-/g, "+").replace(/_/g, "/"))
  return Uint8Array.from(binary, character => character.charCodeAt(0))
}

export function NotificationsPage({ data }: { data: NotificationsData }) {
  const lang = data.Lang || "en"
  const [active, setActive] = React.useState(false)
  const [checking, setChecking] = React.useState(true)
  const [busy, setBusy] = React.useState(false)
  const [status, setStatus] = React.useState("")
  // The server owns the selection: the checkboxes only move once it confirms.
  const [muted, setMuted] = React.useState<string[]>(() => (data.Topics || []).filter(topic => topic.Muted).map(topic => topic.Key))

  const post = React.useCallback(async (path: string, body: URLSearchParams): Promise<TopicsAnswer> => {
    const response = await fetch(path, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": data.CSRFToken }, body })
    if (!response.ok) throw new Error(`${path} answered ${response.status}`)
    return await response.json() as TopicsAnswer
  }, [data.CSRFToken])

  const refresh = React.useCallback(async () => {
    try {
      if (!("serviceWorker" in navigator) || !("PushManager" in window)) throw new Error("push unavailable")
      const registration = await navigator.serviceWorker.ready
      const subscription = await registration.pushManager.getSubscription()
      setActive(Boolean(subscription))
      if (subscription) setStatus(t(lang, "push.active"))
    } catch {
      setActive(false)
    } finally { setChecking(false) }
  }, [lang])

  React.useEffect(() => { void refresh() }, [refresh])

  async function enable() {
    setBusy(true)
    try {
      if (!("Notification" in window) || !("serviceWorker" in navigator) || !("PushManager" in window)) throw new Error("push unavailable")
      const permission = await Notification.requestPermission()
      if (permission !== "granted") { setStatus(t(lang, "push.denied")); return }
      const registration = await navigator.serviceWorker.ready
      const keyResponse = await fetch("/api/push/key", { credentials: "same-origin" })
      if (!keyResponse.ok) throw new Error("could not fetch VAPID key")
      const { publicKey } = await keyResponse.json() as { publicKey: string }
      const subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: vapidBytes(publicKey) as BufferSource })
      const json = subscription.toJSON()
      const body = new URLSearchParams({ csrf_token: data.CSRFToken, endpoint: json.endpoint || "", p256dh: json.keys?.p256dh || "", auth: json.keys?.auth || "" })
      const response = await fetch("/api/push/subscribe", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": data.CSRFToken }, body })
      if (!response.ok) throw new Error("could not save push subscription")
      setStatus(t(lang, "push.active"))
      await refresh()
    } catch {
      setStatus(t(lang, "push.failed"))
    } finally { setBusy(false) }
  }

  async function disable() {
    setBusy(true)
    try {
      const registration = await navigator.serviceWorker.ready
      const subscription = await registration.pushManager.getSubscription()
      if (subscription) {
        const body = new URLSearchParams({ csrf_token: data.CSRFToken, endpoint: subscription.endpoint })
        const response = await fetch("/api/push/unsubscribe", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": data.CSRFToken }, body })
        if (!response.ok) throw new Error("could not remove push subscription")
        await subscription.unsubscribe()
      }
      await refresh()
      setStatus(t(lang, "push.off"))
    } catch {
      setStatus(t(lang, "push.failed"))
    } finally { setBusy(false) }
  }

  // The whole selection travels with every save, so clearing the last box
  // means "every event again" instead of leaving a stale partial answer.
  async function toggleTopic(key: string, wantsNotifications: boolean) {
    setBusy(true)
    try {
      const next = wantsNotifications ? muted.filter(topic => topic !== key) : [...muted, key]
      const body = new URLSearchParams({ csrf_token: data.CSRFToken })
      for (const topic of next) body.append("topic", topic)
      const answer = await post("/api/push/topics", body)
      setMuted(Array.isArray(answer.muted) ? answer.muted : next)
      setStatus(t(lang, "toast.saved"))
    } catch {
      setStatus(t(lang, "push.topicsFailed"))
    } finally { setBusy(false) }
  }

  // The check names what the channel did instead of reporting a success it
  // cannot know: permission and this device's subscription are browser facts,
  // and the server answers whether a device took the message.
  async function sendTest() {
    setBusy(true)
    try {
      if (!("Notification" in window) || !("serviceWorker" in navigator) || !("PushManager" in window)) { setStatus(t(lang, "push.testUnsupported")); return }
      if (Notification.permission !== "granted") { setStatus(t(lang, "push.testNoPermission")); return }
      const registration = await navigator.serviceWorker.ready
      if (!(await registration.pushManager.getSubscription())) { setStatus(t(lang, "push.testNoSubscription")); return }
      const answer = await post("/api/push/test", new URLSearchParams({ csrf_token: data.CSRFToken }))
      setStatus(t(lang, answer.outcome === "delivered" ? "push.testSent" : answer.outcome === "no_channel" ? "push.testNoDevices" : "push.testFailed"))
    } catch {
      setStatus(t(lang, "push.testFailed"))
    } finally { setBusy(false) }
  }

  // Invoices and pay runs are owner territory: a member gets a 403 on both, so
  // offering those events would promise notifications that never arrive.
  const managerOnly = ["payroll.paid", "invoice.paid", "invoice.payment_link"]
  const topics = (data.Topics || []).filter(topic => data.CanManage || !managerOnly.includes(topic.Key))

  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    <Card><CardHeader><CardTitle>{t(lang, "push.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "push.blurb")}</p></CardHeader><CardContent className="grid gap-3">
      <div className="flex flex-wrap items-center gap-2"><Button type="button" onClick={() => void enable()} disabled={busy || checking || active}><Bell aria-hidden="true" />{t(lang, "push.enable")}</Button>{active && <Button type="button" variant="outline" onClick={() => void disable()} disabled={busy}><BellOff aria-hidden="true" />{t(lang, "push.disable")}</Button>}</div>
      <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-xs text-muted-foreground">{t(lang, "push.devices")}: <strong>{data.DeviceCount}</strong></p><Button type="button" variant="outline" size="sm" onClick={() => void sendTest()} disabled={busy} data-notification-test><Send aria-hidden="true" />{t(lang, "push.test")}</Button></div>
      {data.DeviceCount === 0 && <p className="text-xs text-muted-foreground">{t(lang, "push.emptyHint")}</p>}
      <p className="min-h-4 text-xs text-muted-foreground" role="status" aria-live="polite" data-notification-status>{status}</p>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>{t(lang, "push.events")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "push.topicsHint")}</p></CardHeader><CardContent>
      <ul className="grid gap-2">{topics.map(topic => <li key={topic.Key}><label className="flex cursor-pointer items-center gap-3 text-sm"><Checkbox checked={!muted.includes(topic.Key)} disabled={busy} onCheckedChange={checked => void toggleTopic(topic.Key, checked === true)} aria-label={t(lang, topic.Label)} /><span>{t(lang, topic.Label)}</span></label></li>)}
        {data.CanManage && <li className="flex items-baseline gap-2 text-sm text-muted-foreground"><span>{t(lang, "push.ev2")}</span><span className="text-xs">{t(lang, "push.ev2hint")}</span></li>}
      </ul>
    </CardContent></Card>
  </main>
}