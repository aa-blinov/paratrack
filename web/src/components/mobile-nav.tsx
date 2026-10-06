import * as React from "react"
import { Ellipsis, Languages, LogOut, Moon, Play, Sun, SunMoon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { available, Icon, nav, type AppSidebarShell, type ShellNavItem } from "@/components/app-sidebar"
import { translate as t } from "@/i18n"

// The four places a phone opens most. Everything else lives under «Ещё», so
// the bar keeps the 3–5 destinations Material allows and the sheet stays a
// list of choices rather than a second copy of the drawer.
const PRIMARY = ["dashboard", "timesheet", "stats", "graph"]

// The floating action belongs to the screens whose job is to look at time.
// Everywhere else the screen already leads with an action of its own — a new
// project, a new invoice, a report to run — and a second floating button
// would cover the control underneath it.
const TIMER_FAB_SCREENS = ["/timesheet", "/stats", "/graph"]

function useTheme() {
  const [mode, setMode] = React.useState(() => document.documentElement.dataset.themeMode || "auto")
  function cycle() {
    const next = mode === "auto" ? "light" : mode === "light" ? "dark" : "auto"
    setMode(next)
    try { localStorage.setItem("paratrack-theme", next) } catch { /* storage can be disabled */ }
    document.documentElement.dataset.themeMode = next
    const dark = next === "dark" || (next === "auto" && matchMedia("(prefers-color-scheme: dark)").matches)
    document.documentElement.dataset.theme = dark ? "paratrack-dark" : "paratrack-light"
    document.querySelectorAll('meta[name="theme-color"]').forEach(meta => meta.setAttribute("content", dark ? "#090909" : "#ffffff"))
  }
  return [mode, cycle] as const
}

function isActive(shell: AppSidebarShell, item: ShellNavItem) {
  return shell.active === item.key || shell.requestPath.split("?")[0] === item.href
}

function SheetRow({ href, children, active }: { href?: string; children: React.ReactNode; active?: boolean }) {
  const className = `mobile-nav-row ${active ? "mobile-nav-row-active" : ""}`
  if (href) return <a href={href} className={className} aria-current={active ? "page" : undefined}>{children}</a>
  return <span className={className}>{children}</span>
}

export function MobileNav({ shell }: { shell: AppSidebarShell }) {
  const lang = shell.lang
  const [mode, cycleTheme] = useTheme()
  const [open, setOpen] = React.useState(false)
  // Choosing something closes the sheet under the thumb, the way a bottom
  // sheet is meant to behave on a phone.
  const close = React.useCallback(() => setOpen(false), [])
  const visible = React.useMemo(() => nav.filter(item => available(item, shell)), [shell])
  const primary = PRIMARY.map(key => visible.find(item => item.key === key)).filter(Boolean) as ShellNavItem[]
  const rest = visible.filter(item => !PRIMARY.includes(item.key))
  const ThemeIcon = mode === "dark" ? Moon : mode === "light" ? Sun : SunMoon

  return <>
    {/* Material puts the screen's primary action where the thumb already is.
        The overview keeps its own wide «Старт» right there, so the button only
        appears on the screens that do not have one: it takes the person to the
        timer and puts the caret in the field. */}
    {TIMER_FAB_SCREENS.includes(shell.requestPath.split("?")[0]) && (
      <Button asChild className="mobile-nav-fab">
        <a href="/?new=1" aria-label={t(lang, "dash.newTimer")} title={t(lang, "dash.newTimer")}><Play aria-hidden="true" /></a>
      </Button>
    )}
    <nav data-mobile-nav className="mobile-nav" aria-label={t(lang, "nav.primary")}>
      {primary.map(item => <a key={item.key} href={item.href} aria-current={isActive(shell, item) ? "page" : undefined}
        className={`mobile-nav-item ${isActive(shell, item) ? "mobile-nav-item-active" : ""}`}>
        <Icon name={item.icon} /><span>{t(lang, item.label)}</span>
      </a>)}
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>
          <button type="button" className="mobile-nav-item" aria-label={t(lang, "nav.more")}>
            <Ellipsis aria-hidden="true" /><span>{t(lang, "nav.more")}</span>
          </button>
        </SheetTrigger>
        <SheetContent side="bottom" className="mobile-nav-sheet">
          <SheetHeader>
            <SheetTitle className="text-left">{t(lang, "nav.more")}</SheetTitle>
            <SheetDescription className="text-left text-sm text-muted-foreground">
              {shell.team?.name || t(lang, "app.tagline")}
            </SheetDescription>
          </SheetHeader>
          {/* Only a link closes the sheet, and it closes a tick later: closing
              in the handler unmounted the link before the app's own click
              interceptor could turn it into a client-side navigation, and it
              unmounted the switch and sign-out forms before they could submit. */}
          <div className="mobile-nav-sheet-body" onClick={event => {
            if ((event.target as HTMLElement).closest("a")) setTimeout(close, 0)
          }}>
            {/* The phone has no drawer, so the workspace switcher the drawer
                header carries lives here — otherwise a person in two spaces
                could not move between them at all. */}
            {shell.userTeams.length > 1 && <>
              <p className="mobile-nav-sheet-label">{t(lang, "nav.workspaces")}</p>
              {shell.userTeams.map(team => <form key={team.id} method="post" action="/api/team/switch">
                <input type="hidden" name="csrf_token" value={shell.csrfToken} />
                <input type="hidden" name="team_id" value={team.id} />
                <input type="hidden" name="next" value={shell.requestPath} />
                <button type="submit" className={`mobile-nav-row w-full ${team.id === shell.team?.id ? "mobile-nav-row-active" : ""}`}
                  aria-current={team.id === shell.team?.id ? "true" : undefined}>
                  {team.name}<span className="ml-auto text-xs text-muted-foreground">{t(lang, `team.role.${team.role}`)}</span>
                </button>
              </form>)}
              <div className="my-2 h-px bg-border" role="separator" />
            </>}
            <p className="mobile-nav-sheet-label">{t(lang, "nav.menu")}</p>
            {rest.map(item => <SheetRow key={item.key} href={item.href} active={isActive(shell, item)}>
              <Icon name={item.icon} />{t(lang, item.label)}
            </SheetRow>)}
            <div className="my-2 h-px bg-border" role="separator" />
            <p className="mobile-nav-sheet-label">{t(lang, "set.title")}</p>
            {shell.canManage && <SheetRow href="/settings/team"><SettingsIcon />{t(lang, "nav.teamSettings")}</SheetRow>}
            <SheetRow href="/settings/profile"><SettingsIcon />{t(lang, "nav.profile")}</SheetRow>
            <SheetRow href="/settings/tokens"><SettingsIcon />{t(lang, "nav.tokens")}</SheetRow>
            <SheetRow href="/help"><SettingsIcon />{t(lang, "nav.help")}</SheetRow>
            <div className="my-2 h-px bg-border" role="separator" />
            <SheetRow href={`/lang/${lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`}>
              <LanguagesIcon />{t(lang, "nav.language")}: <span className="ml-auto">{lang.toUpperCase()}</span>
            </SheetRow>
            <button type="button" onClick={cycleTheme} data-theme-toggle className="mobile-nav-row w-full text-left">
              <ThemeIcon />{t(lang, "nav.theme")}<span className="ml-auto">{t(lang, mode === "dark" ? "theme.dark" : mode === "light" ? "theme.light" : "theme.auto")}</span>
            </button>
            {shell.user && (
              <form method="post" action="/api/logout" onClick={close}>
                <input type="hidden" name="csrf_token" value={shell.csrfToken} />
                <button type="submit" className="mobile-nav-row w-full text-left">
                  <LogOut aria-hidden="true" />{t(lang, "nav.logout")}
                </button>
              </form>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </nav>
  </>
}

const SettingsIcon = () => <Icon name="settings" />
const LanguagesIcon = () => <Languages aria-hidden="true" />