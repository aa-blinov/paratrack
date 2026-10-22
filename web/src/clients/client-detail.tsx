import { Flash } from "@/components/flash"
import { ArrowLeft, FolderKanban, Receipt } from "lucide-react"
import type { ReactNode } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import type { ClientDetailData, ClientDetailInvoice, ClientDetailProject } from "@/dashboard/types"
import { ColorMark } from "@/components/color-mark"

function InvoiceStatus({ lang, status }: { lang: string; status: string }) {
  // Как в invoices-page: цвет статуса живёт в роли .doc-status, а variant
  // подкрашивает свой фон и перебивает её — поэтому остаётся secondary.
  return <Badge variant="secondary" className={`doc-status is-${status}`}>{t(lang, `status.${status}`)}</Badge>
}

function Fact({ label, value }: { label: string; value: string }) {
  return <div><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-1 font-mono text-base font-medium tabular-nums text-foreground">{value}</dd></div>
}

function ProjectRow({ lang, project }: { lang: string; project: ClientDetailProject }) {
  return <a href={`/projects/${encodeURIComponent(project.Slug)}`} className="group grid gap-2 border-b pb-3 last:border-0 last:pb-0 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:gap-4">
    <div className="flex min-w-0 items-center gap-2">
      <ColorMark color={project.Color} />
      <span className="min-w-0 truncate font-medium underline-offset-4 group-hover:underline" title={project.Name}>{project.Name}</span>
      {project.Archived && <Badge variant="secondary">{t(lang, "projects.archivedBadge")}</Badge>}
      {/* Перерасход — предупреждение, поэтому янтарный, а не разрушающий красный. */}
      {project.Over && <span className="shrink-0 text-xs font-medium text-warning-ink">{t(lang, "budget.over")}</span>}
    </div>
    {/* Денег нет, пока не задан потолок: пустой блок читался бы как ноль. */}
    <dl className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <div className="flex items-baseline gap-1"><dt>{t(lang, "clients.tracked")}</dt><dd className="font-mono tabular-nums text-foreground">{project.TrackedLabel}</dd></div>
      {project.BudgetLabel && <div className="flex items-baseline gap-1"><dt>{t(lang, "budget.amount")}</dt><dd className="font-mono tabular-nums text-foreground">{project.BudgetLabel}</dd></div>}
      {project.BudgetLabel && <div className="flex items-baseline gap-1"><dt>{t(lang, "budget.spentShort")}</dt><dd className="font-mono tabular-nums text-foreground">{project.SpentLabel}</dd></div>}
    </dl>
  </a>
}

function InvoiceRow({ lang, invoice }: { lang: string; invoice: ClientDetailInvoice }) {
  return <a href={`/invoices/${invoice.ID}`} className="group flex flex-wrap items-center gap-x-4 gap-y-2 border-b pb-3 last:border-0 last:pb-0">
    <span className="font-mono font-medium underline-offset-4 group-hover:underline">{invoice.Number}</span>
    <span className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{invoice.Period}</span>
    <span className="ms-auto flex items-center gap-3">
      <span className="font-mono tabular-nums">{invoice.TotalLabel}</span>
      <InvoiceStatus lang={lang} status={invoice.Status} />
    </span>
  </a>
}

// Inside a card the empty state reads like the card's body: left-aligned, with
// the next step beside the fact when there is one.
function EmptyBlock({ icon, text, hint }: { icon: ReactNode; text: string; hint?: string }) {
  return <Card><CardContent className="flex flex-col items-start gap-2 py-6 text-left">
    {icon}
    <p className="text-sm text-muted-foreground">{text}</p>
    {hint && <p className="text-sm text-muted-foreground">{hint}</p>}
  </CardContent></Card>
}

export function ClientDetailPage({ data }: { data: ClientDetailData }) {
  const lang = data.Lang
  const client = data.Client
  const projects = data.Projects ?? []
  const invoices = data.Invoices ?? []

  return <main className="mx-auto w-full max-w-5xl space-y-5">
    <header className="space-y-3">
      <Button asChild variant="outline" size="sm">
        <a href="/clients"><ArrowLeft aria-hidden="true" />{t(lang, "clients.heading")}</a>
      </Button>
      {/* Архивный клиент остаётся на экране, но приглушён: его проекты и счета
          ещё ссылаются на него, и спрятать карточку — значит потерять историю. */}
      <div className={client.Archived ? "opacity-60" : undefined}>
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="min-w-0 text-2xl font-semibold tracking-tight [overflow-wrap:anywhere]">{client.Name}</h1>
          {client.Archived && <Badge variant="secondary">{t(lang, "projects.archivedBadge")}</Badge>}
        </div>
        {client.Email && <p className="mt-1 text-sm text-muted-foreground [overflow-wrap:anywhere]">{client.Email}</p>}
      </div>
    </header>

    <Flash message={data.Flash} ok={data.FlashOK} />

    {data.CanManage && <p className="text-xs text-muted-foreground">{t(lang, "clients.allTimeCaption")}</p>}
    <dl className="grid grid-cols-2 gap-3 text-xs text-muted-foreground sm:grid-cols-4">
      <Fact label={t(lang, "clients.tracked")} value={client.TrackedLabel} />
      {/* Money is for the people who bill (D-2). */}
      {data.CanManage && <>
        <Fact label={t(lang, "clients.billed")} value={client.BilledLabel} />
        <Fact label={t(lang, "clients.unbilled")} value={client.UnbilledLabel} />
        <Fact label={t(lang, "clients.invoicedTotal")} value={client.BilledTotal} />
      </>}
    </dl>

    {/* Пустое поле не рисуем: заголовок без текста обесценивает сам факт его отсутствия. */}
    {(client.Details || client.Notes) && <Card><CardContent className="grid gap-4 p-5">
      {client.Details && <div><h2 className="text-xs font-semibold tracking-wide text-muted-foreground">{t(lang, "clients.details")}</h2><p className="mt-1 whitespace-pre-line text-sm [overflow-wrap:anywhere]">{client.Details}</p></div>}
      {client.Notes && <div><h2 className="text-xs font-semibold tracking-wide text-muted-foreground">{t(lang, "clients.notes")}</h2><p className="mt-1 whitespace-pre-line text-sm [overflow-wrap:anywhere]">{client.Notes}</p></div>}
    </CardContent></Card>}

    <section className="space-y-3">
      <h2 className="text-sm font-medium">{t(lang, "nav.projects")}</h2>
      {projects.length ? <Card><CardContent className="grid gap-3 p-5">
        {projects.map(project => <ProjectRow key={project.Slug} lang={lang} project={project} />)}
      </CardContent></Card> : <EmptyBlock icon={<FolderKanban aria-hidden="true" className="size-5 text-muted-foreground" />} text={t(lang, "projects.empty")} />}
    </section>

    {data.CanManage && data.InvoicesOn && <section className="space-y-3">
      <h2 className="text-sm font-medium">{t(lang, "inv.title")}</h2>
      {invoices.length ? <Card><CardContent className="grid gap-3 p-5">
        {invoices.map(invoice => <InvoiceRow key={invoice.ID} lang={lang} invoice={invoice} />)}
      </CardContent></Card> : <EmptyBlock icon={<Receipt aria-hidden="true" className="size-5 text-muted-foreground" />} text={t(lang, "inv.empty")} hint={t(lang, "clients.noInvoicesHint")} />}
    </section>}
  </main>
}
