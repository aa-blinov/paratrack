import { openDisclosure } from "@/components/ui/collapsible"
import { Disclosure, DisclosureTrigger } from "@/components/ui/collapsible"
import * as React from "react"
import { AlertTriangle, ChevronDown, Download, Link as LinkIcon, Mail, Plus, Printer, RefreshCw, Send, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { DisclosureSection } from "@/components/disclosure-section"
import { translate as t } from "@/i18n"
import type { InvoiceDetailData, InvoicesData } from "@/dashboard/types"

function InvoiceStatus({ lang, status }: { lang: string; status: string }) {
  // The badge variant stays neutral on purpose: `variant` paints a utility
  // background that outranks the role styles in ui.css, and a paid invoice
  // measured as a near-black pill because of exactly that. The word stays,
  // the colour comes from .doc-status.
  return <Badge variant="secondary" className={`doc-status is-${status}`}>{t(lang, `status.${status}`)}</Badge>
}

function InvoiceFlash({ data }: { data: { Flash: string; FlashOK: boolean } }) {
  if (!data.Flash) return null
  return <p className={data.FlashOK ? "text-sm" : "text-sm text-destructive"} role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>
}

type ClientFields = { name: string; email: string; details: string }

export function InvoicesPage({ data }: { data: InvoicesData }) {
  const [project, setProject] = React.useState(data.Projects.find(item => item.Selected)?.ID.toString() ?? "__all__")
  const [client, setClient] = React.useState<ClientFields>({ name: data.Prefill.ClientName, email: data.Prefill.ClientEmail, details: data.Prefill.ClientDetails })
  const autoFilled = React.useRef<Record<keyof ClientFields, boolean>>({ name: false, email: false, details: false })
  const lang = data.Lang

  function setProjectAndPrefill(value: string) {
    setProject(value)
    const selected = data.Projects.find(item => String(item.ID) === value)
    if (!selected) return
    setClient(previous => {
      const next = { ...previous }
      const values: ClientFields = { name: selected.ClientName, email: selected.ClientEmail, details: selected.ClientDetails }
      for (const field of Object.keys(values) as Array<keyof ClientFields>) {
        if (!next[field] || autoFilled.current[field]) {
          next[field] = values[field]
          autoFilled.current[field] = Boolean(values[field])
        }
      }
      return next
    })
  }

  function editClientField(field: keyof ClientFields, value: string) {
    autoFilled.current[field] = false
    setClient(previous => ({ ...previous, [field]: value }))
  }

  // Money is priced from rounded time. Say the rule once, quietly, instead of
  // letting a person wonder why the amount does not match the hours.
  const rounding = (() => {
    if (!data.Billable) return null
    if (!data.RoundMinutes) return t(lang, "inv.roundHint", t(lang, "inv.roundNone"))
    const [up, down] = data.RoundMode === "up" ? [true, false] : data.RoundMode === "down" ? [false, true] : [false, false]
    const rule = t(lang, up ? "inv.roundUp" : down ? "inv.roundDown" : "inv.roundNearest", data.RoundMinutes)
    return t(lang, "inv.roundHint", rule)
  })()

  const flash = <InvoiceFlash data={data} />
  return <main className="mx-auto w-full max-w-6xl space-y-5">
    <header className="flex flex-wrap items-end justify-between gap-3"><div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "inv.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "inv.blurb")}</p></div><Button asChild size="sm"><a href="#new" onClick={() => openDisclosure("new")}><Plus aria-hidden="true" />{t(lang, "inv.generate")}</a></Button></header>
    {flash}

    {rounding && <p data-rounding className="text-xs text-muted-foreground">{rounding}</p>}

    {!data.Billable && <div role="alert" className="flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/40 bg-amber-500/5 p-4 text-sm">
      <AlertTriangle aria-hidden="true" className="size-5 shrink-0 text-amber-700 dark:text-amber-300" />
      <div className="min-w-0 flex-1"><p className="font-medium">{t(lang, "inv.noRateTitle")}</p><p className="text-muted-foreground">{t(lang, "inv.noRateHint")}</p></div>
      <Button asChild size="sm" className="ml-auto shrink-0"><a href={data.Projects.length ? "/projects" : "/projects/new"}>{data.Projects.length ? t(lang, "inv.noRateCta") : t(lang, "inv.createProject")}</a></Button>
    </div>}

    {!!data.Unbilled.length && <Card>
      <CardHeader><CardTitle className="text-base">{t(lang, "inv.unbilled")}</CardTitle></CardHeader>
      <CardContent><ul className="divide-y">
        {data.Unbilled.map(item => <li key={item.ProjectID} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0">
          <a href={`/projects/${item.Slug}`} className="min-w-0 max-w-full truncate py-2 font-medium underline-offset-4 hover:underline">{item.ProjectName}</a>
          <span className="whitespace-nowrap font-mono tabular-nums">{item.Hours} {t(lang, "inv.hoursShort")} / {item.Amount}</span>
          <span className="text-xs text-muted-foreground">{t(lang, "inv.since")} {item.Since}</span>
          <Button asChild size="sm" className="ml-auto"><a href={`/invoices?project=${item.ProjectID}&from=${item.SinceISO}#new`}>{t(lang, "inv.billNow")}</a></Button>
        </li>)}
      </ul></CardContent>
    </Card>}

    {!!data.Unassigned.length && <Disclosure className="group rounded-lg border bg-card">
      <DisclosureTrigger className="flex cursor-pointer list-none items-start gap-3 p-4 text-sm marker:hidden">
        <AlertTriangle aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1"><strong>{t(lang, "inv.unassignedTitle")}</strong><span className="mt-1 block text-muted-foreground">{t(lang, "inv.unassignedHint")}</span></span><ChevronDown aria-hidden="true" className="mt-0.5 size-4 shrink-0 transition-transform group-data-[state=open]:rotate-180" />
      </DisclosureTrigger>
      <div className="space-y-4 px-4 pb-4">
        {data.Unassigned.map(activity => <div key={activity.ID} className="min-w-0 border-t pt-3">
          <p className="break-words font-medium">{activity.Name} <span className="font-normal text-sm text-muted-foreground">{t(lang, "inv.assignCount", activity.Sessions)}</span></p>
          {activity.Billed ? <p className="mt-1 text-sm text-muted-foreground">{t(lang, "inv.assignLocked")}</p> : data.Billable ? <form method="POST" action="/invoices/assign" className="mt-2 flex flex-col items-start gap-3">
            <input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="activity_id" value={activity.ID} />
            <div className="w-full max-w-md space-y-2"><Label htmlFor={`assign-activity-${activity.ID}`}>{t(lang, "inv.assignProject")}</Label>
              <Select name="project_id" required><SelectTrigger id={`assign-activity-${activity.ID}`}><SelectValue placeholder={t(lang,"inv.assignChoose")}/></SelectTrigger><SelectContent>{data.Projects.filter(item=>item.Eligible).map(item=><SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent></Select>
            </div>
            <label className="flex cursor-pointer items-start gap-2 text-sm"><Checkbox name="confirm_history" value="1" required className="mt-0.5" /><span>{t(lang, "inv.assignHistory")}</span></label>
            <Button type="submit" size="sm">{t(lang, "inv.assignAction")}</Button>
          </form> : <p className="mt-1 text-sm text-muted-foreground">{t(lang, "inv.noRateHint")} <a href="/projects/new" className="underline underline-offset-4">{t(lang, "projects.createNew")}</a></p>}
        </div>)}
      </div>
    </Disclosure>}



    {data.Items.length ? <Card><CardHeader><CardTitle>{t(lang, "inv.list")}</CardTitle></CardHeader><CardContent>
      <div className="grid gap-3 sm:hidden">{data.Items.map(item => <article key={item.ID} className="grid gap-2 border-b pb-3 last:border-0 last:pb-0"><div className="flex flex-wrap items-center justify-between gap-2"><a href={`/invoices/${item.ID}`} className="py-1 font-mono font-medium underline-offset-4 hover:underline">{item.Number}</a><InvoiceStatus lang={lang} status={item.Status} /></div><p className="font-medium">{item.Client}</p><p className="text-xs text-muted-foreground">{item.Period}</p><dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm"><div><dt className="text-xs text-muted-foreground">{t(lang, "inv.hours")}</dt><dd className="font-mono tabular-nums">{item.Hours}</dd></div><div><dt className="text-xs text-muted-foreground">{t(lang, "inv.amount")}</dt><dd className="font-mono tabular-nums">{item.Total}</dd></div></dl></article>)}</div>
      <div className="hidden overflow-x-auto sm:block"><Table>
        <TableHeader><TableRow><TableHead>{t(lang, "inv.number")}</TableHead><TableHead>{t(lang, "inv.client")}</TableHead><TableHead>{t(lang, "inv.period")}</TableHead><TableHead>{t(lang, "inv.hours")}</TableHead><TableHead>{t(lang, "inv.amount")}</TableHead><TableHead>{t(lang, "team.status")}</TableHead></TableRow></TableHeader>
        <TableBody>{data.Items.map(item => <TableRow key={item.ID}><TableCell><a href={`/invoices/${item.ID}`} className="whitespace-nowrap font-mono underline-offset-4 hover:underline">{item.Number}</a></TableCell><TableCell>{item.Client}</TableCell><TableCell className="whitespace-nowrap text-xs text-muted-foreground">{item.Period}</TableCell><TableCell className="whitespace-nowrap font-mono tabular-nums">{item.Hours}</TableCell><TableCell className="whitespace-nowrap font-mono tabular-nums">{item.Total}</TableCell><TableCell><InvoiceStatus lang={lang} status={item.Status} /></TableCell></TableRow>)}</TableBody>
      </Table>
      </div>
    </CardContent></Card> : <p className="text-sm text-muted-foreground">{t(lang, "inv.empty")}</p>}
    <DisclosureSection id="new" title={t(lang, "inv.generate")} description={t(lang, "inv.newHint")} defaultOpen={!data.Items.length || project !== "__all__" || location.hash === "#new" || !data.FlashOK && Boolean(data.Flash)}>
      <form method="POST" action="/invoices" className="grid gap-4 sm:grid-cols-2">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <input type="hidden" name="project_id" value={project === "__all__" ? "" : project} />
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="inv-project">{t(lang, "dash.projectLabel")}</Label>
          <Select value={project} onValueChange={setProjectAndPrefill}><SelectTrigger id="inv-project" className="min-h-10 w-full"><SelectValue /></SelectTrigger><SelectContent>
            <SelectItem value="__all__">{t(lang, "stats.all")}</SelectItem>{data.Projects.map(option => <SelectItem key={option.ID} value={String(option.ID)}>{option.Name}</SelectItem>)}
          </SelectContent></Select><p className="text-xs text-muted-foreground">{t(lang, "inv.projectHint")}</p>
        </div>
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="inv-client">{t(lang, "inv.client")}</Label><Input id="inv-client" type="text" name="client" required placeholder={t(lang, "ph.client")} value={client.name} onChange={event => editClientField("name", event.target.value)} /></div>

        <div className="space-y-2"><Label htmlFor="inv-start">{t(lang, "inv.from")}</Label><Input id="inv-start" type="date" name="start" required defaultValue={data.DefStart} /></div>
        <div className="space-y-2"><Label htmlFor="inv-end">{t(lang, "inv.to")}</Label><Input id="inv-end" type="date" name="end" required defaultValue={data.DefEnd} /></div>
        <Disclosure open={Boolean(client.email || client.details)} className="sm:col-span-2">
          <DisclosureTrigger className="cursor-pointer text-sm text-muted-foreground">{t(lang, "inv.optionalFields")}</DisclosureTrigger>
          <div className="mt-3 grid gap-4 sm:grid-cols-2">
        <div className="space-y-2"><Label htmlFor="inv-email">{t(lang, "inv.clientEmail")} <span className="text-muted-foreground">({t(lang, "projects.optional")})</span></Label><Input id="inv-email" type="email" name="client_email" autoComplete="off" placeholder="buh@romashka.ru" value={client.email} onChange={event => editClientField("email", event.target.value)} /></div>
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="inv-client-details">{t(lang, "inv.clientDetails")} <span className="text-muted-foreground">({t(lang, "projects.optional")})</span></Label><Textarea id="inv-client-details" name="client_details" rows={3} maxLength={2000} placeholder={t(lang, "inv.clientDetailsPh")} value={client.details} onChange={event => editClientField("details", event.target.value)} /></div>
        <label className="flex cursor-pointer items-start gap-2 text-sm sm:col-span-2"><Checkbox name="by_person" value="1" className="mt-0.5" /><span>{t(lang, "inv.byPerson")}<span className="mt-1 block text-xs text-muted-foreground">{t(lang, "inv.byPersonHint")}</span></span></label>
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="inv-notes">{t(lang, "stats.note")}</Label><Input id="inv-notes" type="text" name="notes" placeholder={t(lang, "inv.notesPh")} /></div>
          </div>
        </Disclosure>
        <div className="sm:col-span-2"><Button type="submit"><Plus aria-hidden="true" />{t(lang, "inv.create")}</Button></div>
      </form>
      <p className="mt-3 text-xs text-muted-foreground">{t(lang, "inv.rateHint")}</p>
    </DisclosureSection>
  </main>
}

function InvoiceActions({ data }: { data: InvoiceDetailData }) {
  const { Inv: inv } = data
  const lang = data.Lang
  return <div className="no-print space-y-3">
    <div className="flex flex-wrap items-center gap-2 text-sm"><a href="/invoices" className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{t(lang, "inv.title")}</a><span aria-hidden="true" className="text-muted-foreground">/</span><span className="font-mono">{inv.Number}</span><InvoiceStatus lang={lang} status={inv.Status} /></div>
    <div className="flex flex-wrap items-center gap-2">
      {inv.Status === "draft" && <form method="POST" action={`/invoices/${inv.ID}/status`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="status" value="sent" /><Button type="submit" size="sm"><Send aria-hidden="true" />{t(lang, "inv.markSent")}</Button></form>}
      {inv.Status !== "paid" && <form method="POST" action={`/invoices/${inv.ID}/paid`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" size="sm" variant={inv.Status === "sent" ? "default" : "ghost"}>{t(lang, "inv.markPaid")}</Button></form>}
      <Button asChild size="sm" variant="ghost"><a href={`/invoices/${inv.ID}/pdf`}><Download aria-hidden="true" />PDF</a></Button>
      <Button asChild size="sm" variant="ghost"><a href={`/invoices/${inv.ID}/act`}>{t(lang, "act.title")}</a></Button>
      <Button type="button" size="sm" variant="ghost" data-print><Printer aria-hidden="true" />{t(lang, "inv.print")}</Button>
      {inv.PaymentURL ? <Button asChild size="sm" variant="ghost"><a href={inv.PaymentURL} target="_blank" rel="noopener"><LinkIcon aria-hidden="true" />{t(lang, "inv.payOnline")}</a></Button> : data.StripeReady && <form method="POST" action={`/invoices/${inv.ID}/pay`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="mode" value="stripe" /><Button type="submit" size="sm" variant="ghost"><LinkIcon aria-hidden="true" />{t(lang, "inv.payLink")}</Button></form>}
      <form method="POST" action={`/invoices/${inv.ID}/delete`} className="ml-auto" data-confirm={t(lang, "inv.confirmDelete", inv.Number)}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" size="icon" variant="ghost" className="text-destructive" aria-label={t(lang, "inv.delete")}><Trash2 aria-hidden="true" /></Button></form>
    </div>
  </div>
}

export function InvoiceDetailPage({ data }: { data: InvoiceDetailData }) {
  const { Inv: inv } = data
  const lang = data.Lang
  return <main className="mx-auto w-full max-w-5xl space-y-5">
    <InvoiceFlash data={data} />
    <InvoiceActions data={data} />

    <Card className="print:border-0"><CardContent className="space-y-5 p-5 sm:p-7">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>{inv.Logo && <img src={inv.Logo} alt="" className="mb-3 h-12 max-w-48 object-contain" />}<h1 className="text-2xl font-semibold tracking-tight">{t(lang, "inv.invoice")} <span className="font-mono">{inv.Number}</span></h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "pdf.issued")} {inv.IssuedLabel}</p></div>
      </header>
      <dl className="grid gap-4 sm:grid-cols-2">
        <div><dt className="text-xs text-muted-foreground">{t(lang, "pdf.from")}</dt><dd className="font-medium">{data.Seller}</dd>{inv.SellerDetails && <dd className="mt-1 whitespace-pre-line text-sm">{inv.SellerDetails}</dd>}</div>
        <div><dt className="text-xs text-muted-foreground">{t(lang, "pdf.billedTo")}</dt><dd className="font-medium">{inv.ClientName}</dd>{inv.ClientDetails && <dd className="mt-1 whitespace-pre-line text-sm">{inv.ClientDetails}</dd>}</div>
        <div className="sm:col-span-2"><dt className="text-xs text-muted-foreground">{t(lang, "inv.period")}</dt><dd className="font-mono text-sm">{inv.PeriodLabel}</dd></div>
      </dl>

      <InvoiceLineCards data={data} />
      <div className="hidden overflow-x-auto sm:block print:block"><Table className="doc-table">
        <TableHeader><TableRow><TableHead>{t(lang, "pdf.work")}</TableHead><TableHead className="text-right">{t(lang, "inv.hours")}</TableHead><TableHead className="text-right">{t(lang, "inv.rate")}</TableHead><TableHead className="text-right">{t(lang, "inv.amount")}</TableHead></TableRow></TableHeader>
        <TableBody>{inv.Lines.map((line, index) => <TableRow key={`${line.Label}:${index}`}><TableCell>{line.Label}</TableCell><TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{line.Hours}</TableCell><TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{line.Rate}</TableCell><TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{line.Amount}</TableCell></TableRow>)}</TableBody>
        <tfoot><tr className="border-t-2 font-semibold"><td className="p-2">{t(lang, "pdf.total")}</td><td className="whitespace-nowrap p-2 text-right font-mono">{inv.Hours}</td><td /><td className="whitespace-nowrap p-2 text-right font-mono">{inv.Total}</td></tr></tfoot>
      </Table></div>
      {inv.VATNote && <p className="text-sm">{inv.VATNote}</p>}
      {inv.Receipt && <p className="text-sm"><span className="text-muted-foreground">{t(lang, "inv.receipt")}:</span> <span className="break-all">{inv.Receipt}</span></p>}
      {inv.PaymentURL && <p className="text-sm"><span className="text-muted-foreground">{t(lang, "inv.payOnline")}:</span> <a href={inv.PaymentURL} className="break-all underline-offset-4 hover:underline">{inv.PaymentURL}</a></p>}
      {inv.Notes && <p className="whitespace-pre-line text-sm">{inv.Notes}</p>}
      <p className="text-xs text-muted-foreground">{t(lang, "inv.footNote")}</p>
    </CardContent></Card>

    <Card className="no-print"><CardHeader><CardTitle className="text-base">{t(lang, "inv.send")}</CardTitle></CardHeader><CardContent className="space-y-5">
      <div>{data.MailReady ? <><form method="POST" action={`/invoices/${inv.ID}/send`} className="flex flex-wrap gap-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Label className="sr-only" htmlFor="send-to">{t(lang, "inv.clientEmail")}</Label><Input id="send-to" type="email" name="to" required defaultValue={inv.ClientEmail} placeholder="buh@romashka.ru" className="min-w-56 flex-1" /><Button type="submit" size="sm"><Mail aria-hidden="true" />{t(lang, "inv.send")}</Button></form><p className="mt-1 text-xs text-muted-foreground">{t(lang, "inv.sendHint")}</p></> : <><Button asChild size="sm" variant="ghost"><a href={data.MailtoURL}><Mail aria-hidden="true" />{t(lang, "inv.sendMailto")}</a></Button><p className="mt-1 text-xs text-muted-foreground">{t(lang, "inv.mailtoHint")}</p></>}</div>

      {inv.Status === "draft" && <Disclosure className="border-t pt-4">
        <DisclosureTrigger className="cursor-pointer font-medium">{t(lang, "inv.edit")}</DisclosureTrigger>
        <form method="POST" action={`/invoices/${inv.ID}/edit`} className="mt-3 grid gap-3 sm:grid-cols-2">
          <input type="hidden" name="csrf_token" value={data.CSRFToken} />
          <div className="space-y-2"><Label htmlFor="edit-client">{t(lang, "inv.client")}</Label><Input id="edit-client" type="text" name="client" required defaultValue={inv.ClientName} /></div>
          <div className="space-y-2"><Label htmlFor="edit-email">{t(lang, "inv.clientEmail")}</Label><Input id="edit-email" type="email" name="client_email" defaultValue={inv.ClientEmail} /></div>
          <div className="space-y-2 sm:col-span-2"><Label htmlFor="edit-details">{t(lang, "inv.clientDetails")}</Label><Textarea id="edit-details" name="client_details" rows={3} maxLength={2000} defaultValue={inv.ClientDetails} /></div>
          <div className="space-y-2 sm:col-span-2"><Label htmlFor="edit-notes">{t(lang, "stats.note")}</Label><Input id="edit-notes" type="text" name="notes" defaultValue={inv.Notes} /></div>
          <div className="sm:col-span-2"><Button type="submit" size="sm">{t(lang, "inv.save")}</Button></div>
        </form>
        <form method="POST" action={`/invoices/${inv.ID}/rebuild`} className="mt-3 flex flex-wrap items-center gap-2 border-t pt-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" size="sm" variant="ghost"><RefreshCw aria-hidden="true" />{t(lang, "inv.rebuild")}</Button><span className="text-xs text-muted-foreground">{t(lang, "inv.rebuildHint")}</span></form>
      </Disclosure>}

      <form method="POST" action={`/invoices/${inv.ID}/receipt`} className="grid gap-2 border-t pt-4">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} /><Label htmlFor="receipt">{t(lang, "inv.receipt")}</Label>
        <div className="flex gap-2"><Input id="receipt" type="text" name="receipt" defaultValue={inv.Receipt} maxLength={300} placeholder={t(lang, "inv.receiptPh")} className="min-w-0 flex-1" /><Button type="submit" size="sm" variant="ghost">{t(lang, "inv.save")}</Button></div>
        <span className="text-xs text-muted-foreground">{t(lang, "inv.receiptHint")}</span>
      </form>
    </CardContent></Card>
  </main>
}

export function InvoiceActPage({ data }: { data: InvoiceDetailData }) {
  const { Inv: invoice, Seller: seller, Lang: lang = "en" } = data
  return <main className="mx-auto grid w-full max-w-4xl gap-4">
    <div className="no-print flex flex-wrap items-center gap-2"><a href={`/invoices/${invoice.ID}`} className="text-sm text-muted-foreground underline underline-offset-4">{t(lang, "inv.invoice")} {invoice.Number}</a><a href={`/invoices/${invoice.ID}/act.pdf`} className="ms-auto inline-flex items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted"><Download aria-hidden="true" />PDF</a><Button type="button" size="sm" variant="ghost" data-print><Printer aria-hidden="true" />{t(lang, "inv.print")}</Button></div>
    <Card className="print:border-0"><CardContent className="grid gap-4 p-6">
      {invoice.Logo && <img src={invoice.Logo} alt="" className="h-12 max-w-48 self-start object-contain" />}
      <h1 className="text-2xl font-semibold">{t(lang, "act.heading")} <span className="font-mono">{invoice.Number}</span></h1>
      <p className="text-sm text-muted-foreground">{t(lang, "pdf.issued")} {invoice.IssuedLabel}, {t(lang, "inv.period")}: {invoice.PeriodLabel}</p>
      <dl className="grid gap-4 text-sm sm:grid-cols-2"><div><dt className="text-xs text-muted-foreground">{t(lang, "pdf.from")}</dt><dd className="font-medium">{seller}</dd>{invoice.SellerDetails && <dd className="mt-1 whitespace-pre-line">{invoice.SellerDetails}</dd>}</div><div><dt className="text-xs text-muted-foreground">{t(lang, "pdf.billedTo")}</dt><dd className="font-medium">{invoice.ClientName}</dd>{invoice.ClientDetails && <dd className="mt-1 whitespace-pre-line">{invoice.ClientDetails}</dd>}</div></dl>
      <InvoiceLineCards data={data} />
      <div className="hidden overflow-x-auto sm:block print:block"><Table className="doc-table"><TableHeader><TableRow><TableHead>{t(lang, "pdf.work")}</TableHead><TableHead className="text-right">{t(lang, "inv.hours")}</TableHead><TableHead className="text-right">{t(lang, "inv.rate")}</TableHead><TableHead className="text-right">{t(lang, "inv.amount")}</TableHead></TableRow></TableHeader><TableBody>{invoice.Lines.map((line, index) => <TableRow key={`${line.Label}-${index}`}><TableCell>{line.Label}</TableCell><TableCell className="whitespace-nowrap text-right font-mono">{line.Hours}</TableCell><TableCell className="whitespace-nowrap text-right font-mono">{line.Rate}</TableCell><TableCell className="whitespace-nowrap text-right font-mono">{line.Amount}</TableCell></TableRow>)}</TableBody><tfoot><TableRow className="border-t-2 font-semibold"><td>{t(lang, "pdf.total")}</td><td className="text-right font-mono">{invoice.Hours}</td><td></td><td className="text-right font-mono">{invoice.Total}</td></TableRow></tfoot></Table></div>
      {invoice.VATNote && <p className="text-sm">{invoice.VATNote}</p>}{invoice.Receipt && <p className="break-all text-sm"><span className="text-muted-foreground">{t(lang, "inv.receipt")}:</span> {invoice.Receipt}</p>}
      <p className="text-sm">{t(lang, "act.statement")}</p><div className="grid grid-cols-2 gap-8 pt-6 text-sm"><div><div className="text-xs text-muted-foreground">{t(lang, "pdf.from")}</div><div className="h-8 border-b border-foreground/40"></div></div><div><div className="text-xs text-muted-foreground">{t(lang, "pdf.billedTo")}</div><div className="h-8 border-b border-foreground/40"></div></div></div>
    </CardContent></Card>
  </main>
}

function InvoiceLineCards({ data }: { data: InvoiceDetailData }) {
  const lang = data.Lang
  const invoice = data.Inv
  return <div className="grid gap-3 sm:hidden print:hidden">
    {invoice.Lines.map((line, index) => <article key={`${line.Label}:${index}`} className="grid gap-3 border-b pb-3 last:border-0 last:pb-0">
      <h2 className="break-words font-medium">{line.Label}</h2>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
        <div><dt className="text-xs text-muted-foreground">{t(lang, "inv.hours")}</dt><dd className="font-mono tabular-nums">{line.Hours}</dd></div>
        <div><dt className="text-xs text-muted-foreground">{t(lang, "inv.rate")}</dt><dd className="font-mono tabular-nums">{line.Rate}</dd></div>
        <div><dt className="text-xs text-muted-foreground">{t(lang, "inv.amount")}</dt><dd className="font-mono font-medium tabular-nums">{line.Amount}</dd></div>
      </dl>
    </article>)}
    <dl className="grid grid-cols-2 gap-x-4 border-t-2 pt-3 text-sm font-semibold">
      <div><dt className="text-xs text-muted-foreground">{t(lang, "stats.total")}, {t(lang, "inv.hours")}</dt><dd className="font-mono tabular-nums">{invoice.Hours}</dd></div>
      <div><dt className="text-xs text-muted-foreground">{t(lang, "stats.total")}, {t(lang, "inv.amount")}</dt><dd className="font-mono tabular-nums">{invoice.Total}</dd></div>
    </dl>
  </div>
}
