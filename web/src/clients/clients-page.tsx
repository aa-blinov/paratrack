import { Flash } from "@/components/flash"
import * as React from "react"
import { Archive, ArchiveRestore, Pencil, Plus, Users } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { translate as t } from "@/i18n"
import type { ClientRow, ClientsPageData, CurrencyOption } from "@/dashboard/types"

// Пустой код валюты означает «как у команды», а Radix-селект не умеет держать
// пустое значение — поэтому служебный пункт, который в форму уходит как "".
const inheritCurrency = "__workspace_currency__"

const blankClient = { Name: "", Email: "", Details: "", Notes: "", Currency: "" }

// Поля формы создания и правки одни и те же: бэкенд ждёт одинаковый набор в
// обоих POST, и держать две копии значит дать им разойтись.
function ClientFields({ id, lang, currencies, values, autoFocus = false }: {
  id: string
  lang: string
  currencies: CurrencyOption[]
  values: { Name: string; Email: string; Details: string; Notes: string; Currency: string }
  autoFocus?: boolean
}) {
  const [currency, setCurrency] = React.useState(values.Currency || inheritCurrency)

  return <>
    <input type="hidden" name="currency" value={currency === inheritCurrency ? "" : currency} />
    <div className="space-y-2 sm:col-span-2">
      <Label htmlFor={`${id}-name`}>{t(lang, "clients.name")}</Label>
      <Input id={`${id}-name`} name="name" required minLength={1} defaultValue={values.Name} placeholder={t(lang, "clients.namePlaceholder")} autoFocus={autoFocus} />
    </div>
    <div className="space-y-2 sm:col-span-2">
      <Label htmlFor={`${id}-email`}>{t(lang, "clients.email")}</Label>
      <Input id={`${id}-email`} name="email" type="email" defaultValue={values.Email} placeholder={t(lang, "clients.emailPlaceholder")} />
    </div>
    <div className="space-y-2">
      <Label htmlFor={`${id}-details`}>{t(lang, "clients.details")}</Label>
      <Textarea id={`${id}-details`} name="details" defaultValue={values.Details} placeholder={t(lang, "clients.detailsPlaceholder")} />
    </div>
    <div className="space-y-2">
      <Label htmlFor={`${id}-notes`}>{t(lang, "clients.notes")}</Label>
      <Textarea id={`${id}-notes`} name="notes" defaultValue={values.Notes} placeholder={t(lang, "clients.notesPlaceholder")} />
    </div>
    <div className="space-y-2">
      <Label htmlFor={`${id}-currency`}>{t(lang, "clients.currency")}</Label>
      <Select value={currency} onValueChange={setCurrency}>
        <SelectTrigger id={`${id}-currency`} className="w-full"><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value={inheritCurrency}>{t(lang, "clients.currencyInherit")}</SelectItem>
          {currencies.map(option => <SelectItem key={option.Code} value={option.Code}>{option.Label}</SelectItem>)}
        </SelectContent>
      </Select>
    </div>
  </>
}

// The four numbers are table cells on a wide screen. A phone drops those cells
// and shows the same numbers under the name, so none of them is lost.
const numberCell = "hidden px-4 py-3 text-right font-mono tabular-nums xl:table-cell"

function ClientRows({ client, lang, csrf, currencies, canManage, editing, onToggle }: {
  client: ClientRow
  lang: string
  csrf: string
  currencies: CurrencyOption[]
  canManage: boolean
  editing: boolean
  onToggle: (open: boolean) => void
}) {
  // Архивный клиент остаётся видимым, но приглушён: спрятать его совсем —
  // значит потерять историю проектов, которые на него ссылаются.
  const archiveAction = client.Archived ? "restore" : "archive"
  const numbers = [
    [t(lang, "clients.projects"), client.Projects],
    [t(lang, "clients.tracked"), client.TrackedLabel],
    [t(lang, "clients.billed"), client.BilledLabel],
    [t(lang, "clients.unbilled"), client.UnbilledLabel],
  ] as const

  return <>
    <tr className={`border-t align-top ${client.Archived ? "opacity-60" : ""}`}>
      <th scope="row" className="px-4 py-3 text-left font-normal xl:min-w-56">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {/* The name is the way into the client's own page: its projects,
              its documents and the notes that only make sense there. */}
          <a href={`/clients/${client.ID}`} className="font-medium hover:underline [overflow-wrap:anywhere]">{client.Name}</a>
          {client.Archived && <Badge variant="secondary">{t(lang, "projects.archivedBadge")}</Badge>}
        </div>
        {client.Email && <p className="mt-1 text-sm text-muted-foreground [overflow-wrap:anywhere]">{client.Email}</p>}
        {client.Details && <p className="mt-1 whitespace-pre-line text-sm text-muted-foreground [overflow-wrap:anywhere]">{client.Details}</p>}
        {client.Notes && <p className="mt-1 whitespace-pre-line text-xs text-muted-foreground [overflow-wrap:anywhere]">{client.Notes}</p>}
        <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 text-xs text-muted-foreground xl:hidden">
          {numbers.map(([label, value]) => <div key={label} className="grid min-w-0 gap-0.5"><dt>{label}</dt><dd className="break-words font-mono text-sm tabular-nums text-foreground">{value}</dd></div>)}
        </dl>
      </th>
      <td className={numberCell}>{client.Projects}</td>
      <td className={numberCell}>{client.TrackedLabel}</td>
      <td className={numberCell}>{client.BilledLabel}</td>
      <td className={numberCell}>{client.UnbilledLabel}</td>
      <td className="px-4 py-3 text-right whitespace-nowrap">
        {canManage && <div className="flex items-center justify-end gap-1">
          {/* On a phone the two actions are icons, so the name keeps the room
              it needs; the words come back from sm up and stay for screen readers. */}
          <Button type="button" variant="ghost" size="sm" className="whitespace-nowrap" aria-expanded={editing} onClick={() => onToggle(!editing)}>
            <Pencil aria-hidden="true" /><span className="sr-only sm:not-sr-only sm:whitespace-nowrap">{t(lang, "clients.edit")}</span>
          </Button>
          <form method="POST" action={`/clients/${client.ID}/${archiveAction}`} data-confirm={client.Archived ? undefined : t(lang, "clients.confirmArchive")}>
            <input type="hidden" name="csrf_token" value={csrf} />
            <Button type="submit" variant="ghost" size="sm" className="whitespace-nowrap">
              {client.Archived ? <ArchiveRestore aria-hidden="true" /> : <Archive aria-hidden="true" />}
              {/* not-sr-only sets white-space: normal, so the label would wrap inside the pill. */}
              <span className="sr-only sm:not-sr-only sm:whitespace-nowrap">{t(lang, client.Archived ? "clients.restore" : "clients.archive")}</span>
            </Button>
          </form>
        </div>}
      </td>
    </tr>
    {canManage && editing && <tr className="border-t">
      <td colSpan={6} className="px-4 pb-4">
        <form method="POST" action={`/clients/${client.ID}`} className="grid gap-5 border-t pt-4 sm:grid-cols-2">
          <input type="hidden" name="csrf_token" value={csrf} />
          <ClientFields id={`client-${client.ID}`} lang={lang} currencies={currencies} values={client} />
          <div className="flex items-center justify-between gap-2 border-t pt-4 sm:col-span-2">
            <Button type="button" variant="ghost" size="sm" onClick={() => onToggle(false)}>{t(lang, "clients.cancel")}</Button>
            <Button type="submit" size="sm">{t(lang, "clients.save")}</Button>
          </div>
        </form>
      </td>
    </tr>}
  </>
}

