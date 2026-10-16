import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Activity, BarChart3, Bell, CalendarDays, ChevronDown, CircleHelp, Clock3, Contact, Download, Flag, Folder, Languages, Link2, LogOut, Settings, Tag, Upload, Users, Zap } from "lucide-react"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { Button, sidebarRow } from "@/components/ui/button"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
  useSidebar,
} from "@/components/ui/sidebar"
import { translate as t } from "@/i18n"

type NavGroup = "core" | "analysis" | "money" | "team" | "data" | "settings"
export type ShellNavItem = { group: NavGroup; key: string; href: string; icon: string; label: string; module?: string; manage?: boolean; matchPrefix?: string }
export type AppSidebarShell = {
  active: string
  lang: string
  user: { id: number; name: string; email: string } | null
  team: { id: number; name: string } | null
  userTeams: { id: number; name: string; role: string }[]
  requestPath: string
  csrfToken: string
  canManage: boolean
  mods: Record<string, boolean> | null
  /** The phone bar's four tabs, as chosen on the Interface page. */
  tabs?: { key: string }[]
}

export const nav: ShellNavItem[] = [
  { group: "core", key: "dashboard", href: "/", icon: "activity", label: "nav.dashboard" },
  { group: "core", key: "projects", href: "/projects", icon: "folder", label: "nav.projects" },
  // Sits next to Projects because a project is where a client is picked, and
  // the two are read together. It was missing from this table once already —
  // the Go-side navItems only feed the mobile tabs and the settings screen,
  // so a route registered there alone is unreachable from the desktop menu.
  { group: "core", key: "clients", href: "/clients", icon: "contact", label: "nav.clients" },
  { group: "core", key: "timesheet", href: "/timesheet", icon: "calendar", label: "nav.timesheet" },
  { group: "core", key: "stats", href: "/stats", icon: "chart", label: "nav.stats" },
  { group: "analysis", key: "graph", href: "/graph", icon: "clock", label: "nav.graph", module: "graph" },
  { group: "analysis", key: "goals", href: "/goals", icon: "flag", label: "nav.goals", module: "goals" },
  { group: "analysis", key: "reports", href: "/reports", icon: "chart", label: "nav.reports", module: "reports", manage: true },
  { group: "money", key: "invoices", href: "/invoices", icon: "zap", label: "nav.invoices", module: "invoices", manage: true },
  { group: "money", key: "payroll", href: "/payroll", icon: "users", label: "nav.payroll", module: "payroll", manage: true },
  { group: "team", key: "schedule", href: "/schedule", icon: "calendar", label: "nav.schedule", module: "schedule" },
  // Members and invites live under Settings → Пространство only: listed here
  // too they had two homes, and both lit up on the same page.
  { group: "data", key: "tags", href: "/tags", icon: "tag", label: "nav.tags", module: "tags" },
  { group: "data", key: "integrations", href: "/integrations", icon: "link", label: "nav.integrations", module: "integrations" },
  { group: "data", key: "import", href: "/import", icon: "download", label: "nav.import", module: "import" },
  { group: "data", key: "export", href: "/export", icon: "upload", label: "nav.csvExport" },
  // Settings has ten screens behind one tab strip. The panel used to list two
  // of them — «Под себя» and «Разделы» — under a heading that promised the
  // rest, so the other eight had no way in from anywhere. One entry now, and
  // the prefix keeps it lit on every one of the ten.
  { group: "settings", key: "settings", href: "/settings/profile", matchPrefix: "/settings/", icon: "settings", label: "set.title" },
]

const groups: Array<{ key: NavGroup; label: string }> = [
  { key: "core", label: "nav.menu" },
  { key: "analysis", label: "nav.group.analysis" },
  { key: "money", label: "nav.group.money" },
  { key: "team", label: "nav.group.team" },
  { key: "data", label: "nav.group.data" },
  { key: "settings", label: "set.title" },
]

export function available(item: ShellNavItem, shell: AppSidebarShell) {
  return (!item.module || shell.mods === null || shell.mods[item.module] === true) && (!item.manage || shell.canManage)
}

// isCurrent marks the panel entry the open screen belongs to. An item with a
// matchPrefix stands for a whole section: «Настройки» lights up on every settings
// screen, members and invites included (they are no longer listed in the panel).
export function isCurrent(item: ShellNavItem, shell: AppSidebarShell, items: ShellNavItem[] = nav) {
  const path = shell.requestPath.split("?")[0]
  const exact = shell.active === item.key || path === item.href
  if (!item.matchPrefix) return exact
  // The page names its section in shell.active, so the section entry matches by
  // key on every settings screen. It yields to a more specific entry that owns the screen.
  if (items.some(other => other !== item && (other.href === path || (other.key === shell.active && other.key !== item.key)))) return false
  return exact || path.startsWith(item.matchPrefix)
}

