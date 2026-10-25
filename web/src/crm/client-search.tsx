import * as React from "react"
import { Search } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { translate as t } from "@/i18n"
import { responseMessage } from "@/lib/response-message"

type SearchItem = { title: string; detail: string; href: string }
type SearchGroup = { kind: string; label: string; items: SearchItem[] }

// The CRM search box looks across the clients and the contacts of the workspace.
// It asks the same global search endpoint as the menu, with room for fifty rows
// of each kind, and keeps only those two groups.
export function ClientSearch({ lang }: { lang: string }) {
  const [text, setText] = React.useState("")
  const [groups, setGroups] = React.useState<SearchGroup[]>([])
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")
  const trimmed = text.trim()
  const long = [...trimmed].length >= 2

  // The answer waits for a pause in typing, and a newer text cancels the older request.
  React.useEffect(() => {
    if ([...text.trim()].length < 2) {
      setGroups([])
      setError("")
      setBusy(false)
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(async () => {
      setBusy(true)
      try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(text.trim())}&limit=50`, {
          credentials: "same-origin",
          headers: { Accept: "application/json" },
          signal: controller.signal,
        })
        if (!response.ok) throw new Error(await responseMessage(response))
        const json = (await response.json()) as { groups: SearchGroup[] }
        setGroups(json.groups.filter(group => group.kind === "clients" || group.kind === "contacts"))
        setError("")
      } catch (cause) {
        if (controller.signal.aborted) return
        setError(cause instanceof Error && cause.message ? cause.message : t(lang, "err.generic"))
      } finally {
        if (!controller.signal.aborted) setBusy(false)
      }
    }, 200)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [text, lang])

  const answered = long && !busy && !error

  return <Card>
    <CardHeader><CardTitle>{t(lang, "crm.search.title")}</CardTitle></CardHeader>
    <CardContent className="grid gap-4">
      <div className="flex items-center gap-2">
        <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <Input type="search" value={text} onChange={event => setText(event.target.value)} placeholder={t(lang, "crm.search.placeholder")} aria-label={t(lang, "crm.search.title")} aria-busy={busy || undefined} className="min-w-0" style={{ fontSize: 16 }} />
      </div>
      <p className="text-xs text-muted-foreground">{t(lang, "crm.search.hint")}</p>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {groups.map(group => <section key={group.kind} aria-label={group.label} className="grid gap-1">
        <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{group.label}</h2>
        <ul className="grid divide-y rounded-lg border">
          {group.items.map(item => <li key={`${item.href}|${item.title}`}>
            <a href={item.href} className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 px-3 py-2 hover:bg-accent">
              <span className="min-w-0 font-medium [overflow-wrap:anywhere]">{item.title}</span>
              {item.detail && <span className="min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">{item.detail}</span>}
            </a>
          </li>)}
        </ul>
      </section>)}
      {answered && groups.length === 0 && <p className="text-sm text-muted-foreground">{t(lang, "search.empty")}</p>}
    </CardContent>
  </Card>
}
