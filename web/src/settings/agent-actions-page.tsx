import { Flash } from "@/components/flash"
import { Check, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import { SettingsNav, settingsTitle } from "@/settings/settings-nav"
import type { AgentActionsData } from "@/dashboard/types"

// Each decision is a form the server answers with a redirect back here, carrying the
// outcome. A confirmation runs the action, so its form asks first.
export function AgentActionsPage({ data }: { data: AgentActionsData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-6xl gap-4">
    <h1 className="text-2xl font-semibold tracking-tight">{settingsTitle(lang, data.Active)}</h1>
    <SettingsNav active={data.Active} lang={lang} canManage />
    <Flash message={data.Flash} ok={data.FlashOK} />
    <Card><CardHeader><CardTitle>{t(lang, "agent.title")}</CardTitle></CardHeader><CardContent className="grid gap-3">
      {data.Items.length ? data.Items.map(item => <article key={item.ID} className="grid gap-3 rounded-md border p-3">
        <p className="font-medium">{t(lang, `agent.kind.${item.Kind}`)}</p>
        <dl className="grid gap-1 text-sm sm:grid-cols-[8rem_minmax(0,1fr)]">
          <dt className="text-muted-foreground">{t(lang, "agent.target")}</dt><dd className="break-words">{item.Target}</dd>
          <dt className="text-muted-foreground">{t(lang, "agent.reason")}</dt><dd className="whitespace-pre-line break-words">{item.Reason}</dd>
          <dt className="text-muted-foreground">{t(lang, "agent.requestedBy")}</dt><dd className="break-words">{item.RequestedBy}</dd>
          <dt className="text-muted-foreground">{t(lang, "agent.requestedAt")}</dt><dd>{item.When}</dd>
        </dl>
        {item.Claimed ? <p className="text-sm text-muted-foreground">{t(lang, "agent.running")}</p>
          : item.CanDecide ? <div className="flex flex-wrap items-center gap-2">
            <form method="post" action={`/api/agent-actions/${item.ID}/confirm`} data-confirm={t(lang, "agent.confirmAsk")}>
              <input type="hidden" name="csrf_token" value={data.CSRFToken} />
              <Button type="submit"><Check aria-hidden="true" />{t(lang, "agent.confirm")}</Button>
            </form>
            <form method="post" action={`/api/agent-actions/${item.ID}/reject`}>
              <input type="hidden" name="csrf_token" value={data.CSRFToken} />
              <Button type="submit" variant="outline"><X aria-hidden="true" />{t(lang, "agent.reject")}</Button>
            </form>
          </div>
          : <p className="text-sm text-muted-foreground">{t(lang, "agent.ownerOnly")}</p>}
      </article>) : <p className="text-sm text-muted-foreground">{t(lang, "agent.empty")}</p>}
    </CardContent></Card>
  </main>
}
