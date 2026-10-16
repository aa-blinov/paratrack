import { useCallback, useState } from "react"
import { responseMessage } from "@/lib/response-message"

// One way for a choice to save: the whole form goes up in one request, so a
// server that rewrites every field from the form (preferences, sections) stays
// correct, and the page does not reload. The status line is the feedback.
export type SaveState = { kind: "idle" | "saving" | "saved" | "error"; text: string }

export async function postForm(form: HTMLFormElement, submitter?: HTMLButtonElement | null): Promise<{ ok: true } | { ok: false; text: string }> {
  const body = new URLSearchParams()
  for (const [key, value] of new FormData(form)) {
    if (typeof value === "string") body.append(key, value)
  }
  // The button that was pressed is part of the form, as a browser would send it.
  if (submitter?.name) body.append(submitter.name, submitter.value)
  try {
    const response = await fetch(form.action, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", Accept: "text/html" },
      body,
    })
    if (!response.ok) return { ok: false, text: await responseMessage(response) }
    // The server answers with a redirect to the page. A failure carries its
    // message as «e:…» in the flash; a success carries a plain code (or none).
    const flash = new URL(response.url).searchParams.get("flash") || ""
    if (flash.startsWith("e:")) {
      return { ok: false, text: decodeURIComponent(flash.slice(2)) }
    }
    // A bare «bad_request» is a refusal too; it used to read as saved.
    if (flash === "bad_request") return { ok: false, text: "" }
    return { ok: true }
  } catch {
    return { ok: false, text: "" }
  }
}

// The status a choice shows next to itself: saving, saved, or why it failed.
export function useSaveStatus(savedText: string, failedText: string) {
  const [state, setState] = useState<SaveState>({ kind: "idle", text: "" })
  const save = useCallback(async (form: HTMLFormElement, submitter?: HTMLButtonElement | null) => {
    setState({ kind: "saving", text: "" })
    const result = await postForm(form, submitter)
    setState(result.ok ? { kind: "saved", text: savedText } : { kind: "error", text: result.text || failedText })
    return result.ok
  }, [savedText, failedText])
  return [state, save] as const
}
