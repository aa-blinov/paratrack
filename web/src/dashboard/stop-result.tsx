import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { responseMessage } from "@/lib/response-message"
import { translate as t } from "@/i18n"

// The one-line result of the work that just stopped, left on its task (TASK-02).
// It sits on the stop card, so a person answers in one line and never leaves the page.
export function StopResult({ taskId, csrfToken, lang }: { taskId: number; csrfToken: string; lang: string }) {
  const [body, setBody] = useState("")
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState("")

  if (saved) return <p role="status" className="text-sm text-muted-foreground">{t(lang, "dash.resultSaved")}</p>

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const text = body.trim()
    if (!text) return
    setBusy(true)
    setError("")
    try {
      const response = await fetch(`/api/tasks/${taskId}/comments`, {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": csrfToken },
        body: new URLSearchParams({ csrf_token: csrfToken, body: text }),
      })
      if (!response.ok) throw new Error(await responseMessage(response))
      setSaved(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  return <form onSubmit={save} className="flex min-w-0 basis-full flex-wrap items-center gap-2">
    <Input aria-label={t(lang, "dash.resultLabel")} placeholder={t(lang, "dash.resultPlaceholder")} value={body} maxLength={2000} disabled={busy}
      className="min-w-0 flex-1 basis-56" onChange={event => setBody(event.target.value)} />
    <Button type="submit" variant="outline" size="sm" disabled={busy || !body.trim()}>{t(lang, "dash.resultSave")}</Button>
    {error && <p role="alert" className="basis-full text-sm text-destructive">{error}</p>}
  </form>
}
