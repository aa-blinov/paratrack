import * as React from "react"
import { X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FieldIssue, FormIssue } from "@/components/form-issue"
import { StatusLine } from "@/components/status-line"
import { translate as t } from "@/i18n"
import { ApiError, apiJSON, type Issue } from "@/lib/json-api"

// The server keeps the same limits (model.NormalizeClientTags), so a tag the form
// refuses here is one the server would refuse too.
const MAX_TAGS = 10
const MAX_LENGTH = 40

function sameTags(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((tag, index) => tag === b[index])
}

/**
 * The tags of a client card. Every member reads them as text. A manager also
 * adds and removes them and saves the whole set at once, which is how the
 * server stores them too.
 */
export function ClientTags({ lang, csrf, clientID, tags, canManage }: {
  lang: string
  csrf: string
  clientID: number
  tags: string[]
  canManage: boolean
}) {
  const [stored, setStored] = React.useState(tags)
  const [items, setItems] = React.useState(tags)
  const [draft, setDraft] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [saved, setSaved] = React.useState(false)
  const dirty = !sameTags(items, stored)

  function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const tag = draft.trim().toLowerCase()
    if (!tag) return
    setSaved(false)
    setIssue(null)
    if (items.includes(tag)) {
      setDraft("")
      return
    }
    if (items.length >= MAX_TAGS || [...tag].length > MAX_LENGTH || tag.includes(",")) {
      setIssue({ key: "tags", message: t(lang, "clients.tagsInvalid") })
      return
    }
    setItems([...items, tag])
    setDraft("")
  }

  function remove(tag: string) {
    setSaved(false)
    setItems(items.filter(item => item !== tag))
  }

  async function save() {
    setBusy(true)
    setIssue(null)
    setSaved(false)
    try {
      const body = await apiJSON<{ tags: string[] }>(`/api/clients/${clientID}/tags`, "POST", csrf, { tags: items })
      setStored(body.tags)
      setItems(body.tags)
      setSaved(true)
    } catch (cause) {
      const message = cause instanceof ApiError && cause.field === "tags"
        ? t(lang, "clients.tagsInvalid")
        : cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic")
      setIssue({ key: cause instanceof ApiError && cause.field === "tags" ? "tags" : "", message })
    } finally {
      setBusy(false)
    }
  }

  if (!canManage) {
    if (tags.length === 0) return null
    return <div className="grid gap-2">
      <h2 className="text-xs font-semibold tracking-wide text-muted-foreground">{t(lang, "clients.tags")}</h2>
      <ul className="flex flex-wrap gap-2">{tags.map(tag => <li key={tag}><Badge variant="secondary">{tag}</Badge></li>)}</ul>
    </div>
  }

  return <div className="grid gap-3" aria-busy={busy}>
    <h2 className="text-xs font-semibold tracking-wide text-muted-foreground">{t(lang, "clients.tags")}</h2>
    {items.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "clients.tagsEmpty")}</p> : <ul className="flex flex-wrap gap-2">
      {items.map(tag => <li key={tag}>
        <Badge variant="secondary" className="gap-1 pe-1">
          <span>{tag}</span>
          <button type="button" aria-label={t(lang, "clients.tagRemove", tag)} onClick={() => remove(tag)} className="rounded-sm p-0.5 hover:bg-accent">
            <X aria-hidden="true" className="size-3" />
          </button>
        </Badge>
      </li>)}
    </ul>}
    <form onSubmit={add} className="grid gap-2 sm:max-w-md">
      <div className="flex gap-2">
        <Input id="client-tag-draft" value={draft} onChange={event => setDraft(event.target.value)} aria-label={t(lang, "clients.tagAdd")} placeholder={t(lang, "clients.tagPlaceholder")} maxLength={MAX_LENGTH} aria-invalid={issue?.key === "tags" || undefined} aria-describedby={issue?.key === "tags" ? "client-tag-issue" : undefined} style={{ fontSize: 16 }} />
        <Button type="submit" variant="outline" disabled={!draft.trim()}>{t(lang, "clients.tagAdd")}</Button>
      </div>
      <FieldIssue issue={issue} name="tags" id="client-tag-issue" />
    </form>
    <FormIssue issue={issue} />
    <div className="flex flex-wrap items-center gap-3">
      <Button type="button" disabled={busy || !dirty} onClick={() => void save()}>{t(lang, "clients.tagsSave")}</Button>
      <StatusLine text={saved ? t(lang, "clients.tagsSaved") : ""} />
    </div>
    <p className="text-xs text-muted-foreground">{t(lang, "clients.tagsHint")}</p>
  </div>
}
