import * as React from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { PreferencesData } from "@/dashboard/types"

export function PreferencesPage({ data }: { data: PreferencesData }) {
  const lang = data.Lang || "en"
  const [selectedTabs, setSelectedTabs] = React.useState(() => data.TabOpts.filter(tab => tab.On).map(tab => tab.Key))
  const toggleTab = (key: string, checked: boolean) => setSelectedTabs(current => checked ? [...current, key].slice(0, 4) : current.filter(item => item !== key))
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    {data.Flash && <p role={data.FlashOK ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${data.FlashOK ? "border-border" : "border-destructive/40 text-destructive"}`}>{data.Flash}</p>}
    <form method="post" action="/api/me/preferences" className="grid gap-4">
      <input type="hidden" name="csrf_token" value={data.CSRFToken} />
      <div className="grid gap-4 lg:grid-cols-2 lg:items-start">
        <Card><CardHeader><CardTitle>{t(lang, "prefs.formats")}</CardTitle></CardHeader><CardContent className="grid gap-3">
          <div className="grid gap-1.5"><Label htmlFor="duration">{t(lang, "prefs.duration")}</Label><Select name="duration" defaultValue={data.P.Duration || "hm"}><SelectTrigger id="duration"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="hm">{t(lang, "prefs.dur.hm")}</SelectItem><SelectItem value="decimal">{t(lang, "prefs.dur.decimal")}</SelectItem><SelectItem value="clock">{t(lang, "prefs.dur.clock")}</SelectItem></SelectContent></Select></div>
          <div className="grid gap-1.5"><Label htmlFor="week-start">{t(lang, "prefs.weekStart")}</Label><Select name="week_start" defaultValue={data.P.WeekStart === "sun" ? "sun" : "mon"}><SelectTrigger id="week-start"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="mon">{t(lang, "prefs.mon")}</SelectItem><SelectItem value="sun">{t(lang, "prefs.sun")}</SelectItem></SelectContent></Select></div>
          <div className="grid gap-1.5"><Label htmlFor="timezone">{t(lang, "prefs.tz")}</Label><Select name="tz" defaultValue={data.P.TZ || "auto"}><SelectTrigger id="timezone"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="auto">{t(lang, "prefs.tzAuto")}</SelectItem>{data.Zones.map(zone => <SelectItem key={zone} value={zone}>{zone}</SelectItem>)}</SelectContent></Select><p className="text-xs text-muted-foreground">{t(lang, "prefs.tzHint")}</p></div>
        </CardContent></Card>
        <Card><CardHeader><CardTitle>{t(lang, "prefs.dashboard")}</CardTitle></CardHeader><CardContent className="grid gap-3">
          {data.Projects.length > 0 && <div className="grid gap-1.5"><Label htmlFor="default-project">{t(lang, "prefs.defProject")}</Label><Select name="default_project" defaultValue={String(data.DefProj || 0)}><SelectTrigger id="default-project"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="0">{t(lang, "prefs.defProjectLast")}</SelectItem>{data.Projects.map(project => <SelectItem key={project.ID} value={String(project.ID)}>{project.Name}</SelectItem>)}</SelectContent></Select></div>}
          <fieldset className="grid gap-1"><legend className="mb-1 text-sm font-medium">{t(lang, "prefs.blocks")}</legend>{data.Widgets.map(item => <label key={item.Key} className="flex cursor-pointer items-center gap-3 py-1.5 text-sm"><Checkbox name="widgets" value={item.Key} defaultChecked={item.On} /><span>{t(lang, item.Label)}</span></label>)}</fieldset>
        </CardContent></Card>
        {data.Sections.length > 0 && <Card><CardHeader><CardTitle>{t(lang, "prefs.sections")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "prefs.sectionsHint")}</p></CardHeader><CardContent className="grid gap-1">{data.Sections.map(item => <React.Fragment key={item.Key}><input type="hidden" name="sections_all" value={item.Key} /><label className="flex cursor-pointer items-center gap-3 py-1.5 text-sm"><Checkbox name="sections" value={item.Key} defaultChecked={item.On} /><span>{t(lang, item.Label)}</span></label></React.Fragment>)}</CardContent></Card>}
        <Card className={data.Sections.length === 0 ? "lg:col-span-2" : ""}><CardHeader><CardTitle>{t(lang, "prefs.tabs")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "prefs.tabsHint")}</p></CardHeader><CardContent><fieldset className="grid gap-x-4 sm:grid-cols-2"><legend className="sr-only">{t(lang, "prefs.tabs")}</legend>{data.TabOpts.map(item => <label key={item.Key} className="flex cursor-pointer items-center gap-3 py-1.5 text-sm has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-45"><Checkbox name="tabs" value={item.Key} checked={selectedTabs.includes(item.Key)} disabled={selectedTabs.length >= 4 && !selectedTabs.includes(item.Key)} onCheckedChange={checked => toggleTab(item.Key, checked === true)} /><span>{t(lang, item.Label)}</span></label>)}</fieldset></CardContent></Card>
      </div>
      <Button type="submit" className="w-fit">{t(lang, "profile.save")}</Button>
    </form>
  </main>
}
