import * as React from "react"
import { Button } from "@/components/ui/button"
import { translate as t } from "@/i18n"

// The start and the end of today's working day (TIME-06). The start is kept once; the end
// can be given again, and the last one counts. The mark goes by fetch, so offline it waits
// in the queue: the answer then says it is saved, not sent, and the page keeps its state.
export function WorkdayPanel({ lang, csrfToken, started, ended }: { lang: string; csrfToken: string; started: string; ended: string }) {
  const [busy, setBusy] = React.useState(false)
  const [message, setMessage] = React.useState("")

  async function mark(action: "start" | "end") {
    setBusy(true)
    setMessage("")
    try {
      const body = new URLSearchParams({ csrf_token: csrfToken, action })
      const response = await fetch("/api/workday", {
        method: "POST",
        credentials: "same-origin",
        headers: { "X-CSRF-Token": csrfToken, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
        body,
      })
      if (response.headers.get("X-Offline-Queued")) {
        setMessage(t(lang, "dash.workdayQueued"))
        return
      }
      window.location.reload()
    } catch {
      setMessage(t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  return <div className="flex flex-wrap items-center gap-2 text-sm" data-workday>
    {!started && <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void mark("start")}>{t(lang, "dash.workdayStart")}</Button>}
    {started && <>
      <span className="text-muted-foreground">{t(lang, "dash.workdayStarted", started)}</span>
      {ended && <span className="text-muted-foreground">{t(lang, "dash.workdayEnded", ended)}</span>}
      <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void mark("end")}>{t(lang, ended ? "dash.workdayEndAgain" : "dash.workdayEnd")}</Button>
    </>}
    {message && <span role="status" className="text-muted-foreground">{message}</span>}
  </div>
}
