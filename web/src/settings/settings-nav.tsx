import { translate as t } from "@/i18n"

// Two groups, each under its own label: what is mine, and what belongs to the
// workspace (managers only). They used to share one row of ten tabs, with the
// API tokens stuck between the workspace's members and its webhooks.
const mine = [
  ["settings-profile", "/settings/profile", "profile.title"],
  ["settings-prefs", "/settings/preferences", "prefs.title"],
  ["settings-notify", "/settings/notifications", "sheet.notifications"],
  ["settings-tokens", "/settings/tokens", "set.tabTokens"],
] as const
const workspace = [
  ["settings-team", "/settings/team", "team.workspaceTab"],
  ["settings-money", "/settings/money", "set.tabMoney"],
  ["settings-members", "/settings/members", "team.members"],
  ["settings-invites", "/settings/invites", "set.tabInvites"],
  ["settings-sections", "/settings/sections", "nav.sections"],
  ["settings-webhooks", "/settings/webhooks", "set.tabWebhooks"],
  ["settings-audit", "/settings/audit", "set.tabAudit"],
] as const

export function settingsTitle(lang: string, active: string) {
  // The workspace's first tab is «Основное»; as a page heading that says
  // nothing, so the page is named for what it is.
  if (active === "settings-team") return t(lang, "team.workspaceTitle")
  const tab = [...mine, ...workspace].find(([id]) => id === active)
  return t(lang, tab?.[2] || "set.title")
}

function TabRow({ label, tabs, active, lang }: { label: string; tabs: ReadonlyArray<readonly [string, string, string]>; active: string; lang: string }) {
  return <nav aria-label={label} className="grid gap-1 sm:grid-cols-[8rem_minmax(0,1fr)] sm:items-center">
    <span className="text-xs font-medium text-muted-foreground">{label}</span>
    <div className="flex gap-1 overflow-x-auto [scrollbar-width:none]">
      {tabs.map(([id, href, key]) => <a key={id} href={href} aria-current={active === id ? "page" : undefined} className={`inline-flex min-h-11 shrink-0 items-center rounded-md px-3 text-sm ${active === id ? "bg-secondary font-medium text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}>{t(lang, key)}</a>)}
    </div>
  </nav>
}

export function SettingsNav({ active, lang, canManage }: { active: string; lang: string; canManage: boolean }) {
  return <>
    <div className="grid gap-2 border-b pb-3">
      <TabRow label={t(lang, "set.groupMine")} tabs={mine} active={active} lang={lang} />
      {canManage && <TabRow label={t(lang, "set.groupWorkspace")} tabs={workspace} active={active} lang={lang} />}
    </div>
    <p className="text-sm text-muted-foreground">{t(lang, `set.desc.${active}`)}</p>
  </>
}
