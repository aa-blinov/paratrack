import { Filter, Link2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { AuditData } from "@/dashboard/types"

// Native controls in one GET form: the whole filter works from the keyboard
// and submits without JavaScript, and the resulting address carries the state,
// so a link from a notification reopens the same list. The action control is
// named "event" because a form control called "action" shadows form.action and
// the browser would submit to the control instead of the journal.
const selectClass = "h-9 w-full min-w-0 rounded-lg border border-input px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm dark:bg-input/30"

export function AuditPage({ data }: { data: AuditData }) {
  const lang = data.Lang || "en"
  const actionLabel = (action: string) => {
    const key = `audit.act.${action}`
    const label = t(lang, key)
    return label === key ? action : label
  }
  const rows = data.Items || []
  const people = data.People || []
  const actions = data.Actions || []
  const from = data.From || ""
  const to = data.To || ""
  const userID = data.UserID || 0
  const action = data.Action || ""
  const shownLimit = data.Window || 100
  const personName = (id: number) => people.find(person => person.ID === id)?.Name || String(id)
  const rowsList = rows.length ? <div className="grid gap-2">{rows.map((item, index) => <div key={`${item.Time}-${index}`} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center"><span className="whitespace-nowrap font-mono text-xs text-muted-foreground">{item.Time}</span><span className="w-fit rounded-md bg-muted px-2 py-1 text-xs" title={item.Action}>{actionLabel(item.Action)}</span><span className="break-all text-xs">{item.Target}</span><span className="font-mono text-xs text-muted-foreground">{item.IP}</span></div>)}</div> : data.Filtered ? <div className="grid justify-items-center gap-2 py-6 text-center"><p className="font-medium">{t(lang, "audit.noMatch")}</p><p className="text-sm text-muted-foreground">{t(lang, "audit.noMatchHint")}</p><Button asChild size="sm" variant="outline" className="mt-1"><a href="/settings/audit">{t(lang, "stats.clearFilters")}</a></Button></div> : <div className="grid justify-items-center gap-2 py-6 text-center"><p className="font-medium">{t(lang, "audit.empty")}</p><p className="text-sm text-muted-foreground">{t(lang, "audit.emptyHint")}</p><Button asChild size="sm" className="mt-1"><a href="/settings/webhooks"><Link2 aria-hidden="true" />{t(lang, "audit.emptyCta")}</a></Button></div>
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage />
    <Card><CardHeader><CardTitle>{t(lang, "audit.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "audit.blurbWindow", shownLimit)}</p></CardHeader><CardContent className="grid gap-3">
      <form method="get" action="/settings/audit" className="grid gap-3">
        <fieldset className="grid gap-3"><legend className="mb-1.5 text-sm font-medium">{t(lang, "audit.filters")}</legend>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <div className="grid gap-1.5"><Label htmlFor="audit-from">{t(lang, "inv.from")}</Label><Input id="audit-from" name="from" type="date" defaultValue={from} /></div>
            <div className="grid gap-1.5"><Label htmlFor="audit-to">{t(lang, "inv.to")}</Label><Input id="audit-to" name="to" type="date" defaultValue={to} /></div>
            <div className="grid gap-1.5"><Label htmlFor="audit-user">{t(lang, "stats.person")}</Label><select id="audit-user" name="user" defaultValue={String(userID)} className={selectClass}><option value="">{t(lang, "stats.everyone")}</option>{people.map(person => <option key={person.ID} value={String(person.ID)}>{person.Name}</option>)}</select></div>
            <div className="grid gap-1.5"><Label htmlFor="audit-action">{t(lang, "audit.action")}</Label><select id="audit-action" name="event" defaultValue={action} className={selectClass}><option value="">{t(lang, "stats.all")}</option>{actions.map(code => <option key={code} value={code}>{actionLabel(code)}</option>)}</select></div>
          </div>
        </fieldset>
        {shownLimit > 100 && <input type="hidden" name="size" value={shownLimit} />}
        <div className="flex flex-wrap items-center gap-2"><Button type="submit"><Filter aria-hidden="true" />{t(lang, "audit.apply")}</Button><p className="text-sm text-muted-foreground">{t(lang, "audit.shown", rows.length)}</p></div>
      </form>
      {data.Filtered && <div className="flex flex-wrap items-center gap-2"><span className="text-xs uppercase text-muted-foreground">{t(lang, "stats.filteredBy")}</span>{from && <Badge variant="secondary">{t(lang, "inv.from")}: {from}</Badge>}{to && <Badge variant="secondary">{t(lang, "inv.to")}: {to}</Badge>}{!!userID && <Badge variant="secondary">{t(lang, "stats.person")}: {personName(userID)}</Badge>}{action && <Badge variant="secondary">{t(lang, "audit.action")}: {actionLabel(action)}</Badge>}<Button asChild size="sm" variant="ghost" className="ml-auto"><a href="/settings/audit">{t(lang, "stats.clearFilters")}</a></Button></div>}
      {rowsList}
      {data.MoreURL && <div><Button asChild size="sm" variant="outline"><a href={data.MoreURL}>{t(lang, "audit.more")}</a></Button></div>}
    </CardContent></Card>
  </main>
}