import * as React from "react"
import { Flash } from "@/components/flash"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { StatusLine } from "@/components/status-line"
import { translate as t } from "@/i18n"
import { apiJSON } from "@/lib/json-api"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { CalendarSyncData, CalendarSyncProvider } from "@/dashboard/types"

// The provider's own name is a brand, so the card title is the translated key only for
// the two providers the server offers.
const providerTitle: Record<string, string> = { google: "calsync.google", microsoft: "calsync.outlook" }

// The codes the server keeps for a failed sync; anything else is the provider and its
// HTTP status, which the page shows as it is.
const knownErrors = new Set(["token_rejected", "unreachable", "internal"])

type SyncAnswer = { connections?: Array<{ provider: string; last_synced_at: string; last_error: string }> }

export function CalendarSyncPage({ data }: { data: CalendarSyncData }) {
  const lang = data.Lang || "en"
  const [providers, setProviders] = React.useState<CalendarSyncProvider[]>(data.Providers || [])
  const [busy, setBusy] = React.useState(false)
  const [status, setStatus] = React.useState("")
  const [statusOK, setStatusOK] = React.useState(true)
  const anyConnected = providers.some(provider => provider.Connected)

  // The sync runs every connection of the member at once; the answer replaces what each
  // card shows, so the page needs no reload to report the outcome.
  async function syncNow() {
    setBusy(true)
    setStatus(t(lang, "calsync.syncing"))
    try {
      const answer = await apiJSON<SyncAnswer>("/api/calendar/sync", "POST", data.CSRFToken, {})
      const served = answer.connections || []
      setProviders(current => current.map(provider => {
        const found = served.find(connection => connection.provider === provider.Key)
        return found ? { ...provider, LastSyncedAt: found.last_synced_at, LastError: found.last_error } : provider
      }))
      const failed = served.some(connection => connection.last_error !== "")
      setStatusOK(!failed)
      setStatus(failed ? t(lang, "calsync.syncFailed") : t(lang, "calsync.synced"))
    } catch {
      setStatusOK(false)
      setStatus(t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  function errorText(code: string) {
    return knownErrors.has(code) ? t(lang, `calsync.err.${code}`) : code
  }

  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    <Flash message={data.Flash} ok={data.FlashOK} />
    <div className="grid gap-4 md:grid-cols-2">
      {providers.map(provider => {
        const title = providerTitle[provider.Key] ? t(lang, providerTitle[provider.Key]) : provider.Key
        const statusText = !provider.Enabled && !provider.Connected
          ? t(lang, "calsync.status.disabled")
          : provider.Connected ? t(lang, "calsync.status.connected") : t(lang, "calsync.status.notConnected")
        return <Card key={provider.Key}>
          <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
          <CardContent className="grid gap-3">
            <p className="text-sm font-medium">{statusText}</p>
            {provider.Connected && <p className="text-sm text-muted-foreground">{provider.LastSyncedAt ? t(lang, "calsync.lastSync", provider.LastSyncedAt.replace("T", " ")) : t(lang, "calsync.neverSynced")}</p>}
            {provider.Connected && provider.LastError && <p role="alert" className="text-sm text-destructive">{t(lang, "calsync.lastError", errorText(provider.LastError))}</p>}
            <div className="flex flex-wrap gap-2">
              {!provider.Connected && provider.Enabled && <form method="post" action={`/settings/calendar-sync/${provider.Key}/connect`}>
                <input type="hidden" name="csrf_token" value={data.CSRFToken} />
                <Button type="submit">{t(lang, "calsync.connect")}</Button>
              </form>}
              {provider.Connected && <form method="post" action={`/settings/calendar-sync/${provider.Key}/disconnect`} data-confirm={t(lang, "calsync.disconnectConfirm")}>
                <input type="hidden" name="csrf_token" value={data.CSRFToken} />
                <Button type="submit" variant="outline">{t(lang, "calsync.disconnect")}</Button>
              </form>}
            </div>
          </CardContent>
        </Card>
      })}
    </div>
    {anyConnected && <Card><CardContent className="grid gap-3 p-4">
      <p className="text-sm text-muted-foreground">{t(lang, "calsync.disconnectHint")}</p>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" onClick={() => void syncNow()} disabled={busy}>{t(lang, "calsync.syncNow")}</Button>
        <StatusLine text={status} className={statusOK ? "text-xs text-muted-foreground" : "text-xs text-destructive"} />
      </div>
    </CardContent></Card>}
  </main>
}
