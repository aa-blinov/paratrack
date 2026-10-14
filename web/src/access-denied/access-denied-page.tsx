import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { translate as t } from "@/i18n"

export type AccessDeniedData = { Lang: string; Message: string }

// The screen a member sees on a manager-only page. The server says why; the
// screen only offers the way back.
export function AccessDeniedPage({ data }: { data: AccessDeniedData }) {
  const lang = data.Lang || "en"
  return <main className="mx-auto grid w-full max-w-6xl gap-4 p-4 sm:p-6">
    <Card>
      <CardContent className="grid gap-3 p-5 sm:p-6">
        <h1 className="text-xl font-semibold tracking-tight">{t(lang, "err.accessDeniedTitle")}</h1>
        <p className="text-sm text-muted-foreground">{data.Message}</p>
        <Button asChild size="sm" className="w-fit"><a href="/">{t(lang, "err.backToDashboard")}</a></Button>
      </CardContent>
    </Card>
  </main>
}
