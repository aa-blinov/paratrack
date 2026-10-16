import { BarChart3, Folder, Link2, Settings, Timer, Users, Zap } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import type { HelpData } from "@/dashboard/types"

const topics = [
  { icon: Timer, title: "help.trackTitle", items: [["help.track1", "help.track1d"], ["help.track2", "help.track2d"], ["help.track3", "help.track3d"]] },
  { icon: BarChart3, title: "help.reviewTitle", items: [["help.review1", "help.review1d"], ["help.review2", "help.review2d"], ["help.review3", "help.review3d"], ["help.review4", "help.review4d"]] },
  { icon: Folder, title: "help.orgTitle", items: [["help.org1", "help.org1d"], ["help.org2", "help.org2d"], ["help.org3", "help.org3d"]] },
  { icon: Zap, title: "help.moneyTitle", items: [["help.money1", "help.money1d"], ["help.money2", "help.money2d"], ["help.money3", "help.money3d"]] },
  { icon: Users, title: "help.teamTitle", items: [["help.team1", "help.team1d"], ["help.team2", "help.team2d"]] },
  { icon: Link2, title: "help.ecoTitle", items: [["help.eco1", "help.eco1d"], ["help.eco2", "help.eco2d"], ["help.eco3", "help.eco3d"], ["help.eco4", "help.eco4d"]] },
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
  // A reader follows every link here, so a destination behind a manager-only
  // guard is left out rather than offered and refused.
  const reachable = destinations.filter(([, , , need]) => need !== "manage" || data.CanManage)
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <header><h1 className="mb-1 text-2xl font-semibold tracking-tight">{t(lang, "help.title")}</h1></header>
    <div className="columns-1 gap-4 lg:columns-2 [&>*]:mb-4 [&>*]:break-inside-avoid">{topics.map(topic => { const Icon = topic.icon; return <Card key={topic.title}><CardHeader><CardTitle className="flex items-center gap-2"><Icon aria-hidden="true" className="size-5" />{t(lang, topic.title)}</CardTitle></CardHeader><CardContent><dl className="grid gap-2 text-sm">{topic.items.map(([term, description]) => <div key={term}><dt className="font-medium">{t(lang, term)}</dt><dd className="text-muted-foreground">{t(lang, description)}</dd></div>)}</dl></CardContent></Card> })}
      <Card><CardHeader><CardTitle className="flex items-center gap-2"><Settings aria-hidden="true" className="size-5" />{t(lang, "help.whereTitle")}</CardTitle></CardHeader><CardContent><dl className="grid gap-2 text-sm">{reachable.map(([title, description, href]) => <div key={href}><dt><a href={href} className="inline-block py-1 font-medium underline underline-offset-4">{t(lang, title)}</a></dt><dd className="text-muted-foreground">{t(lang, description)}</dd></div>)}</dl></CardContent></Card>
    </div>
  </main>
}
