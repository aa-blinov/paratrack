import * as React from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { requestConfirmation } from "@/components/confirmation-dialog"
import { translate as t } from "@/i18n"
import { apiJSON, ApiError } from "@/lib/json-api"
import type { ProfileData } from "@/dashboard/types"

type Setup = { secret: string; uri: string }

/**
 * The second factor of the sign-in (SEC-02). A set-up shows the key once, takes a code from the
 * app to prove it, and then shows the recovery codes once. Turning it off asks the password and
 * a current code.
 */
export function TwoFactorCard({ data }: { data: ProfileData }) {
  const lang = data.Lang || "en"
  const [enabled, setEnabled] = React.useState(data.TwoFactorEnabled)
  const [setup, setSetup] = React.useState<Setup | null>(null)
  const [codes, setCodes] = React.useState<string[] | null>(null)
  const [code, setCode] = React.useState("")
  const [password, setPassword] = React.useState("")
  const [error, setError] = React.useState("")
  const [busy, setBusy] = React.useState(false)

  const failed = (cause: unknown) => setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))

  async function start() {
    setBusy(true)
    setError("")
    try {
      setSetup(await apiJSON<Setup>("/api/profile/2fa/setup", "POST", data.CSRFToken))
    } catch (cause) {
      failed(cause)
    } finally {
      setBusy(false)
    }
  }

  async function confirm(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      const body = await apiJSON<{ recovery_codes: string[] }>("/api/profile/2fa/enable", "POST", data.CSRFToken, { code: code.trim() })
      setCodes(body.recovery_codes)
      setEnabled(true)
      setSetup(null)
      setCode("")
    } catch (cause) {
      failed(cause)
    } finally {
      setBusy(false)
    }
  }

  async function disable(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!await requestConfirmation(t(lang, "profile.twoFactor.disableConfirm"), { title: t(lang, "profile.twoFactor.title"), action: t(lang, "profile.twoFactor.disable") })) return
    setBusy(true)
    setError("")
    try {
      await apiJSON("/api/profile/2fa/disable", "POST", data.CSRFToken, { password, code: code.trim() })
      setEnabled(false)
      setPassword("")
      setCode("")
    } catch (cause) {
      failed(cause)
    } finally {
      setBusy(false)
    }
  }

  return <Card><CardHeader><CardTitle>{t(lang, "profile.twoFactor.title")}</CardTitle><CardDescription>{t(lang, "profile.twoFactor.blurb")}</CardDescription></CardHeader>
    <CardContent className="grid max-w-xl gap-4" aria-busy={busy}>
      <p className="text-sm font-medium">{enabled ? t(lang, "profile.twoFactor.on") : t(lang, "profile.twoFactor.off")}</p>
      {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
      {codes && <div className="grid gap-2 rounded-md border p-3">
        <p className="font-medium">{t(lang, "profile.twoFactor.codesTitle")}</p>
        <p className="text-sm text-muted-foreground">{t(lang, "profile.twoFactor.codesHint")}</p>
        <ul className="grid grid-cols-2 gap-1 font-mono text-sm">{codes.map(item => <li key={item}>{item}</li>)}</ul>
        <Button type="button" className="w-fit" onClick={() => setCodes(null)}>{t(lang, "profile.twoFactor.done")}</Button>
      </div>}
      {!enabled && !setup && !codes && <Button type="button" className="w-fit" disabled={busy} onClick={() => void start()}>{t(lang, "profile.twoFactor.enable")}</Button>}
      {!enabled && setup && <div className="grid gap-3">
        <p className="text-sm text-muted-foreground">{t(lang, "profile.twoFactor.keyHint")}</p>
        <div className="grid gap-1"><span className="text-xs text-muted-foreground">{t(lang, "profile.twoFactor.keyLabel")}</span><code className="break-all rounded-md border bg-muted/40 p-2 font-mono text-sm">{setup.secret}</code></div>
        <a href={setup.uri} className="w-fit text-sm underline underline-offset-4">{t(lang, "profile.twoFactor.openApp")}</a>
        <form onSubmit={confirm} className="grid gap-3">
          <div className="grid gap-1.5"><Label htmlFor="2fa-confirm-code">{t(lang, "profile.twoFactor.confirmCode")}</Label><Input id="2fa-confirm-code" type="text" inputMode="numeric" autoComplete="one-time-code" required maxLength={6} value={code} onChange={event => setCode(event.target.value)} style={{ fontSize: 16 }} /></div>
          <Button type="submit" className="w-fit" disabled={busy}>{t(lang, "profile.twoFactor.confirm")}</Button>
        </form>
      </div>}
      {enabled && !codes && <form onSubmit={disable} className="grid gap-3">
        <p className="text-sm text-muted-foreground">{t(lang, "profile.twoFactor.disableHint")}</p>
        <div className="grid gap-1.5"><Label htmlFor="2fa-password">{t(lang, "profile.twoFactor.password")}</Label><Input id="2fa-password" type="password" autoComplete="current-password" required value={password} onChange={event => setPassword(event.target.value)} style={{ fontSize: 16 }} /></div>
        <div className="grid gap-1.5"><Label htmlFor="2fa-disable-code">{t(lang, "profile.twoFactor.confirmCode")}</Label><Input id="2fa-disable-code" type="text" autoComplete="one-time-code" required value={code} onChange={event => setCode(event.target.value)} style={{ fontSize: 16 }} /></div>
        <Button type="submit" variant="outline" className="w-fit" disabled={busy}>{t(lang, "profile.twoFactor.disable")}</Button>
      </form>}
    </CardContent></Card>
}
