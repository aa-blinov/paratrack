import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { ProfileData } from "@/dashboard/types"

export function ProfilePage({ data }: { data: ProfileData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    <Card><CardHeader><CardTitle>{t(lang, "profile.title")}</CardTitle></CardHeader><CardContent>
      <form method="post" action="/api/profile" className="grid max-w-xl gap-3">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="grid gap-1.5"><Label htmlFor="name">{t(lang, "auth.displayName")}</Label><Input id="name" name="name" required minLength={1} defaultValue={data.User.Name} /></div>
        <Button type="submit" className="w-fit">{t(lang, "projects.save")}</Button>
      </form>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>{t(lang, "profile.changeEmail")}</CardTitle><CardDescription>{t(lang, "profile.changeEmailHint")}</CardDescription></CardHeader><CardContent>
      <form method="post" action="/api/profile/email" className="grid max-w-xl gap-3">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="grid gap-1.5"><Label htmlFor="email">{t(lang, "auth.email")}</Label><Input id="email" name="email" type="email" autoComplete="email" required defaultValue={data.User.Email} /></div>
        <div className="grid gap-1.5"><Label htmlFor="email_current_password">{t(lang, "profile.currentPassword")}</Label><Input id="email_current_password" type="password" name="current_password" autoComplete="current-password" required /></div>
        <Button type="submit" className="w-fit">{t(lang, "profile.updateEmail")}</Button>
      </form>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>{t(lang, "profile.changePassword")}</CardTitle></CardHeader><CardContent>
      <form method="post" action="/api/profile/password" className="grid max-w-xl gap-3">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="grid gap-1.5"><Label htmlFor="current_password">{t(lang, "profile.currentPassword")}</Label><Input id="current_password" type="password" name="current_password" autoComplete="current-password" required /></div>
        <div className="grid gap-1.5"><Label htmlFor="new_password">{t(lang, "auth.newPassword")}</Label><Input id="new_password" type="password" name="new_password" minLength={8} maxLength={72} autoComplete="new-password" required /></div>
        <Button type="submit" className="w-fit">{t(lang, "profile.updatePassword")}</Button>
      </form>
    </CardContent></Card>
  </main>
}
