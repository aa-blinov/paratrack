import { settingsMenu } from "@/settings/settings-nav"
import { useTheme } from "@/lib/theme"
import * as React from "react"
import { type ReactNode } from "react"
import { Moon, Sun, SunMoon } from "lucide-react"
import { Button, sidebarRow } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuLabel, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { ConfirmationDialog } from "@/components/confirmation-dialog"
import { SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage } from "@/components/ui/breadcrumb"
import { AppSidebar, type ShellNavItem } from "@/components/app-sidebar"
import { MobileNav } from "@/components/mobile-nav"
import { OfflineBanner } from "@/offline/offline-banner"
import { translate as t } from "@/i18n"

type ShellUser = { id: number; name: string; email: string } | null
type ShellTeam = { id: number; name: string } | null
type ShellTeamMembership = { id: number; name: string; role: string }
export type AppShellData = { title: string; active: string; user: ShellUser; team: ShellTeam; userTeams: ShellTeamMembership[]; requestPath: string; csrfToken: string; lang: string; canManage: boolean; mods: Record<string, boolean> | null; tabs: ShellNavItem[]; appVersion: string }
function ThemeControl({ shell }: { shell: AppShellData }) {
  const [mode, cycle] = useTheme()
  const labelKey = mode === "light" ? "theme.light" : mode === "dark" ? "theme.dark" : "theme.auto"
  const Icon = mode === "dark" ? Moon : mode === "light" ? Sun : SunMoon
  // One spelling for the visible label and the accessible name («Авто»).
  const modeLabel = (label => label.charAt(0).toLocaleUpperCase() + label.slice(1))(t(shell.lang, labelKey))
  return <Button type="button" variant="ghost" size="sm" data-theme-toggle className={sidebarRow} title={`${t(shell.lang, "nav.theme")} ${modeLabel}`} aria-label={`${t(shell.lang, "nav.theme")} ${modeLabel}`} onClick={cycle}>
    <Icon className="size-4" />
    <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.theme")}</span>
    <span className="group-data-[collapsible=icon]:hidden ml-auto text-xs text-sidebar-foreground/60">{modeLabel}</span>
  </Button>
}
function AccountMenu({ shell }: { shell: AppShellData }) {
  if (!shell.user) return <div className="flex gap-2"><Button asChild variant="ghost" size="sm"><a href="/login">{t(shell.lang, "nav.login")}</a></Button><Button asChild size="sm"><a href="/register">{t(shell.lang, "nav.signup")}</a></Button></div>
  return <DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost" size="sm">{shell.user.name}</Button></DropdownMenuTrigger><DropdownMenuContent align="end">
    <DropdownMenuLabel className="px-2 py-2"><div className="text-xs text-muted-foreground">{t(shell.lang,"nav.signedInAs")}</div><div className="truncate text-sm">{shell.user.email}</div></DropdownMenuLabel>
    {settingsMenu(shell.canManage).map(([href,key])=><DropdownMenuItem asChild key={href}><a href={href}>{t(shell.lang,key)}</a></DropdownMenuItem>)}
    <DropdownMenuSeparator/>
    {[['/help','nav.help'],...(shell.mods?.integrations !== false ? [['/integrations','nav.integrations']] : [])].map(([href,key])=><DropdownMenuItem asChild key={href}><a href={href}>{t(shell.lang,key)}</a></DropdownMenuItem>)}
    <DropdownMenuSeparator/><form method="post" action="/api/logout"><input type="hidden" name="csrf_token" value={shell.csrfToken}/><DropdownMenuItem asChild><Button variant="ghost" type="submit" className="w-full">{t(shell.lang,"nav.logout")}</Button></DropdownMenuItem></form>
  </DropdownMenuContent></DropdownMenu>
}
// The shortcuts the public docs promise (site/docs/index.md). They were
// lost with the move off app-navigation.js, so they live with the shell
// again. Navigation goes through the sidebar links on purpose: the link
// interceptor in main.tsx then applies the same rules as a real click.
export function useShellShortcuts() {
  React.useEffect(() => {
    function isTyping(target: EventTarget | null) {
      const el = target as HTMLElement | null
      return Boolean(el?.closest?.("input, textarea, select, [contenteditable=true]"))
    }
    function go(href: string) {
      const link = document.querySelector<HTMLAnchorElement>(`nav[aria-label] a[href="${href}"]`)
      link?.click()
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return
      const here = window.location.pathname
      switch (event.key.toLowerCase()) {
        case "g": if (here !== "/graph") { event.preventDefault(); go("/graph") } break
        case "s": if (here !== "/stats") { event.preventDefault(); go("/stats") } break
        case "d": if (here !== "/") { event.preventDefault(); go("/") } break
        case "t": document.querySelector<HTMLButtonElement>("[data-theme-toggle]")?.click(); break
        case "n": {
          event.preventDefault()
          const field = document.querySelector<HTMLInputElement>('input[name="activity"]')
          if (field) { field.focus(); break }
          go("/")
          // The overview arrives through the router, so the field is not in
          // the DOM yet: focus it as soon as it lands.
          const focusField = () => {
            const next = document.querySelector<HTMLInputElement>('input[name="activity"]')
            if (!next) return false
            next.focus()
            return true
          }
          if (!focusField()) {
            const observer = new MutationObserver(() => { if (focusField()) observer.disconnect() })
            observer.observe(document.body, { childList: true, subtree: true })
          }
          break
        }
        case "p": {
          if (here === "/") { window.dispatchEvent(new Event("paratrack:pause-all")); break }
          // Off the overview nothing listens for that event, so ask the
          // server directly. No reload: on the other screens pausing changes
          // nothing you are looking at, and a reload would throw away the
          // filters and the scroll position.
          const token = decodeURIComponent(
            document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/)?.[1] ?? "",
          )
          void fetch("/api/active/pause-all", {
            method: "POST",
            credentials: "same-origin",
            headers: { "X-CSRF-Token": token },
          })
          break
        }
      }
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [])
}

// The keyboard hint is for a pointer and a keyboard; a touch screen gets none.
function useFinePointer() {
  const [fine, setFine] = React.useState(false)
  React.useEffect(() => {
    const query = window.matchMedia("(pointer: fine)")
    const update = () => setFine(query.matches)
    update()
    query.addEventListener("change", update)
    return () => query.removeEventListener("change", update)
  }, [])
  return fine
}

export function ApplicationShell({ shell, children }: { shell: AppShellData; children: ReactNode }) {
  useShellShortcuts()
  const finePointer = useFinePointer()
  // A build without a stamped version reports "dev"; that is not a version to show.
  const releaseVersion = Boolean(shell.appVersion) && shell.appVersion !== "dev"
  // /welcome is where the sections get picked; a menu of all nine before that
  // choice promised what the choice might take away.
  const navigable = Boolean(shell.user) && shell.active !== "welcome"
  return <TooltipProvider><SidebarProvider className="app-shell-provider min-h-svh bg-background text-foreground">
    <ConfirmationDialog lang={shell.lang}/><a href="#main" className="app-shell-skip">{t(shell.lang, "nav.skip")}</a>
    {navigable && <AppSidebar shell={shell} themeControl={<ThemeControl shell={shell} />} />}
    <div data-slot="sidebar-inset" className="app-shell-inset relative flex w-full min-w-0 flex-1 flex-col bg-background">
      {shell.user && <header className="app-shell-header no-print">
        <div className="app-shell-header-inner">
          {navigable && <SidebarTrigger className="app-shell-sidebar-trigger max-lg:hidden" aria-label={t(shell.lang, "nav.menu")} />}
          <a href="/" className="app-shell-brand md:hidden">paratrack</a>
          <Breadcrumb className="hidden md:flex app-shell-breadcrumb"><BreadcrumbList><BreadcrumbItem><BreadcrumbPage>{shell.title}</BreadcrumbPage></BreadcrumbItem></BreadcrumbList></Breadcrumb>
          <div className="app-shell-actions"><AccountMenu shell={shell} /></div>
        </div>
      </header>}
      <OfflineBanner lang={shell.lang} />
      <div id="main" tabIndex={-1} className={`app-shell-main ${shell.user ? "" : "app-shell-public"}`}>{children}</div>
      {navigable && <MobileNav shell={shell} />}
      {shell.user && <footer className="app-shell-footer">{releaseVersion && <span>{shell.appVersion}</span>}{finePointer && shell.requestPath !== "/welcome" && <span className="hidden sm:inline">{t(shell.lang, "kbd.title")}: N {t(shell.lang, "kbd.new")}, D {t(shell.lang, "kbd.overview")}, S {t(shell.lang, "kbd.stats")}, G {t(shell.lang, "kbd.graph")}, P {t(shell.lang, "kbd.pause")}, T {t(shell.lang, "kbd.theme")}</span>}<div className="flex-1" /><Button variant="ghost" type="button" data-install hidden className="text-sm underline">{t(shell.lang, "pwa.install")}</Button></footer>}
    </div>
  </SidebarProvider></TooltipProvider>
}