export function Icon({ name }: { name: string }) {
  const props = { className: "size-4", "aria-hidden": true as const, strokeWidth: 1.8 }
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
    case "contact": return <Contact {...props} />
    case "link": return <Link2 {...props} />
    case "download": return <Download {...props} />
    case "upload": return <Upload {...props} />
    case "settings": return <Settings {...props} />
    case "bell": return <Bell {...props} />
    case "help": return <CircleHelp {...props} />
    default: return <Activity {...props} />
  }
}

// A tall screen (1000px and up) has room for every group: a collapsible group
// there opens the way the person left it, opened by default on the first visit.
const TALL_SCREEN = "(min-height: 1000px)"
const REMEMBER_KEY = "paratrack.nav.data.open"

function useTallScreen() {
  const [tall, setTall] = useState(() => typeof window !== "undefined" && window.matchMedia(TALL_SCREEN).matches)
  useEffect(() => {
    const query = window.matchMedia(TALL_SCREEN)
    const update = () => setTall(query.matches)
    query.addEventListener("change", update)
    return () => query.removeEventListener("change", update)
  }, [])
  return tall
}

function readRemembered(): boolean {
  try {
    return localStorage.getItem(REMEMBER_KEY) !== "0"
  } catch {
    return true
  }
}

function NavigationGroup({ title, items, shell, collapsible = false, group }: { title: string; items: ShellNavItem[]; shell: AppSidebarShell; collapsible?: boolean; group?: string }) {
  const { setOpenMobile, state } = useSidebar()
  const visible = items.filter(item => available(item, shell))
  const holdsCurrent = visible.some(item => isCurrent(item, shell))
  const tall = useTallScreen()
  const [expanded, setExpanded] = useState(false)
  const [remembered, setRemembered] = useState(readRemembered)
  if (!visible.length) return null
  // On a short screen a collapsible group folds unless the person is inside it
  // (the whole menu does not fit 900px); on a tall one the remembered choice wins.
  // Collapsed to icons the rail has no group headings, so a folded group had
  // no way to open: import and export were out of reach. Icons are never folded.
  const open = !collapsible || state === "collapsed" || holdsCurrent || (tall ? remembered : expanded)
  const toggle = () => {
    if (!tall) return setExpanded(value => !value)
    setRemembered(value => {
      const next = !value
      try { localStorage.setItem(REMEMBER_KEY, next ? "1" : "0") } catch { /* private mode: the choice simply is not kept */ }
      return next
    })
  }
  // A group of one reads better without a heading that repeats it: «Настройки»
  // above a link also called «Настройки».
  // A group of one has no heading only where it would repeat its item («Настройки»).
  // «Расписание» alone under «Команда» read as part of «Деньги» or «Анализ».
  const heading = visible.length > 1 || group === "team" ? title : null
  // Tighter vertical padding between groups: six groups at p-2 push the folded menu past a 900px screen.
  return <SidebarGroup className="py-1">
    {heading && collapsible && !holdsCurrent ? <SidebarGroupLabel asChild><button type="button" aria-expanded={open} className="flex w-full items-center justify-between" onClick={toggle}>{heading}<ChevronDown aria-hidden="true" className={`size-4 transition-transform ${open ? "rotate-180" : ""}`} /></button></SidebarGroupLabel> : heading ? <SidebarGroupLabel>{heading}</SidebarGroupLabel> : null}
    {/* A folded group keeps its links in the page but out of sight: height 0 with the
        contents clipped, and inert so they neither take focus nor reach assistive tech. */}
    <div className={`grid ${open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"}`} inert={!open}><div className="min-h-0 overflow-hidden"><SidebarGroupContent><SidebarMenu>{visible.map(item => <SidebarMenuItem key={item.key}>
      <SidebarMenuButton asChild isActive={isCurrent(item, shell)} tooltip={t(shell.lang, item.label)}>
        <a href={item.href} onClick={() => setOpenMobile(false)} aria-current={isCurrent(item, shell) ? "page" : undefined}>
          <Icon name={item.icon} /><span>{t(shell.lang, item.label)}</span>
        </a>
      </SidebarMenuButton>
    </SidebarMenuItem>)}</SidebarMenu></SidebarGroupContent></div></div>
  </SidebarGroup>
}

