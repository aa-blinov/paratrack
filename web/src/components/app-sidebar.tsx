import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Activity, BarChart3, Bell, CalendarDays, ChevronDown, CircleHelp, Clock3, Download, Flag, Folder, Languages, Link2, LogOut, Settings, Tag, Users, Zap } from "lucide-react"
import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"
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
export type ShellNavItem = { group: NavGroup; key: string; href: string; icon: string; label: string; module?: string; manage?: boolean }
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
}

export const nav: ShellNavItem[] = [
  { group: "core", key: "dashboard", href: "/", icon: "activity", label: "nav.dashboard" },
  { group: "core", key: "projects", href: "/projects", icon: "folder", label: "nav.projects" },
  { group: "core", key: "timesheet", href: "/timesheet", icon: "calendar", label: "nav.timesheet" },
  { group: "core", key: "stats", href: "/stats", icon: "chart", label: "nav.stats" },
  { group: "analysis", key: "graph", href: "/graph", icon: "clock", label: "nav.graph", module: "graph" },
  { group: "analysis", key: "goals", href: "/goals", icon: "flag", label: "nav.goals", module: "goals" },
  { group: "analysis", key: "reports", href: "/reports", icon: "chart", label: "nav.reports", module: "reports", manage: true },
  { group: "money", key: "invoices", href: "/invoices", icon: "zap", label: "nav.invoices", module: "invoices", manage: true },
  { group: "money", key: "payroll", href: "/payroll", icon: "users", label: "nav.payroll", module: "payroll", manage: true },
  { group: "team", key: "schedule", href: "/schedule", icon: "calendar", label: "nav.schedule", module: "schedule" },
  { group: "team", key: "settings-members", href: "/settings/members", icon: "users", label: "team.members", manage: true },
  { group: "team", key: "settings-invites", href: "/settings/invites", icon: "users", label: "set.tabInvites", manage: true },
  { group: "data", key: "tags", href: "/tags", icon: "tag", label: "nav.tags", module: "tags" },
  { group: "data", key: "integrations", href: "/integrations", icon: "link", label: "nav.integrations", module: "integrations" },
  { group: "data", key: "import", href: "/import", icon: "download", label: "nav.import", module: "import" },
  { group: "data", key: "export", href: "/export", icon: "download", label: "nav.csvExport" },
  { group: "settings", key: "settings-prefs", href: "/settings/preferences", icon: "settings", label: "nav.prefs" },
  { group: "settings", key: "settings-sections", href: "/settings/sections", icon: "settings", label: "nav.sections", manage: true },
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
    case "link": return <Link2 {...props} />
    case "download": return <Download {...props} />
    case "settings": return <Settings {...props} />
    case "bell": return <Bell {...props} />
    case "help": return <CircleHelp {...props} />
    default: return <Activity {...props} />
  }
}

function NavigationGroup({ title, items, shell }: { title: string; items: ShellNavItem[]; shell: AppSidebarShell }) {
  const { setOpenMobile } = useSidebar()
  const visible = items.filter(item => available(item, shell))
  if (!visible.length) return null
  return <SidebarGroup>
    <SidebarGroupLabel>{title}</SidebarGroupLabel>
    <SidebarGroupContent><SidebarMenu>{visible.map(item => <SidebarMenuItem key={item.key}>
      <SidebarMenuButton asChild isActive={(shell.active === item.key || shell.requestPath.split("?")[0] === item.href)} tooltip={t(shell.lang, item.label)}>
        <a href={item.href} onClick={() => setOpenMobile(false)} aria-current={(shell.active === item.key || shell.requestPath.split("?")[0] === item.href) ? "page" : undefined}>
          <Icon name={item.icon} /><span>{t(shell.lang, item.label)}</span>
        </a>
      </SidebarMenuButton>
    </SidebarMenuItem>)}</SidebarMenu></SidebarGroupContent>
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

export function AppSidebar({ shell, themeControl }: { shell: AppSidebarShell; themeControl: ReactNode }) {
  const { setOpenMobile } = useSidebar()
  return <Sidebar collapsible="icon" variant="sidebar">
    <SidebarHeader className="gap-3 p-3">
      <a href="/" className="flex h-10 items-center gap-2 px-2 font-semibold tracking-tight" aria-label="paratrack">
        <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground"><Activity className="size-4" aria-hidden="true" /></span>
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
            {shell.userTeams.map(team=><form key={team.id} method="post" action="/api/team/switch"><input type="hidden" name="csrf_token" value={shell.csrfToken}/><input type="hidden" name="team_id" value={team.id}/><input type="hidden" name="next" value={shell.requestPath}/><DropdownMenuItem asChild><button type="submit" className="w-full justify-between" aria-current={team.id === shell.team?.id ? "true" : undefined}>{team.name}<span className="text-xs text-muted-foreground">{t(shell.lang,`team.role.${team.role}`)}</span></button></DropdownMenuItem></form>)}
            {shell.canManage && <DropdownMenuItem asChild><a href="/settings/team" onClick={()=>setOpenMobile(false)}>{t(shell.lang,"nav.manageWorkspaces")}</a></DropdownMenuItem>}
          </DropdownMenuContent></DropdownMenu>)}
    </SidebarHeader>
    <SidebarContent>
      <nav className="app-shell-desktop-nav" aria-label={t(shell.lang, "nav.menu")}>
        {groups.map(group => <NavigationGroup key={group.key} title={t(shell.lang, group.label)} items={nav.filter(item => item.group === group.key)} shell={shell} />) }
      </nav>
    </SidebarContent>
    <SidebarSeparator />
    <SidebarFooter className="gap-0.5 p-3">
      {shell.user && (
        <div className="mb-1 flex min-w-0 items-center gap-2 rounded-md px-2 py-1 group-data-[collapsible=icon]:hidden" title={shell.user.email}>
          <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-sidebar-accent text-[.7rem] font-semibold text-sidebar-accent-foreground" aria-hidden="true">{initials(shell.user.name)}</span>
          <span className="flex min-w-0 flex-col">
            <span className="truncate text-sm font-medium leading-tight">{shell.user.name}</span>
            <span className="truncate text-xs leading-tight text-sidebar-foreground/55">{shell.user.email}</span>
          </span>
        </div>
      )}
      <Button asChild variant="ghost" size="sm" className="h-8 w-full justify-start gap-2 rounded-md px-2 group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center" title={t(shell.lang, "nav.language")}>
        <a onClick={() => setOpenMobile(false)} href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`}>
          <Languages className="size-4" aria-hidden="true" />
          <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.language")}</span>
          <span className="group-data-[collapsible=icon]:hidden ml-auto text-xs text-sidebar-foreground/55">{shell.lang.toUpperCase()}</span>
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
            <Button type="submit" variant="ghost" size="sm" className="h-8 w-full justify-start gap-2 rounded-md px-2 group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center" title={t(shell.lang, "nav.logout")}>
              <LogOut className="size-4" aria-hidden="true" />
              <span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.logout")}</span>
            </Button>
          </form>
        </>
      )}
    </SidebarFooter>
  </Sidebar>
}
