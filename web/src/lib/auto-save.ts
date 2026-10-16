import { useCallback, useRef, useState } from "react"
import { responseMessage } from "@/lib/response-message"

// One way for a choice to save: the whole form goes up in one request, so a
// server that rewrites every field from the form (preferences, sections) stays
// correct, and the page does not reload. The status line is the feedback.
export type SaveState = { kind: "idle" | "saving" | "saved" | "error"; text: string }

// url is the page the server redirected to, when there is one. A preset uses
// it to show the saved set as the page, not a stale copy of it. answer is the
// body a JSON endpoint sent back.
export type SaveResult = { ok: boolean; text: string; url?: string; answer?: unknown }

// The one request behind every save. A form answers with a redirect, read as it
// always was; a JSON endpoint answers with its body, returned as answer. A
// failure carries the message from responseMessage.
export async function postSave<T = unknown>(action: string, body: URLSearchParams, csrf?: string): Promise<SaveResult & { answer?: T }> {
  try {
    const response = await fetch(action, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", Accept: "text/html", ...(csrf ? { "X-CSRF-Token": csrf } : {}) },
      body,
    })
    if (!response.ok) return { ok: false, text: await responseMessage(response) }
    if (response.headers.get("Content-Type")?.includes("application/json")) return { ok: true, text: "", answer: await response.json() as T }
    // The server answers with a redirect to the page. A failure carries its
    // message as «e:…» in the flash; a success carries a plain code (or none).
    const target = new URL(response.url)
    const url = target.pathname.startsWith("/api/") ? undefined : target.href
    const flash = target.searchParams.get("flash") || ""
    if (flash.startsWith("e:")) return { ok: false, text: decodeURIComponent(flash.slice(2)), url }
    // A bare «bad_request» is a refusal too; it used to read as saved.
    if (flash === "bad_request") return { ok: false, text: "", url }
    return { ok: true, text: "", url }
  } catch {
    return { ok: false, text: "" }
  }
}

export async function postForm(form: HTMLFormElement, submitter?: HTMLButtonElement | null): Promise<SaveResult> {
  const body = new URLSearchParams()
  for (const [key, value] of new FormData(form)) {
    if (typeof value === "string") body.append(key, value)
  }
  // The button that was pressed is part of the form, as a browser would send it.
  if (submitter?.name) body.append(submitter.name, submitter.value)
  return postSave(form.action, body)
}

type Job = { key: string; send: () => Promise<SaveResult>; done: Array<(result: SaveResult) => void> }

// A form is its own key, so two forms that post to the same action (the preset
// cards and the list on the sections page) never merge into one request.
const formKeys = new WeakMap<HTMLFormElement, string>()
let formCount = 0
function formKey(form: HTMLFormElement): string {
  let key = formKeys.get(form)
  if (!key) { key = `form-${++formCount}`; formKeys.set(form, key) }
  return key
}

// The status a choice shows next to itself: saving, saved, or why it failed.
// One request goes at a time. A choice made while one is in flight waits and
// goes up after it, reading the form as it is then, so the last click is the
// one that is kept; each caller gets the answer of the request that carried it.
// A save with the key of the one waiting last takes its place; another key
// queues behind it, so a topic list and a form can share this hook.
export function useSaveStatus(savedText: string, failedText: string) {
  const [state, setState] = useState<SaveState>({ kind: "idle", text: "" })
  const queue = useRef<Job[]>([])
  const running = useRef(false)
  const drain = useCallback(async () => {
    if (running.current) return
    running.current = true
    try {
      for (let job = queue.current.shift(); job; job = queue.current.shift()) {
        setState({ kind: "saving", text: "" })
        const result = await job.send()
        setState(result.ok ? { kind: "saved", text: savedText } : { kind: "error", text: result.text || failedText })
        job.done.forEach(done => done(result))
      }
    } finally {
      running.current = false
    }
  }, [savedText, failedText])
  const save = useCallback((key: string, send: () => Promise<SaveResult>) => new Promise<SaveResult>(resolve => {
    const last = queue.current[queue.current.length - 1]
    if (last && last.key === key) {
      last.send = send
      last.done.push(resolve)
    } else {
      queue.current.push({ key, send, done: [resolve] })
    }
    void drain()
  }), [drain])
  const saveForm = useCallback((form: HTMLFormElement, submitter?: HTMLButtonElement | null) => save(formKey(form), () => postForm(form, submitter)), [save])
  return [state, saveForm, save] as const
}
