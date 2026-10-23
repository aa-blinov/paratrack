import * as React from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { translate as t } from "@/i18n"

// The question a run that pays hours already paid must answer (FIN-22). The reason is
// required, and the server keeps it in the audit trail; the confirm button waits for it.
export function OverlapConfirm({ lang, csrfToken, overlap, start, end, notes }: { lang: string; csrfToken: string; overlap: string; start: string; end: string; notes: string }) {
  const [reason, setReason] = React.useState("")
  return <div role="alert" className="flex flex-wrap items-start gap-3 rounded-lg border border-warning/40 bg-warning/5 p-4 text-sm">
    <span className="min-w-0 flex-1">{t(lang, "pay.overlap", overlap)}</span>
    <form method="POST" action="/payroll" className="flex w-full flex-wrap items-end gap-2">
      <input type="hidden" name="csrf_token" value={csrfToken} />
      <input type="hidden" name="start" value={start} />
      <input type="hidden" name="end" value={end} />
      <input type="hidden" name="notes" value={notes} />
      <input type="hidden" name="confirm" value="1" />
      <div className="grid min-w-0 flex-1 gap-1.5">
        <Label htmlFor="overlap-reason" className="text-xs">{t(lang, "pay.overlapReason")}</Label>
        <Input id="overlap-reason" name="overlap_reason" required maxLength={200} value={reason} onChange={event => setReason(event.target.value)} />
      </div>
      <Button type="submit" size="sm" disabled={!reason.trim()}>{t(lang, "pay.overlapCreate")}</Button>
      <Button asChild type="button" size="sm" variant="ghost"><a href="/payroll">{t(lang, "pay.overlapCancel")}</a></Button>
    </form>
  </div>
}
