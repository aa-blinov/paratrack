import { Link as LinkIcon } from "lucide-react"
import * as React from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { responseMessage } from "@/lib/response-message"
import { translate as t } from "@/i18n"

// A link a client opens without an account (FIN-13). The address is shown when it is
// made; the page keeps no copy, so a new one is made for each client who needs it.
export function ClientLink({ invoiceID, csrfToken, lang }: { invoiceID: number; csrfToken: string; lang: string }) {
  const [link, setLink] = React.useState<{ url: string; expires: string } | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")

  async function make() {
    setBusy(true)
    setError("")
    try {
      const response = await fetch(`/api/invoices/${invoiceID}/share`, {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": csrfToken },
        body: new URLSearchParams({ csrf_token: csrfToken }),
      })
      if (!response.ok) throw new Error(await responseMessage(response))
      const json = (await response.json()) as { url: string; expires_at: string }
      setLink({ url: json.url, expires: json.expires_at })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  if (!link) return <div className="grid gap-2">
    <div><Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void make()}><LinkIcon aria-hidden="true" />{t(lang, "inv.linkMake")}</Button></div>
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
  </div>

  return <div className="grid max-w-2xl gap-2">
    <Label htmlFor="client-link" className="text-xs">{t(lang, "inv.linkTitle")}</Label>
    <Input id="client-link" readOnly value={link.url} onFocus={event => event.currentTarget.select()} />
    <p className="text-xs text-muted-foreground">{t(lang, "inv.linkExpires", link.expires)}</p>
  </div>
}
