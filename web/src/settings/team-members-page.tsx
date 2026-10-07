import { Crown, Trash2, UserRound } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { TeamMembersData } from "@/dashboard/types"

export function TeamMembersPage({ data }: { data: TeamMembersData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <header className="flex flex-wrap items-end justify-between gap-3"><h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>{data.CanManage && <Button asChild size="sm"><a href="/settings/invites">{t(lang, "set.tabInvites")}</a></Button>}</header>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    <Card><CardHeader><CardTitle>{t(lang, "team.members")} ({data.Members.length})</CardTitle></CardHeader><CardContent className="grid gap-2">
      {data.Members.length ? data.Members.map(member => {
        const pay = data.Pay[String(member.UserID)] || { Rate: "", Capacity: 0 }
        const ownerLocked = member.Role === "owner" && !data.IsOwner
        return <article key={member.UserID} className="grid min-w-0 gap-3 rounded-md border p-3 lg:grid-cols-[minmax(12rem,1.2fr)_minmax(10rem,1fr)_minmax(18rem,1.5fr)_auto] lg:items-center">
          <div className="min-w-0"><p className="flex items-center gap-1.5 font-medium"><UserRound className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" /><span className="truncate">{member.Name}</span></p><p className="truncate text-sm text-muted-foreground">{member.Email}</p></div>
          <div>{member.Role === "owner" ? <span className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs"><Crown className="size-3" aria-hidden="true" />{t(lang, "team.roleOwner")}</span> : data.IsOwner ? <form method="post" action={`/api/team/members/${member.UserID}/role`} className="flex items-center gap-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Select name="role" defaultValue={member.Role}><SelectTrigger aria-label={t(lang, "team.roleCol")} className="h-8"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="member">{t(lang, "team.roleMember")}</SelectItem><SelectItem value="admin">{t(lang, "team.roleAdmin")}</SelectItem></SelectContent></Select><Button type="submit" size="sm" variant="outline">{t(lang, "projects.save")}</Button></form> : <span className="text-sm text-muted-foreground">{member.Role === "admin" ? t(lang, "team.roleAdmin") : t(lang, "team.roleMember")}</span>}</div>
          {ownerLocked ? <div className="flex gap-4 text-sm"><span>{t(lang, "pay.payCol")}: {pay.Rate || "–"}</span><span>{t(lang, "pay.capCol")}: {pay.Capacity || "–"}</span></div> : <form method="post" action="/api/member/pay" className="grid grid-cols-2 items-end gap-2 sm:grid-cols-[minmax(5rem,1fr)_minmax(5rem,0.8fr)_auto]"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="user_id" value={member.UserID} /><div className="grid gap-1"><Label htmlFor={`pay-${member.UserID}`} className="text-xs">{t(lang, "pay.payCol")}</Label><Input id={`pay-${member.UserID}`} name="hourly_pay" inputMode="decimal" autoComplete="off" defaultValue={pay.Rate} placeholder="0" className="h-8" /></div><div className="grid gap-1"><Label htmlFor={`capacity-${member.UserID}`} className="text-xs">{t(lang, "pay.capCol")}</Label><Input id={`capacity-${member.UserID}`} type="number" name="capacity_minutes" min="0" step="30" defaultValue={pay.Capacity || ""} placeholder="480" className="h-8" /></div><Button type="submit" size="sm" variant="ghost" className="col-span-2 justify-center sm:col-span-1 sm:justify-start">{t(lang, "projects.save")}</Button></form>}
          <div className="flex justify-end">{member.UserID !== data.User.ID ? <form method="post" action={`/api/team/members/${member.UserID}/remove`} data-confirm={t(lang, "team.confirmRemove")}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" size="sm" variant="ghost"><Trash2 aria-hidden="true" />{t(lang, "team.remove")}</Button></form> : <span className="text-xs text-muted-foreground">{t(lang, "team.you")}</span>}</div>
        </article>
      }) : <p className="text-sm text-muted-foreground">{t(lang, "team.noMembers")}</p>}
    </CardContent></Card>
    {data.IsOwner && data.Members.length > 1 && <Card><CardHeader><CardTitle>{t(lang, "team.transferTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.transferHint")}</p></CardHeader><CardContent><form method="post" action="/api/team/transfer" data-confirm={t(lang, "team.transferConfirm")} className="flex flex-wrap items-end gap-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid min-w-[12rem] flex-1 gap-1.5"><Label htmlFor="transfer-member">{t(lang, "team.transferPick")}</Label><Select name="user_id" required><SelectTrigger id="transfer-member"><SelectValue placeholder="…" /></SelectTrigger><SelectContent>{data.Members.filter(member => member.UserID !== data.User.ID).map(member => <SelectItem key={member.UserID} value={String(member.UserID)}>{member.Name} ({member.Email})</SelectItem>)}</SelectContent></Select></div><Button type="submit" variant="outline" className="border-destructive text-destructive">{t(lang, "team.transferBtn")}</Button></form></CardContent></Card>}
  </main>
}
