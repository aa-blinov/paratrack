import { Disclosure, DisclosureTrigger } from "@/components/ui/collapsible"
import { BarChart3, CalendarDays, Clock3, Flag, Folder, Link2, Sparkles, Tag, Upload, UserRound, Users, Zap } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { SectionsData } from "@/dashboard/types"

const icons = { user: UserRound, zap: Zap, users: Users, clock: Clock3, flag: Flag, tag: Tag, "bar-chart-3": BarChart3, calendar: CalendarDays, link: Link2, download: Upload, folder: Folder, sparkles: Sparkles }

export function SectionsPage({ data }: { data: SectionsData }) {
  const lang = data.Lang || "en"
  // A set that matches no card was changed by hand. Onboarding starts from
  // "everything on", which is also no card, so only the settings page has to
  // explain the difference.
  const handPicked = !data.Welcome && !data.Presets.some(preset => preset.Active)
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    {data.Welcome ? <header className="grid gap-1"><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "welcome.title")}</h1><p className="text-sm text-muted-foreground">{t(lang, "welcome.blurb")}</p></header> : <><h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1><SettingsNav active={data.Active} lang={lang} canManage /></>}
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    <section className="grid gap-2" aria-labelledby="presets-title"><h2 id="presets-title" className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t(lang, "sections.presets")}</h2><div className="grid gap-2 sm:grid-cols-3">{data.Presets.map(preset => { const Icon = icons[preset.Icon as keyof typeof icons] || Sparkles; return <form key={preset.Key} method="post" action="/api/team/modules"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="preset" value={preset.Key} />{data.Welcome && <input type="hidden" name="from" value="welcome" />}<Button variant="ghost" type="submit" aria-current={preset.Active ? "true" : undefined} className={`grid h-full w-full justify-items-start whitespace-normal gap-2 rounded-md border p-4 text-left transition-colors hover:bg-muted/50 ${preset.Active ? "border-primary bg-muted/40" : "border-border"}`}><span className="flex items-center gap-2 font-semibold"><Icon aria-hidden="true" className="size-4" />{t(lang, preset.Title)}</span><span className="text-sm text-muted-foreground">{t(lang, preset.Blurb)}</span><span className="text-xs text-muted-foreground">{preset.Names.map(name => t(lang, name)).join(", ")}</span></Button></form> })}</div></section>
    {handPicked && <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/40 p-3"><p className="text-sm">{t(lang, "sections.handPicked")}</p><form method="post" action="/api/team/modules"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="preset" value="studio" /><Button type="submit" variant="outline" size="sm">{t(lang, "sections.restoreAll")}</Button></form></div>}
    <Disclosure className="rounded-lg border" open={!data.Welcome}><DisclosureTrigger className="cursor-pointer p-4 font-medium">{t(lang, "sections.custom")}</DisclosureTrigger><form method="post" action="/api/team/modules" className="grid gap-3 px-4 pb-4"><input type="hidden" name="csrf_token" value={data.CSRFToken} />{data.Welcome && <input type="hidden" name="from" value="welcome" />}<p className="text-sm text-muted-foreground">{t(lang, "sections.core")}</p><ul className="grid border-t sm:grid-cols-2 sm:gap-x-5">{data.Modules.map(module => { const Icon = icons[module.Icon as keyof typeof icons] || Sparkles; return <li key={module.Key} className="border-b"><label className="flex cursor-pointer items-start gap-3 py-3"><Checkbox name="modules" value={module.Key} defaultChecked={module.On} className="mt-0.5" /><span className="min-w-0"><span className="flex items-center gap-1.5 font-medium"><Icon aria-hidden="true" className="size-4" />{t(lang, module.Label)}</span><span className="block text-sm text-muted-foreground">{t(lang, module.Hint)}{module.Manage ? ` ${t(lang, "sections.managersNote")}` : ""}</span></span></label></li> })}</ul><Button type="submit" className="w-fit">{t(lang, "profile.save")}</Button></form></Disclosure>
    {data.Welcome && <p className="text-sm"><a href="/" className="underline underline-offset-4">{t(lang, "welcome.skip")}</a><span className="mt-1 block text-xs text-muted-foreground">{t(lang, "welcome.later")}</span></p>}
  </main>
}
