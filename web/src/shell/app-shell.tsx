import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react"
import { Activity, BarChart3, Bell, CalendarDays, ChevronDown, CircleHelp, Clock3, Download, Flag, Folder, Languages, Link2, LogOut, Menu, Moon, Settings, Sun, SunMoon, Tag, Users, Zap } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { translate as t } from "@/i18n"

type ShellNavItem = { key: string; href: string; icon: string; label: string }
type ShellUser = { id: number; name: string; email: string } | null
type ShellTeam = { id: number; name: string } | null
type ShellTeamMembership = { id: number; name: string; role: string }
export type AppShellData = { title: string; active: string; user: ShellUser; team: ShellTeam; userTeams: ShellTeamMembership[]; requestPath: string; csrfToken: string; lang: string; canManage: boolean; mods: Record<string, boolean> | null; tabs: ShellNavItem[] }

const nav: ShellNavItem[] = [
  { key: "dashboard", href: "/", icon: "activity", label: "nav.dashboard" },
  { key: "stats", href: "/stats", icon: "chart", label: "nav.stats" },
  { key: "timesheet", href: "/timesheet", icon: "calendar", label: "nav.timesheet" },
  { key: "projects", href: "/projects", icon: "folder", label: "nav.projects" },
  { key: "graph", href: "/graph", icon: "clock", label: "nav.graph", module: "graph" } as ShellNavItem & { module: string },
  { key: "goals", href: "/goals", icon: "flag", label: "nav.goals", module: "goals" } as ShellNavItem & { module: string },
  { key: "tags", href: "/tags", icon: "tag", label: "nav.tags", module: "tags" } as ShellNavItem & { module: string },
  { key: "invoices", href: "/invoices", icon: "zap", label: "nav.invoices", module: "invoices", manage: true } as ShellNavItem & { module: string; manage: boolean },
  { key: "payroll", href: "/payroll", icon: "users", label: "nav.payroll", module: "payroll", manage: true } as ShellNavItem & { module: string; manage: boolean },
  { key: "schedule", href: "/schedule", icon: "calendar", label: "nav.schedule", module: "schedule" } as ShellNavItem & { module: string },
  { key: "reports", href: "/reports", icon: "chart", label: "nav.reports", module: "reports", manage: true } as ShellNavItem & { module: string; manage: boolean },
  { key: "import", href: "/import", icon: "download", label: "nav.import", module: "import" } as ShellNavItem & { module: string },
  { key: "export", href: "/export", icon: "download", label: "nav.csvExport" },
  { key: "integrations", href: "/integrations", icon: "link", label: "nav.integrations", module: "integrations" } as ShellNavItem & { module: string },
  { key: "preferences", href: "/settings/preferences", icon: "settings", label: "nav.prefs" },
  { key: "sections", href: "/settings/sections", icon: "settings", label: "nav.sections", manage: true } as ShellNavItem & { manage: boolean },
]

