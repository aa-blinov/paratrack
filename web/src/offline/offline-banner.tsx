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
  } else if (state.failedCount > 0) {
    // The round went on past the refusals: what applied, what did not, and
    // the server's own words for the refusal — a bare code says nothing.
    const parts: string[] = []
    if (state.appliedCount > 0) parts.push(message("pwa.queueApplied", state.appliedCount))
    parts.push(message("pwa.queueFailedSome", state.failedCount))
    if (state.lastError) parts.push(t(lang, "pwa.queueLastError", state.lastError))
    text = parts.join(". ")
    actions = true
  } else if (!state.online) {
    text = state.queueLength ? message("pwa.offlinePending", state.queueLength) : t(lang, "pwa.offline")
  } else if (state.blockedStatus !== null && state.queueLength > 0) {
    text = t(lang, "pwa.queueNetworkFailed")
    actions = true
  } else if (state.queueLength) {
    text = message("pwa.syncing", state.queueLength)
  }
  if (!text) return null

  return <div role="status" aria-live="polite" data-offline-banner className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-muted px-4 py-2 text-sm">
    <span>{text}</span>
    {actions && <div className="flex gap-2">
      <Button type="button" size="sm" variant="outline" onClick={retryQueue}>{t(lang, "pwa.queueRetry")}</Button>
      <Button type="button" size="sm" variant="ghost" onClick={() => void discardFirstQueued(lang)}>{t(lang, "pwa.queueDiscard")}</Button>
    </div>}
  </div>
}
