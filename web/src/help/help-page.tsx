import * as React from "react"
import { Keyboard, Search, Settings, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t, translateOptional } from "@/i18n"
import type { HelpData } from "@/dashboard/types"
import { matchesHelpQuery } from "@/help/search"

// One entry per screen. The copy is in help.g.<id>.*; the href opens the screen.
// A screen names the section it needs (the menu hides it when the section is off),
// and a manager-only screen is left out for a member: the reader follows every link.
const guides: ReadonlyArray<{ id: string; href: string; section?: string; manage?: boolean }> = [
  { id: "dashboard", href: "/" },
  { id: "projects", href: "/projects" },
  { id: "clients", href: "/clients" },
  { id: "timesheet", href: "/timesheet" },
  { id: "stats", href: "/stats" },
  { id: "graph", href: "/graph", section: "graph" },
  { id: "goals", href: "/goals", section: "goals" },
  { id: "tags", href: "/tags", section: "tags" },
  { id: "invoices", href: "/invoices", section: "invoices", manage: true },
  { id: "payroll", href: "/payroll", section: "payroll", manage: true },
  { id: "reports", href: "/reports", section: "reports", manage: true },
  { id: "schedule", href: "/schedule", section: "schedule" },
  { id: "integrations", href: "/integrations", section: "integrations" },
  { id: "import", href: "/import", section: "import" },
  { id: "export", href: "/export" },
  { id: "settings-me", href: "/settings/profile" },
  { id: "settings-workspace", href: "/settings/sections", manage: true },
] as const

const faqNumbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] as const

const keys = [
  ["N", "help.keyN"], ["S", "help.keyS"], ["G", "help.keyG"], ["T", "help.keyT"], ["P", "help.keyP"],
] as const

const destinations = [
  ["team.currencyTitle", "help.where6", "/settings/money#currency", "manage"],
  ["bill.prefix", "help.where7", "/settings/money#billing", "manage"],
  ["team.reqTitle", "help.where8", "/settings/money#requisites", "manage"],
  ["bill.logo", "help.where9", "/settings/money#logo", "manage"],
  ["stripe.title", "help.where10", "/settings/money#stripe", "manage"],
  ["set.tabInvites", "help.where11", "/settings/invites", "manage"],
  ["nav.sections", "help.where12", "/settings/sections", "manage"],
  ["sheet.notifications", "help.where4", "/settings/notifications"],
  ["set.tabTokens", "help.where1", "/settings/tokens"],
  ["set.tabWebhooks", "help.where3", "/settings/webhooks", "manage"],
  ["nav.integrations", "help.where2", "/integrations"],
  ["nav.timesheet", "help.where13", "/timesheet"],
  ["nav.goals", "help.where14", "/goals"],
  ["nav.tags", "help.where15", "/tags"],
  ["nav.clients", "help.where16", "/clients"],
  ["nav.payroll", "help.where17", "/payroll", "manage"],
  ["nav.reports", "help.where18", "/reports", "manage"],
  ["nav.csvExport", "help.where19", "/export"],
  ["nav.import", "help.where5", "/import"],
] as const

