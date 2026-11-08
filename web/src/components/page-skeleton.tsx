import { translate as t } from "@/i18n"

// The screen a page is loading into. It has the shape of a page (a title, a
// line of description, blocks where the content goes), so the person sees
// where they are going rather than a grey box or the page they left, dimmed.
// With a title it names the destination at once: the click was for that page.
export function PageSkeleton({ lang, title }: { lang: string; title?: string }) {
  return <div className="grid gap-4" role="status" aria-busy="true" data-page-skeleton>
    <span className="sr-only">{t(lang, "page.loading")}</span>
    <header className="grid gap-2">
      {title
        ? <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        : <span className="h-8 w-48 max-w-full rounded-md bg-muted motion-safe:animate-pulse" aria-hidden="true" />}
      <span className="h-4 w-80 max-w-full rounded bg-muted motion-safe:animate-pulse" aria-hidden="true" />
    </header>
    <div className="grid gap-3 sm:grid-cols-2" aria-hidden="true">
      <span className="h-28 rounded-xl border bg-card motion-safe:animate-pulse" />
      <span className="h-28 rounded-xl border bg-card motion-safe:animate-pulse" />
    </div>
  </div>
}
