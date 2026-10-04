import * as React from "react"
import { Check, Copy, Plus, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { TokensData } from "@/dashboard/types"

const tabs = [
  ["settings-profile", "/settings/profile", "profile.title"],
  ["settings-prefs", "/settings/preferences", "prefs.title"],
  ["settings-notify", "/settings/notifications", "sheet.notifications"],
  ["settings-tokens", "/settings/tokens", "set.tabTokens"],
] as const
const managerTabs = [
  ["settings-team", "/settings/team", "team.workspaceTab"],
  ["settings-sections", "/settings/sections", "nav.sections"],
  ["settings-members", "/settings/members", "team.members"],
  ["settings-invites", "/settings/invites", "set.tabInvites"],
  ["settings-webhooks", "/settings/webhooks", "set.tabWebhooks"],
  ["settings-audit", "/settings/audit", "set.tabAudit"],
] as const

export function TokensPage({ data }: { data: TokensData }) {
  const lang = data.Lang || "en"
  const [expiresDays, setExpiresDays] = React.useState("90")
  const [copied, setCopied] = React.useState(false)
  const [copyError, setCopyError] = React.useState(false)
  const links = [...tabs.slice(0, 3), ...(data.CanManage ? managerTabs.slice(0, 4) : []), tabs[3], ...(data.CanManage ? managerTabs.slice(4) : [])]
  async function copyToken() {
    try { await navigator.clipboard.writeText(data.JustCreated); setCopied(true); setCopyError(false) }
    catch { setCopyError(true) }
  }
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "set.title")}</h1>
    <nav role="tablist" aria-label={t(lang, "set.title")} className="flex gap-1 overflow-x-auto border-b" style={{ scrollbarWidth: "none" }}>{links.map(([active, href, key]) => <a key={active} role="tab" href={href} aria-current={data.Active === active ? "page" : undefined} className={`shrink-0 border-b-2 px-3 py-2 text-sm ${data.Active === active ? "border-primary font-medium" : "border-transparent text-muted-foreground hover:text-foreground"}`}>{t(lang, key)}</a>)}</nav>
    <p className="text-sm text-muted-foreground">{t(lang, `set.desc.${data.Active}`)}</p>
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    {data.JustCreated && <Card className="border-primary/50"><CardContent className="grid gap-2 p-4"><div className="flex flex-wrap items-center justify-between gap-2"><p className="font-medium">{t(lang, "tokens.once")}</p><Button type="button" size="sm" variant="outline" onClick={() => void copyToken()}>{copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}{copied ? t(lang, "tokens.copied") : t(lang, "tokens.copy")}</Button></div><code className="block break-all rounded-md bg-muted p-3 text-sm">{data.JustCreated}</code>{copyError && <p role="alert" className="text-xs text-destructive">{t(lang, "err.generic")}</p>}</CardContent></Card>}
    <Card><CardHeader><CardTitle>{t(lang, "tokens.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "tokens.blurb")}</p></CardHeader><CardContent className="grid gap-4">
      <form method="post" action="/api/tokens" className="grid gap-3 border-b pb-4 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-end"><input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="grid gap-1.5"><Label htmlFor="token-name">{t(lang, "tokens.name")}</Label><Input id="token-name" name="name" required maxLength={40} placeholder={t(lang, "tokens.namePh")} /></div>
        <div className="grid gap-1.5"><Label htmlFor="token-expires">{t(lang, "tokens.expires")}</Label><Select name="expires_days" value={expiresDays} onValueChange={setExpiresDays}><SelectTrigger id="token-expires"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="30">{t(lang, "tokens.exp30")}</SelectItem><SelectItem value="90">{t(lang, "tokens.exp90")}</SelectItem><SelectItem value="365">{t(lang, "tokens.exp365")}</SelectItem><SelectItem value="0">{t(lang, "tokens.expNever")}</SelectItem></SelectContent></Select></div>
        <Button type="submit"><Plus aria-hidden="true" />{t(lang, "tokens.create")}</Button>
        <label className="flex items-center gap-2 text-sm sm:col-span-3"><Checkbox name="read_only" value="1" /><span>{t(lang, "tokens.readOnly")}</span></label><p className="text-xs text-muted-foreground sm:col-span-3">{t(lang, "tokens.scopeHint")}</p>
      </form>
      {data.Tokens?.length ? <div className="grid gap-2">{data.Tokens.map(token => <div key={token.ID} className="grid gap-3 rounded-md border p-3 sm:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center"><div className="min-w-0"><p className="font-medium">{token.Name}</p><p className="font-mono text-xs text-muted-foreground">{token.Prefix}…</p></div><div className="text-sm">{token.Team && <span>{token.Team}: </span>}{token.ReadOnly ? t(lang, "tokens.roShort") : t(lang, "tokens.rwShort")}</div><div className={`text-sm ${token.Expired ? "text-destructive" : "text-muted-foreground"}`}>{token.Expires || t(lang, "tokens.expNever")}</div><form method="post" action={`/api/tokens/${token.ID}/delete`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="ghost" size="sm"><Trash2 aria-hidden="true" />{t(lang, "nav.delete")}</Button></form></div>)}</div> : <p className="text-center text-sm text-muted-foreground">{t(lang, "tokens.empty")}</p>}
    </CardContent></Card>
  </main>
}
