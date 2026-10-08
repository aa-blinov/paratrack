import { LogIn, Plus, Users } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import type { InviteAcceptData } from "@/dashboard/types"

export function InviteAcceptPage({ data }: { data: InviteAcceptData }) {
  const lang = data.Lang || "en"
  const valid = Boolean(data.Team.ID)
  return <main className="mx-auto mt-12 w-full max-w-md">
    <Card><CardContent className="grid gap-3 p-6">
      {valid ? <>
        <h1 className="text-xl font-semibold">{data.Invite.Expired || data.Invite.Used ? data.Team.Name : `${t(lang, "invite.accept")} ${data.Team.Name}`}</h1>
        <p className="mb-1 text-sm text-muted-foreground max-w-[50ch]">{t(lang, "invite.blurb")}</p>
        {data.Invite.Expired ? <p role="status" className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">{t(lang, "invite.expired")}</p> : data.Invite.Used ? <p role="status" className="rounded-md border p-3 text-sm">{t(lang, "invite.used")}</p> : data.LoggedIn ? <>
          <form method="post" action={`/api/invites/${data.Token}/accept`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" className="w-full"><Users aria-hidden="true" />{t(lang, "invite.accept")} {data.Team.Name}</Button></form>
          <div className="text-center text-xs text-muted-foreground">{t(lang, "invite.joinedAsFull")} <span className="font-mono">{data.User.Email}</span>. {t(lang, "invite.switchAccountsFull")} <form method="post" action="/api/logout" className="inline"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><button type="submit" className="underline underline-offset-4">{t(lang, "invite.logout")}</button></form>.</div>
        </> : <><p className="text-sm text-muted-foreground">{t(lang, "invite.signInAccept")}</p><Button asChild className="w-full"><a href={`/login?next=/invites/${data.Token}`}><LogIn aria-hidden="true" />{t(lang, "nav.login")}</a></Button><Button asChild variant="ghost" className="w-full"><a href={`/register?next=/invites/${data.Token}`}><Plus aria-hidden="true" />{t(lang, "invite.createAccount")}</a></Button></>}
      </> : <><h1 className="text-xl font-semibold">{t(lang, "invite.notFound")}</h1><p className="text-sm text-muted-foreground">{t(lang, "invite.invalid")}</p><Button asChild className="mt-2 w-full"><a href="/login">{t(lang, "invite.backToLogin")}</a></Button></>}
    </CardContent></Card>
  </main>
}