export function HelpPage({ data }: { data: HelpData }) {
  const lang = data.Lang || "en"
  const [query, setQuery] = React.useState("")
  const mods = data.Mods || {}
  const searching = query.trim() !== ""

  const guideEntries = guides
    .filter(guide => (!guide.manage || data.CanManage) && (!guide.section || mods[guide.section]))
    .map(guide => {
      const name = t(lang, `help.g.${guide.id}.name`)
      const what = t(lang, `help.g.${guide.id}.what`)
      const steps = (["s1", "s2", "s3"] as const).map(step => translateOptional(lang, `help.g.${guide.id}.${step}`) || "").filter(Boolean)
      return { guide, name, what, steps, text: [name, what, ...steps].join(" ") }
    })
    .filter(entry => matchesHelpQuery(entry.text, query))

  const faqEntries = faqNumbers
    .map(n => ({ n, q: t(lang, `help.faq.${n}.q`), a: t(lang, `help.faq.${n}.a`) }))
    .filter(entry => matchesHelpQuery(`${entry.q} ${entry.a}`, query))

  // A reader follows every link here, so a destination behind a manager-only
  // guard is left out rather than offered and refused.
  const whereEntries = destinations
    .filter(([, , , need]) => need !== "manage" || data.CanManage)
    .filter(([title, description]) => matchesHelpQuery(`${t(lang, title)} ${t(lang, description)}`, query))

  const keyEntries = keys.filter(([letter, key]) => matchesHelpQuery(`${letter} ${t(lang, key)}`, query))

  const found = guideEntries.length + faqEntries.length + whereEntries.length + keyEntries.length

  return <main className="mx-auto grid w-full max-w-6xl gap-6">
    <header className="grid gap-2">
      <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "help.title")}</h1>
      <p className="text-sm text-muted-foreground">{t(lang, "help.subtitle")}</p>
    </header>

    <div role="search" className="grid gap-2">
      <Label htmlFor="help-search">{t(lang, "help.searchLabel")}</Label>
      <div className="relative flex items-center gap-2">
        <Search aria-hidden="true" className="pointer-events-none absolute start-3 size-4 text-muted-foreground" />
        <Input id="help-search" type="search" autoComplete="off" value={query} placeholder={t(lang, "help.searchPlaceholder")} className="ps-9" onChange={event => setQuery(event.target.value)} />
        {searching && <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "help.clearSearch")} onClick={() => setQuery("")}><X aria-hidden="true" /></Button>}
      </div>
      <p role="status" className="text-sm text-muted-foreground">{searching ? (found ? `${t(lang, "help.found")}: ${found}` : t(lang, "help.noResults")) : ""}</p>
    </div>

    {guideEntries.length > 0 && <section aria-labelledby="help-guides" className="grid gap-3">
      <h2 id="help-guides" className="text-lg font-semibold">{t(lang, "help.guidesTitle")}</h2>
      <div className="grid gap-3 sm:grid-cols-2">{guideEntries.map(({ guide, name, what, steps }) => <Card key={guide.id}><CardContent className="grid gap-3 p-4">
        <div className="flex items-start justify-between gap-3"><h3 className="font-medium">{name}</h3><Button asChild variant="outline" size="sm"><a href={guide.href}>{t(lang, "help.open")}</a></Button></div>
        <p className="text-sm text-muted-foreground">{what}</p>
        {steps.length > 0 && <ol className="grid list-decimal gap-1 ps-5 text-sm">{steps.map(step => <li key={step}>{step}</li>)}</ol>}
      </CardContent></Card>)}</div>
    </section>}

    {faqEntries.length > 0 && <section aria-labelledby="help-faq" className="grid gap-3">
      <h2 id="help-faq" className="text-lg font-semibold">{t(lang, "help.faqTitle")}</h2>
      <div className="grid gap-2">{faqEntries.map(({ n, q, a }) => <details key={n} className="group rounded-xl border bg-card p-4">
        <summary className="cursor-pointer list-none font-medium marker:hidden">{q}</summary>
        <p className="mt-2 text-sm text-muted-foreground">{a}</p>
      </details>)}</div>
    </section>}

    {/* The index repeats the screen cards, so it starts closed; a search opens it where the match is. */}
    {whereEntries.length > 0 && <details open={searching} className="rounded-xl border bg-card p-4">
      <summary className="flex cursor-pointer list-none items-center gap-2 font-medium marker:hidden"><Settings aria-hidden="true" className="size-5" />{t(lang, "help.whereTitle")}</summary>
      <dl className="mt-4 grid gap-2 text-sm">{whereEntries.map(([title, description, href]) => <div key={href}><dt><a href={href} className="inline-block py-1 font-medium underline underline-offset-4">{t(lang, title)}</a></dt><dd className="text-muted-foreground">{t(lang, description)}</dd></div>)}</dl>
    </details>}

    {keyEntries.length > 0 && <Card><CardHeader><CardTitle className="flex items-center gap-2"><Keyboard aria-hidden="true" className="size-5" />{t(lang, "help.keysTitle")}</CardTitle></CardHeader><CardContent><dl className="grid gap-2 text-sm sm:grid-cols-2">{keyEntries.map(([letter, key]) => <div key={letter} className="flex items-center gap-3"><dt><kbd className="rounded-md border bg-muted px-2 py-0.5 font-mono text-xs">{letter}</kbd></dt><dd className="text-muted-foreground">{t(lang, key)}</dd></div>)}</dl></CardContent></Card>}
  </main>
}