function Icon({ name, className }: { name: string; className?: string }) {
  const props = { className, "aria-hidden": true as const, strokeWidth: 1.8 }
  switch (name) {
    case "activity": return <Activity {...props} />
    case "chart": return <BarChart3 {...props} />
    case "calendar": return <CalendarDays {...props} />
    case "folder": return <Folder {...props} />
    case "clock": return <Clock3 {...props} />
    case "flag": return <Flag {...props} />
    case "tag": return <Tag {...props} />
    case "zap": return <Zap {...props} />
    case "users": return <Users {...props} />
    case "link": return <Link2 {...props} />
    case "download": return <Download {...props} />
    case "settings": return <Settings {...props} />
    case "bell": return <Bell {...props} />
    case "help": return <CircleHelp {...props} />
    case "language": return <Languages {...props} />
    case "logout": return <LogOut {...props} />
    case "menu": return <Menu {...props} />
    default: return <Activity {...props} />
  }
}
function available(item: ShellNavItem, shell: AppShellData) {
  const access = item as ShellNavItem & { module?: string; manage?: boolean }
  return (!access.module || shell.mods === null || shell.mods[access.module] === true) && (!access.manage || shell.canManage)
}
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
    document.querySelectorAll('meta[name="theme-color"]').forEach(meta => meta.setAttribute("content", dark ? "#1a1d23" : "#ffffff"))
  }
  return <Button type="button" variant="ghost" size="sm" data-theme-toggle title={`${t(shell.lang, "nav.theme")} (T)`} aria-label={`${t(shell.lang, "nav.theme")}: ${t(shell.lang, labelKey)}`} onClick={cycle}>{mode === "dark" ? <Moon /> : mode === "light" ? <Sun /> : <SunMoon />}<span>{t(shell.lang, labelKey)}</span></Button>
}
function LinkList({ items, shell, onNavigate }: { items: ShellNavItem[]; shell: AppShellData; onNavigate?: () => void }) {
  return <>{items.filter(item => available(item, shell)).map(item => <a key={item.key} href={item.href} onClick={onNavigate} aria-current={shell.active === item.key ? "page" : undefined} className={`app-shell-link ${shell.active === item.key ? "is-active" : ""}`}><Icon name={item.icon} /><span>{t(shell.lang, item.label)}</span></a>)}</>
}
function WorkspaceSwitcher({ shell }: { shell: AppShellData }) {
	if (!shell.team) return null
	if (shell.userTeams.length < 2) return <span className="app-shell-workspace-label" title={shell.team.name}>{shell.team.name}</span>
	return <details className="relative"><summary className="app-shell-workspace" title={`${t(shell.lang, "nav.switchWorkspace")}: ${shell.team.name}`}>{shell.team.name}<ChevronDown aria-hidden="true" /></summary><div className="app-shell-popover"><div className="px-2 pb-2 text-xs font-medium text-muted-foreground">{t(shell.lang, "nav.workspaces")}</div>{shell.userTeams.map(team => <form key={team.id} method="post" action="/api/team/switch"><input type="hidden" name="csrf_token" value={shell.csrfToken} /><input type="hidden" name="team_id" value={team.id} /><input type="hidden" name="next" value={shell.requestPath} /><button className="app-shell-popover-item" type="submit" aria-current={team.id === shell.team?.id ? "true" : undefined}>{team.name}<span>{t(shell.lang, `team.role.${team.role}`)}</span></button></form>)}{shell.canManage && <a className="app-shell-popover-item" href="/settings/team">{t(shell.lang, "nav.manageWorkspaces")}</a>}</div></details>
}
function AccountMenu({ shell }: { shell: AppShellData }) {
  if (!shell.user) return <div className="flex gap-2"><Button asChild variant="ghost" size="sm"><a href="/login">{t(shell.lang, "nav.login")}</a></Button><Button asChild size="sm"><a href="/register">{t(shell.lang, "nav.signup")}</a></Button></div>
  return <details className="relative"><summary className="app-shell-account">{shell.user.name}</summary><div className="app-shell-popover right-0"><div className="px-2 pb-2"><div className="text-xs text-muted-foreground">{t(shell.lang, "nav.signedInAs")}</div><div className="truncate text-sm">{shell.user.email}</div></div>{shell.canManage && <a className="app-shell-popover-item" href="/settings/team">{t(shell.lang, "nav.teamSettings")}</a>}<a className="app-shell-popover-item" href="/settings/profile">{t(shell.lang, "nav.profile")}</a><a className="app-shell-popover-item" href="/settings/tokens">{t(shell.lang, "nav.tokens")}</a><a className="app-shell-popover-item" href="/help">{t(shell.lang, "nav.help")}</a>{shell.mods?.integrations !== false && <a className="app-shell-popover-item" href="/integrations">{t(shell.lang, "nav.integrations")}</a>}<button type="button" data-install hidden className="app-shell-popover-item">{t(shell.lang, "pwa.installTitle")}</button><Separator className="my-1" /><form method="post" action="/api/logout"><input type="hidden" name="csrf_token" value={shell.csrfToken} /><button className="app-shell-popover-item" type="submit">{t(shell.lang, "nav.logout")}</button></form></div></details>
}
function MoreSheet({ shell, open, close }: { shell: AppShellData; open: boolean; close: () => void }) {
  const panelRef = useRef<HTMLElement>(null)
  const dragStart = useRef<number | null>(null)
  if (!open) return null
  const more = nav.filter(item => !["dashboard", "stats", "timesheet", "projects"].includes(item.key))
  function finishDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    if (dragStart.current === null) return
    const distance = event.clientY - dragStart.current
    dragStart.current = null
    const panel = panelRef.current
    if (!panel) return
    panel.style.transition = "transform 180ms ease-out"
    if (distance > 90) {
      panel.style.transform = "translateY(100%)"
      window.setTimeout(close, 180)
    } else {
      panel.style.transform = ""
      window.setTimeout(() => { panel.style.transition = "" }, 180)
    }
  }
  return <div className="app-shell-overlay" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) close() }}>
    <section ref={panelRef} className="app-shell-sheet" role="dialog" aria-modal="true" aria-label={t(shell.lang, "nav.more")}>
      <button type="button" className="app-shell-sheet-grab" onClick={close} aria-label={t(shell.lang, "sheet.close")}
        onPointerDown={event => { dragStart.current = event.clientY; event.currentTarget.setPointerCapture(event.pointerId) }}
        onPointerMove={event => { if (dragStart.current !== null && panelRef.current) panelRef.current.style.transform = `translateY(${Math.max(0, event.clientY - dragStart.current)}px)` }}
        onPointerUp={finishDrag} onPointerCancel={() => { dragStart.current = null; if (panelRef.current) { panelRef.current.style.transform = ""; panelRef.current.style.transition = "" } }} />
      <div className="app-shell-sheet-content">
      {shell.user && <div className="mb-3"><div className="font-semibold">{shell.user.name}</div><div className="text-sm text-muted-foreground">{shell.user.email}</div></div>}
      <nav className="app-shell-sheet-links" aria-label={t(shell.lang, "nav.more")}><LinkList items={more} shell={shell} onNavigate={close} /></nav>
      {shell.userTeams.length > 1 && <><h2 className="app-shell-section-heading">{t(shell.lang, "nav.workspaces")}</h2><div className="app-shell-sheet-links">{shell.userTeams.map(team => <form key={team.id} method="post" action="/api/team/switch"><input type="hidden" name="csrf_token" value={shell.csrfToken} /><input type="hidden" name="team_id" value={team.id} /><input type="hidden" name="next" value={shell.requestPath} /><button type="submit" className="app-shell-link"><Users /><span>{team.name}</span></button></form>)}</div></>}
      <h2 className="app-shell-section-heading">{t(shell.lang, "sheet.account")}</h2>
      <nav className="app-shell-sheet-links" aria-label={t(shell.lang, "sheet.account")}>
        {shell.canManage && shell.team && <a className="app-shell-link" href="/settings/team" onClick={close}><Settings /><span>{shell.team.name}</span></a>}
        <a className="app-shell-link" href="/settings/profile" onClick={close}><Users /><span>{t(shell.lang, "nav.profile")}</span></a>
        <a className="app-shell-link" href="/settings/notifications" onClick={close}><Bell /><span>{t(shell.lang, "sheet.notifications")}</span></a>
        <a className="app-shell-link" href="/help" onClick={close}><CircleHelp /><span>{t(shell.lang, "nav.help")}</span></a>
      </nav>
      <div className="mt-3 flex flex-wrap gap-2"><Button asChild variant="outline" size="sm"><a href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`}><Languages />{shell.lang === "ru" ? "English" : "Русский"}</a></Button><ThemeControl shell={shell} /></div>
      {shell.user && <form method="post" action="/api/logout" className="mt-2"><input type="hidden" name="csrf_token" value={shell.csrfToken} /><Button className="w-full" variant="ghost" type="submit"><LogOut />{t(shell.lang, "nav.logout")}</Button></form>}
      </div>
    </section>
  </div>
}
export function ApplicationShell({ shell, children }: { shell: AppShellData; children: ReactNode }) {
  const [moreOpen, setMoreOpen] = useState(false)
  useEffect(() => {
    if (!moreOpen) return
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = "hidden"
    const panel = document.querySelector<HTMLElement>(".app-shell-sheet")
    const focusable = () => panel?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled])') || []
    focusable()[0]?.focus()
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") { setMoreOpen(false); return }
      if (event.key !== "Tab") return
      const items = [...focusable()]
      if (!items.length) return
      const first = items[0], last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKeyDown)
    return () => { document.removeEventListener("keydown", onKeyDown); document.body.style.overflow = previousOverflow; previous?.focus() }
  }, [moreOpen])
  const primaryTabs = shell.tabs.filter(item => available(item, shell))
  const desktopPrimary = nav.slice(0, 4)
  const desktopMore = nav.slice(4)
  const isMoreActive = !primaryTabs.some(item => item.key === shell.active)
  const Main = shell.active === "dashboard" ? "main" : "div"
  return <div className="app-shell min-h-screen bg-background text-foreground"><a href="#main" className="app-shell-skip">{t(shell.lang, "nav.skip")}</a><header className="app-shell-header"><div className="app-shell-header-inner"><a href="/" className="app-shell-brand">paratrack</a>{shell.user && <nav className="app-shell-desktop-nav" aria-label={t(shell.lang, "nav.menu")}><WorkspaceSwitcher shell={shell} /><LinkList items={desktopPrimary} shell={shell} /><details className="relative"><summary className={`app-shell-more-trigger ${isMoreActive ? "is-active" : ""}`}><Menu />{t(shell.lang, "nav.more")}</summary><div className="app-shell-popover"><LinkList items={desktopMore} shell={shell} /></div></details></nav>}<div className="app-shell-actions"><AccountMenu shell={shell} />{shell.user && <ThemeControl shell={shell} />}</div></div></header><Main id="main" tabIndex={-1} className="app-shell-main">{children}</Main><footer className="app-shell-footer"><span>{t(shell.lang, "version")}</span>{shell.user && <span className="hidden sm:inline">{t(shell.lang, "kbd.title")}: N {t(shell.lang, "kbd.new")}, S {t(shell.lang, "kbd.stats")}, G {t(shell.lang, "kbd.graph")}, P {t(shell.lang, "kbd.pause")}, T {t(shell.lang, "kbd.theme")}</span>}<div className="flex-1" /><button type="button" data-install hidden className="text-sm underline">{t(shell.lang, "pwa.install")}</button><a href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`} aria-label={t(shell.lang, "nav.language")}><Languages className="inline size-4" /> {shell.lang.toUpperCase()}</a></footer>{shell.user && <><nav className="app-shell-tabbar" aria-label={t(shell.lang, "nav.menu")}>{primaryTabs.map(item => <a key={item.key} href={item.href} aria-current={shell.active === item.key ? "page" : undefined} className={shell.active === item.key ? "is-active" : ""}><Icon name={item.icon} /><span>{t(shell.lang, item.label)}</span></a>)}<button type="button" className={isMoreActive ? "is-active" : ""} onClick={() => setMoreOpen(true)}><Menu /><span>{t(shell.lang, "nav.more")}</span></button></nav><MoreSheet shell={shell} open={moreOpen} close={() => setMoreOpen(false)} /></>}</div>
}
