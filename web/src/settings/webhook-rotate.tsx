import * as React from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { responseMessage } from "@/lib/response-message"
import { requestConfirmation } from "@/components/confirmation-dialog"
import { translate as t } from "@/i18n"

// Makes a new signing secret for one webhook and shows it once (ADM-13). The old secret stops
// signing at once, so the receiver has to be given the new one. The page keeps it only while
// it is open on this screen.
export function WebhookRotate({ id, csrfToken, lang }: { id: number; csrfToken: string; lang: string }) {
  const [secret, setSecret] = React.useState("")
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")

  async function rotate() {
    if (!(await requestConfirmation(t(lang, "wh.rotateConfirm")))) return
    setBusy(true)
    setError("")
    try {
      const response = await fetch(`/api/webhooks/${id}/secret`, {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": csrfToken },
        body: new URLSearchParams({ csrf_token: csrfToken }),
      })
      if (!response.ok) throw new Error(await responseMessage(response))
      const json = (await response.json()) as { secret: string }
      setSecret(json.secret)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  return <div className="grid w-full gap-2">
    <div><Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void rotate()}>{t(lang, "wh.rotate")}</Button></div>
    {secret && <div role="status" className="grid gap-1 rounded-md border border-warning/40 bg-warning/5 p-3 text-sm">
      <span>{t(lang, "wh.rotateOnce")}</span>
      <Input aria-label={t(lang, "wh.rotateSecret")} readOnly value={secret} onFocus={event => event.currentTarget.select()} className="font-mono text-xs" />
    </div>}
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
  </div>
}
