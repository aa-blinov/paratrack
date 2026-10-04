import * as React from "react"
import { Download, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { ImportData } from "@/dashboard/types"

const providers = ["toggl", "harvest", "clockify"] as const

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
      <div className="grid gap-1.5"><Label htmlFor="import-from">{t(lang, "inv.from")}</Label><Input id="import-from" type="date" name="from" /></div><div className="grid gap-1.5"><Label htmlFor="import-to">{t(lang, "inv.to")}</Label><Input id="import-to" type="date" name="to" /></div>
      <Button type="submit" className="w-fit sm:col-span-2"><Download aria-hidden="true" />{t(lang, "imp.preview")}</Button>
    </form><p className="mt-3 text-xs text-muted-foreground">{t(lang, "imp.hint")}</p></CardContent></Card>
    {data.Entries?.length > 0 && <Card><CardHeader><CardTitle>{t(lang, "imp.previewTitle")} ({data.Entries.length})</CardTitle></CardHeader><CardContent><form method="post" action="/import/run" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="provider" value={data.Provider} /><input type="hidden" name="secret" value={data.Secret} /><input type="hidden" name="extra" value={data.Extra} /><input type="hidden" name="from" value={data.From} /><input type="hidden" name="to" value={data.To} /><input type="hidden" name="tz" value={data.TZ} /><div className="grid gap-2">{data.Entries.map((entry, index) => <div key={`${entry.ExtID}-${index}`} className="grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto_auto]"><p className="min-w-0 truncate font-medium">{entry.Activity}</p><span className="font-mono text-xs text-muted-foreground">{timeLabel(entry.Start)} – {timeLabel(entry.End)}</span><span className="truncate text-xs text-muted-foreground">{entry.Note}</span></div>)}</div><Button type="submit" className="w-fit"><Plus aria-hidden="true" />{t(lang, "imp.run")} ({data.Entries.length})</Button></form></CardContent></Card>}
  </main>
}
