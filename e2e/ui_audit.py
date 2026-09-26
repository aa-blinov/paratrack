"""Full UI audit: walk every screen and flow, capture screenshots.

Run:  . .venv/bin/activate && python e2e/ui_audit.py
Saves to e2e/screenshots/ui-audit/.
"""
from __future__ import annotations

import os
import re
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8888")
OUT = Path(__file__).parent / "screenshots" / "ui-audit"
OUT.mkdir(parents=True, exist_ok=True)

results: list[tuple[str, bool, str]] = []
console_errors: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    results.append((name, ok, detail))
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if detail else ""))


def shot(page, name: str) -> None:
    path = OUT / f"{name}.png"
    page.screenshot(path=str(path), full_page=True)
    print(f"          📸 {path.name}")


def main() -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx = browser.new_context(viewport={"width": 1280, "height": 900})
        page = ctx.new_page()
        page.on("console", lambda m: console_errors.append(f"{m.type}: {m.text}") if m.type == "error" else None)
        page.on("pageerror", lambda e: console_errors.append(f"pageerror: {e}"))
        page.on("dialog", lambda d: d.accept())

        # ---- Auth screens -------------------------------------------------
        print("== A. Public auth screens")
        page.goto(BASE + "/login")
        shot(page, "a1-login")
        check("login renders form", page.locator('input[name="email"]').count() == 1)

        page.goto(BASE + "/register")
        shot(page, "a2-register")
        check("register renders form", page.locator('input[name="name"]').count() == 1)

        # Register
        import time as _time
        email = f"ui-audit-{int(_time.time())}@paratrack.test"
        page.fill('input[name="name"]', "UI Audit")
        page.fill('input[name="email"]', email)
        page.fill('input[name="password"]', "longenoughpw")
        page.click('button[type="submit"]')
        page.wait_for_url(BASE + "/")
        check("register lands on dashboard", "/register" not in page.url and page.url.rstrip("/").endswith(BASE.split("//")[-1].rstrip("/")))

        # Seed some data first so pages aren't empty
        print("== B. Seed data via UI")
        page.fill('input[name="activity"]', "reading")
        page.fill('input[name="note"]', "chapter 1")
        page.click('button[type="submit"]:has-text("Start")')
        page.wait_for_timeout(500)
        check("session started", page.locator("#active-list").inner_text().lower().find("reading") >= 0)

        page.fill('input[name="activity"]', "writing")
        page.click('button[type="submit"]:has-text("Start")')
        page.wait_for_timeout(500)
        check("second session started", page.locator("#active-list tr").count() >= 2)

        shot(page, "b1-dashboard-active")

        # Pause / resume / stop via UI
        print("== C. Session controls")
        page.locator('button:has-text("Pause")').first.click()
        page.wait_for_timeout(400)
        check("pause flips status", page.locator(".status-pill.is-paused").count() >= 1)
        shot(page, "c1-dashboard-paused")

        page.locator('button:has-text("Resume")').first.click()
        page.wait_for_timeout(400)
        check("resume flips back", page.locator(".status-pill.is-active").count() >= 1)

        # Stop writing (keep reading active briefly then stop both later)
        page.locator('button:has-text("Stop")').last.click()
        page.wait_for_timeout(300)
        # htmx confirm dialog
        # hx-confirm may auto-appear; handle if needed
        page.wait_for_timeout(400)
        shot(page, "c2-dashboard-after-stop")

        # ---- Dashboard ----------------------------------------------------
        print("== D. Dashboard full")
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
        shot(page, "d1-dashboard-light")
        h1 = page.locator("h1").inner_text()
        check("dashboard h1", h1.strip() == "Dashboard", h1)
        check("title is Dashboard", "Dashboard | paratrack" in page.title(), page.title())
        check("nav highlights Dashboard", page.locator('nav a.btn-active:has-text("Dashboard")').count() == 1)
        # Today card present
        check("Today card present", page.locator("text=Tracked today").count() >= 1)
        check("Recent card present", page.locator("text=Recent sessions").count() >= 1)

        # ---- Stats --------------------------------------------------------
        print("== E. Stats")
        page.goto(BASE + "/stats")
        page.wait_for_load_state("load")
        shot(page, "e1-stats-light")
        check("stats h1", page.locator("h1").inner_text().strip() == "Stats")
        check("stats title", "Stats | paratrack" in page.title(), page.title())
        rows = page.locator('table [id^="row-"]').count()
        check("stats has session rows", rows >= 1, f"rows={rows}")
        # Duration labels should be human format, not HH:MM:SS
        body = page.inner_text("body")
        hhmmss = re.findall(r"\b\d{2}:\d{2}:\d{2}\b", body)
        check("no HH:MM:SS duration labels", len(hhmmss) == 0, f"found={hhmmss[:3]}")
        # Inline edit: change duration on first row
        first_row = page.locator('tr[id^="row-"]').first
        row_id = first_row.get_attribute("id")
        dur = first_row.locator('input[name="duration"]')
        if dur.count():
            dur.fill("2h 15m")
            page.keyboard.press("Tab")
            page.wait_for_timeout(600)
            shot(page, "e2-stats-inline-edit")
            new_dur = page.locator(f"#{row_id} input[name='duration']").input_value()
            check("duration edit round-trips", "2h" in new_dur or "135" in new_dur, f"val={new_dur}")
            # tags must survive inline edit
            # (row re-rendered; tags column still present)
            check("row still has tags cell", page.locator(f"#{row_id} [data-label='Tags']").count() == 1)

        # Period tab keeps nothing weird
        page.goto(BASE + "/stats?period=week")
        page.wait_for_load_state("load")
        shot(page, "e3-stats-week")
        check("period week tab active", page.locator(".period-tabs a.active").inner_text().strip() == "This week")

        # ---- Graph --------------------------------------------------------
        print("== F. Graph")
        page.goto(BASE + "/graph")
        page.wait_for_load_state("load")
        page.wait_for_timeout(800)
        shot(page, "f1-graph-light")
        check("graph h1", page.locator("h1").inner_text().strip() == "Graph")
        check("graph title", "Graph | paratrack" in page.title(), page.title())
        check("last month tab exists", page.locator('.period-tabs a:has-text("Last month")').count() == 1)
        canvas = page.locator("#echart-canvas canvas").count()
        check("echarts canvas", canvas >= 1, f"canvas={canvas}")

        # ---- Goals --------------------------------------------------------
        print("== G. Goals")
        page.goto(BASE + "/goals")
        page.wait_for_load_state("load")
        shot(page, "g1-goals-empty-or-list")
        check("goals h1", page.locator("h1").inner_text().strip() == "Goals")
        check("goals title", "Goals | paratrack" in page.title(), page.title())
        check("goals nav active", page.locator('nav a.btn-active:has-text("Goals")').count() == 1)

        page.fill("#g-activity", "reading")
        page.select_option("#g-period", "daily")
        page.fill("#g-minutes", "120")
        page.click('button:has-text("Set goal")')
        page.wait_for_timeout(600)
        shot(page, "g2-goals-after-create")
        list_text = page.locator("#goals-list").inner_text()
        check("goal appears in list via HTMX", "reading" in list_text and "2h" in list_text, list_text[:80])
        check("goals list not JSON", not list_text.strip().startswith("{"))

        # Delete via HTMX
        page.locator('#goals-list button[aria-label="Delete goal"]').first.click()
        page.wait_for_timeout(400)
        # confirm dialog
        page.wait_for_timeout(500)
        shot(page, "g3-goals-after-delete")
        list_text2 = page.locator("#goals-list").inner_text()
        check("goal delete refreshes list", "Current goals" not in list_text2 or "reading" not in list_text2, list_text2[:80])

        # re-create for later screenshots
        page.fill("#g-activity", "reading")
        page.select_option("#g-period", "daily")
        page.fill("#g-minutes", "120")
        page.click('button:has-text("Set goal")')
        page.wait_for_timeout(500)

        # ---- Tags ---------------------------------------------------------
        print("== H. Tags")
        page.goto(BASE + "/tags")
        page.wait_for_load_state("load")
        shot(page, "h1-tags")
        check("tags h1", page.locator("h1").inner_text().strip() == "Tags")
        check("tags title", "Tags | paratrack" in page.title(), page.title())
        check("tags nav active", page.locator('nav a.btn-active:has-text("Tags")').count() == 1)

        page.fill("#t-name", "deep-work")
        page.click('button:has-text("Add")')
        page.wait_for_timeout(600)
        shot(page, "h2-tags-after-add")
        tags_text = page.locator("#tags-list").inner_text()
        check("tag appears via HTMX", "deep-work" in tags_text, tags_text[:80])
        check("tags list not JSON", not tags_text.strip().startswith("{"))

        # Attach tag on stats (via the + button — also covers the Enter path below)
        page.goto(BASE + "/stats")
        page.wait_for_load_state("load")
        tag_input = page.locator('input[placeholder="+ tag"]').first
        tag_input.fill("deep-work")
        tag_input.press("Enter")
        page.wait_for_timeout(800)
        shot(page, "h3-stats-tag-attached")
        row_html = page.locator('tr[id^="row-"]').first.inner_html()
        check("tag chip on session row (Enter)", "#deep-work" in row_html or "deep-work" in row_html,
              row_html[row_html.find('data-label="Tags"'):row_html.find('data-label="Tags"')+180] if 'data-label="Tags"' in row_html else row_html[:100])
        check("row not wiped after tag", page.locator('tr[id^="row-"]').count() >= 1)

        # Also exercise the + button path with a second tag
        tag_input2 = page.locator('input[placeholder="+ tag"]').first
        tag_input2.fill("focus")
        page.locator('button[aria-label="Attach tag"]').first.click()
        page.wait_for_timeout(800)
        shot(page, "h3b-stats-tag-button")
        row_html2 = page.locator('tr[id^="row-"]').first.inner_html()
        check("tag chip via + button", "#focus" in row_html2 or "focus" in row_html2, row_html2[row_html2.find('data-label="Tags"'):row_html2.find('data-label="Tags"')+200] if 'data-label="Tags"' in row_html2 else row_html2[:100])

        # Tag filter
        page.goto(BASE + "/stats?tag=deep-work")
        page.wait_for_load_state("load")
        shot(page, "h4-stats-tag-filter")
        check("filter banner", page.locator("text=FILTERED BY").count() >= 1)
        # clear
        page.click("text=Clear filters")
        page.wait_for_load_state("load")
        check("clear filters works", page.locator("text=FILTERED BY").count() == 0)

        # ---- Projects -----------------------------------------------------
        print("== I. Projects")
        page.goto(BASE + "/projects")
        page.wait_for_load_state("load")
        shot(page, "i1-projects-empty")
        check("projects h1", page.locator("h1").inner_text().strip() == "Projects")
        check("projects nav active", page.locator('nav a.btn-active:has-text("Projects")').count() == 1)

        page.goto(BASE + "/projects/new")
        page.wait_for_load_state("load")
        shot(page, "i2-project-new")
        page.fill('input[name="name"]', "EORA RAG")
        page.click('button:has-text("Create")')
        page.wait_for_url(re.compile(r".*/projects/.*"))
        page.wait_for_load_state("load")
        shot(page, "i3-project-detail")
        check("project detail shows name", "EORA RAG" in page.inner_text("h1"))
        check("project detail title", "EORA RAG" in page.title(), page.title())
        check("All time card present", page.locator("text=All time").count() >= 1)

        page.goto(BASE + "/projects")
        page.wait_for_load_state("load")
        shot(page, "i4-projects-list")
        check("project card on list", page.locator("text=EORA RAG").count() >= 1)

        # Assign project on start
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
        page.fill('input[name="activity"]', "rag-eval")
        page.select_option("#project_id", label="EORA RAG")
        page.click('button[type="submit"]:has-text("Start")')
        page.wait_for_timeout(600)
        shot(page, "i5-dashboard-project-badge")
        check("project badge on active row",
              page.locator(f"#active-list a.badge:has-text('EORA RAG')").count() >= 1)

        # Stats project filter
        page.goto(BASE + "/stats")
        page.wait_for_load_state("load")
        shot(page, "i6-stats-projects")
        page.goto(BASE + "/stats?project=eora-rag")
        page.wait_for_load_state("load")
        shot(page, "i7-stats-project-filter")
        check("project filter banner", "eora-rag" in page.inner_text("body").lower())

        # ---- Settings -----------------------------------------------------
        print("== J. Settings / team")
        page.goto(BASE + "/settings/team")
        page.wait_for_load_state("load")
        shot(page, "j1-settings-team")
        check("settings tabs", page.locator(".tabs a").count() >= 4)
        check("danger zone present", page.locator("text=Danger zone").count() >= 1)

        page.goto(BASE + "/settings/members")
        page.wait_for_load_state("load")
        shot(page, "j2-settings-members")
        check("members table", page.locator("text=Members").count() >= 1)

        page.goto(BASE + "/settings/invites")
        page.wait_for_load_state("load")
        shot(page, "j3-settings-invites")
        page.click('button:has-text("Generate new invite")')
        page.wait_for_load_state("load")
        shot(page, "j4-settings-invites-created")
        check("invite created", "Invite created" in page.inner_text("body") or page.locator("table tbody tr").count() >= 1)

        page.goto(BASE + "/settings/profile")
        page.wait_for_load_state("load")
        shot(page, "j5-settings-profile")
        check("current password required",
              page.locator('input[name="current_password"]').get_attribute("required") is not None)

        # Create second team + switch
        page.goto(BASE + "/settings/team")
        page.fill('form[action="/api/team/create"] input[name="name"]', "Side Project")
        page.click('form[action="/api/team/create"] button')
        page.wait_for_load_state("load")
        shot(page, "j6-settings-team-created")
        check("team switcher lists 2", page.locator(".dropdown-content form").count() >= 2)

        # ---- Theme --------------------------------------------------------
        print("== K. Theme")
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
        # force dark
        page.evaluate("() => { localStorage.setItem('paratrack-theme','dark'); location.reload(); }")
        page.wait_for_load_state("load")
        page.wait_for_timeout(400)
        shot(page, "k1-dashboard-dark")
        theme = page.evaluate("() => document.documentElement.dataset.theme")
        check("dark theme applied", theme == "paratrack-dark", str(theme))

        page.goto(BASE + "/stats")
        page.wait_for_load_state("load")
        shot(page, "k2-stats-dark")
        page.goto(BASE + "/graph")
        page.wait_for_load_state("load")
        page.wait_for_timeout(800)
        shot(page, "k3-graph-dark")
        page.goto(BASE + "/goals")
        page.wait_for_load_state("load")
        shot(page, "k4-goals-dark")
        page.goto(BASE + "/projects")
        page.wait_for_load_state("load")
        shot(page, "k5-projects-dark")

        # back to light for final
        page.evaluate("() => { localStorage.setItem('paratrack-theme','light'); location.reload(); }")
        page.wait_for_load_state("load")

        # ---- Mobile viewport ----------------------------------------------
        print("== L. Mobile 390x844")
        page.set_viewport_size({"width": 390, "height": 844})
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
        shot(page, "l1-mobile-dashboard")
        check("mobile: dashboard no h-overflow",
              page.evaluate("() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 2"),
              page.evaluate("() => `${document.documentElement.scrollWidth} vs ${document.documentElement.clientWidth}`"))
        page.goto(BASE + "/stats")
        page.wait_for_load_state("load")
        shot(page, "l2-mobile-stats")
        check("mobile: stats no h-overflow",
              page.evaluate("() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 2"),
              page.evaluate("() => `${document.documentElement.scrollWidth} vs ${document.documentElement.clientWidth}`"))
        page.goto(BASE + "/goals")
        page.wait_for_load_state("load")
        shot(page, "l3-mobile-goals")
        check("mobile: goals no h-overflow",
              page.evaluate("() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 2"),
              page.evaluate("() => `${document.documentElement.scrollWidth} vs ${document.documentElement.clientWidth}`"))
        # mobile nav collapses to menu
        check("mobile: nav links hidden behind menu",
              page.locator('header a:has-text("Dashboard")').first.is_hidden()
              or page.locator('header .dropdown button:has-text("☰")').count() >= 1)

        page.set_viewport_size({"width": 1280, "height": 900})

        # ---- Invite accept (logged-out view) -------------------------------
        print("== M. Invite accept page (fresh context)")
        # Switch back to the first workspace (where the invite was created).
        page.goto(BASE + "/settings/invites")
        page.wait_for_load_state("load")
        # If current team has no invites, switch to the other workspace first.
        if page.locator('a[href^="/invites/"]').count() == 0:
            page.goto(BASE + "/settings/team")
            page.wait_for_load_state("load")
            # workspace switcher in topbar
            page.locator('header button:has-text("Side Project")').first.click()
            page.wait_for_timeout(200)
            # Actually create invite on THIS team if still empty — simpler:
            pass
        page.goto(BASE + "/settings/invites")
        page.wait_for_load_state("load")
        if page.locator('button:has-text("Generate new invite")').count() and page.locator('a[href^="/invites/"]').count() == 0:
            page.click('button:has-text("Generate new invite")')
            page.wait_for_load_state("load")
        href = None
        if page.locator('a[href^="/invites/"]').count():
            href = page.locator('a[href^="/invites/"]').first.get_attribute("href")
        ctx2 = browser.new_context(viewport={"width": 1280, "height": 900})
        page2 = ctx2.new_page()
        if href:
            page2.goto(BASE + href)
            page2.wait_for_load_state("load")
            shot(page2, "m1-invite-accept-anon")
            check("invite page for anon", "Sign in" in page2.inner_text("body") or "Join" in page2.inner_text("body"))
        else:
            check("invite link found", False, "no /invites/ link")
        ctx2.close()

        browser.close()

    # Summary
    print("\n" + "=" * 60)
    passed = sum(1 for _, ok, _ in results if ok)
    print(f"  {passed}/{len(results)} checks passed")
    if console_errors:
        print(f"\n  console errors ({len(console_errors)}):")
        for e in console_errors[:15]:
            print(f"    {e[:160]}")
    print(f"  screenshots: {OUT}")
    return 0 if passed == len(results) and not console_errors else 1


if __name__ == "__main__":
    sys.exit(main())
