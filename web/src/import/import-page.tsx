import * as React from "react"
import { Download, Plus, Upload } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { ImportData } from "@/dashboard/types"
import { DateField } from "@/components/date-field"

const providers = ["toggl", "harvest", "clockify"] as const

type RestoredWebhook = { url: string; secret: string }

// RestoreAnswer is what the restore reports: the webhooks it recreated, the counts it
// restored, and the connections it did not carry over (ADM-11).
type RestoreAnswer = { webhooks: RestoredWebhook[]; members: number; projects: number; invoices: number; skipped_integrations: number }

// WorkspaceRestore uploads a whole-workspace export. The file goes up as the raw
// request body, streamed from disk, with the CSRF header; a refused file says why,
// here, and nothing is written. The restored webhooks come back with new secrets,
// shown once on this screen before the restored workspace opens, next to a summary of
// what was restored and the integrations to connect again.
function WorkspaceRestore({ lang, csrf }: { lang: string; csrf: string }) {
  const [file, setFile] = React.useState<File | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")
  const [restored, setRestored] = React.useState<RestoreAnswer | null>(null)
  async function restore(event: React.FormEvent) {
    event.preventDefault()
    if (!file) {
      setError(t(lang, "imp.workspaceNeedFile"))
      return
    }
    setBusy(true)
    setError("")
    try {
      const response = await fetch("/api/workspace/restore", {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json", "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: file,
      })
      const answer = (await response.json().catch(() => ({}))) as { error?: string } & Partial<RestoreAnswer>
      if (!response.ok) throw new Error(answer.error || response.statusText)
      setRestored({ webhooks: answer.webhooks ?? [], members: answer.members ?? 0, projects: answer.projects ?? 0, invoices: answer.invoices ?? 0, skipped_integrations: answer.skipped_integrations ?? 0 })
      setBusy(false)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      setBusy(false)
    }
  }
  if (restored) {
    return <Card><CardHeader><CardTitle>{t(lang, "imp.restoreDoneTitle")}</CardTitle></CardHeader><CardContent className="grid gap-3">
      <ul className="grid gap-1 text-sm"><li>{t(lang, "imp.restoreMembers", restored.members)}</li><li>{t(lang, "imp.restoreProjects", restored.projects)}</li><li>{t(lang, "imp.restoreInvoices", restored.invoices)}</li></ul>
      {restored.skipped_integrations > 0 && <p role="note" className="rounded-md border border-border bg-muted/40 p-3 text-sm">{t(lang, "imp.restoreIntegrations", restored.skipped_integrations)} <a href="/integrations" className="underline underline-offset-4">{t(lang, "imp.restoreIntegrationsLink")}</a></p>}
      {restored.webhooks.length > 0 && <div className="grid gap-2"><p className="text-sm font-medium">{t(lang, "imp.workspaceWebhooksTitle")}</p><p className="text-sm text-muted-foreground">{t(lang, "imp.workspaceWebhooksBlurb")}</p>
      <ul className="grid gap-2">{restored.webhooks.map(hook => <li key={hook.url} className="grid min-w-0 gap-1 rounded-md border p-3"><span className="truncate text-sm font-medium">{hook.url}</span><code className="break-all font-mono text-xs">{hook.secret}</code></li>)}</ul></div>}
      <Button type="button" className="w-fit" onClick={() => window.location.assign("/")}>{t(lang, "imp.workspaceOpen")}</Button>
    </CardContent></Card>
  }
  return <Card><CardHeader><CardTitle>{t(lang, "imp.workspaceTitle")}</CardTitle></CardHeader><CardContent className="grid gap-3">
    <p className="text-sm text-muted-foreground">{t(lang, "imp.workspaceBlurb")}</p>
    <form onSubmit={event => void restore(event)} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end" aria-label={t(lang, "imp.workspaceTitle")}>
      <div className="grid min-w-0 gap-1.5">
        <Label htmlFor="workspace-file" className="text-xs">{t(lang, "imp.workspaceFile")}</Label>
        <Input id="workspace-file" type="file" accept=".json,application/json" onChange={event => setFile(event.target.files?.[0] ?? null)} />
      </div>
      <Button type="submit" disabled={busy}><Upload aria-hidden="true" />{busy ? t(lang, "imp.workspaceUploading") : t(lang, "imp.workspaceRun")}</Button>
    </form>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
  </CardContent></Card>
}

