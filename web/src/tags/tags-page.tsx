import * as React from "react"
import { Plus, Tag as TagIcon, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"
import type { TagView, TagsData } from "@/dashboard/types"

type TagResponse = { id: number; name: string; session_count: number }

export function TagsPage({ data }: { data: TagsData }) {
  const [tags, setTags] = React.useState(data.Tags)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState("")
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
    if (!window.confirm(t(lang, "tags.confirmDelete"))) return
    setBusy(true); setError("")
    try { await send(`/api/tags?id=${tag.ID}`, "DELETE"); await refresh() }
    catch (cause) { setError(cause instanceof Error ? cause.message : t(lang, "err.generic")) }
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
      <ul className="flex flex-wrap gap-2" aria-label={t(lang, "tags.all")}>
        {tags.map(tag => <li key={tag.ID} className="inline-flex max-w-full items-center gap-2 rounded-full border px-3 py-1.5 text-sm">
          <TagIcon aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" /><span className="min-w-0 truncate">#{tag.Name}</span><span aria-hidden="true" className="text-muted-foreground">/</span><span className="font-mono font-semibold tabular-nums" title={t(lang, "tags.countTitle")}>{tag.SessionCount}</span>
          {data.CanManage && <Button type="button" variant="ghost" size="icon" className="size-7" disabled={busy} title={t(lang, "tags.delete")} aria-label={`${t(lang, "tags.delete")}: ${tag.Name}`} onClick={() => void remove(tag)}><Trash2 aria-hidden="true" /></Button>}
        </li>)}
      </ul>
      <p className="mt-4 text-sm text-muted-foreground">{t(lang, "tags.filterHint")}</p>
    </CardContent></Card> : <div className="py-8 text-center text-sm text-muted-foreground"><p className="font-medium">{t(lang, "tags.noTags")}</p><p>{t(lang, "tags.noTagsHint")}</p></div>}
  </main>
}
