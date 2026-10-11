import { openDisclosure } from "@/components/ui/collapsible"
import { ArrowLeft, Printer, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { DisclosureSection } from "@/components/disclosure-section"
import { translate as t } from "@/i18n"
import type { PayrollData, PayrollDetailData } from "@/dashboard/types"

function Status({ lang, status }: { lang: string; status: string }) {
  // Same reason as the invoice status: the colour belongs to the role in
  // ui.css, so the variant must not paint a background of its own.
  return <Badge variant="secondary" className={`doc-status is-${status}`}>{t(lang, `status.${status}`)}</Badge>
}

export function PayrollPage({ data }: { data: PayrollData }) {
  const lang = data.Lang
  return <main className="mx-auto w-full max-w-6xl space-y-5">
    {data.Flash && <p className={data.FlashOK ? "text-sm" : "text-sm text-destructive"} role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>}
    <header className="flex flex-wrap items-end justify-between gap-3"><div><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "pay.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "pay.blurb")}</p></div><Button asChild size="sm"><a href="#new-payroll" onClick={() => openDisclosure("new-payroll")}>{t(lang, "pay.generate")}</a></Button></header>

    {!!data.Overlap && <div role="alert" className="flex flex-wrap items-start gap-3 rounded-lg border border-amber-500/40 bg-amber-500/5 p-4 text-sm">
      <span className="min-w-0 flex-1">{t(lang, "pay.overlap", data.Overlap)}</span>
      <form method="POST" action="/payroll" className="flex flex-wrap items-center gap-2">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="start" value={data.DefStart} /><input type="hidden" name="end" value={data.DefEnd} /><input type="hidden" name="notes" value={data.DefNotes} /><input type="hidden" name="confirm" value="1" />
        <Button type="submit" size="sm">{t(lang, "pay.overlapCreate")}</Button><Button asChild type="button" size="sm" variant="ghost"><a href="/payroll">{t(lang, "pay.overlapCancel")}</a></Button>
      </form>
    </div>}



    {data.Items.length ? <Card><CardHeader><CardTitle>{t(lang, "pay.list")}</CardTitle></CardHeader><CardContent>
      <div className="grid gap-3 sm:hidden">{data.Items.map(item => <article key={item.ID} className="grid gap-2 border-b pb-3 last:border-0 last:pb-0"><div className="flex flex-wrap items-center justify-between gap-2"><a href={`/payroll/${item.ID}`} className="py-1 font-mono font-medium underline-offset-4 hover:underline">{item.Number}</a><Status lang={lang} status={item.Status} /></div><p className="text-xs text-muted-foreground">{item.Period}</p><dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm"><div><dt className="text-xs text-muted-foreground">{t(lang, "inv.hours")}</dt><dd className="font-mono tabular-nums">{item.Hours}</dd></div><div><dt className="text-xs text-muted-foreground">{t(lang, "inv.amount")}</dt><dd className="font-mono tabular-nums">{item.Total}</dd></div></dl></article>)}</div>
      <div className="hidden overflow-x-auto sm:block"><Table>
        <TableHeader><TableRow><TableHead>{t(lang, "inv.number")}</TableHead><TableHead>{t(lang, "inv.period")}</TableHead><TableHead>{t(lang, "inv.hours")}</TableHead><TableHead>{t(lang, "inv.amount")}</TableHead><TableHead>{t(lang, "team.status")}</TableHead></TableRow></TableHeader>
        <TableBody>{data.Items.map(item => <TableRow key={item.ID}>
          <TableCell><a href={`/payroll/${item.ID}`} className="font-mono underline-offset-4 hover:underline">{item.Number}</a></TableCell><TableCell className="whitespace-nowrap text-xs text-muted-foreground">{item.Period}</TableCell><TableCell className="whitespace-nowrap font-mono tabular-nums">{item.Hours}</TableCell><TableCell className="whitespace-nowrap font-mono tabular-nums">{item.Total}</TableCell><TableCell><Status lang={lang} status={item.Status} /></TableCell>
        </TableRow>)}</TableBody>
      </Table>
      </div>
    </CardContent></Card> : <p className="text-sm text-muted-foreground">{t(lang, "pay.emptyHint")}</p>}
    <DisclosureSection id="new-payroll" title={t(lang, "pay.generate")} description={t(lang, "pay.newHint")} defaultOpen={!data.Items.length || Boolean(data.Overlap) || !data.FlashOK && Boolean(data.Flash)}>
      <form method="POST" action="/payroll" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 lg:items-end">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="space-y-2"><Label htmlFor="pay-start">{t(lang, "inv.from")}</Label><Input id="pay-start" type="date" name="start" required defaultValue={data.DefStart} /></div>
        <div className="space-y-2"><Label htmlFor="pay-end">{t(lang, "inv.to")}</Label><Input id="pay-end" type="date" name="end" required defaultValue={data.DefEnd} /></div>
        <div className="flex min-w-0 items-end gap-2 sm:col-span-2"><Input type="text" name="notes" defaultValue={data.DefNotes} aria-label={t(lang, "stats.note")} className="min-w-0 flex-1" placeholder={t(lang, "pay.notesPh")} /><Button type="submit" className="shrink-0">{t(lang, "pay.create")}</Button></div>
      </form>
      <p className="mt-3 text-xs text-muted-foreground">{t(lang, "pay.rateHint")} <a href="/settings/members" className="underline underline-offset-4">{t(lang, "pay.rateLink")}</a></p>
    </DisclosureSection>
  </main>
}

export function PayrollDetail({ data }: { data: PayrollDetailData }) {
  const lang = data.Lang
  return <main className="mx-auto w-full max-w-5xl space-y-5">
    {data.Flash && <p className={data.FlashOK ? "text-sm" : "text-sm text-destructive"} role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>}
    <div className="no-print space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-sm"><a href="/payroll" className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground"><ArrowLeft aria-hidden="true" className="size-4" />{t(lang, "pay.title")}</a><span aria-hidden="true" className="text-muted-foreground">/</span><span className="font-mono">{data.Run.Number}</span><Status lang={lang} status={data.Run.Status} /></div>
      <div className="flex flex-wrap items-center gap-2">
        {data.Run.Status !== "paid" && <form method="POST" action={`/payroll/${data.Run.ID}/paid`}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" size="sm">{t(lang, "pay.markPaid")}</Button></form>}
        <Button type="button" variant="outline" size="sm" data-print><Printer aria-hidden="true" />{t(lang, "inv.print")}</Button>
        <form method="POST" action={`/payroll/${data.Run.ID}/delete`} className="ms-auto" data-confirm={t(lang, "pay.confirmDelete", data.Run.Number)}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="destructive" size="sm" aria-label={t(lang, "pay.delete")}><Trash2 aria-hidden="true" />{t(lang, "pay.delete")}</Button></form>
      </div>
    </div>

    <Card className="print:border-0 print:shadow-none"><CardContent className="space-y-5 p-5 sm:p-7">
      <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "pay.run")} <span className="font-mono">{data.Run.Number}</span></h1><p className="mt-1 font-mono text-sm text-muted-foreground">{data.Run.PeriodLabel}</p></header>
      <div className="grid gap-3 sm:hidden print:hidden">{data.Run.Lines.map((line, index) => <article key={`${line.Label}:${index}`} className="grid gap-3 border-b pb-3 last:border-0 last:pb-0"><h2 className="break-words font-medium">{line.Label}</h2><dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm"><div><dt className="text-xs text-muted-foreground">{t(lang, "inv.hours")}</dt><dd className="font-mono tabular-nums">{line.Hours}</dd></div><div><dt className="text-xs text-muted-foreground">{t(lang, "pay.payRate")}</dt><dd className="font-mono tabular-nums">{line.Rate}</dd></div><div><dt className="text-xs text-muted-foreground">{t(lang, "inv.amount")}</dt><dd className="font-mono font-medium tabular-nums">{line.Amount}</dd></div></dl></article>)}<dl className="grid grid-cols-2 gap-x-4 border-t-2 pt-3 text-sm font-semibold"><div><dt className="text-xs text-muted-foreground">{t(lang, "stats.total")}, {t(lang, "inv.hours")}</dt><dd className="font-mono tabular-nums">{data.Run.Hours}</dd></div><div><dt className="text-xs text-muted-foreground">{t(lang, "stats.total")}, {t(lang, "inv.amount")}</dt><dd className="font-mono tabular-nums">{data.Run.Total}</dd></div></dl></div>
      <div className="hidden overflow-x-auto sm:block print:block"><Table className="doc-table">
        <TableHeader><TableRow><TableHead>{t(lang, "pay.member")}</TableHead><TableHead className="text-right">{t(lang, "inv.hours")}</TableHead><TableHead className="text-right">{t(lang, "pay.payRate")}</TableHead><TableHead className="text-right">{t(lang, "inv.amount")}</TableHead></TableRow></TableHeader>
        <TableBody>{data.Run.Lines.map((line, index) => <TableRow key={`${line.Label}:${index}`}><TableCell>{line.Label}</TableCell><TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{line.Hours}</TableCell><TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{line.Rate}</TableCell><TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{line.Amount}</TableCell></TableRow>)}</TableBody>
        <tfoot><tr className="border-t-2 font-semibold"><td className="p-2">{t(lang, "pdf.total")}</td><td className="whitespace-nowrap p-2 text-right font-mono">{data.Run.Hours}</td><td /><td className="whitespace-nowrap p-2 text-right font-mono">{data.Run.Total}</td></tr></tfoot>
      </Table></div>
      {data.Run.Notes && <p className="whitespace-pre-line text-sm">{data.Run.Notes}</p>}
      <p className="text-xs text-muted-foreground">{t(lang, "pay.footNote")}</p>
    </CardContent></Card>
  </main>
}