function initials(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (!words.length) return "?"
  const chars = words.length > 1
    ? [Array.from(words[0])[0], Array.from(words[words.length - 1])[0]]
    : Array.from(words[0]).slice(0, 2)
  return chars.filter(Boolean).join("").toUpperCase()
}

// The navigation list is taller than a laptop window, and the scroll
// container Chromium draws for it is an overlay one: it takes no layout
// space and only shows itself once you scroll. On a 1280x720 window the
// last row was therefore cut in half with nothing to say the list goes
// on. Track the overflow so the bottom edge can mark it, and keep a thin
// scrollbar on screen so the gesture is discoverable before that.
function useListOverflow(ref: React.RefObject<HTMLElement | null>) {
  const [below, setBelow] = useState(false)
  useEffect(() => {
    const nav = ref.current
    const scroller = nav?.closest('[data-slot="sidebar-content"]')
    if (!nav || !scroller) return
    const update = () => {
      // Collapsed to icons the container stops scrolling; a marker there
      // would promise a continuation the rail cannot scroll to.
      const scrolls = ["auto", "scroll"].includes(getComputedStyle(scroller).overflowY)
      setBelow(scrolls && scroller.scrollHeight - scroller.clientHeight - scroller.scrollTop > 4)
    }
    update()
    scroller.addEventListener("scroll", update, { passive: true })
    const observer = new ResizeObserver(update)
    observer.observe(scroller)
    for (const group of Array.from(nav.children)) observer.observe(group)
    return () => {
      scroller.removeEventListener("scroll", update)
      observer.disconnect()
    }
  }, [ref])
  return below
}

