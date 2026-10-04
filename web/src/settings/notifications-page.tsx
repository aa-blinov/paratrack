import * as React from "react"
import { Bell, BellOff } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import { SettingsNav } from "@/settings/settings-nav"
import type { NotificationsData } from "@/dashboard/types"

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

  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "set.title")}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    <Card><CardHeader><CardTitle>{t(lang, "push.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "push.blurb")}</p></CardHeader><CardContent className="grid gap-3">
      <div className="flex flex-wrap items-center gap-2"><Button type="button" onClick={() => void enable()} disabled={busy || checking || active}><Bell aria-hidden="true" />{t(lang, "push.enable")}</Button>{active && <Button type="button" variant="outline" onClick={() => void disable()} disabled={busy}><BellOff aria-hidden="true" />{t(lang, "push.disable")}</Button>}</div>
      <p className="text-xs text-muted-foreground">{t(lang, "push.devices")}: <strong>{data.DeviceCount}</strong></p>
      {data.DeviceCount === 0 && <p className="text-xs text-muted-foreground">{t(lang, "push.emptyHint")}</p>}
      <p className="min-h-4 text-xs text-muted-foreground" role="status" aria-live="polite">{status}</p>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>{t(lang, "push.events")}</CardTitle></CardHeader><CardContent><ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">{["push.ev1", "push.ev2", "push.ev3", "push.ev4"].map(key => <li key={key}>{t(lang, key)}</li>)}</ul></CardContent></Card>
  </main>
}
