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
  const [notice, setNotice] = React.useState("")
  const [links, setLinks] = React.useState<Array<{ id: number; created: string; expires: string }>>([])

  // The links that still open, so one sent to the wrong person is closed
  // alone and the others keep working.
  const load = React.useCallback(async () => {
    try {
      const response = await fetch(`/api/invoices/${invoiceID}/share`, { credentials: "same-origin", headers: { Accept: "application/json" } })
      if (!response.ok) return
      const json = (await response.json()) as { links: Array<{ id: number; created: string; expires: string }> }
      setLinks(json.links ?? [])
    } catch {
      // The list is a convenience; the make and revoke buttons still work.
    }
  }, [invoiceID])
  React.useEffect(() => { void load() }, [load])

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
      void load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  // A link sent to the wrong person, or to an invoice that was redone, stops
  // opening now instead of after thirty days.
  async function revoke(linkID?: number) {
    setBusy(true)
    setError("")
    try {
      const response = await fetch(`/api/invoices/${invoiceID}/share${linkID ? `?link=${linkID}` : ""}`, {
        method: "DELETE",
        credentials: "same-origin",
        headers: { Accept: "application/json", "X-CSRF-Token": csrfToken },
      })
      if (!response.ok) throw new Error(await responseMessage(response))
      const json = (await response.json()) as { revoked: number }
      if (!linkID) setLink(null)
      setNotice(t(lang, "inv.linkRevoked", json.revoked))
      void load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    } finally {
      setBusy(false)
    }
  }

  const revokeButton = <Button type="button" size="sm" variant="ghost" disabled={busy || links.length === 0} onClick={() => void revoke()}>{t(lang, "inv.linkRevoke")}</Button>
  const list = links.length > 1 && <ul className="grid gap-1 text-xs text-muted-foreground">{links.map(l => <li key={l.id} className="flex flex-wrap items-center gap-2">
    <span>{t(lang, "inv.linkItem", l.created, l.expires)}</span>
    <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-xs" disabled={busy} onClick={() => void revoke(l.id)}>{t(lang, "inv.linkRevokeOne")}</Button>
  </li>)}</ul>

  if (!link) return <div className="grid gap-2">
    <div className="flex flex-wrap gap-2"><Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void make()}><LinkIcon aria-hidden="true" />{t(lang, "inv.linkMake")}</Button>{revokeButton}</div>
    {list}
    {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
  </div>

  return <div className="grid max-w-2xl gap-2">
    <Label htmlFor="client-link" className="text-xs">{t(lang, "inv.linkTitle")}</Label>
    <Input id="client-link" readOnly value={link.url} onFocus={event => event.currentTarget.select()} />
    <p className="text-xs text-muted-foreground">{t(lang, "inv.linkExpires", link.expires)}</p>
    <div>{revokeButton}</div>
    {list}
  </div>
}
