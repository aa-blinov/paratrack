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
} from "@/components/ui/sidebar"
import { translate as t } from "@/i18n"

export type ShellNavItem = { key: string; href: string; icon: string; label: string; module?: string; manage?: boolean }
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
  { key: "dashboard", href: "/", icon: "activity", label: "nav.dashboard" },
  { key: "stats", href: "/stats", icon: "chart", label: "nav.stats" },
  { key: "timesheet", href: "/timesheet", icon: "calendar", label: "nav.timesheet" },
  { key: "projects", href: "/projects", icon: "folder", label: "nav.projects" },
  { key: "graph", href: "/graph", icon: "clock", label: "nav.graph", module: "graph" },
  { key: "goals", href: "/goals", icon: "flag", label: "nav.goals", module: "goals" },
  { key: "tags", href: "/tags", icon: "tag", label: "nav.tags", module: "tags" },
  { key: "invoices", href: "/invoices", icon: "zap", label: "nav.invoices", module: "invoices", manage: true },
  { key: "payroll", href: "/payroll", icon: "users", label: "nav.payroll", module: "payroll", manage: true },
  { key: "schedule", href: "/schedule", icon: "calendar", label: "nav.schedule", module: "schedule" },
  { key: "reports", href: "/reports", icon: "chart", label: "nav.reports", module: "reports", manage: true },
  { key: "import", href: "/import", icon: "download", label: "nav.import", module: "import" },
  { key: "export", href: "/export", icon: "download", label: "nav.csvExport" },
  { key: "integrations", href: "/integrations", icon: "link", label: "nav.integrations", module: "integrations" },
  { key: "preferences", href: "/settings/preferences", icon: "settings", label: "nav.prefs" },
  { key: "sections", href: "/settings/sections", icon: "settings", label: "nav.sections", manage: true },
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
  const visible = items.filter(item => available(item, shell))
  if (!visible.length) return null
  return <SidebarGroup>
    <SidebarGroupLabel>{title}</SidebarGroupLabel>
    <SidebarGroupContent><SidebarMenu>{visible.map(item => <SidebarMenuItem key={item.key}>
      <SidebarMenuButton asChild isActive={shell.active === item.key} tooltip={t(shell.lang, item.label)}>
        <a href={item.href} aria-current={shell.active === item.key ? "page" : undefined}>
          <Icon name={item.icon} /><span>{t(shell.lang, item.label)}</span>
        </a>
      </SidebarMenuButton>
    </SidebarMenuItem>)}</SidebarMenu></SidebarGroupContent>
  </SidebarGroup>
}

export function AppSidebar({ shell, themeControl }: { shell: AppSidebarShell; themeControl: ReactNode }) {
  const core = nav.slice(0, 4)
  const work = nav.slice(4, 14)
  const settings = nav.slice(14)
  return <Sidebar collapsible="icon" variant="sidebar">
    <SidebarHeader className="gap-3 p-3">
      <a href="/" className="flex h-10 items-center gap-2 px-2 font-semibold tracking-tight" aria-label="paratrack">
        <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground"><Activity className="size-4" aria-hidden="true" /></span>
        <span className="text-base group-data-[collapsible=icon]:hidden">paratrack</span>
      </a>
      {shell.team && (shell.userTeams.length < 2
        ? <span className="app-shell-workspace-label group-data-[collapsible=icon]:hidden truncate rounded-md border bg-background px-3 py-2 text-sm" title={shell.team.name}>{shell.team.name}</span>
        : <details className="group relative group-data-[collapsible=icon]:hidden">
          <summary className="app-shell-workspace flex w-full items-center justify-between rounded-md border bg-background px-3 py-2 text-sm" title={`${t(shell.lang, "nav.switchWorkspace")}: ${shell.team.name}`}>
            <span className="truncate">{shell.team.name}</span><ChevronDown className="size-4 shrink-0" aria-hidden="true" />
          </summary>
          <div className="app-shell-popover">
            {shell.userTeams.map(team => <form key={team.id} method="post" action="/api/team/switch">
              <input type="hidden" name="csrf_token" value={shell.csrfToken} /><input type="hidden" name="team_id" value={team.id} /><input type="hidden" name="next" value={shell.requestPath} />
              <button className="app-shell-popover-item" type="submit" aria-current={team.id === shell.team?.id ? "true" : undefined}>{team.name}<span>{t(shell.lang, `team.role.${team.role}`)}</span></button>
            </form>)}
            {shell.canManage && <a className="app-shell-popover-item" href="/settings/team">{t(shell.lang, "nav.manageWorkspaces")}</a>}
          </div>
        </details>)}
    </SidebarHeader>
    <SidebarContent>
      <nav className="app-shell-desktop-nav" aria-label={t(shell.lang, "nav.menu")}>
        <NavigationGroup title={t(shell.lang, "nav.menu")} items={core} shell={shell} />
        <NavigationGroup title={t(shell.lang, "nav.more")} items={work} shell={shell} />
        <NavigationGroup title={t(shell.lang, "nav.prefs")} items={settings} shell={shell} />
      </nav>
    </SidebarContent>
    <SidebarSeparator />
    <SidebarFooter className="p-3">
      {shell.user && <div className="truncate px-2 text-sm font-medium group-data-[collapsible=icon]:hidden" title={shell.user.email}>{shell.user.name}</div>}
      <div className="grid grid-cols-2 gap-1 group-data-[collapsible=icon]:grid-cols-1">
        <Button asChild variant="ghost" size="sm" className="justify-start group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:p-0">
          <a href={`/lang/${shell.lang === "ru" ? "en" : "ru"}?next=${encodeURIComponent(shell.requestPath)}`} title={t(shell.lang, "nav.language")}>
            <Languages aria-hidden="true" /><span className="group-data-[collapsible=icon]:hidden">{shell.lang.toUpperCase()}</span>
          </a>
        </Button>
        <div className="group-data-[collapsible=icon]:hidden">{themeControl}</div>
      </div>
      {shell.user && <form method="post" action="/api/logout">
        <input type="hidden" name="csrf_token" value={shell.csrfToken} />
        <Button type="submit" variant="ghost" size="sm" className="w-full justify-start group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:p-0" title={t(shell.lang, "nav.logout")}>
          <LogOut aria-hidden="true" /><span className="group-data-[collapsible=icon]:hidden">{t(shell.lang, "nav.logout")}</span>
        </Button>
      </form>}
    </SidebarFooter>
  </Sidebar>
}
