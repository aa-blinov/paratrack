import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import type { StatsData } from "@/dashboard/types"
import { translate as t } from "@/i18n"

type MarkWeek = NonNullable<StatsData["MarkWeek"]>

// The viewer's own mark on a day (TIME-13): an overload or an idle day, with a note the
// manager reads on the team week. Marks are only set on the viewer's own days.
export function DayMarkCard({ week, lang, csrfToken }: { week: MarkWeek; lang: string; csrfToken: string }) {
  const today = week.Days.find(day => day.Today) ?? week.Days[0]
  const selectClass = "h-9 rounded-md border border-input bg-background px-3 text-sm"
  return <Card>
    <CardHeader><CardTitle>{t(lang, "stats.markTitle")}</CardTitle><CardDescription>{t(lang, "stats.markBlurb")}</CardDescription></CardHeader>
    <CardContent className="grid gap-4">
      <form method="post" action="/api/day-marks" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.5fr)_auto] lg:items-end">
        <input type="hidden" name="csrf_token" value={csrfToken} />
        <div className="grid gap-1.5"><Label htmlFor="mark-day">{t(lang, "stats.markDay")}</Label>
          <select id="mark-day" name="day" defaultValue={today.Value} className={selectClass}>
            {week.Days.map(day => <option key={day.Value} value={day.Value}>{day.Label}</option>)}
          </select></div>
        <div className="grid gap-1.5"><Label htmlFor="mark-kind">{t(lang, "stats.markKind")}</Label>
          <select id="mark-kind" name="kind" defaultValue="overload" className={selectClass}>
            <option value="overload">{t(lang, "stats.markOverload")}</option>
            <option value="idle">{t(lang, "stats.markIdle")}</option>
          </select></div>
        <div className="grid min-w-0 gap-1.5"><Label htmlFor="mark-note">{t(lang, "stats.markNote")}</Label>
          <Input id="mark-note" name="note" maxLength={200} /></div>
        <Button type="submit" variant="outline">{t(lang, "stats.markSave")}</Button>
      </form>
      {week.Marks.length ? <ul className="grid divide-y">
        {week.Marks.map(mark => <li key={mark.Day} className="flex flex-wrap items-center justify-between gap-3 py-2 text-sm">
          <span><span className="font-medium">{mark.DayLabel}</span>{": "}{mark.KindLabel}{mark.Note ? ` (${mark.Note})` : ""}</span>
          <form method="post" action="/api/day-marks">
            <input type="hidden" name="csrf_token" value={csrfToken} />
            <input type="hidden" name="day" value={mark.Day} />
            <input type="hidden" name="kind" value="" />
            <Button type="submit" size="sm" variant="ghost">{t(lang, "stats.markClear")}</Button>
          </form>
        </li>)}
      </ul> : <p className="text-sm text-muted-foreground">{t(lang, "stats.markNone")}</p>}
    </CardContent>
  </Card>
}
