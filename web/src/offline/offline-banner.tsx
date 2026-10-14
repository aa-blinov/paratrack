import { useSyncExternalStore } from "react"
import { Button } from "@/components/ui/button"
import { translate as t } from "@/i18n"
import { discardFirstQueued, getOfflineState, retryQueue, subscribeOffline } from "./controller"

// The strip above the page: offline, waiting to send, sending, or a refused
// action that needs a decision. Hidden when there is nothing to say.
export function OfflineBanner({ lang }: { lang: string }) {
  const state = useSyncExternalStore(subscribeOffline, getOfflineState, getOfflineState)
  const message = (key: string, n?: number) => t(lang, key).replace("{n}", String(n ?? ""))

  let text: string | null = null
  let actions = false
  if (state.hint === "iosInstall") {
    text = t(lang, "pwa.iosInstall")
  } else if (state.blockedStatus !== null && state.queueLength > 0) {
    text = state.blockedStatus === 0
      ? t(lang, "pwa.queueNetworkFailed")
      : message("pwa.queueFailed", state.blockedStatus)
    actions = true
  } else if (!state.online) {
    text = state.queueLength ? message("pwa.offlinePending", state.queueLength) : t(lang, "pwa.offline")
  } else if (state.queueLength) {
    text = message("pwa.syncing", state.queueLength)
  }
  if (!text) return null

  return <div role="status" aria-live="polite" data-offline-banner className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-muted px-4 py-2 text-sm">
    <span>{text}</span>
    {actions && <div className="flex gap-2">
      <Button type="button" size="sm" variant="outline" onClick={retryQueue}>{t(lang, "pwa.queueRetry")}</Button>
      <Button type="button" size="sm" variant="ghost" onClick={discardFirstQueued}>{t(lang, "pwa.queueDiscard")}</Button>
    </div>}
  </div>
}
