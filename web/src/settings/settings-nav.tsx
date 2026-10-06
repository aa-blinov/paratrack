import { translate as t } from "@/i18n"

const tabs = [
  ["settings-profile", "/settings/profile", "profile.title"],
  ["settings-prefs", "/settings/preferences", "prefs.title"],
  ["settings-notify", "/settings/notifications", "sheet.notifications"],
  ["settings-tokens", "/settings/tokens", "set.tabTokens"],
] as const
const managerTabs = [
  ["settings-team", "/settings/team", "team.workspaceTab"],
  ["settings-sections", "/settings/sections", "nav.sections"],
  ["settings-members", "/settings/members", "team.members"],
  ["settings-invites", "/settings/invites", "set.tabInvites"],
  ["settings-webhooks", "/settings/webhooks", "set.tabWebhooks"],
  ["settings-audit", "/settings/audit", "set.tabAudit"],
] as const

export function settingsTitle(lang: string, active: string) {
  const tab = [...tabs, ...managerTabs].find(([id]) => id === active)
  return t(lang, tab?.[2] || "set.title")
}

export function SettingsNav({ active, lang, canManage }: { active: string; lang: string; canManage: boolean }) {
  const links = [...tabs.slice(0, 3), ...(canManage ? managerTabs.slice(0, 4) : []), tabs[3], ...(canManage ? managerTabs.slice(4) : [])]
  return <>
    <nav aria-label={t(lang, "set.title")} className="flex flex-wrap gap-1 border-b">
      {links.map(([id, href, key]) => <a key={id} href={href} aria-current={active === id ? "page" : undefined} className={`shrink-0 border-b-2 px-3 py-2 text-sm ${active === id ? "border-primary font-medium" : "border-transparent text-muted-foreground hover:text-foreground"}`}>{t(lang, key)}</a>)}
    </nav>
    <p className="text-sm text-muted-foreground">{t(lang, `set.desc.${active}`)}</p>
  </>
}
