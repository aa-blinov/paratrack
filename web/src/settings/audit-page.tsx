import { Link2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { AuditData } from "@/dashboard/types"

export function AuditPage({ data }: { data: AuditData }) {
  const lang = data.Lang || "en"
  const actionLabel = (action: string) => {
    const key = `audit.act.${action}`
    const label = t(lang, key)
    return label === key ? action : label
  }
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage />
    <Card><CardHeader><CardTitle>{t(lang, "audit.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "audit.blurb")}</p></CardHeader><CardContent>
      {data.Items.length ? <div className="grid gap-2">{data.Items.map((item, index) => <div key={`${item.Time}-${index}`} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center"><span className="whitespace-nowrap font-mono text-xs text-muted-foreground">{item.Time}</span><span className="w-fit rounded-md bg-muted px-2 py-1 text-xs" title={item.Action}>{actionLabel(item.Action)}</span><span className="break-all text-xs">{item.Target}</span><span className="font-mono text-xs text-muted-foreground">{item.IP}</span></div>)}</div> : <div className="grid justify-items-center gap-2 py-6 text-center"><p className="font-medium">{t(lang, "audit.empty")}</p><p className="text-sm text-muted-foreground">{t(lang, "audit.emptyHint")}</p><Button asChild size="sm" className="mt-1"><a href="/settings/webhooks"><Link2 aria-hidden="true" />{t(lang, "audit.emptyCta")}</a></Button></div>}
    </CardContent></Card>
  </main>
}
