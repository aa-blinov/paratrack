import { Link2, X } from "lucide-react"
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
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    <Card><CardHeader><CardTitle>{t(lang, "team.inviteTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.inviteBlurb")}</p></CardHeader><CardContent><form method="post" action="/api/team/invites" className="grid items-end gap-2 sm:grid-cols-[minmax(0,1fr)_auto]"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-1.5"><Label htmlFor="invite-email">{t(lang, "invite.email")}</Label><Input id="invite-email" type="email" name="email" autoComplete="off" placeholder="colleague@studio.ru" /></div><Button type="submit"><Link2 aria-hidden="true" />{t(lang, "team.generateInvite")}</Button><p className="text-xs text-muted-foreground sm:col-span-2">{t(lang, "invite.emailHint")}</p></form></CardContent></Card>
    <Card><CardHeader><CardTitle>{t(lang, "team.outstanding")} ({data.Invites.length})</CardTitle></CardHeader><CardContent className="grid gap-2">
      {data.Invites.length ? data.Invites.map(invite => <article key={invite.Token} className="grid gap-3 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto_auto_auto] sm:items-center"><div className="min-w-0">{invite.Live ? <a className="font-mono text-xs underline underline-offset-4" href={`/invites/${invite.Token}`}>{invite.Token.slice(0, 12)}…</a> : <span className="font-mono text-xs text-muted-foreground">{invite.Token.slice(0, 12)}…</span>}</div><span className={`w-fit rounded-md border px-2 py-1 text-xs ${invite.Expired ? "text-amber-700 dark:text-amber-300" : invite.Live ? "text-emerald-700 dark:text-emerald-300" : "text-muted-foreground"}`}>{t(lang, invite.Used ? "team.used" : invite.Expired ? "team.expired" : "team.live")}</span><div className="grid text-xs text-muted-foreground"><span>{t(lang, "team.created")}: {formatDate(invite.CreatedAt)}</span><span>{t(lang, "team.expires")}: {formatDate(invite.ExpiresAt)}</span></div><div className="flex justify-end">{invite.Live && <form method="post" action={`/api/team/invites/${invite.Token}/revoke`} data-confirm={t(lang, "team.confirmRevoke")}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="ghost" size="sm"><X aria-hidden="true" />{t(lang, "team.revoke")}</Button></form>}</div></article>) : <p className="text-sm text-muted-foreground">{t(lang, "team.noInvites")}</p>}
    </CardContent></Card>
  </main>
}
