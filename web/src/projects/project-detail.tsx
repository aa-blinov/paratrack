import * as React from "react"
import { ArrowLeft, Play, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { DisclosureSection } from "@/components/disclosure-section"
import { translate as t } from "@/i18n"
import type { ProjectDetailData } from "@/dashboard/types"
import { ColorMark } from "@/components/color-mark"

function SummaryCard({ title, value }: { title: string; value: string }) {
  return <Card><CardContent className="space-y-2 p-5"><h3 className="text-xs font-semibold tracking-wide text-muted-foreground">{title}</h3><p className="font-mono text-3xl tabular-nums">{value}</p></CardContent></Card>
}

// Пустое значение client_id = «без клиента», а Radix-селект не умеет держать
// пустое значение — поэтому служебный пункт, который в форму уходит как "".
const noClient = "__no_client__"

function BudgetCard({ data }: { data: ProjectDetailData }) {
  const lang = data.Lang
  const budget = data.Budget
  // Бэкенд дописывается параллельно: без блока бюджета показывать нечего.
  if (!budget) return null

  return <Card>
    <CardHeader>
      <CardTitle>{t(lang, "budget.title")}</CardTitle>
      <p className="text-sm text-muted-foreground">{t(lang, "budget.hint")}</p>
    </CardHeader>
    <CardContent className="grid gap-4">
      {budget.HasBudget ? <dl className="grid grid-cols-2 gap-3 text-xs text-muted-foreground">
        <div><dt>{t(lang, "budget.amount")}</dt><dd className="mt-1 font-mono text-base font-medium tabular-nums text-foreground">{budget.BudgetLabel}</dd></div>
        {/* Источник валюты — отдельной строкой под суммой. Разделитель «·» перед подписью выглядел как случайный символ и переносился по словам. */}
        <div><dt>{t(lang, "budget.spent")}</dt><dd className="mt-1 font-mono text-base font-medium tabular-nums text-foreground">{budget.SpentLabel}</dd>{budget.CurrencySourceLabel && <p className="mt-0.5 font-normal text-muted-foreground">{budget.CurrencySourceLabel}</p>}</div>
        {/* Перерасход — предупреждение, поэтому янтарный, а не разрушающий красный. */}
        <div><dt>{t(lang, budget.Over ? "budget.overBy" : "budget.remaining")}</dt><dd className={`mt-1 font-mono text-base font-medium tabular-nums ${budget.Over ? "text-warning-ink" : "text-positive-ink"}`}>{budget.Over ? budget.OverByLabel : budget.RemainingLabel}</dd></div>
        <div><dt>{t(lang, "inv.unbilled")}</dt><dd className="mt-1 font-mono text-base font-medium tabular-nums text-foreground">{budget.UnbilledLabel}</dd></div>
      </dl> : <p className="text-sm text-muted-foreground">{t(lang, "budget.unset")}</p>}
      {!budget.HasRate && <p className="text-sm text-muted-foreground">{t(lang, "budget.noRate")}</p>}
      {budget.Over && <p role="status" className="rounded-md border border-warning/40 bg-warning/5 p-3 text-sm text-warning-ink">{t(lang, "budget.over")}</p>}
    </CardContent>
  </Card>
}

function ProjectEdit({ data }: { data: ProjectDetailData }) {
  const [color, setColor] = React.useState(data.Project.Color)
  const [currency, setCurrency] = React.useState(data.Currency || "")
  const inheritValue = "__workspace_currency__"
  const [clientID, setClientID] = React.useState(data.Client && data.Client.ID > 0 ? String(data.Client.ID) : noClient)
  const budgetRef = React.useRef<HTMLInputElement>(null)
  // Кнопке «Убрать бюджет» надо отправить пустой budget. Кнопка с name="budget"
  // не годится: сервер берёт первое значение, а первым идёт поле формы.
  // Поэтому чистим поле и сабмитим ту же форму — остальные поля уходят как есть.
  function clearBudget() {
    if (budgetRef.current) budgetRef.current.value = ""
    budgetRef.current?.form?.requestSubmit()
  }
  // Клиент может уйти в архив и выпасть из списка — тогда его имя всё равно
  // должно быть видно в селекте, иначе поле выглядит пустым.
  const clientOptions = [...(data.ClientOptions ?? [])]
  if (data.Client && data.Client.ID > 0 && !clientOptions.some(option => option.ID === data.Client?.ID)) {
    clientOptions.unshift({ ID: data.Client.ID, Name: data.Client.Name })
  }

  return <DisclosureSection id="project-settings" title={t(data.Lang, "projects.edit")} description={t(data.Lang, "projects.settingsHint")}><div className="space-y-5">
      <form method="POST" action={`/projects/${encodeURIComponent(data.Project.Slug)}`} className="grid gap-4 md:grid-cols-2">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="space-y-2"><Label htmlFor="project-name">{t(data.Lang, "projects.name")}</Label><Input id="project-name" name="name" defaultValue={data.Project.Name} placeholder={t(data.Lang, "projects.name")} required /></div>

        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">{t(data.Lang, "projects.color")}</legend>
          <div className="flex items-center gap-2">
            <Input type="color" aria-label={t(data.Lang, "projects.colorPicker")} value={color} onChange={event => setColor(event.target.value)} className="size-10 cursor-pointer rounded-md border border-input bg-background p-1" />
            <Input name="color" aria-label={t(data.Lang, "projects.colorHex")} value={color} onChange={event => setColor(event.target.value)} pattern="^#[0-9a-fA-F]{6}$" className="font-mono" required />
          </div>
        </fieldset>

        <div className="flex items-start gap-2">
          <Checkbox id="project-archived" name="archived" value="1" defaultChecked={data.Archived} />
          <Label htmlFor="project-archived" className="font-normal">{t(data.Lang, "projects.archivedLabel")} <span className="text-muted-foreground">({t(data.Lang, "projects.archivedHint")})</span></Label>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="project-estimate">{t(data.Lang, "projects.estimate")}</Label>
          <p className="text-xs text-muted-foreground">{t(data.Lang, "projects.estimateHint")}</p>
          <Input id="project-estimate" type="number" name="estimate_minutes" min="0" step="30" defaultValue={data.EstimateInput} placeholder="480" />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="project-rate">{t(data.Lang, "bill.rate")} {t(data.Lang, "bill.cents")}</Label>
          <p className="text-xs text-muted-foreground">{t(data.Lang, "bill.rateHint")}</p>
          <Input id="project-rate" type="text" name="rate" inputMode="decimal" autoComplete="off" defaultValue={data.RateInput} placeholder={t(data.Lang, "bill.ratePh")} />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="project-budget">{t(data.Lang, "budget.amount")}</Label>
          <p className="text-xs text-muted-foreground">{t(data.Lang, "budget.hint")}</p>
          <Input id="project-budget" ref={budgetRef} type="text" name="budget" inputMode="decimal" autoComplete="off" defaultValue={data.Budget?.BudgetInput ?? ""} placeholder={t(data.Lang, "budget.amountPlaceholder")} />
          {data.Budget?.HasBudget && <Button type="button" variant="ghost" size="sm" onClick={clearBudget}>{t(data.Lang, "budget.clear")}</Button>}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="project-currency">{t(data.Lang, "proj.currency")}</Label>
          <input type="hidden" name="currency" value={currency} />
          <Select value={currency || inheritValue} onValueChange={value => setCurrency(value === inheritValue ? "" : value)}>
            <SelectTrigger id="project-currency" aria-label={t(data.Lang, "proj.currency")} className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={inheritValue}>{t(data.Lang, "proj.currencyInherit")} ({data.TeamCurrency})</SelectItem>
              {data.Currencies.map(option => <SelectItem key={option.Code} value={option.Code}>{option.Label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>

        <div className="flex items-center gap-2">
          <Checkbox id="project-billable" name="billable" value="1" defaultChecked={data.Project.Billable} />
          <Label htmlFor="project-billable" className="font-normal">{t(data.Lang, "bill.billable")}</Label>
        </div>

        {!!clientOptions.length && <div className="space-y-1.5">
          <Label htmlFor="project-client">{t(data.Lang, "clients.heading")}</Label>
          <input type="hidden" name="client_id" value={clientID === noClient ? "" : clientID} />
          <Select value={clientID} onValueChange={value => setClientID(value === noClient ? "" : value)}>
            <SelectTrigger id="project-client" aria-label={t(data.Lang, "clients.heading")} className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={noClient}>{t(data.Lang, "project.noClient")}</SelectItem>
              {clientOptions.map(option => <SelectItem key={option.ID} value={String(option.ID)}>{option.Name}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>}
        <Button type="submit" className="w-full md:col-span-2">{t(data.Lang, "projects.save")}</Button>
      </form>

      <form method="POST" action={`/projects/${encodeURIComponent(data.Project.Slug)}/delete`} data-confirm={t(data.Lang, "projects.confirmDelete")}>
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <Button variant="destructive" type="submit" className="w-full"><Trash2 aria-hidden="true" />{t(data.Lang, "projects.delete")}</Button>
      </form>
    </div></DisclosureSection>
}

export function ProjectDetail({ data }: { data: ProjectDetailData }) {
  const project = data.Project
  const percent = Math.min(100, Math.max(0, data.EstimatePercent))
  const clientName = data.Client && data.Client.ID > 0 ? data.Client.Name : ""

  return <main className="space-y-6">
    <a href="/projects" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft aria-hidden="true" className="size-4" />{t(data.Lang, "nav.projects")}</a>

    <header className="flex flex-wrap items-start justify-between gap-4">
      <div className="flex min-w-0 items-start gap-3">
        <ColorMark color={project.Color} />
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2"><h1 className="max-w-full min-w-0 text-2xl font-semibold tracking-tight" title={project.Name}><span className="block truncate">{project.Name}</span></h1>{data.Archived && <Badge variant="secondary">{t(data.Lang, "projects.archivedBadge")}</Badge>}</div>
        </div>
      </div>
      {!data.Archived && <Button asChild size="sm"><a href={`/?project=${project.ID}`}><Play aria-hidden="true" />{t(data.Lang, "projects.trackTime")}</a></Button>}
    </header>

    {data.Flash && <p className={data.FlashOK ? "text-sm" : "text-sm text-destructive"} role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>}

    <section aria-label={t(data.Lang, "projects.allTime")} className={`grid gap-4 sm:grid-cols-2 ${data.EstimateLabel ? "lg:grid-cols-3" : "lg:grid-cols-2"}`}>
      <SummaryCard title={t(data.Lang, "projects.last30")} value={data.MonthTotal} />
      <SummaryCard title={t(data.Lang, "projects.allTime")} value={data.Total} />
      {data.EstimateLabel && <Card><CardContent className="space-y-3 p-5">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground">{t(data.Lang, "est.vsActual")}</h3>
        <p className="font-mono text-lg tabular-nums">{data.EstimateLabel} <span className="text-muted-foreground">/</span> {data.Total}</p>
        <Progress value={percent} aria-label={t(data.Lang, "est.vsActual")} />
        <p className="text-xs text-muted-foreground">{data.EstimatePercent}%</p>
      </CardContent></Card>}
    </section>

    <div className="grid min-w-0 gap-6 lg:grid-cols-3 lg:items-start">
      <Card className="min-w-0 lg:col-span-2">
        <CardHeader><CardTitle>{t(data.Lang, "projects.recentSessions")}</CardTitle></CardHeader>
        <CardContent>
          {data.Sessions?.length ? <Table>
            <TableHeader><TableRow><TableHead>{t(data.Lang, "dash.activity")}</TableHead><TableHead>{t(data.Lang, "dash.started")}</TableHead><TableHead>{t(data.Lang, "dash.duration")}</TableHead></TableRow></TableHeader>
            <TableBody>{data.Sessions.map(session => <TableRow key={session.ID}>
              <TableCell><ColorMark color={session.Color} />{session.ActivityName}</TableCell>
              <TableCell className="font-mono text-xs text-muted-foreground">{session.StartLocal}</TableCell>
              <TableCell className="font-mono text-xs tabular-nums">{session.Duration}</TableCell>
            </TableRow>)}</TableBody>
          </Table> : <p className="text-sm text-muted-foreground">{t(data.Lang, "projects.noSessions")}</p>}
        </CardContent>
      </Card>

      <aside className="grid min-w-0 gap-4">
        <Card><CardHeader><CardTitle>{t(data.Lang, "clients.heading")}</CardTitle></CardHeader><CardContent>
          {/* A link, not a label: the customer's page holds the projects you
              cannot see from here, and the notes about how they pay. */}
          {clientName
            ? <a href={`/clients/${data.Client!.ID}`} className="text-sm [overflow-wrap:anywhere] hover:underline">{clientName}</a>
            : <p className="text-sm [overflow-wrap:anywhere]">{t(data.Lang, "project.noClient")}</p>}
        </CardContent></Card>

        <BudgetCard data={data} />

        {!!data.Unbilled?.length && <Card><CardHeader><CardTitle>{t(data.Lang, "inv.unbilled")}</CardTitle></CardHeader><CardContent className="divide-y divide-border">
          {data.Unbilled.map(item => <div key={item.ProjectID} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-3 first:pt-0 last:pb-0">
            <a href={`/projects/${encodeURIComponent(item.Slug)}`} className="min-w-0 truncate font-medium">{item.ProjectName}</a>
            <span className="font-mono text-sm tabular-nums">{item.Hours} {t(data.Lang, "inv.hoursShort")} / {item.Amount}</span>
            <span className="text-xs text-muted-foreground">{t(data.Lang, "inv.since")} {item.Since}</span>
            <Button asChild size="sm" className="ms-auto"><a href={`/invoices?project=${item.ProjectID}&from=${encodeURIComponent(item.SinceISO)}#new`}>{t(data.Lang, "inv.billNow")}</a></Button>
          </div>)}
        </CardContent></Card>}

        <Card><CardHeader><CardTitle>{t(data.Lang, "projects.activities")} ({data.Activities?.length ?? 0})</CardTitle></CardHeader><CardContent>
          {data.Activities?.length ? <ul className="space-y-2">{data.Activities.map(activity => <li key={activity.ID} className="flex min-w-0 items-center gap-2 text-sm">
            <ColorMark color={activity.Color} />
            <span className="min-w-0 truncate">{activity.Name}</span>
            {activity.Archived && <Badge variant="secondary" className="h-5">{t(data.Lang, "projects.archivedBadge")}</Badge>}
          </li>)}</ul> : <p className="text-sm text-muted-foreground">{t(data.Lang, "projects.noActivities")}</p>}
        </CardContent></Card>

      </aside>
    </div>
    {data.CanManage && <ProjectEdit data={data} />}
  </main>
}
