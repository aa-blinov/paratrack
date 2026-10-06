import * as React from "react"
import { useState, type ReactNode } from "react"
import { Languages, Moon, Sun, SunMoon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuLabel, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { ConfirmationDialog } from "@/components/confirmation-dialog"
import { SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage } from "@/components/ui/breadcrumb"
import { AppSidebar, type ShellNavItem } from "@/components/app-sidebar"
import { translate as t } from "@/i18n"

type ShellUser = { id: number; name: string; email: string } | null
type ShellTeam = { id: number; name: string } | null
type ShellTeamMembership = { id: number; name: string; role: string }
export type AppShellData = { title: string; active: string; user: ShellUser; team: ShellTeam; userTeams: ShellTeamMembership[]; requestPath: string; csrfToken: string; lang: string; canManage: boolean; mods: Record<string, boolean> | null; tabs: ShellNavItem[] }
function ThemeControl({ shell }: { shell: AppShellData }) {
  const [mode, setMode] = useState(() => document.documentElement.dataset.themeMode || "auto")
  const labelKey = mode === "light" ? "theme.light" : mode === "dark" ? "theme.dark" : "theme.auto"
  const Icon = mode === "dark" ? Moon : mode === "light" ? Sun : SunMoon
  function cycle() {
    const next = mode === "auto" ? "light" : mode === "light" ? "dark" : "auto"
    setMode(next)
    try { localStorage.setItem("paratrack-theme", next) } catch { /* storage can be disabled */ }
    document.documentElement.dataset.themeMode = next
    const dark = next === "dark" || (next === "auto" && matchMedia("(prefers-color-scheme: dark)").matches)
    document.documentElement.dataset.theme = dark ? "paratrack-dark" : "paratrack-light"
    document.querySelectorAll('meta[name="theme-color"]').forEach(meta => meta.setAttribute("content", dark ? "#090909" : "#ffffff"))
  }
  return <Button type="button" variant="ghost" size="sm" data-theme-toggle className="h-8 w-full justify-start gap-2 rounded-md px-2 group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center" title={`${t(shell.lang, "nav.theme")}: ${t(shell.lang, labelKey)}`} aria-label={`${t(shell.lang, "nav.theme")}: ${t(shell.lang, labelKey)}`} onClick={cycle}>
    <Icon className="size-4" />
    <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.theme")}</span>
    <span className="group-data-[collapsible=icon]:hidden ml-auto text-xs text-sidebar-foreground/55">{t(shell.lang, labelKey)}</span>
  </Button>
}
function AccountMenu({ shell }: { shell: AppShellData }) {
  if (!shell.user) return <div className="flex gap-2"><Button asChild variant="ghost" size="sm"><a href="/login">{t(shell.lang, "nav.login")}</a></Button><Button asChild size="sm"><a href="/register">{t(shell.lang, "nav.signup")}</a></Button></div>
  return <DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost" size="sm">{shell.user.name}</Button></DropdownMenuTrigger><DropdownMenuContent align="end">
    <DropdownMenuLabel className="px-2 py-2"><div className="text-xs text-muted-foreground">{t(shell.lang,"nav.signedInAs")}</div><div className="truncate text-sm">{shell.user.email}</div></DropdownMenuLabel>
    {shell.canManage && <DropdownMenuItem asChild><a href="/settings/team">{t(shell.lang,"nav.teamSettings")}</a></DropdownMenuItem>}
    {[['/settings/profile','nav.profile'],['/settings/tokens','nav.tokens'],['/help','nav.help'],...(shell.mods?.integrations !== false ? [['/integrations','nav.integrations']] : [])].map(([href,key])=><DropdownMenuItem asChild key={href}><a href={href}>{t(shell.lang,key)}</a></DropdownMenuItem>)}
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

export function ApplicationShell({ shell, children }: { shell: AppShellData; children: ReactNode }) {
  useShellShortcuts()
  return <TooltipProvider><SidebarProvider className="app-shell-provider min-h-svh bg-background text-foreground">
    <ConfirmationDialog lang={shell.lang}/><a href="#main" className="app-shell-skip">{t(shell.lang, "nav.skip")}</a>
    {shell.user && <AppSidebar shell={shell} themeControl={<ThemeControl shell={shell} />} />}
    <div data-slot="sidebar-inset" className="app-shell-inset relative flex w-full min-w-0 flex-1 flex-col bg-background">
      {shell.user && <header className="app-shell-header no-print">
        <div className="app-shell-header-inner">
          <SidebarTrigger className="app-shell-sidebar-trigger" aria-label={t(shell.lang, "nav.menu")} />
          <a href="/" className="app-shell-brand md:hidden">paratrack</a>
          <Breadcrumb className="hidden md:flex"><BreadcrumbList><BreadcrumbItem><BreadcrumbPage>{shell.title}</BreadcrumbPage></BreadcrumbItem></BreadcrumbList></Breadcrumb>
          <div className="app-shell-actions"><AccountMenu shell={shell} /></div>
        </div>
      </header>}
      <div id="main" tabIndex={-1} className={`app-shell-main ${shell.user ? "" : "app-shell-public"}`}>{children}</div>
      {shell.user && <footer className="app-shell-footer"><span>{t(shell.lang, "version")}</span><span className="hidden sm:inline">{t(shell.lang, "kbd.title")}: N {t(shell.lang, "kbd.new")}, S {t(shell.lang, "kbd.stats")}, G {t(shell.lang, "kbd.graph")}, P {t(shell.lang, "kbd.pause")}, T {t(shell.lang, "kbd.theme")}</span><div className="flex-1" /><Button variant="ghost" type="button" data-install hidden className="text-sm underline">{t(shell.lang, "pwa.install")}</Button><a href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`} aria-label={t(shell.lang, "nav.language")}><Languages className="inline size-4" /> {shell.lang.toUpperCase()}</a></footer>}
    </div>
  </SidebarProvider></TooltipProvider>
}
