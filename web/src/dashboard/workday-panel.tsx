import { Button } from "@/components/ui/button"
import { translate as t } from "@/i18n"

// The start and the end of today's working day (TIME-06). The start is kept once; the end
// can be given again, and the last one counts. Both are plain forms, so the page comes back
// with the time it recorded.
export function WorkdayPanel({ lang, csrfToken, started, ended }: { lang: string; csrfToken: string; started: string; ended: string }) {
  return <div className="flex flex-wrap items-center gap-2 text-sm" data-workday>
    {!started && <form method="post" action="/api/workday">
      <input type="hidden" name="csrf_token" value={csrfToken} />
      <input type="hidden" name="action" value="start" />
      <Button type="submit" size="sm" variant="outline">{t(lang, "dash.workdayStart")}</Button>
    </form>}
    {started && <>
      <span className="text-muted-foreground">{t(lang, "dash.workdayStarted", started)}</span>
      {ended && <span className="text-muted-foreground">{t(lang, "dash.workdayEnded", ended)}</span>}
      <form method="post" action="/api/workday">
        <input type="hidden" name="csrf_token" value={csrfToken} />
        <input type="hidden" name="action" value="end" />
        <Button type="submit" size="sm" variant="outline">{t(lang, ended ? "dash.workdayEndAgain" : "dash.workdayEnd")}</Button>
      </form>
    </>}
  </div>
}
