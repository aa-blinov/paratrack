import { Component, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { translate as t } from "@/i18n"

// A render crash used to leave a blank page with the reason only in the
// console. The boundary keeps the screen alive with the one action that
// always helps: a reload, which re-mounts the app on the page data the
// server already rendered. The dictionary may itself be the casualty, in
// which case the keys stand in for their text — still better than nothing.
export class AppErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(cause: unknown) {
    console.error("React render failed", cause)
  }

  render() {
    if (!this.state.failed) return this.props.children
    const lang = document.documentElement.lang === "en" ? "en" : "ru"
    return <div className="mx-auto grid w-full max-w-md gap-4 p-6">
      <Card>
        <CardHeader><CardTitle>{t(lang, "err.renderTitle")}</CardTitle></CardHeader>
        <CardContent className="grid gap-3">
          <p className="text-sm text-muted-foreground">{t(lang, "err.renderHint")}</p>
          <Button type="button" onClick={() => window.location.reload()}>{t(lang, "err.reload")}</Button>
        </CardContent>
      </Card>
    </div>
  }
}
