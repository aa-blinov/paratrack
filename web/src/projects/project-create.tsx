import { Disclosure, DisclosureTrigger } from "@/components/ui/collapsible"
import * as React from "react"
import { ArrowLeft, ChevronDown, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { translate as t } from "@/i18n"
import type { ProjectCreateData } from "@/dashboard/types"
import { DateField } from "@/components/date-field"

export function ProjectCreate({ data }: { data: ProjectCreateData }) {
  const [color, setColor] = React.useState(data.SuggestedColor || "#6366f1")
  const noLead = "__no_lead__"
  const [lead, setLead] = React.useState(noLead)
  const noClient = "__no_client__"
  const [client, setClient] = React.useState(noClient)
  const [currency, setCurrency] = React.useState("__workspace_currency__")
  const lang = data.Lang

  return <main className="mx-auto w-full max-w-3xl space-y-6">
    <a href="/projects" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft aria-hidden="true" className="size-4" />{t(lang, "nav.projects")}</a>
    <header>
      <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "projects.new")}</h1>
      <p className="mt-1 text-sm text-muted-foreground">{t(lang, "projects.slugHint")}</p>
    </header>
    <Card><CardContent className="p-5 sm:p-6">
      <form method="POST" action="/projects/new" className="grid gap-5 sm:grid-cols-2">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="new-project-name">{t(lang, "projects.name")}</Label><Input id="new-project-name" name="name" required minLength={1} placeholder={t(lang, "ph.project")} autoFocus /></div>
        <div className="space-y-2">
          <Label htmlFor="new-project-rate">{t(lang, "bill.rate")} {t(lang, "bill.cents")} <span className="font-normal text-muted-foreground">({t(lang, "projects.optional")})</span></Label>
          <Input id="new-project-rate" name="rate" inputMode="decimal" autoComplete="off" placeholder={t(lang, "bill.ratePh")} />
          <p className="text-xs text-muted-foreground">{t(lang, "bill.rateHint")}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="new-project-budget">{t(lang, "budget.amount")} {t(lang, "bill.cents")} <span className="font-normal text-muted-foreground">({t(lang, "projects.optional")})</span></Label>
          <Input id="new-project-budget" name="budget" inputMode="decimal" autoComplete="off" placeholder={t(lang, "budget.amountPlaceholder")} />
          <p className="text-xs text-muted-foreground">{t(lang, "budget.hint")}</p>
        </div>
        <Disclosure open={Boolean(data.Mods?.invoices)} className="sm:col-span-2">
          <DisclosureTrigger className="group inline-flex min-h-11 cursor-pointer items-center gap-1 text-sm text-muted-foreground sm:min-h-0">{t(lang, "projects.options")}<ChevronDown aria-hidden="true" className="size-4 transition-transform group-data-[state=open]:rotate-180" /></DisclosureTrigger>
          <div className="mt-4 grid gap-5 sm:grid-cols-2">
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="new-project-slug">{t(lang, "projects.slugOpt")}</Label><Input id="new-project-slug" name="slug" pattern="[a-z0-9](?:[a-z0-9_]|-)*" className="font-mono" placeholder="sait-dlya-klienta" /></div>
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">{t(lang, "projects.color")}</legend>
          <div className="flex items-center gap-2">
            <Input type="color" aria-label={t(lang, "projects.colorPicker")} value={color} onChange={event => setColor(event.target.value)} className="size-10 cursor-pointer rounded-md border border-input bg-background p-1" />
            <Input name="color" aria-label={t(lang, "projects.colorHex")} value={color} onChange={event => setColor(event.target.value)} pattern="^#[0-9a-fA-F]{6}$" className="font-mono" required />
          </div>
        </fieldset>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="new-project-currency">{t(lang, "proj.currency")}</Label>
          <input type="hidden" name="currency" value={currency === "__workspace_currency__" ? "" : currency} />
          <Select value={currency} onValueChange={setCurrency}>
            <SelectTrigger id="new-project-currency" className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__workspace_currency__">{t(lang, "proj.currencyInherit")} ({data.TeamCurrency})</SelectItem>
              {data.Currencies.map(option => <SelectItem key={option.Code} value={option.Code}>{option.Label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="new-project-deadline">{t(lang, "proj.deadlineField")}</Label>
          <DateField id="new-project-deadline" name="deadline" />
        </div>
        <div className="space-y-2">
          <Label htmlFor="new-project-client">{t(lang, "clients.heading")}</Label>
          {data.ClientOptions?.length ? <>
            <input type="hidden" name="client_id" value={client === noClient ? "" : client} />
            <Select value={client} onValueChange={setClient}>
              <SelectTrigger id="new-project-client" className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value={noClient}>{t(lang, "project.noClient")}</SelectItem>
                {data.ClientOptions.map(option => <SelectItem key={option.ID} value={String(option.ID)}>{option.Name}</SelectItem>)}
              </SelectContent>
            </Select>
          </> : <p className="text-sm text-muted-foreground">{t(lang, "project.noClientsYet")} <a href="/clients" className="font-medium text-foreground underline underline-offset-4">{t(lang, "clients.new")}</a></p>}
        </div>
        {!!data.MemberOptions?.length && <div className="space-y-2">
          <Label htmlFor="new-project-lead">{t(lang, "proj.leadField")}</Label>
          <input type="hidden" name="lead_user_id" value={lead === noLead ? "" : lead} />
          <Select value={lead} onValueChange={setLead}>
            <SelectTrigger id="new-project-lead" className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={noLead}>{t(lang, "proj.leadNone")}</SelectItem>
              {data.MemberOptions.map(option => <SelectItem key={option.ID} value={String(option.ID)}>{option.Name}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>}
          </div>
        </Disclosure>
        <div className="flex items-center justify-between gap-2 border-t pt-4 sm:col-span-2">
          <Button asChild variant="ghost" size="sm"><a href="/projects">{t(lang, "projects.cancel")}</a></Button>
          <Button type="submit"><Plus aria-hidden="true" />{t(lang, "projects.create")}</Button>
        </div>
      </form>
    </CardContent></Card>
  </main>
}
