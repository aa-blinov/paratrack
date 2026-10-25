import * as React from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { DisclosureSection } from "@/components/disclosure-section"
import { FormIssue } from "@/components/form-issue"
import { StatusLine } from "@/components/status-line"
import { translate as t } from "@/i18n"
import { apiJSON, issueOf, type Issue } from "@/lib/json-api"
import { formatDateTime } from "@/lib/date-format"
import { intakeExample } from "@/crm/model"

type KeyState = { active: boolean; created_at?: string; key?: string }

/**
 * The website's enquiry key, for managers. The raw key comes back from the server only
 * when it is made, so it is shown here once, with the request that sends an enquiry.
 * Making a new key replaces the old one at once.
 */
export function IntakeKeyCard({ lang, csrf }: { lang: string; csrf: string }) {
  const [state, setState] = React.useState<KeyState | null>(null)
  const [shown, setShown] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [copied, setCopied] = React.useState(false)

  React.useEffect(() => {
    apiJSON<KeyState>("/api/crm/intake-key", "GET", "")
      .then(setState)
      .catch(cause => setIssue(issueOf(cause, {}, t(lang, "err.generic"))))
  }, [lang])

  async function make() {
    setBusy(true)
    setIssue(null)
    setCopied(false)
    try {
      const body = await apiJSON<KeyState>("/api/crm/intake-key", "POST", csrf)
      setState({ active: true, created_at: body.created_at })
      setShown(body.key ?? "")
    } catch (cause) {
      setIssue(issueOf(cause, {}, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  async function revoke() {
    setBusy(true)
    setIssue(null)
    try {
      await apiJSON<KeyState>("/api/crm/intake-key/revoke", "POST", csrf)
      setState({ active: false })
      setShown("")
      setCopied(false)
    } catch (cause) {
      setIssue(issueOf(cause, {}, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(shown)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  const created = state?.created_at ? formatDateTime(state.created_at.slice(0, 16), lang) : ""

  return <DisclosureSection title={t(lang, "crm.intake.title")} description={t(lang, "crm.intake.blurb")} className="w-full">
    <div className="grid gap-4">
      <FormIssue issue={issue} />
      {state && <p className="text-sm">{state.active ? `${t(lang, "crm.intake.active")} ${created}` : t(lang, "crm.intake.none")}</p>}
      {shown && <div className="grid gap-3">
        <p className="text-sm font-medium">{t(lang, "crm.intake.shownOnce")}</p>
        <div className="flex flex-wrap items-center gap-2">
          <Input readOnly value={shown} aria-label={t(lang, "crm.intake.keyLabel")} className="font-mono text-xs" style={{ fontSize: 16 }} onFocus={event => event.currentTarget.select()} />
          <Button type="button" variant="outline" onClick={() => void copy()}>{t(lang, "crm.intake.copy")}</Button>
          <StatusLine text={copied ? t(lang, "crm.intake.copied") : ""} />
        </div>
        <div className="grid gap-1">
          <span className="text-sm text-muted-foreground">{t(lang, "crm.intake.example")}</span>
          <pre className="overflow-x-auto rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap [overflow-wrap:anywhere]">{intakeExample(window.location.origin, shown)}</pre>
        </div>
      </div>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" disabled={busy} onClick={() => void make()}>{state?.active ? t(lang, "crm.intake.replace") : t(lang, "crm.intake.create")}</Button>
        {state?.active && <Button type="button" variant="outline" disabled={busy} onClick={() => void revoke()}>{t(lang, "crm.intake.revoke")}</Button>}
      </div>
      {state?.active && <p className="text-xs text-muted-foreground">{t(lang, "crm.intake.replaceNote")}</p>}
    </div>
  </DisclosureSection>
}