export function AppSidebar({ shell, themeControl }: { shell: AppSidebarShell; themeControl: ReactNode }) {
  const { setOpenMobile } = useSidebar()
  const navRef = useRef<HTMLElement>(null)
  const moreBelow = useListOverflow(navRef)
  // Every settings page lights «Настройки», members and invites included.
  const inSettings = shell.requestPath.startsWith("/settings/")
  return <Sidebar collapsible="icon" variant="sidebar">
    <SidebarHeader className="gap-2 px-3 py-2">
      <a href="/" className="flex h-10 items-center gap-2 px-2 font-semibold tracking-tight" aria-label="paratrack">
        <span className="flex size-7 items-center justify-center rounded-md bg-brand text-brand-foreground"><Activity className="size-4" aria-hidden="true" /></span>
        <span className="text-base group-data-[collapsible=icon]:hidden">paratrack</span>
      </a>
      {shell.team && (shell.userTeams.length < 2
        ? shell.canManage
          ? <a href="/settings/team" onClick={() => setOpenMobile(false)} className="app-shell-workspace group-data-[collapsible=icon]:hidden w-full justify-between rounded-md border bg-background px-3 py-2 text-sm hover:bg-sidebar-accent" title={`${t(shell.lang, "nav.teamSettings")}: ${shell.team.name}`}>
            <span className="line-clamp-2 leading-snug">{shell.team.name}</span><Settings className="size-4 shrink-0" aria-hidden="true" />
          </a>
          : <span className="app-shell-workspace-label group-data-[collapsible=icon]:hidden text-sm" title={shell.team.name}>{shell.team.name}</span>
        : <DropdownMenu><DropdownMenuTrigger asChild><Button variant="outline" className="group-data-[collapsible=icon]:hidden w-full h-auto items-start justify-between whitespace-normal py-2 text-left" title={`${t(shell.lang,"nav.switchWorkspace")}: ${shell.team.name}`}><span className="line-clamp-2 leading-snug">{shell.team.name}</span><ChevronDown className="mt-0.5 shrink-0" aria-hidden="true"/></Button></DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {shell.userTeams.map(team=><form key={team.id} method="post" action="/api/team/switch"><input type="hidden" name="csrf_token" value={shell.csrfToken}/><input type="hidden" name="team_id" value={team.id}/><input type="hidden" name="next" value={shell.requestPath}/><DropdownMenuItem asChild><button type="submit" className="w-full justify-between" aria-current={team.id === shell.team?.id ? "true" : undefined}>{team.name}<span className="text-xs text-muted-foreground">#{team.id} · {t(shell.lang,`team.role.${team.role}`)}</span></button></DropdownMenuItem></form>)}
            {shell.canManage && <DropdownMenuItem asChild><a href="/settings/team" onClick={()=>setOpenMobile(false)}>{t(shell.lang,"nav.manageWorkspaces")}</a></DropdownMenuItem>}
          </DropdownMenuContent></DropdownMenu>)}
    </SidebarHeader>
    <SidebarContent className="[scrollbar-width:thin] [scrollbar-color:var(--sidebar-foreground)_transparent] [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-track]:bg-transparent [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-sidebar-foreground/35 [&::-webkit-scrollbar-thumb:hover]:bg-sidebar-foreground/60">
      <nav ref={navRef} className="app-shell-desktop-nav" aria-label={t(shell.lang, "nav.menu")}>
        {groups.filter(group => group.key !== "settings").map(group => <NavigationGroup key={group.key} title={t(shell.lang, group.label)} items={nav.filter(item => item.group === group.key)} shell={shell} collapsible={group.key === "data"} group={group.key} />) }
        {moreBelow && <span data-nav-more="below" aria-hidden="true" className="pointer-events-none sticky bottom-0 flex h-7 w-full shrink-0 flex-col justify-end">
          <span className="h-3 w-full bg-linear-to-t from-sidebar to-transparent" />
          <span className="flex h-4 items-center justify-center bg-sidebar"><ChevronDown className="size-4 text-sidebar-foreground/70" /></span>
        </span>}
      </nav>
    </SidebarContent>
    <SidebarSeparator />
    <SidebarFooter className="gap-0.5 px-3 py-2">
      {shell.user && (
        <div className="mb-1 flex min-w-0 items-center gap-2 rounded-md px-2 py-1 group-data-[collapsible=icon]:hidden" title={shell.user.email}>
          <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-sidebar-accent text-[.7rem] font-semibold text-sidebar-accent-foreground" aria-hidden="true">{initials(shell.user.name)}</span>
          <span className="flex min-w-0 flex-col">
            <span className="truncate text-sm font-medium leading-tight">{shell.user.name}</span>
            <span className="truncate text-xs leading-tight text-sidebar-foreground/60">{shell.user.email}</span>
          </span>
        </div>
      )}
      {/* Language and theme are preferences, not destinations. They already
          sit together; what was missing is saying so. Every other band of the
          sidebar carries a label, and these two were the only rows without
          one. */}
      {/* Settings sit here, always on screen: as the last group of the
          scrolling list they fell below the fold at 900px tall. The label
          over language and theme leads to the page they belong to. */}
      {shell.user && <Button asChild variant="ghost" size="sm" className={`${sidebarRow} ${inSettings ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground" : ""}`} title={t(shell.lang, "set.title")}>
        <a href="/settings/profile" onClick={() => setOpenMobile(false)} aria-current={inSettings ? "page" : undefined}>
          <Settings className="size-4" aria-hidden="true" />
          <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "set.title")}</span>
        </a>
      </Button>}
      <a href="/settings/preferences" onClick={() => setOpenMobile(false)} className="px-2 pt-2 pb-1 text-xs font-medium tracking-wide text-sidebar-foreground/75 underline decoration-sidebar-foreground/30 underline-offset-4 hover:text-sidebar-foreground hover:decoration-current group-data-[collapsible=icon]:hidden">
        {t(shell.lang, "nav.group.prefs")}
      </a>
      <Button asChild variant="ghost" size="sm" className={sidebarRow} title={t(shell.lang, "nav.language")}
        aria-label={`${t(shell.lang, "nav.language")} ${shell.lang.toUpperCase()}`}>
        <a onClick={() => setOpenMobile(false)} href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`}>
          <Languages className="size-4" aria-hidden="true" />
          <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.language")}</span>
          <span className="group-data-[collapsible=icon]:hidden ml-auto text-xs text-sidebar-foreground/60">{shell.lang.toUpperCase()}</span>
        </a>
      </Button>
      {themeControl}
      {shell.user && (
        <>
          <div className="group-data-[collapsible=icon]:hidden">
            <SidebarSeparator className="mx-0 my-1.5 w-full" />
          </div>
          <form method="post" action="/api/logout">
            <input type="hidden" name="csrf_token" value={shell.csrfToken} />
            <Button type="submit" variant="ghost" size="sm" className={sidebarRow} title={t(shell.lang, "nav.logout")}>
              <LogOut className="size-4" aria-hidden="true" />
              <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.logout")}</span>
            </Button>
          </form>
        </>
      )}
    </SidebarFooter>
  </Sidebar>
}
