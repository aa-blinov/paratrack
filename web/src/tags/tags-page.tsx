import { requestConfirmation } from "@/components/confirmation-dialog"
import * as React from "react"
import { Check, Pencil, Plus, Tag as TagIcon, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import type { TagView, TagsData } from "@/dashboard/types"

type TagResponse = { id: number; name: string; session_count: number }

// A week, not a day: a tag is a cross-cutting label, and "today" is too small a
// window to tell whether the label is earning its place.
function tagTimeQuery(name: string) {
  const query = new URLSearchParams({ period: "week", tag: name })
  return query
}

export function TagsPage({ data }: { data: TagsData }) {
  const [tags, setTags] = React.useState(data.Tags)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")
  const [renaming, setRenaming] = React.useState<number | null>(null)
  const [renameError, setRenameError] = React.useState("")
  const renameButtons = React.useRef(new Map<number, HTMLButtonElement>())
  const lang = data.Lang

  async function refresh() {
    const response = await fetch("/api/tags", { credentials: "same-origin", headers: { Accept: "application/json" } })
    if (!response.ok) throw new Error(response.statusText)
    const payload = await response.json() as { tags: TagResponse[] }
    setTags(payload.tags.map((tag): TagView => ({ ID: tag.id, Name: tag.name, SessionCount: tag.session_count, Lang: data.Lang })))
  }

  async function send(url: string, method: string, body?: URLSearchParams) {
    const response = await fetch(url, { method, credentials: "same-origin", headers: { "X-CSRF-Token": data.CSRFToken, ...(body ? { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" } : {}) }, body })
    if (!response.ok) {
      const text = await response.text()
      throw new Error(text.replace(/<[^>]*>/g, " ").trim() || response.statusText)
    }
  }

  async function create(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const name = String(new FormData(form).get("name") || "").trim()
    if (!name) return
    setBusy(true); setError("")
    try { await send("/api/tags", "POST", new URLSearchParams({ name })); await refresh(); form.reset() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
    finally { setBusy(false) }
  }

  async function remove(tag: TagView) {
    if (!await requestConfirmation(t(lang, "tags.confirmDelete"))) return
    setBusy(true); setError("")
    try { await send(`/api/tags?id=${tag.ID}`, "DELETE"); await refresh() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
    finally { setBusy(false) }
  }

  // A typo is fixed in place: the rename keeps the tag row, so the sessions
  // already carrying it follow the new name instead of losing it.
  function openRename(tag: TagView) { setRenaming(tag.ID); setRenameError("") }
  function closeRename(tagID: number) { setRenaming(null); setRenameError(""); renameButtons.current.get(tagID)?.focus() }

  async function rename(tag: TagView, name: string) {
    if (!name.trim()) return
    setBusy(true); setRenameError("")
    try { await send("/api/tags", "PATCH", new URLSearchParams({ id: String(tag.ID), name: name.trim() })); await refresh(); closeRename(tag.ID) }
    catch (cause) { setRenameError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
    finally { setBusy(false) }
  }

  React.useEffect(() => {
    const timer = window.setInterval(() => void refresh().catch(() => {}), 60_000)
    return () => window.clearInterval(timer)
  }, [])

  return <main className="mx-auto w-full max-w-4xl space-y-5">
    <header><h1 className="text-2xl font-semibold tracking-tight">{t(lang, "stats.tags")}</h1><p className="mt-1 text-sm text-muted-foreground">{t(lang, "tags.blurbFull")}</p></header>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    <Card><CardHeader><CardTitle>{t(lang, "tags.new")}</CardTitle></CardHeader><CardContent>
      <form onSubmit={create} className="flex flex-col gap-3 sm:flex-row sm:items-end" aria-busy={busy}>
        <div className="min-w-0 flex-1 space-y-2"><Label htmlFor="new-tag-name">{t(lang, "projects.name")}</Label><Input id="new-tag-name" name="name" list="known-tags" placeholder={t(lang, "ph.tag")} required /><datalist id="known-tags">{data.AllTagNames.map(name => <option key={name} value={name} />)}</datalist></div>
        <Button type="submit" disabled={busy}><Plus aria-hidden="true" />{t(lang, "tags.add")}</Button>
      </form>
    </CardContent></Card>
    {tags.length ? <Card><CardHeader><CardTitle>{t(lang, "tags.all")} ({tags.length})</CardTitle></CardHeader><CardContent>
      <ul className="flex flex-wrap items-start gap-2" aria-label={t(lang, "tags.all")}>
        {tags.map(tag => <li key={tag.ID} className="inline-flex max-w-full flex-col gap-1 rounded-full border px-3 py-1.5 text-sm">
          <span className="flex max-w-full items-center gap-2">
            <TagIcon aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
            {/* The chip is the way into the time this tag carries — the same
                filter the session row on /stats opens, from the screen that
                owns the tag instead of from a dashboard the user must find. */}
            <a href={`/stats?${tagTimeQuery(tag.Name)}`} title={t(lang, "tags.timeByTag")} aria-label={`${t(lang, "tags.timeByTag")}: #${tag.Name}`} className="min-w-0 truncate underline-offset-4 hover:underline">#{tag.Name}</a>
            <span aria-hidden="true" className="text-muted-foreground">/</span><span className="font-mono font-semibold tabular-nums" title={t(lang, "tags.countTitle")}>{tag.SessionCount}</span>
            {data.CanManage && <><Button type="button" variant="ghost" size="icon" className="size-7" disabled={busy} data-tag-rename ref={node => { if (node) renameButtons.current.set(tag.ID, node); else renameButtons.current.delete(tag.ID) }} aria-expanded={renaming === tag.ID} title={t(lang, "tags.rename")} aria-label={`${t(lang, "tags.rename")}: ${tag.Name}`} onClick={() => openRename(tag)}><Pencil aria-hidden="true" /></Button>
              <Button type="button" variant="ghost" size="icon" className="size-7" disabled={busy} title={t(lang, "tags.delete")} aria-label={`${t(lang, "tags.delete")}: ${tag.Name}`} onClick={() => void remove(tag)}><Trash2 aria-hidden="true" /></Button></>}
          </span>
          {renaming === tag.ID && <RenameTagForm lang={lang} tag={tag} busy={busy} error={renameError} onSubmit={name => void rename(tag, name)} onCancel={() => closeRename(tag.ID)} />}
        </li>)}
      </ul>
      <p className="mt-4 text-sm text-muted-foreground">{t(lang, "tags.filterHint")}</p>
    </CardContent></Card> : <div className="py-8 text-center text-sm text-muted-foreground"><p className="font-medium">{t(lang, "tags.noTags")}</p><p>{t(lang, "tags.noTagsHint")}</p></div>}
  </main>
}

// The rename editor lives inside the tag it edits, so only that row grows.
// The error line is always present — empty when there is nothing to say — so
// a rejected name never shifts the chips around it.
function RenameTagForm({ lang, tag, busy, error, onSubmit, onCancel }: {
  lang: string
  tag: TagView
  busy: boolean
  error: string
  onSubmit: (name: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = React.useState(tag.Name)
  const input = React.useRef<HTMLInputElement>(null)
  React.useEffect(() => { input.current?.focus(); input.current?.select() }, [])
  return <form data-tag-rename-form className="flex min-w-0 flex-col gap-1" onSubmit={event => { event.preventDefault(); onSubmit(value) }} onKeyDown={event => { if (event.key === "Escape") onCancel() }}>
    <div className="flex min-w-0 items-center gap-1">
      <Input ref={input} aria-label={`${t(lang, "tags.rename")}: ${tag.Name}`} aria-invalid={error ? true : undefined} aria-describedby="tag-rename-error" value={value} onChange={event => setValue(event.target.value)} className="h-7 w-48" />
      <Button type="submit" size="icon-sm" variant="outline" disabled={busy} title={t(lang, "projects.save")} aria-label={`${t(lang, "projects.save")}: ${tag.Name}`}><Check aria-hidden="true" /></Button>
      <Button type="button" size="icon-sm" variant="ghost" disabled={busy} title={t(lang, "projects.cancel")} aria-label={`${t(lang, "projects.cancel")}: ${tag.Name}`} onClick={onCancel}><X aria-hidden="true" /></Button>
    </div>
    <span id="tag-rename-error" role="alert" className="max-w-48 text-xs text-destructive [overflow-wrap:anywhere]">{error}</span>
  </form>
}
