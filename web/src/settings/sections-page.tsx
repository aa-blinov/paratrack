import { StatusLine } from "@/components/status-line"
import * as React from "react"
import { useSaveStatus } from "@/lib/auto-save"
import { Flash } from "@/components/flash"
import { Disclosure, DisclosureTrigger } from "@/components/ui/collapsible"
import { BarChart3, CalendarDays, ChevronDown, Clock3, Flag, Folder, Link2, Sparkles, Tag, Upload, UserRound, Users, Zap } from "lucide-react"
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
  // On the settings page a choice saves at once: a preset card or a tick in the
  // list posts the form whole and says so in the status line. Onboarding keeps
  // its own «Continue» button, because there the choice is made once.
  const listRef = React.useRef<HTMLFormElement>(null)
  const [saveState, saveForm] = useSaveStatus(t(lang, "toast.saved"), t(lang, "err.generic"))
  const saveList = () => { if (listRef.current) void saveForm(listRef.current) }
  const presetsRef = React.useRef<HTMLElement>(null)
  React.useEffect(() => {
    if (data.Welcome) return
    const onSubmit = (event: Event) => {
      const form = (event.target as HTMLElement).closest("form")
      if (!form || !presetsRef.current?.contains(form)) return
      event.preventDefault()
      // A preset button is the submitter: its name/value must reach the server.
      const submitter = (event as SubmitEvent).submitter as HTMLButtonElement | null
      // The answer is the page with the saved set: show it in place, so the list
      // and the cards read what was saved, not what was on screen before.
      void saveForm(form, submitter).then(result => {
        if (result.url) window.dispatchEvent(new CustomEvent("paratrack:navigate", { detail: { url: result.url, replace: true } }))
      })
    }
    document.addEventListener("submit", onSubmit, true)
    return () => document.removeEventListener("submit", onSubmit, true)
  }, [data.Welcome, saveForm])
  return <main ref={presetsRef} className="mx-auto grid w-full max-w-6xl gap-4">
    {data.Welcome ? <header className="grid gap-1"><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "welcome.title")}</h1><p className="text-sm text-muted-foreground">{t(lang, "welcome.blurb")}</p></header> : <><h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1><SettingsNav active={data.Active} lang={lang} canManage /></>}
    <Flash message={data.Flash} ok={data.FlashOK} />
    <section className="grid gap-2" aria-labelledby="presets-title"><h2 id="presets-title" className="text-xs font-semibold tracking-wide text-muted-foreground">{t(lang, "sections.presets")}</h2><div className="grid gap-2 sm:grid-cols-3">{data.Presets.map(preset => { const Icon = icons[preset.Icon as keyof typeof icons] || Sparkles; return <form key={preset.Key} method="post" action="/api/team/modules" className="flex"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="preset" value={preset.Key} />{data.Welcome && <input type="hidden" name="from" value="welcome" />}<Button data-card variant="ghost" type="submit" aria-current={preset.Active ? "true" : undefined} className={`grid h-full w-full justify-items-start whitespace-normal gap-2 rounded-xl border p-4 text-left transition-colors hover:bg-muted/50 ${preset.Active ? "border-primary bg-muted/40" : "border-border"}`}><span className="flex items-center gap-2 font-semibold"><Icon aria-hidden="true" className="size-4" />{t(lang, preset.Title)}</span><span className="text-sm text-muted-foreground">{t(lang, preset.Blurb)}</span><span className="text-xs text-muted-foreground">{preset.Names.map(name => t(lang, name)).join(", ")}</span>{preset.Off.length > 0 && <span className="mt-1 block text-xs font-normal text-muted-foreground">{t(lang, "sections.presetOff", preset.Off.join(", "))}</span>}{preset.Hint && <span className="mt-1 block text-xs font-normal text-muted-foreground">{preset.Hint}</span>}</Button></form> })}</div></section>
    {handPicked && <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/40 p-3"><p className="text-sm">{t(lang, "sections.handPicked")}</p><form method="post" action="/api/team/modules"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="preset" value="studio" /><Button type="submit" variant="outline" size="sm">{t(lang, "sections.restoreAll")}</Button></form></div>}
    <Disclosure className="group rounded-lg border" open={!data.Welcome}><DisclosureTrigger className="flex cursor-pointer items-center justify-between gap-2 p-4 font-medium">{t(lang, "sections.custom")}<ChevronDown aria-hidden="true" className="size-4 shrink-0 transition-transform group-data-[state=open]:rotate-180" /></DisclosureTrigger><form ref={listRef} method="post" action="/api/team/modules" className="grid gap-3 px-4 pb-4" onChange={data.Welcome ? undefined : saveList}><input type="hidden" name="csrf_token" value={data.CSRFToken} />{data.Welcome && <input type="hidden" name="from" value="welcome" />}<p className="text-sm text-muted-foreground">{t(lang, "sections.core")}</p><ul className="grid border-t sm:grid-cols-2 sm:gap-x-5">{data.Modules.map(module => { const Icon = icons[module.Icon as keyof typeof icons] || Sparkles; return <li key={module.Key} className="border-b"><label className="flex cursor-pointer items-start gap-3 py-3"><Checkbox name="modules" value={module.Key} defaultChecked={module.On} className="mt-0.5" /><span className="min-w-0"><span className="flex items-center gap-1.5 font-medium"><Icon aria-hidden="true" className="size-4" />{t(lang, module.Label)}</span><span className="block text-sm text-muted-foreground">{t(lang, module.Hint)}{module.Manage ? ` ${t(lang, "sections.managersNote")}` : ""}</span></span></label></li> })}</ul><StatusLine text={saveState.kind === "saving" ? t(lang, "prefs.saving") : saveState.kind === "saved" ? saveState.text : saveState.kind === "error" ? saveState.text : ""} />{data.Welcome && <Button type="submit" className="justify-self-start">{t(lang, "welcome.saveCustom")}</Button>}</form></Disclosure>
    {data.Welcome && <div className="text-sm">
      {/* Skipping used to leave every module on, so a newcomer who wanted only
          their own time landed in the studio set: eighteen items and a blurb
          about a team. Skipping now means what most skippers meant. */}
      <form method="post" action="/api/team/modules" data-welcome-skip><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="preset" value="solo" /><input type="hidden" name="from" value="welcome" /><Button type="submit" variant="link" className="h-auto p-0 align-baseline text-sm underline underline-offset-4">{t(lang, "welcome.skip")}</Button></form>
      <span className="mt-1 block text-xs text-muted-foreground">{t(lang, "welcome.later")}</span></div>}
  </main>
}
