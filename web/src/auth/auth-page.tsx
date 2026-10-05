import { Activity, Mail, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { LoginForm } from "@/components/login-form"
import { translate as t } from "@/i18n"
import type { AuthPageData } from "@/dashboard/types"

function nextHref(path: string, next: string) {
  return next ? `${path}?next=${encodeURIComponent(next)}` : path
}

export function AuthPage({ data }: { data: AuthPageData }) {
  const lang = data.Lang || "en"
  if (data.AuthReact === "login") return <main className="flex min-h-svh w-full items-center justify-center bg-muted/30 px-4 py-10 sm:px-6">
    <div className="flex w-full max-w-sm flex-col gap-6">
      <a href="/" className="flex items-center justify-center gap-2 self-center font-medium" aria-label="paratrack">
        <span className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground"><Activity className="size-4" aria-hidden="true" /></span>
        <span className="text-lg tracking-tight">paratrack</span>
      </a>
      <LoginForm data={data} />
    </div>
  </main>
  return <main className="mx-auto mt-12 w-full max-w-md">
    <Card><CardContent className="grid gap-3 p-6">
      {data.AuthReact === "register" && <>
        <h1 className="text-xl font-semibold">{t(lang, "auth.createAccount")}</h1><p className="mb-1 text-sm text-muted-foreground">{t(lang, "auth.registerBlurb")}</p>
        {data.ErrorMsg && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{data.ErrorMsg}</p>}
        <form method="post" action="/api/register" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="next" value={data.Next} /><div className="grid gap-1.5"><Label htmlFor="name">{t(lang, "auth.displayName")}</Label><Input id="name" type="text" name="name" required minLength={1} autoComplete="name" defaultValue={data.Name} autoFocus /></div><div className="grid gap-1.5"><Label htmlFor="email">{t(lang, "auth.email")}</Label><Input id="email" type="email" name="email" required autoComplete="email" defaultValue={data.Email} /></div><div className="grid gap-1.5"><Label htmlFor="password">{t(lang, "auth.password")}</Label><Input id="password" type="password" name="password" required minLength={8} maxLength={72} autoComplete="new-password" /><p className="text-xs text-muted-foreground">{t(lang, "auth.passwordHint")}</p></div><Button type="submit" className="mt-1 w-full"><Plus aria-hidden="true" />{t(lang, "invite.createAccount")}</Button></form>
        <p className="mt-2 text-center text-sm text-muted-foreground">{t(lang, "auth.haveAccount")} <a href={nextHref("/login", data.Next)} className="underline underline-offset-4">{t(lang, "nav.login")}</a></p>
      </>}
      {data.AuthReact === "forgot-password" && <>
        <h1 className="text-xl font-semibold">{t(lang, "auth.resetPassword")}</h1><p className="mb-1 text-sm text-muted-foreground">{t(lang, "auth.resetBlurb")}</p>
        {data.ErrorMsg && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{data.ErrorMsg}</p>}{data.InfoMsg && <p role="status" className="rounded-md border p-3 text-sm">{data.InfoMsg}</p>}
        <form method="post" action="/api/password/forgot" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-1.5"><Label htmlFor="email">{t(lang, "auth.email")}</Label><Input id="email" type="email" name="email" required autoComplete="email" defaultValue={data.Email} autoFocus /></div><Button type="submit" className="mt-1 w-full"><Mail aria-hidden="true" />{t(lang, "auth.sendReset")}</Button></form><p className="mt-2 text-center text-sm"><a href="/login" className="underline underline-offset-4">{t(lang, "auth.backToSignIn")}</a></p>
      </>}
      {data.AuthReact === "reset-password" && <>
        <h1 className="text-xl font-semibold">{t(lang, "auth.choosePassword")}</h1><p className="mb-1 text-sm text-muted-foreground">{t(lang, "auth.resetOnce")}</p>
        {data.ErrorMsg && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{data.ErrorMsg}</p>}
        <form method="post" action="/api/password/reset" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="token" value={data.Token} /><div className="grid gap-1.5"><Label htmlFor="new_password">{t(lang, "auth.newPassword")}</Label><Input id="new_password" type="password" name="new_password" required minLength={8} maxLength={72} autoComplete="new-password" autoFocus /><p className="text-xs text-muted-foreground">{t(lang, "auth.passwordHint")}</p></div><Button type="submit" className="mt-1 w-full">{t(lang, "profile.updatePassword")}</Button></form>
      </>}
    </CardContent></Card>
  </main>
}
