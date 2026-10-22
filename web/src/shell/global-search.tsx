import * as React from "react"
import { Search } from "lucide-react"
import { Input } from "@/components/ui/input"
import { responseMessage } from "@/lib/response-message"
import { translate as t } from "@/i18n"

type SearchGroup = { label: string; items: Array<{ title: string; detail: string; href: string }> }

// The global search (UI-01): Ctrl+K, or / outside a field, opens one box that looks
// across clients, projects, tasks, tags, people and the viewer's own sessions. Every
// answer is a link to the page that holds it. Esc closes the box.
export function GlobalSearch({ lang }: { lang: string }) {
  const [open, setOpen] = React.useState(false)
  const [text, setText] = React.useState("")
  const [groups, setGroups] = React.useState<SearchGroup[]>([])
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")

  React.useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault()
        setOpen(true)
        return
      }
      const typing = Boolean((event.target as HTMLElement | null)?.closest?.("input, textarea, select, [contenteditable=true]"))
      if (event.key === "/" && !typing && !event.ctrlKey && !event.metaKey && !event.altKey) {
        event.preventDefault()
        setOpen(true)
      }
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [])

  // The answer waits for a pause in typing, and a newer text cancels the older request.
  React.useEffect(() => {
    if (!open) return
    const trimmed = text.trim()
    if ([...trimmed].length < 2) {
      setGroups([])
      setError("")
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(async () => {
      setBusy(true)
      try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(trimmed)}`, {
          credentials: "same-origin",
          headers: { Accept: "application/json" },
          signal: controller.signal,
        })
        if (!response.ok) throw new Error(await responseMessage(response))
        const json = (await response.json()) as { groups: SearchGroup[] }
        setGroups(json.groups)
        setError("")
      } catch (cause) {
        if (controller.signal.aborted) return
        setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      } finally {
        if (!controller.signal.aborted) setBusy(false)
      }
    }, 200)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [open, text, lang])

  if (!open) return null
  const answered = text.trim().length >= 2 && !busy && !error
  return <div
    role="dialog"
    aria-modal="true"
    aria-label={t(lang, "search.title")}
    className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 p-4 pt-[12vh]"
    onMouseDown={event => { if (event.target === event.currentTarget) setOpen(false) }}
    onKeyDown={event => { if (event.key === "Escape") setOpen(false) }}
  >
    <div className="grid w-full max-w-xl gap-3 rounded-lg border bg-popover p-4 text-popover-foreground shadow-lg">
      <div className="flex items-center gap-2">
        <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <Input autoFocus type="search" value={text} onChange={event => setText(event.target.value)} placeholder={t(lang, "search.placeholder")} aria-label={t(lang, "search.title")} className="h-10 min-w-0" />
      </div>
      <p className="text-xs text-muted-foreground">{t(lang, "search.hint")}</p>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {groups.length > 0 && <div className="grid max-h-[60vh] gap-4 overflow-y-auto">
        {groups.map(group => <section key={group.label} aria-label={group.label} className="grid gap-1">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{group.label}</h2>
          <ul className="grid">
            {group.items.map(item => <li key={`${item.href}|${item.title}`}>
              <a href={item.href} onClick={() => setOpen(false)} className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 rounded-md px-2 py-2 hover:bg-accent">
                <span className="min-w-0 truncate font-medium">{item.title}</span>
                {item.detail && <span className="min-w-0 truncate text-xs text-muted-foreground">{item.detail}</span>}
              </a>
            </li>)}
          </ul>
        </section>)}
      </div>}
      {answered && groups.length === 0 && <p className="text-sm text-muted-foreground">{t(lang, "search.empty")}</p>}
    </div>
  </div>
}