export function ImportPage({ data }: { data: ImportData }) {
  const lang = data.Lang || "en"
  const [provider, setProvider] = React.useState(data.Provider || "toggl")
  const formTZ = typeof Intl !== "undefined" ? Intl.DateTimeFormat().resolvedOptions().timeZone : ""
  const timeLabel = (value: string) => `${value.slice(5, 7)}-${value.slice(8, 10)} ${value.slice(11, 16).replace(":", ":")}`
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <header><h1 className="mb-1 text-2xl font-semibold tracking-tight">{t(lang, "imp.title")}</h1><p className="text-sm text-muted-foreground">{t(lang, "imp.blurb")}</p></header>
    {data.Error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{data.Error}</p>}
    <Card><CardHeader><CardTitle>{t(lang, "imp.pick")}</CardTitle></CardHeader><CardContent><form method="post" action="/import/preview" className="grid gap-3 sm:grid-cols-2"><input type="hidden" name="tz" value={formTZ} /><input type="hidden" name="csrf_token" value={data.CSRFToken} />
      <div className="grid gap-1.5"><Label htmlFor="import-provider">{t(lang, "int.provider")}</Label><Select name="provider" value={provider} onValueChange={setProvider}><SelectTrigger id="import-provider"><SelectValue /></SelectTrigger><SelectContent>{providers.map(item => <SelectItem key={item} value={item}>{item === "toggl" ? "Toggl Track" : item === "harvest" ? "Harvest" : "Clockify"}</SelectItem>)}</SelectContent></Select></div>
      <div className="grid gap-1.5"><Label htmlFor="import-secret">{t(lang, "int.secret")}</Label><Input id="import-secret" type="password" name="secret" required placeholder={t(lang, "imp.keyPh")} autoComplete="off" /><p className="text-xs text-muted-foreground">{t(lang, provider === "toggl" ? "imp.key.toggl" : provider === "harvest" ? "imp.key.harvest" : "imp.key.clockify")}</p></div>
      {provider !== "toggl" && <div className="grid gap-1.5 sm:col-span-2"><Label htmlFor="import-extra">{t(lang, provider === "harvest" ? "imp.extraHarvest" : "imp.extraClockify")}</Label><Input id="import-extra" type="text" name="extra" placeholder="123456" /><p className="text-xs text-muted-foreground">{t(lang, provider === "harvest" ? "imp.extraHintHarvest" : "imp.extraHintClockify")}</p></div>}
      <div className="grid gap-1.5"><Label htmlFor="import-from">{t(lang, "inv.from")}</Label><DateField id="import-from" name="from" /></div><div className="grid gap-1.5"><Label htmlFor="import-to">{t(lang, "inv.to")}</Label><DateField id="import-to" name="to" /></div>
      <Button type="submit" className="w-fit sm:col-span-2"><Download aria-hidden="true" />{t(lang, "imp.preview")}</Button>
    </form><p className="mt-3 text-xs text-muted-foreground">{t(lang, "imp.hint")}</p></CardContent></Card>
    <WorkspaceRestore lang={lang} csrf={data.CSRFToken} />
    {data.Entries?.length > 0 && <Card><CardHeader><CardTitle>{t(lang, "imp.previewTitle")} ({data.Entries.length})</CardTitle></CardHeader><CardContent><form method="post" action="/import/run" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="provider" value={data.Provider} /><input type="hidden" name="secret" value={data.Secret} /><input type="hidden" name="extra" value={data.Extra} /><input type="hidden" name="from" value={data.From} /><input type="hidden" name="to" value={data.To} /><input type="hidden" name="tz" value={data.TZ} /><div className="grid gap-2">{data.Entries.map((entry, index) => <div key={`${entry.ExtID}-${index}`} className="grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto_auto]"><p className="min-w-0 truncate font-medium">{entry.Activity}</p><span className="font-mono text-xs text-muted-foreground">{timeLabel(entry.Start)} – {timeLabel(entry.End)}</span><span className="truncate text-xs text-muted-foreground">{entry.Note}</span></div>)}</div><Button type="submit" className="w-fit"><Plus aria-hidden="true" />{t(lang, "imp.run")} ({data.Entries.length})</Button></form></CardContent></Card>}
  </main>
}
