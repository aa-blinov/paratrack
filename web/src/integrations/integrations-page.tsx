import * as React from "react"
import { Activity, Check, ExternalLink, Folder, GitBranch, Link2, Play, RefreshCw, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { IntegrationDetailData, IntegrationsData, MarketplaceData } from "@/dashboard/types"

const providers = ["github", "gitlab", "jira", "trello", "asana", "clickup", "todoist", "notion"]
const icons: Record<string, React.ComponentType<React.SVGProps<SVGSVGElement>>> = { github: GitBranch, gitlab: Activity, jira: Activity, trello: Folder, asana: Check, clickup: Check, todoist: Check, notion: Folder }

function Flash({ message, ok }: { message: string; ok: boolean }) {
  if (!message) return null
  return <p role={ok ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${ok ? "border-border" : "border-destructive/40 text-destructive"}`}>{message}</p>
}

export function IntegrationsPage({ data }: { data: IntegrationsData }) {
  const lang = data.Lang || "en"
  const requested = new URLSearchParams(window.location.search).get("provider")
  const [provider, setProvider] = React.useState(providers.includes(requested || "") ? requested! : "github")
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <Flash message={data.Flash} ok={data.FlashOK} />
    <header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "int.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "int.blurb")}</p></div><Button asChild variant="outline" size="sm"><a href="/integrations/marketplace"><Folder aria-hidden="true" />{t(lang, "mkt.title")}</a></Button></header>
    <Card><CardHeader><CardTitle>{t(lang, "int.connect")}</CardTitle></CardHeader><CardContent><form method="post" action="/integrations" className="grid gap-4 sm:grid-cols-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} />
      <div className="grid gap-1.5"><Label htmlFor="integration-provider">{t(lang, "int.provider")}</Label><Select name="provider" value={provider} onValueChange={setProvider}><SelectTrigger id="integration-provider"><SelectValue /></SelectTrigger><SelectContent>{providers.map(id => <SelectItem key={id} value={id}>{id[0].toUpperCase()+id.slice(1)}</SelectItem>)}</SelectContent></Select></div>
      <div className="grid gap-1.5"><Label htmlFor="integration-name">{t(lang, "int.name")}</Label><Input id="integration-name" name="name" required placeholder={t(lang, "int.namePh")} /><p className="text-xs text-muted-foreground">{t(lang, "int.nameHint")}</p></div>
      <div className="grid gap-1.5 sm:col-span-2"><Label htmlFor="integration-secret">{t(lang, "int.secret")}</Label><Input id="integration-secret" name="secret" type="password" autoComplete="new-password" required /><p className="text-xs text-muted-foreground">{t(lang, `int.key.${provider}`)}</p></div>
      <div className="grid gap-1.5 sm:col-span-2"><Label htmlFor="integration-target">{t(lang, "int.target")}</Label><Input id="integration-target" name="extra" /><p className="text-xs text-muted-foreground">{t(lang, `int.target.${provider}`)}</p>{provider === "jira" && <p className="text-xs text-muted-foreground">{t(lang, "int.jiraSite")}</p>}</div>
      <div className="sm:col-span-2"><Button type="submit"><Link2 aria-hidden="true" />{t(lang, "int.connectBtn")}</Button></div>
    </form></CardContent></Card>
    {data.Items?.length ? <Card><CardHeader><CardTitle>{t(lang, "int.connected")}</CardTitle></CardHeader><CardContent className="grid gap-2">{data.Items.map(item => <div key={item.ID} className="flex flex-wrap items-center gap-3 rounded-md border p-3"><Badge variant="outline" className="uppercase">{item.Provider}</Badge><a className="min-w-0 flex-1 font-medium underline-offset-4 hover:underline" href={`/integrations/${item.ID}`}>{item.Name}</a><span className="font-mono text-sm text-muted-foreground">{item.TaskCount}</span><form method="post" action={`/integrations/${item.ID}/delete`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="ghost" size="icon" aria-label={`${t(lang, "nav.delete")} ${item.Name}`}><Trash2 aria-hidden="true" className="text-destructive" /></Button></form></div>)}</CardContent></Card> : <div className="py-8 text-center"><p className="font-medium">{t(lang, "int.empty")}</p><p className="mt-1 text-sm text-muted-foreground">{t(lang, "int.emptyHint")}</p><Button asChild size="sm" className="mt-3"><a href="/integrations/marketplace">{t(lang, "int.browse")}</a></Button></div>}
  </main>
}

export function IntegrationDetailPage({ data }: { data: IntegrationDetailData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-6xl gap-4"><Flash message={data.Flash} ok={data.FlashOK} />
    <nav className="text-sm"><a href="/integrations" className="text-muted-foreground underline-offset-4 hover:underline">{t(lang, "int.title")}</a><span className="mx-2 text-muted-foreground">/</span><span>{data.Integration.Name}</span></nav>
    <header className="flex flex-wrap items-center justify-between gap-3"><h1 className="text-2xl font-semibold tracking-tight">{data.Integration.Name}</h1><form method="post" action={`/integrations/${data.Integration.ID}/sync`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="outline" size="sm"><RefreshCw aria-hidden="true" />{t(lang, "int.sync")}</Button></form></header>
    <Card><CardHeader><CardTitle>{t(lang, "int.tasks")} ({data.Tasks?.length || 0})</CardTitle></CardHeader><CardContent>{data.Tasks?.length ? <div className="grid gap-2">{data.Tasks.map(task => <div key={task.ID} className="flex flex-wrap items-center gap-3 rounded-md border p-3"><div className="min-w-0 flex-1">{task.URL ? <a href={task.URL} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 font-medium underline-offset-4 hover:underline">{task.Title}<ExternalLink aria-hidden="true" className="size-3" /></a> : <span className="font-medium">{task.Title}</span>}<p className="mt-1 text-xs text-muted-foreground">{task.ExternalID}</p></div><Badge variant="secondary">{task.Status}</Badge><form method="post" action="/integrations/start"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="task_id" value={task.ID} /><Button type="submit" size="sm"><Play aria-hidden="true" />{t(lang, "int.start")}</Button></form></div>)}</div> : <p className="text-sm text-muted-foreground">{t(lang, "int.noTasks")}</p>}</CardContent></Card>
  </main>
}

export function MarketplacePage({ data }: { data: MarketplaceData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-6xl gap-4"><header className="flex flex-wrap items-end justify-between gap-3"><div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "mkt.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "mkt.blurb")}</p></div><Button asChild variant="outline" size="sm"><a href="/integrations"><Link2 aria-hidden="true" />{t(lang, "mkt.manage")}</a></Button></header>
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{data.Items.map(item => { const Icon = icons[item.ID] || Link2; const secret = t(lang, `mkt.${item.ID}.secret`); const target = t(lang, `mkt.${item.ID}.target`); return <Card key={item.ID}><CardContent className="flex h-full flex-col gap-3 p-4"><div className="flex min-w-0 items-center gap-2"><Icon aria-hidden="true" className="size-5 shrink-0 text-muted-foreground" /><h2 className="min-w-0 flex-1 font-semibold">{item.Name}</h2>{item.Connected && <Badge>{t(lang, "mkt.connected")}</Badge>}{!item.Available && <Badge variant="outline">{t(lang, "mkt.soon")}</Badge>}</div><p className="flex-1 text-sm text-muted-foreground">{t(lang, `mkt.${item.ID}.blurb`)}</p>{item.Available && <><Button asChild size="sm" className="self-start"><a href={`/integrations?provider=${encodeURIComponent(item.ID)}`}><Link2 aria-hidden="true" />{t(lang, "mkt.connect")}</a></Button>{(secret || target) && <p className="text-xs text-muted-foreground">{[secret, target].filter(Boolean).join(", ")}</p>}</>}</CardContent></Card>})}</div>
  </main>
}
