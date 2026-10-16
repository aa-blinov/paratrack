import { useTheme } from "@/lib/theme"
import { SegmentedControl } from "@/components/segmented"
import { Flash } from "@/components/flash"
import { useSaveStatus } from "@/lib/auto-save"
import * as React from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { PreferencesData } from "@/dashboard/types"

export function PreferencesPage({ data }: { data: PreferencesData }) {
  const lang = data.Lang || "en"
  const [selectedTabs, setSelectedTabs] = React.useState(() => data.TabOpts.filter(tab => tab.On).map(tab => tab.Key))
  // Every control on this page is a choice, so each one saves when it changes
  // and says so here. The form is posted whole: the server rewrites every field
  // from it, so a single change must carry the rest along unchanged.
  const formRef = React.useRef<HTMLFormElement>(null)
  const [saveState, saveForm] = useSaveStatus(t(lang, "toast.saved"), t(lang, "err.generic"))
  const saveNow = () => { if (formRef.current) void saveForm(formRef.current) }
  const toggleTab = (key: string, checked: boolean) => setSelectedTabs(current => checked ? [...current, key].slice(0, 4) : current.filter(item => item !== key))
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    <Flash message={data.Flash} ok={data.FlashOK} />
    <LanguageAndTheme lang={lang} />
    <p className="min-h-5 text-sm text-muted-foreground" role="status" aria-live="polite" data-pref-status>{saveState.kind === "error" ? saveState.text : saveState.kind === "saved" ? saveState.text : saveState.kind === "saving" ? t(lang, "prefs.saving") : ""}</p>
    <form ref={formRef} method="post" action="/api/me/preferences" className="grid gap-4" onChange={saveNow}>
      <input type="hidden" name="csrf_token" value={data.CSRFToken} />
      <div className="grid gap-4 lg:grid-cols-2 lg:items-start">
        <Card><CardHeader><CardTitle>{t(lang, "prefs.formats")}</CardTitle></CardHeader><CardContent className="grid gap-3">
          <div className="grid gap-1.5"><Label htmlFor="duration">{t(lang, "prefs.duration")}</Label><Select name="duration" defaultValue={data.P.Duration || "hm"}><SelectTrigger id="duration"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="hm">{t(lang, "prefs.dur.hm")}</SelectItem><SelectItem value="decimal">{t(lang, "prefs.dur.decimal")}</SelectItem><SelectItem value="clock">{t(lang, "prefs.dur.clock")}</SelectItem></SelectContent></Select></div>
          <div className="grid gap-1.5"><Label htmlFor="week-start">{t(lang, "prefs.weekStart")}</Label><Select name="week_start" defaultValue={data.P.WeekStart === "sun" ? "sun" : "mon"}><SelectTrigger id="week-start"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="mon">{t(lang, "prefs.mon")}</SelectItem><SelectItem value="sun">{t(lang, "prefs.sun")}</SelectItem></SelectContent></Select></div>
          <div className="grid gap-1.5"><Label htmlFor="timezone">{t(lang, "prefs.tz")}</Label><Select name="tz" defaultValue={data.P.TZ || "auto"}><SelectTrigger id="timezone"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="auto">{t(lang, "prefs.tzAuto")}</SelectItem>{data.Zones.map(zone => <SelectItem key={zone} value={zone}>{zone}</SelectItem>)}</SelectContent></Select><p className="text-xs text-muted-foreground">{t(lang, "prefs.tzHint")}</p></div>
        </CardContent></Card>
        <Card><CardHeader><CardTitle>{t(lang, "prefs.dashboard")}</CardTitle></CardHeader><CardContent className="grid gap-3">
          {data.Projects.length > 0 && <div className="grid gap-1.5"><Label htmlFor="default-project">{t(lang, "prefs.defProject")}</Label><Select name="default_project" defaultValue={String(data.DefProj || 0)}><SelectTrigger id="default-project"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="0">{t(lang, "prefs.defProjectLast")}</SelectItem>{data.Projects.map(project => <SelectItem key={project.ID} value={String(project.ID)}>{project.Name}</SelectItem>)}</SelectContent></Select></div>}
          <fieldset className="grid gap-1"><legend className="mb-1 text-sm font-medium">{t(lang, "prefs.blocks")}</legend>{data.Widgets.map(item => <label key={item.Key} className="flex min-h-11 cursor-pointer items-center gap-3 py-1.5 text-sm"><Checkbox name="widgets" value={item.Key} defaultChecked={item.On} /><span>{t(lang, item.Label)}</span></label>)}</fieldset>
        </CardContent></Card>
        {data.Sections.length > 0 && <Card><CardHeader><CardTitle>{t(lang, "prefs.sections")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "prefs.sectionsHint")}</p></CardHeader><CardContent className="grid gap-1">{data.Sections.map(item => <React.Fragment key={item.Key}><input type="hidden" name="sections_all" value={item.Key} /><label className="flex min-h-11 cursor-pointer items-center gap-3 py-1.5 text-sm"><Checkbox name="sections" value={item.Key} defaultChecked={item.On} /><span>{t(lang, item.Label)}</span></label></React.Fragment>)}</CardContent></Card>}
        <Card className={data.Sections.length === 0 ? "lg:col-span-2" : ""}><CardHeader><CardTitle>{t(lang, "prefs.tabs")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "prefs.tabsHint")}</p></CardHeader><CardContent><fieldset className="grid gap-x-4 sm:grid-cols-2"><legend className="sr-only">{t(lang, "prefs.tabs")}</legend>{data.TabOpts.map(item => <label key={item.Key} className="flex min-h-11 cursor-pointer items-center gap-3 py-1.5 text-sm has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-45"><Checkbox name="tabs" value={item.Key} checked={selectedTabs.includes(item.Key)} disabled={selectedTabs.length >= 4 && !selectedTabs.includes(item.Key)} onCheckedChange={checked => toggleTab(item.Key, checked === true)} /><span>{t(lang, item.Label)}</span></label>)}</fieldset></CardContent></Card>
      </div>
    </form>
  </main>
}

// Language and theme lived only in the menu, away from the page that is
// called «Интерфейс». The quick switches stay there; here they are spelled out.
function LanguageAndTheme({ lang }: { lang: string }) {
  const [mode, , choose] = useTheme()
  const back = encodeURIComponent("/settings/preferences")
  const cap = (text: string) => text.charAt(0).toLocaleUpperCase() + text.slice(1)
  return <Card><CardHeader><CardTitle>{t(lang, "prefs.langTheme")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "prefs.appliesNow")}</p></CardHeader><CardContent className="grid gap-4 sm:grid-cols-2">
    <div className="grid gap-1.5"><span className="text-sm font-medium">{t(lang, "nav.language")}</span>
      <SegmentedControl label={t(lang, "nav.language")} currentAs="true" value={lang === "en" ? "en" : "ru"} options={[
        { key: "ru", label: "Русский", href: `/lang/ru?next=${back}` },
        { key: "en", label: "English", href: `/lang/en?next=${back}` },
      ]} />
    </div>
    <div className="grid gap-1.5"><span className="text-sm font-medium">{t(lang, "nav.theme")}</span>
      <SegmentedControl label={t(lang, "nav.theme")} value={mode} options={[
        { key: "auto", label: cap(t(lang, "theme.auto")), onSelect: () => choose("auto") },
        { key: "light", label: cap(t(lang, "theme.light")), onSelect: () => choose("light") },
        { key: "dark", label: cap(t(lang, "theme.dark")), onSelect: () => choose("dark") },
      ]} />
    </div>
  </CardContent></Card>
}
