import type { Issue } from "@/lib/json-api"

/** The refusal under the input it names; the form-level one is rendered by the form. */
export function FieldIssue({ issue, name, id }: { issue: Issue | null; name: string; id: string }) {
  if (!issue || issue.key !== name) return null
  return <p id={id} role="alert" className="text-sm text-destructive">{issue.message}</p>
}

/** The refusal that names no input, shown once above the form's buttons. */
export function FormIssue({ issue }: { issue: Issue | null }) {
  if (!issue || issue.key !== "") return null
  return <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{issue.message}</p>
}
