import { Plus, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import { SettingsNav } from "@/settings/settings-nav"
import type { WebhooksData } from "@/dashboard/types"

const events = ["session.started", "session.stopped", "invoice.created", "invoice.paid", "invoice.payment_link_created", "import.completed"]

export function WebhooksPage({ data }: { data: WebhooksData }) {
  const lang = data.Lang || "en"
  const eventName = (event: string) => t(lang, `wh.ev.${event}`)
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "set.title")}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage />
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    <Card><CardHeader><CardTitle>{t(lang, "wh.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "wh.blurb")}</p></CardHeader><CardContent className="grid gap-4">
      <form method="post" action="/api/webhooks" className="grid gap-3 border-b pb-4 sm:grid-cols-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-1.5 sm:col-span-2"><Label htmlFor="webhook-url">{t(lang, "wh.urlLabel")}</Label><Input id="webhook-url" type="url" name="url" required placeholder="https://example.com/hook" /></div><div className="grid gap-1.5 sm:col-span-2"><Label htmlFor="webhook-secret">{t(lang, "wh.secretLabel")}</Label><Input id="webhook-secret" type="text" name="secret" required autoComplete="off" placeholder={t(lang, "wh.secret")} /><p className="text-xs text-muted-foreground">{t(lang, "wh.secretHint")}</p></div><fieldset className="grid gap-1 sm:col-span-2"><legend className="mb-1 text-sm font-medium">{t(lang, "wh.eventsLabel")}</legend><div className="grid gap-2 sm:grid-cols-2">{events.map(event => <label key={event} className="flex items-center gap-2 text-sm"><Checkbox name="events" value={event} defaultChecked={event === "session.stopped" || event === "invoice.created"} />{eventName(event)}</label>)}</div></fieldset><Button type="submit" className="w-fit"><Plus aria-hidden="true" />{t(lang, "wh.add")}</Button></form>
      {data.Items.length ? <div className="grid gap-3">{data.Items.map(hook => <article key={hook.ID} className="grid gap-3 rounded-md border p-3"><div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start"><div className="grid min-w-0 gap-1"><p className="break-all font-mono text-xs">{hook.URL}</p><p className="text-xs text-muted-foreground">{hook.Events.split(",").map(value => value.trim()).filter(Boolean).map(eventName).join(", ")}</p></div><form method="post" action={`/api/webhooks/${hook.ID}/delete`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" size="icon" variant="ghost" title={t(lang, "wh.delete")} aria-label={t(lang, "wh.delete")}><Trash2 aria-hidden="true" /></Button></form></div>{hook.Deliveries.length > 0 && <div><p className="mb-1 text-xs font-medium">{t(lang, "wh.deliveries")}</p><ul className="grid gap-1">{hook.Deliveries.map((delivery, index) => <li key={`${delivery.When}-${index}`} className="flex flex-wrap gap-x-2 text-xs"><span className={`font-mono ${delivery.OK ? "text-emerald-700 dark:text-emerald-300" : "text-destructive"}`}>{delivery.Status || "—"}</span><span className="text-muted-foreground">{delivery.When}: {eventName(delivery.Event)}</span>{delivery.Error && <span className="truncate text-muted-foreground">{delivery.Error}</span>}</li>)}</ul></div>}</article>)}</div> : <p className="text-sm text-muted-foreground">{t(lang, "wh.empty")}</p>}
      {data.Items.length > 0 && <p className="text-xs text-muted-foreground">{t(lang, "wh.sig")}</p>}
    </CardContent></Card>
  </main>
}
