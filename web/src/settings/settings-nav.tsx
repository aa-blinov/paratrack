import { useEffect, useRef, useState } from "react"
import { translate as t } from "@/i18n"

// Two groups, each under its own label: what is mine, and what belongs to the
// workspace (managers only). They used to share one row of ten tabs, with the
// API tokens stuck between the workspace's members and its webhooks.
const mine = [
  ["settings-profile", "/settings/profile", "profile.title"],
  ["settings-prefs", "/settings/preferences", "prefs.title"],
  ["settings-notify", "/settings/notifications", "sheet.notifications"],
  ["settings-calendar-sync", "/settings/calendar-sync", "calsync.tab"],
  ["settings-tokens", "/settings/tokens", "set.tabTokens"],
  ["settings-help", "/help", "nav.help"],
] as const
const workspace = [
  ["settings-team", "/settings/team", "team.workspaceTab"],
  ["settings-money", "/settings/money", "set.tabMoney"],
  ["settings-members", "/settings/members", "team.members"],
  ["settings-invites", "/settings/invites", "set.tabInvites"],
  ["settings-sections", "/settings/sections", "nav.sections"],
  ["settings-agent-actions", "/settings/agent-actions", "set.tabAgentActions"],
  ["settings-webhooks", "/settings/webhooks", "set.tabWebhooks"],
  ["settings-audit", "/settings/audit", "set.tabAudit"],
] as const

// The same settings, in the same order, wherever they are offered: the
// account menu and the phone sheet used to list three of eleven pages.
export function settingsMenu(canManage: boolean): Array<readonly [string, string]> {
  return [...mine.map(([, href, key]) => [href, key] as const), ...(canManage ? [["/settings/team", "nav.teamSettings"] as const] : [])]
}

export function settingsTitle(lang: string, active: string) {
  // The workspace's first tab is «Основное»; as a page heading that says
  // nothing, so the page is named for what it is.
  if (active === "settings-team") return t(lang, "team.workspaceTitle")
  const tab = [...mine, ...workspace].find(([id]) => id === active)
  return t(lang, tab?.[2] || "set.title")
}

function TabRow({ label, tabs, active, lang }: { label: string; tabs: ReadonlyArray<readonly [string, string, string]>; active: string; lang: string }) {
  // On a phone a row scrolls sideways. The current tab is brought into view
  // («Журнал» sat 300px past the edge), and the edge fades only while there
  // is more to the right.
  const row = useRef<HTMLDivElement>(null)
  const [more, setMore] = useState(false)
  const [earlier, setEarlier] = useState(false)
  useEffect(() => {
    const el = row.current
    if (!el) return
    const current = el.querySelector<HTMLElement>('[aria-current="page"]')
    if (current) {
      const offset = current.getBoundingClientRect().left - el.getBoundingClientRect().left + el.scrollLeft
      el.scrollLeft = Math.max(0, offset - (el.clientWidth - current.offsetWidth) / 2)
    }
    // The fade names the side that has more: a tab hidden to the left needs its
    // own cue, not only the one on the right.
    const check = () => {
      setMore(el.scrollLeft + el.clientWidth < el.scrollWidth - 1)
      setEarlier(el.scrollLeft > 1)
    }
    check()
    el.addEventListener("scroll", check, { passive: true })
    return () => el.removeEventListener("scroll", check)
  }, [active])
  return <nav aria-label={label} className="grid gap-1 sm:grid-cols-[8rem_minmax(0,1fr)] sm:items-center">
    <span className="text-xs font-medium text-muted-foreground">{label}</span>
    <div ref={row} className={`flex gap-1 overflow-x-auto [scrollbar-width:none] ${more && earlier ? "[mask-image:linear-gradient(to_right,transparent,#000_12%,#000_85%,transparent)]" : more ? "[mask-image:linear-gradient(to_right,#000_85%,transparent)]" : earlier ? "[mask-image:linear-gradient(to_right,transparent,#000_12%)]" : ""}`}>
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
    <p className="text-sm text-muted-foreground">{t(lang, active === "settings-notify" && !canManage ? "set.desc.settings-notify-member" : `set.desc.${active}`)}</p>
  </>
}
