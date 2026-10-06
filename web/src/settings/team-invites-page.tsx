import * as React from "react"
import { Check, Copy, Link2, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { TeamInvitesData } from "@/dashboard/types"

export function TeamInvitesPage({ data }: { data: TeamInvitesData }) {
  const lang = data.Lang || "en"
  const formatDate = (value: string) => new Intl.DateTimeFormat(lang, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value))
  // A just-created link arrives as its own query parameter so it can be shown
  // as a link with a copy button rather than buried inside the flash sentence.
  const created = React.useMemo(() => new URLSearchParams(window.location.search).get("invite") || "", [])
  const [copied, setCopied] = React.useState(false)
  const [copyRefused, setCopyRefused] = React.useState(false)
  const createdRef = React.useRef<HTMLAnchorElement | null>(null)
  async function copyInviteLink() {
    try {
      if (!navigator.clipboard) throw new Error("clipboard unavailable")
      await navigator.clipboard.writeText(created)
      setCopied(true)
      setCopyRefused(false)
    } catch {
      // The browser may refuse clipboard access (no permission, insecure
      // origin). Select the link so it can still be copied by hand instead of
      // leaving a button that silently did nothing.
      setCopied(false)
      setCopyRefused(true)
      const link = createdRef.current
      if (link?.textContent) {
        const range = document.createRange()
        range.selectNodeContents(link)
        const selection = window.getSelection()
        selection?.removeAllRanges()
        selection?.addRange(range)
      }
    }
  }
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    {data.Flash && <div className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}><p role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>{created && <div className="mt-2 grid gap-2"><a ref={createdRef} className="break-all font-mono text-xs underline underline-offset-4" href={created}>{created}</a><div className="flex flex-wrap items-center gap-2"><Button type="button" size="sm" variant="outline" onClick={() => void copyInviteLink()}>{copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}{copied ? t(lang, "tokens.copied") : t(lang, "tokens.copy")}</Button>{copied && <span aria-live="polite" className="sr-only">{t(lang, "tokens.copied")}</span>}</div>{copyRefused && <p role="alert" className="text-xs text-destructive">{t(lang, "team.copyFailed")}</p>}</div>}</div>}
    <Card><CardHeader><CardTitle>{t(lang, "team.inviteTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.inviteBlurb")}</p></CardHeader><CardContent><form method="post" action="/api/team/invites" className="grid items-end gap-2 sm:grid-cols-[minmax(0,1fr)_auto]"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-1.5"><Label htmlFor="invite-email">{t(lang, "invite.email")}</Label><Input id="invite-email" type="email" name="email" autoComplete="off" placeholder="colleague@studio.ru" /></div><Button type="submit"><Link2 aria-hidden="true" />{t(lang, "team.generateInvite")}</Button><p className="text-xs text-muted-foreground sm:col-span-2">{t(lang, "invite.emailHint")}</p></form></CardContent></Card>
    <Card><CardHeader><CardTitle>{t(lang, "team.outstanding")} ({data.Invites.length})</CardTitle></CardHeader><CardContent className="grid gap-2">
      {data.Invites.length ? data.Invites.map(invite => <article key={invite.Token} className="grid gap-3 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto_auto_auto] sm:items-center"><div className="min-w-0">{invite.Live ? <a className="font-mono text-xs underline underline-offset-4" href={`/invites/${invite.Token}`}>{invite.Token.slice(0, 12)}…</a> : <span className="font-mono text-xs text-muted-foreground">{invite.Token.slice(0, 12)}…</span>}</div><span className={`w-fit rounded-md border px-2 py-1 text-xs ${invite.Expired ? "text-amber-700 dark:text-amber-300" : invite.Live ? "text-emerald-700 dark:text-emerald-300" : "text-muted-foreground"}`}>{t(lang, invite.Used ? "team.used" : invite.Expired ? "team.expired" : "team.live")}</span><div className="grid text-xs text-muted-foreground"><span>{t(lang, "team.created")}: {formatDate(invite.CreatedAt)}</span><span>{t(lang, "team.expires")}: {formatDate(invite.ExpiresAt)}</span></div><div className="flex justify-end"><form method="post" action={`/api/team/invites/${invite.Token}/revoke`} data-confirm={invite.Live ? t(lang, "team.confirmRevoke") : t(lang, "team.confirmRemoveInvite")}><input type="hidden" name="csrf_token" value={data.CSRFToken} />{/* A spent or expired invitation is only a leftover row, so it gets a cleanup button; both paths go through the confirmation dialog. */}<Button type="submit" variant="ghost" size="sm">{invite.Live ? <X aria-hidden="true" /> : <Trash2 aria-hidden="true" />}{invite.Live ? t(lang, "team.revoke") : t(lang, "team.removeInvite")}</Button></form></div></article>) : <div className="grid gap-1 py-2 text-center"><p className="text-sm text-muted-foreground">{t(lang, "team.noInvites")}</p><p className="text-xs text-muted-foreground">{t(lang, "team.noInvitesHint")}</p></div>}
    </CardContent></Card>
  </main>
}