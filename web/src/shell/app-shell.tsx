import { useState, type ReactNode } from "react"
import { Languages, Moon, Sun, SunMoon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
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
  function cycle() {
    const next = mode === "auto" ? "light" : mode === "light" ? "dark" : "auto"
    setMode(next)
    try { localStorage.setItem("paratrack-theme", next) } catch { /* storage can be disabled */ }
    document.documentElement.dataset.themeMode = next
    const dark = next === "dark" || (next === "auto" && matchMedia("(prefers-color-scheme: dark)").matches)
    document.documentElement.dataset.theme = dark ? "paratrack-dark" : "paratrack-light"
    document.querySelectorAll('meta[name="theme-color"]').forEach(meta => meta.setAttribute("content", dark ? "#090909" : "#ffffff"))
  }
  return <Button type="button" variant="ghost" size="sm" data-theme-toggle title={`${t(shell.lang, "nav.theme")} (T)`} aria-label={`${t(shell.lang, "nav.theme")}: ${t(shell.lang, labelKey)}`} onClick={cycle}>{mode === "dark" ? <Moon /> : mode === "light" ? <Sun /> : <SunMoon />}<span>{t(shell.lang, labelKey)}</span></Button>
}
function AccountMenu({ shell }: { shell: AppShellData }) {
  if (!shell.user) return <div className="flex gap-2"><Button asChild variant="ghost" size="sm"><a href="/login">{t(shell.lang, "nav.login")}</a></Button><Button asChild size="sm"><a href="/register">{t(shell.lang, "nav.signup")}</a></Button></div>
  return <details className="relative"><summary className="app-shell-account">{shell.user.name}</summary><div className="app-shell-popover right-0"><div className="px-2 pb-2"><div className="text-xs text-muted-foreground">{t(shell.lang, "nav.signedInAs")}</div><div className="truncate text-sm">{shell.user.email}</div></div>{shell.canManage && <a className="app-shell-popover-item" href="/settings/team">{t(shell.lang, "nav.teamSettings")}</a>}<a className="app-shell-popover-item" href="/settings/profile">{t(shell.lang, "nav.profile")}</a><a className="app-shell-popover-item" href="/settings/tokens">{t(shell.lang, "nav.tokens")}</a><a className="app-shell-popover-item" href="/help">{t(shell.lang, "nav.help")}</a>{shell.mods?.integrations !== false && <a className="app-shell-popover-item" href="/integrations">{t(shell.lang, "nav.integrations")}</a>}<button type="button" data-install hidden className="app-shell-popover-item">{t(shell.lang, "pwa.installTitle")}</button><Separator className="my-1" /><form method="post" action="/api/logout"><input type="hidden" name="csrf_token" value={shell.csrfToken} /><button className="app-shell-popover-item" type="submit">{t(shell.lang, "nav.logout")}</button></form></div></details>
}
export function ApplicationShell({ shell, children }: { shell: AppShellData; children: ReactNode }) {
  const Main = shell.active === "dashboard" ? "main" : "div"
  return <TooltipProvider><SidebarProvider className="app-shell-provider min-h-svh bg-background text-foreground">
    <a href="#main" className="app-shell-skip">{t(shell.lang, "nav.skip")}</a>
    {shell.user && <AppSidebar shell={shell} themeControl={<ThemeControl shell={shell} />} />}
    <div data-slot="sidebar-inset" className="app-shell-inset relative flex w-full min-w-0 flex-1 flex-col bg-background">
      {shell.user && <header className="app-shell-header">
        <div className="app-shell-header-inner">
          <SidebarTrigger className="app-shell-sidebar-trigger" aria-label={t(shell.lang, "nav.menu")} />
          <a href="/" className="app-shell-brand lg:hidden">paratrack</a>
          <Breadcrumb className="hidden md:flex"><BreadcrumbList><BreadcrumbItem><BreadcrumbPage>{shell.title}</BreadcrumbPage></BreadcrumbItem></BreadcrumbList></Breadcrumb>
          <div className="app-shell-actions"><AccountMenu shell={shell} /></div>
        </div>
      </header>}
      <Main id="main" tabIndex={-1} className={`app-shell-main ${shell.user ? "" : "app-shell-public"}`}>{children}</Main>
      {shell.user && <footer className="app-shell-footer"><span>{t(shell.lang, "version")}</span><span className="hidden sm:inline">{t(shell.lang, "kbd.title")}: N {t(shell.lang, "kbd.new")}, S {t(shell.lang, "kbd.stats")}, G {t(shell.lang, "kbd.graph")}, P {t(shell.lang, "kbd.pause")}, T {t(shell.lang, "kbd.theme")}</span><div className="flex-1" /><button type="button" data-install hidden className="text-sm underline">{t(shell.lang, "pwa.install")}</button><a href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`} aria-label={t(shell.lang, "nav.language")}><Languages className="inline size-4" /> {shell.lang.toUpperCase()}</a></footer>}
    </div>
  </SidebarProvider></TooltipProvider>
}
