import { StatusLine } from "@/components/status-line"
import { Flash } from "@/components/flash"
import { formatDate } from "@/lib/date-format"
import { useEffect, useState } from "react"
import { useSaveStatus } from "@/lib/auto-save"
import { ImageUp, Pencil, Plus, Trash2, Upload } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { DisclosureSection } from "@/components/disclosure-section"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { TeamSettingsData } from "@/dashboard/types"

export function TeamSettingsPage({ data }: { data: TeamSettingsData }) {
  const lang = data.Lang || "en"
  const money = data.Active === "settings-money"
  // The currency is a choice: it saves when picked, in its own card.
  const [currencyState, saveCurrency] = useSaveStatus(t(lang, "toast.saved"), t(lang, "err.generic"))
  const saveFormNow = (form: HTMLFormElement) => saveCurrency(form)
  const currencyStatus = currencyState.kind === "saving" ? t(lang, "prefs.saving") : currencyState.kind === "saved" || currencyState.kind === "error" ? currencyState.text : ""
  // A money form returns to its own card (#currency, #billing, …); the answer
  // is shown there, not at the top the page has scrolled away from.
  const savedCard = money && data.Flash ? location.hash.slice(1) : ""
  // The page is drawn by the client after the redirect, so the browser never
  // scrolled to #stripe: the answer sat 1000px below an empty top.
  useEffect(() => {
    if (savedCard) document.getElementById(savedCard)?.scrollIntoView({ block: "start" })
  }, [savedCard])
  const cardFlash = (id: string) => savedCard === id ? <Flash message={data.Flash} ok={data.FlashOK} /> : null
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage={data.CanManage} />
    {!savedCard && <Flash message={data.Flash} ok={data.FlashOK} />}
    {/* Money has its own tab: currency, invoice rules, requisites and Stripe
        used to share one page with the workspace name and its deletion. */}
    {money ? <div className="grid gap-4 lg:grid-cols-2 lg:items-start">
      <Card id="currency" className="scroll-mt-20"><CardHeader><CardTitle>{t(lang, "team.currencyTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.currencyHint")}</p>{cardFlash("currency")}</CardHeader><CardContent><form method="post" action="/api/team/currency" className="flex gap-2" onChange={event => { const form = event.currentTarget; void saveFormNow(form) }}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Label className="sr-only" htmlFor="team-currency">{t(lang, "team.currencyTitle")}</Label><Select name="currency" defaultValue={data.Currency}><SelectTrigger id="team-currency" className="flex-1"><SelectValue /></SelectTrigger><SelectContent>{data.Currencies.map(currency => <SelectItem key={currency.Code} value={currency.Code}>{currency.Label}</SelectItem>)}</SelectContent></Select><StatusLine text={currencyStatus} /></form></CardContent></Card>
      <Card id="billing" className="scroll-mt-20"><CardHeader><CardTitle>{t(lang, "bill.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "settings.billingHint")}</p>{cardFlash("billing")}</CardHeader><CardContent>
        <form method="post" action="/api/team/billing" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-3 sm:grid-cols-2"><div className="grid gap-1.5"><Label htmlFor="round-minutes">{t(lang, "bill.round")}</Label><Select name="round_minutes" defaultValue={String(data.Billing.RoundMinutes)}><SelectTrigger id="round-minutes"><SelectValue /></SelectTrigger><SelectContent>{data.RoundOpts.map(minutes => <SelectItem key={minutes} value={String(minutes)}>{minutes === 0 ? t(lang, "bill.roundOff") : t(lang, "bill.roundTo", minutes)}</SelectItem>)}</SelectContent></Select></div><div className="grid gap-1.5"><Label htmlFor="round-mode">{t(lang, "bill.mode")}</Label><Select name="round_mode" defaultValue={data.Billing.RoundMode === "up" ? "up" : "nearest"}><SelectTrigger id="round-mode"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="nearest">{t(lang, "bill.nearest")}</SelectItem><SelectItem value="up">{t(lang, "bill.up")}</SelectItem></SelectContent></Select></div></div><p className="text-xs text-muted-foreground">{t(lang, "bill.roundHint")}</p><div className="grid gap-1.5"><Label htmlFor="invoice-prefix">{t(lang, "bill.prefix")}</Label><Input id="invoice-prefix" name="invoice_prefix" required maxLength={12} pattern="(?:\p{L}|\p{N}|-)+" defaultValue={data.Billing.InvoicePrefix} onInvalid={event => event.currentTarget.setCustomValidity(t(lang, "bill.prefixInvalid"))} onInput={event => event.currentTarget.setCustomValidity("")} className="font-mono" /><p className="text-xs text-muted-foreground">{t(lang, "bill.prefixHint", data.Billing.InvoicePrefix)}</p></div><Button type="submit" className="w-fit">{t(lang, "profile.save")}</Button></form>
      </CardContent></Card>
      <Card id="requisites" className="scroll-mt-20 lg:col-span-2"><CardHeader><CardTitle>{t(lang, "team.reqTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.reqHint")}</p>{cardFlash("requisites")}</CardHeader><CardContent><div className="grid gap-4"><form method="post" action="/api/team/requisites" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-1.5"><Label htmlFor="requisites-text">{t(lang, "team.reqLabel")}</Label><Textarea id="requisites-text" name="requisites" rows={6} maxLength={2000} className="font-mono text-sm" placeholder={t(lang, "team.reqPh")} defaultValue={data.Requisites} /></div><div className="grid gap-1.5"><Label htmlFor="vat-note">{t(lang, "team.vatLabel")}</Label><Input id="vat-note" name="vat_note" maxLength={200} defaultValue={data.VATNote} placeholder={t(lang, "team.vatPh")} /></div><Button type="submit" className="w-fit">{t(lang, "profile.save")}</Button></form></div></CardContent></Card>
      <Card id="logo" className="scroll-mt-20"><CardHeader><CardTitle>{t(lang, "bill.logo")}</CardTitle>{cardFlash("logo")}</CardHeader><CardContent>
        <div className="grid gap-2">{data.Billing.HasLogo && <img src={data.LogoURL} alt={t(lang, "bill.logo")} className="max-h-14 max-w-48 rounded border bg-white p-1 object-contain" />}<form method="post" action="/api/team/logo" encType="multipart/form-data" className="flex flex-wrap items-center gap-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><LogoFile lang={lang} /><Button type="submit"><Upload aria-hidden="true" />{t(lang, "bill.upload")}</Button></form>{data.Billing.HasLogo && <form method="post" action="/api/team/logo"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="remove" value="1" /><Button type="submit" size="sm" variant="ghost">{t(lang, "bill.removeLogo")}</Button></form>}<p className="text-xs text-muted-foreground">{t(lang, "bill.logoHint")}</p></div>
      </CardContent></Card>
      <Card id="stripe" className="scroll-mt-20"><CardHeader><CardTitle>{t(lang, "stripe.title")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "stripe.blurb")}</p>{cardFlash("stripe")}</CardHeader><CardContent><div className="grid gap-4"><form method="post" action="/api/team/stripe" className="grid gap-3"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><div className="grid gap-1.5"><Label htmlFor="stripe-key">{t(lang, "stripe.keyLabel")}</Label><Input id="stripe-key" type="password" name="stripe_key" autoComplete="off" placeholder="sk_live_… / sk_test_…" /><p className="text-xs text-muted-foreground">{t(lang, "stripe.keyHint")}</p></div><div className="grid gap-1.5"><Label htmlFor="stripe-webhook-secret">{t(lang, "stripe.whLabel")}</Label><Input id="stripe-webhook-secret" type="password" name="stripe_webhook_secret" autoComplete="off" placeholder="whsec_…" /><p className="text-xs text-muted-foreground">{t(lang, "stripe.whHint")}</p><code className="w-fit select-all break-all rounded bg-muted px-2 py-1 font-mono text-xs">{`${location.origin}/api/stripe/webhook`}</code></div><Button type="submit" className="w-fit">{t(lang, "stripe.save")}</Button></form><p className="text-sm">{t(lang, "stripe.state", t(lang, data.StripeKeySet ? "stripe.set" : "stripe.unset"), t(lang, data.StripeSecretSet ? "stripe.set" : "stripe.unset"))} <span className="text-muted-foreground">{t(lang, "stripe.keepHint")}</span></p>{(data.StripeKeySet || data.StripeSecretSet) && <form method="post" action="/api/team/stripe" data-confirm={t(lang, "stripe.confirmClear")}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><input type="hidden" name="clear" value="1" /><Button type="submit" variant="ghost" size="sm" className="text-destructive">{t(lang, "stripe.clear")}</Button></form>}</div></CardContent></Card>
    </div> : <div className="grid gap-4 lg:grid-cols-2">
      <Card><CardHeader><CardTitle>{t(lang, "team.current")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.switchHint")}</p></CardHeader><CardContent className="grid gap-3"><form method="post" action="/api/team/rename" className="flex flex-col gap-2 sm:flex-row sm:items-center"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Label className="sr-only" htmlFor="team-name">{t(lang, "team.nameCol")}</Label><Input id="team-name" name="name" required minLength={1} defaultValue={data.Team.Name} className="flex-1" /><Button type="submit"><Pencil aria-hidden="true" />{t(lang, "team.rename")}</Button></form><dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm"><dt className="text-muted-foreground">{t(lang, "team.createdCol")}</dt><dd>{formatDate(data.Team.CreatedAt.slice(0, 10), lang)}</dd></dl></CardContent></Card>
      <Card><CardHeader><CardTitle>{t(lang, "team.createTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t(lang, "team.createBlurb")}</p></CardHeader><CardContent><form method="post" action="/api/team/create" className="flex items-center gap-2"><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Label className="sr-only" htmlFor="team-new-name">{t(lang, "projects.name")}</Label><Input id="team-new-name" name="name" required minLength={1} placeholder={t(lang, "ph.team")} className="flex-1" /><Button type="submit"><Plus aria-hidden="true" />{t(lang, "projects.create")}</Button></form></CardContent></Card>
      {data.IsOwner && <DisclosureSection className="lg:col-span-2" title={t(lang, "team.danger")} description={t(lang, "team.dangerBlurb")}><div className="grid gap-4"><form method="post" action="/api/team/delete" data-confirm={t(lang, "team.confirmDelete")}><input type="hidden" name="csrf_token" value={data.CSRFToken} /><Button type="submit" variant="destructive" size="sm"><Trash2 aria-hidden="true" />{t(lang, "team.delete")}</Button></form></div></DisclosureSection>}
    </div>}
  </main>
}

// The browser's own file picker said «Choose File / No file chosen» in English
// on a Russian screen and was cut to «Choose File No …sen» on a phone. The real
// input stays (it is what the form posts); the visible part is ours.
function LogoFile({ lang }: { lang: string }) {
  const [name, setName] = useState("")
  return <span className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
    <input id="team-logo" type="file" name="logo" accept="image/png,image/jpeg" required className="peer sr-only" onChange={event => setName(event.target.files?.[0]?.name || "")} />
    <Button asChild variant="outline" size="sm"><label htmlFor="team-logo" className="cursor-pointer peer-focus-visible:border-ring peer-focus-visible:ring-3 peer-focus-visible:ring-ring/50"><ImageUp aria-hidden="true" />{t(lang, "bill.chooseFile")}</label></Button>
    <span className="min-w-0 truncate text-sm text-muted-foreground">{name || t(lang, "bill.noFile")}</span>
  </span>
}
