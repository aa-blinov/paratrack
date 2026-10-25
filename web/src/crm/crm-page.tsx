import * as React from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { DisclosureSection } from "@/components/disclosure-section"
import { translate as t } from "@/i18n"
import type { CRMData } from "@/dashboard/types"
import { apiJSON, ApiError } from "@/lib/json-api"
import { formatDate } from "@/lib/date-format"
import { formatMoney } from "@/lib/money"
import { loadClients, loadMembers, type Client, type Member } from "@/lib/team-directory"
import type { Deal, Pipeline } from "@/crm/model"
import { DealForm } from "@/crm/deal-form"
import { DealDetail } from "@/crm/deal-detail"
import { ContactsPanel } from "@/crm/contacts-panel"
import { StageEditor } from "@/crm/stage-editor"
import { LeadsPanel } from "@/crm/leads-panel"
import { FunnelPanel } from "@/crm/funnel-panel"
import { ClientSearch } from "@/crm/client-search"

type DealsResponse = { today: string; deals: Deal[]; pipeline: Pipeline }

/**
 * The pipeline as a list grouped by stage. The stage tiles double as the stage
 * filter, and the overdue tile narrows the list to deals whose next step has
 * passed. Counts come from the server and ignore both filters.
 */
export function CRMPage({ data, viewerID }: { data: CRMData; viewerID: number }) {
  const lang = data.Lang
  const [deals, setDeals] = React.useState<Deal[]>([])
  const [pipeline, setPipeline] = React.useState<Pipeline>({ stages: [], overdue: 0 })
  const [stage, setStage] = React.useState("")
  const [overdueOnly, setOverdueOnly] = React.useState(false)
  const [selectedID, setSelectedID] = React.useState<number | null>(null)
  const [clients, setClients] = React.useState<Client[]>([])
  const [members, setMembers] = React.useState<Member[]>([])
  const [loaded, setLoaded] = React.useState(false)
  const [error, setError] = React.useState("")
  const [tab, setTab] = React.useState<"deals" | "leads" | "funnel">("deals")

  const clientNames = React.useMemo(() => new Map(clients.map(client => [client.id, client.name])), [clients])
  const total = pipeline.stages.reduce((sum, item) => sum + item.count, 0)

  const load = React.useCallback(async () => {
    const query = new URLSearchParams()
    if (stage) query.set("stage", stage)
    if (overdueOnly) query.set("overdue", "1")
    try {
      const body = await apiJSON<DealsResponse>(`/api/crm/deals?${query}`, "GET", "")
      setDeals(body.deals)
      setPipeline(body.pipeline)
      setError("")
    } catch (cause) {
      setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))
    } finally {
      setLoaded(true)
    }
  }, [stage, overdueOnly, lang])

  React.useEffect(() => { void load() }, [load])

  React.useEffect(() => {
    loadClients().then(setClients).catch(() => setError(t(lang, "err.generic")))
    if (data.CanManage) loadMembers().then(setMembers).catch(() => setError(t(lang, "err.generic")))
  }, [data.CanManage, lang])

  const groups = pipeline.stages
    .filter(item => !stage || item.stage === stage)
    .map(item => ({ stage: item, deals: deals.filter(deal => deal.stage === item.stage) }))
    .filter(group => group.deals.length > 0)

  const emptyText = loaded && deals.length === 0 ? (stage || overdueOnly ? t(lang, "crm.emptyFiltered") : t(lang, "crm.empty")) : ""

  return <main className="mx-auto w-full max-w-5xl space-y-5">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "nav.crm")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "crm.blurb")}</p></header>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    <ClientSearch lang={lang} />
    <div role="group" aria-label={t(lang, "crm.tabsLabel")} className="flex flex-wrap gap-2">
      <Button type="button" variant={tab === "deals" ? "secondary" : "outline"} aria-pressed={tab === "deals"} onClick={() => setTab("deals")}>{t(lang, "crm.tabDeals")}</Button>
      <Button type="button" variant={tab === "leads" ? "secondary" : "outline"} aria-pressed={tab === "leads"} onClick={() => setTab("leads")}>{t(lang, "crm.tabLeads")}</Button>
      <Button type="button" variant={tab === "funnel" ? "secondary" : "outline"} aria-pressed={tab === "funnel"} onClick={() => setTab("funnel")}>{t(lang, "crm.tabFunnel")}</Button>
    </div>
    {tab === "leads" ? <LeadsPanel lang={lang} csrf={data.CSRFToken} viewerID={viewerID} canManage={data.CanManage} members={members} /> : tab === "funnel" ? <FunnelPanel lang={lang} /> : <>
    <DisclosureSection title={t(lang, "crm.newDeal")}>
      <DealForm lang={lang} csrf={data.CSRFToken} viewerID={viewerID} canManage={data.CanManage} clients={clients} members={members} onSaved={() => void load()} />
    </DisclosureSection>
    <Card>
      <CardHeader><CardTitle>{t(lang, "crm.deals")}</CardTitle></CardHeader>
      <CardContent className="grid gap-5">
        <div role="group" aria-label={t(lang, "crm.deals")} className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
          <Button type="button" variant={!stage && !overdueOnly ? "secondary" : "outline"} aria-pressed={!stage && !overdueOnly} className="h-auto flex-col items-start gap-1 py-2" onClick={() => { setStage(""); setOverdueOnly(false) }}>
            <span className="text-xs font-normal text-muted-foreground">{t(lang, "crm.allStages")}</span><span className="font-mono text-base tabular-nums">{total}</span>
          </Button>
          {pipeline.stages.map(item => {
            const active = stage === item.stage
            return <Button key={item.stage} type="button" variant={active ? "secondary" : "outline"} aria-pressed={active} className="h-auto flex-col items-start gap-1 py-2" onClick={() => setStage(active ? "" : item.stage)}>
              <span className="text-xs font-normal text-muted-foreground">{item.name}</span><span className="font-mono text-base tabular-nums">{item.count}</span>
            </Button>
          })}
          <Button type="button" variant={overdueOnly ? "secondary" : "outline"} aria-pressed={overdueOnly} className="h-auto flex-col items-start gap-1 py-2" onClick={() => setOverdueOnly(value => !value)}>
            <span className="text-xs font-normal text-muted-foreground">{t(lang, "crm.overdue")}</span><span className="font-mono text-base tabular-nums">{pipeline.overdue}</span>
          </Button>
        </div>
        {emptyText && <p className="py-6 text-center text-sm text-muted-foreground">{emptyText}</p>}
        {groups.map(group => <section key={group.stage.stage} aria-labelledby={`stage-${group.stage.stage}`} className="grid gap-2">
          <h2 id={`stage-${group.stage.stage}`} className="flex items-center gap-2 text-sm font-semibold">{group.stage.name}<Badge variant="secondary">{group.deals.length}</Badge></h2>
          <ul className="grid divide-y rounded-lg border">
            {group.deals.map(deal => <li key={deal.id} className="px-3">
              <button type="button" aria-expanded={selectedID === deal.id} onClick={() => setSelectedID(current => current === deal.id ? null : deal.id)} className="grid w-full gap-1 py-3 text-left">
                <span className="flex flex-wrap items-center gap-2"><span className="min-w-0 font-medium [overflow-wrap:anywhere]">{deal.title}</span>{deal.overdue && <Badge variant="destructive">{t(lang, "crm.overdueBadge")}</Badge>}</span>
                <span className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
                  <span>{clientNames.get(deal.client_id) || t(lang, "crm.noClient")}</span>
                  {deal.amount_visible && (deal.amount_cents ?? 0) > 0 && <span className="font-mono tabular-nums text-foreground">{formatMoney(deal.amount_cents ?? 0, deal.currency ?? "", lang)}</span>}
                  <span>{deal.probability}%</span>
                  {deal.next_step_at && <span>{t(lang, "crm.nextStep")}: {formatDate(deal.next_step_at, lang)}</span>}
                </span>
              </button>
              {selectedID === deal.id && <div className="pb-4">
                <DealDetail deal={deal} stages={pipeline.stages} lang={lang} csrf={data.CSRFToken} viewerID={viewerID} canManage={data.CanManage} clients={clients} members={members} onChanged={() => void load()} />
              </div>}
            </li>)}
          </ul>
        </section>)}
      </CardContent>
    </Card>
    {data.CanManage && <StageEditor lang={lang} csrf={data.CSRFToken} stages={pipeline.stages} onChanged={() => void load()} />}
    <ContactsPanel lang={lang} csrf={data.CSRFToken} clients={clients} />
    </>}
  </main>
}