export function ClientsPage({ data }: { data: ClientsPageData }) {
  const lang = data.Lang
  // Правка открывается строкой под клиентом: список остаётся на экране, и
  // видно, к кому относится правка.
  const [editing, setEditing] = React.useState<number | null>(null)
  const clients = data.Clients ?? []
  const archivedURL = data.ShowArchived ? "/clients" : "/clients?archived=1"

  return <main className="mx-auto w-full max-w-5xl space-y-5">
    <header className="flex flex-wrap items-end justify-between gap-4">
      <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "clients.heading")}</h1>
      <div className="flex flex-wrap items-center gap-2">
        <Button asChild variant="outline" size="sm">
          <a href={archivedURL}>{t(lang, data.ShowArchived ? "clients.hideArchived" : "clients.showArchived")}</a>
        </Button>
      </div>
    </header>

    <Flash message={data.Flash} ok={data.FlashOK} />

    {data.CanManage && <details id="client-new" open={!clients.length} className="group rounded-xl border bg-card text-card-foreground"><summary className="flex cursor-pointer list-none items-center gap-2 p-4 font-medium marker:hidden"><Plus aria-hidden="true" className="size-4" />{t(lang, "clients.new")}</summary><div className="p-5 pt-0 sm:p-6 sm:pt-0">
      <form method="POST" action="/clients" className="grid gap-5 sm:grid-cols-2">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <ClientFields id="new-client" lang={lang} currencies={data.Currencies} values={blankClient} autoFocus={!clients.length} />
        <div className="flex justify-end border-t pt-4 sm:col-span-2">
          <Button type="submit"><Plus aria-hidden="true" />{t(lang, "clients.save")}</Button>
        </div>
      </form>
    </div></details>}

    {clients.length ? <Card><CardContent className="p-0">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="text-left text-xs text-muted-foreground">
              <th scope="col" className="px-4 py-3 font-medium">{t(lang, "clients.name")}</th>
              <th scope="col" className="hidden px-4 py-3 text-right font-medium xl:table-cell">{t(lang, "clients.projects")}</th>
              <th scope="col" className="hidden px-4 py-3 text-right font-medium xl:table-cell">{t(lang, "clients.tracked")}</th>
              <th scope="col" className="hidden px-4 py-3 text-right font-medium xl:table-cell">{t(lang, "clients.billed")}</th>
              <th scope="col" className="hidden px-4 py-3 text-right font-medium xl:table-cell">{t(lang, "clients.unbilled")}</th>
              <th scope="col" className="px-4 py-3"><span className="sr-only">{t(lang, "clients.actions")}</span></th>
            </tr>
          </thead>
          <tbody>
            {clients.map(client => <ClientRows key={client.ID}
              client={client}
              lang={lang}
              csrf={data.CSRFToken}
              currencies={data.Currencies}
              canManage={data.CanManage}
              editing={editing === client.ID}
              onToggle={open => setEditing(open ? client.ID : null)}
            />)}
          </tbody>
        </table>
      </div>
    </CardContent></Card> : <Card>
      <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
        <Users aria-hidden="true" className="size-5 text-muted-foreground" />
        <p className="text-sm text-muted-foreground">{t(lang, "clients.empty")}</p>
        <p className="max-w-md text-sm text-muted-foreground">{t(lang, "clients.emptyHint")}</p>
        {data.CanManage && <Button asChild size="sm"><a href="#client-new"><Plus aria-hidden="true" />{t(lang, "clients.new")}</a></Button>}
      </CardContent>
    </Card>}
  </main>
}
