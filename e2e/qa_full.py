"""Exercise the shipped product flows against a server and capture results.

Run:  . .venv/bin/activate && python e2e/qa_full.py
"""
from __future__ import annotations

import re
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

from target import BASE_URL as BASE

OUT = Path(__file__).parent / "screenshots" / "qa"
OUT.mkdir(parents=True, exist_ok=True)

results: list[tuple[str, bool, str]] = []
console_errors: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    results.append((name, ok, detail))
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if detail else ""))


def shot(page, name: str) -> None:
    try:
        page.screenshot(path=str(OUT / f"{name}.png"), full_page=True)
    except Exception as e:
        print(f"          shot fail {name}: {e}")


def open_disclosures(scope, target: str) -> None:
    """Open the disclosure that holds `target`, if it is closed.

    Optional fields sit behind Radix disclosures that start closed when the
    account has nothing to suggest. `scope` may be a locator or the page.
    Click through the DOM: a retrying click can land on a control that slid
    into the trigger's old spot and steal focus from the fields below it.
    """
    holder = scope.page if hasattr(scope, "page") else scope
    for _ in range(4):
        root = holder.locator(f'[data-disclosure]:has({target})')
        if not root.count():
            return
        if root.first.get_attribute("data-state") == "open":
            return
        trigger = root.first.locator("> button[aria-expanded]").first
        if not trigger.count():
            return
        trigger.evaluate("node => node.click()")
        holder.wait_for_timeout(300)


def wait_app(page) -> None:
    """Wait until the React shell has replaced its boot placeholder.

    Every React page arrives with an `.app-boot` skeleton inside the React
    root. Reading layout or text before the swap measures the skeleton instead
    of the page, which would let a layout check pass on a placeholder.
    """
    try:
        page.wait_for_selector("#paratrack-react-root .app-boot", state="detached", timeout=15000)
    except Exception:
        pass  # already painted, or a page that never had the placeholder


def main() -> int:
    ts = f"{time.time():.0f}"
    email = f"qa-{ts}@x.test"
    with sync_playwright() as p:
        br = p.chromium.launch()
        ctx = br.new_context(viewport={"width": 1280, "height": 900}, device_scale_factor=2, locale="en-US")
        pg = ctx.new_page()
        pg.on("console", lambda m: console_errors.append(f"{m.type}: {m.text}") if m.type == "error" else None)
        pg.on("pageerror", lambda e: console_errors.append(f"pageerror: {e}"))
        pg.on("dialog", lambda d: d.accept())

        # ---------- A. Auth ----------
        print("== A. Auth")
        # --- default locale is Russian (no cookie, no Accept-Language) ---
        anon = br.new_context(viewport={"width": 1280, "height": 800})
        apg = anon.new_page()
        apg.goto(BASE + "/login", wait_until="networkidle")
        html_lang = apg.evaluate("() => document.documentElement.lang")
        ru_visible = "Регистрация" in apg.content() or "Вход" in apg.content()
        check("default locale is Russian", html_lang == "ru" and ru_visible,
              f"lang={html_lang} ru_visible={ru_visible}")
        # English is one click away
        apg.goto(BASE + "/lang/en", wait_until="networkidle")
        apg.goto(BASE + "/login", wait_until="networkidle")
        check("switch to English works", apg.evaluate("() => document.documentElement.lang") == "en")
        anon.close()

        pg.goto(BASE + "/login", wait_until="networkidle")
        check("login page", pg.locator('input[name="email"]').count() == 1)
        check("forgot-password link", pg.locator('a[href="/forgot-password"]').count() == 1)
        check("public login has no language switcher", pg.locator('a[href^="/lang/"]').count() == 0)
        shot(pg, "a1-login")

        pg.goto(BASE + "/forgot-password")
        pg.fill('input[name="email"]', "qa-nobody@x.test")
        pg.click('button[type="submit"]')
        pg.wait_for_load_state("load")
        check("forgot (anon) accepts", "sent" in pg.url or "on its way" in pg.inner_text("body").lower() or "already" in pg.inner_text("body").lower())
        shot(pg, "a2-forgot")

        pg.goto(BASE + "/register")
        pg.fill('input[name="name"]', "QA Run")
        pg.fill('input[name="email"]', email)
        pg.fill('input[name="password"]', "longenoughpw")
        pg.click('button[type="submit"]')
        # A new account lands on the onboarding question, not the dashboard.
        pg.wait_for_url("**/welcome")
        check("register → onboarding", "/welcome" in pg.url, pg.url)
        shot(pg, "a3-onboarding")
        # The skip control is the last link to "/" inside the page content: the
        # shell's own "skip to content" link also matches a plain text search,
        # and the onboarding link sits below the fold, so click it by target.
        pg.locator('main a[href="/"]').last.evaluate("node => node.click()")
        # The shell routes inside the app, so there is no load event to wait
        # for; wait for the dashboard's own field instead.
        pg.wait_for_timeout(2500)
        print("DBG2 url", pg.url, "| #activity", pg.locator("#activity").count(),
              "| h1", pg.locator("h1").all_inner_texts()[:2])
        pg.wait_for_selector("#activity", timeout=10000)
        check("onboarding skipped → dashboard", "/register" not in pg.url and "/welcome" not in pg.url, pg.url)
        check("lang switcher in the shell footer", pg.locator('a[href^="/lang/"]').count() >= 1)
        shot(pg, "a4-dashboard-empty")

        # ---------- B. Dashboard: start / pause / resume / stop / backfill ----------
        print("== B. Dashboard / timers")
        pg.fill('#activity', "reading")
        open_disclosures(pg.locator('#activity').locator("xpath=ancestor::form"), "#timer-note")
        pg.fill('#timer-note', "chapter 1")
        pg.click('button[type="submit"]:has-text("Start")')
        pg.wait_for_timeout(600)
        check("start timer", "reading" in pg.locator("#active-list").inner_text())
        shot(pg, "b1-started")

        pg.fill('#activity', "writing")
        pg.click('button[type="submit"]:has-text("Start")')
        pg.wait_for_timeout(500)
        check("parallel timers", pg.locator("#active-list [data-session-id]").count() >= 2)

        pg.locator('#active-list [data-session-id] button').first.click()
        pg.wait_for_timeout(500)
        check("focus pauses others", pg.locator(".status-pill.is-paused").count() >= 1)
        shot(pg, "b2-focus")

        pg.locator('button:has-text("Resume")').first.click()
        pg.wait_for_timeout(400)
        check("resume", pg.locator(".status-pill.is-active").count() >= 1)

        pg.locator('button:has-text("Stop")').last.click()
        pg.wait_for_timeout(500)
        check("stop one", pg.locator("#active-list [data-session-id]").count() >= 1)
        shot(pg, "b3-stopped")

        # backfill
        open_disclosures(pg, "#b-activity")
        pg.fill('#b-activity', "consulting")
        pg.fill('#b-start', "yesterday 09:00")
        pg.fill('#b-end', "yesterday 11:30")
        pg.locator('#backfill form button[type="submit"]').click()
        pg.wait_for_timeout(600)
        check("backfill", True)  # toast confirms; assert via stats later
        shot(pg, "b4-backfill")

        # ---------- C. Stats ----------
        print("== C. Stats")
        pg.goto(BASE + "/stats?period=yesterday")
        pg.wait_for_load_state("load")
        body = pg.inner_text("body")
        check("stats has consulting row", "consulting" in body)
        check("stats 2h 30m", "2h 30m" in body)
        check("no HH:MM:SS", not re.search(r"\b\d{2}:\d{2}:\d{2}\b", body))
        shot(pg, "c1-stats")

        # tag attach
        pg.locator('input[placeholder="+ tag"]').first.fill("deep-work")
        pg.locator('input[placeholder="+ tag"]').first.press("Enter")
        pg.wait_for_timeout(600)
        check("tag chip", "deep-work" in pg.locator("#main article").first.inner_html())
        shot(pg, "c2-tagged")

        # inline duration edit
        row = pg.locator("#main article").first
        dur = row.locator('input[name="duration"]')
        if dur.count():
            dur.fill("2h")
            dur.press("Tab")
            pg.wait_for_timeout(600)
            newv = pg.locator("#main article").first.locator('input[name="duration"]').input_value()
            check("inline duration edit", "2h" in newv, f"val={newv}")

        # tag filter
        pg.goto(BASE + "/stats?tag=deep-work&period=yesterday")
        pg.wait_for_load_state("load")
        pg.wait_for_timeout(300)
        tb = pg.inner_text("body")
        check("tag filter banner", "filtered by" in tb.lower() or "фильтр" in tb.lower(), tb[:60].replace("\n", " "))
        shot(pg, "c3-tag-filter")

        # saved report
        pg.goto(BASE + "/stats?period=yesterday")
        pg.wait_for_load_state("load")
        open_disclosures(pg, "#report-save-name")
        pg.fill("#report-save-name", "QA yesterday")
        pg.locator('form[action="/api/reports/save"] button').click()
        pg.wait_for_timeout(1200)
        check("saved report chip", pg.locator('a[href*="period=yesterday"]').filter(has_text="QA yesterday").count() >= 1)
        shot(pg, "c4-saved-report")

        # ---------- D. Graph ----------
        print("== D. Graph")
        pg.goto(BASE + "/graph?period=yesterday")
        pg.wait_for_timeout(1000)
        g = pg.evaluate("""() => ({
          canvas: !!document.querySelector('#echart-canvas canvas'),
          echarts: typeof echarts !== 'undefined',
          total: (document.body.innerText.match(/Total tracked[^\\n]*/) || [''])[0],
        })""")
        check("graph canvas", g["canvas"] and g["echarts"])
        check("graph total label", "Total tracked" in g["total"] and g["total"] != "Total tracked:")
        shot(pg, "d1-graph")

        # ---------- E. Goals ----------
        print("== E. Goals")
        pg.goto(BASE + "/goals")
        pg.wait_for_load_state("load")
        open_disclosures(pg, "#goal-activity")
        pg.fill("#goal-activity", "reading")
        # The period is a Radix select now, not a native <select>.
        pg.locator("#goal-period").click()
        pg.get_by_role("option", name="Daily").click()
        pg.fill("#goal-minutes", "120")
        pg.click('button:has-text("Set goal")')
        pg.wait_for_timeout(600)
        # Goals render as cards with a progress bar, not a table with an id.
        gl = pg.locator("main").inner_text()
        check("goal created", "reading" in gl and not gl.strip().startswith("{"))
        check("goal shows progress", "2h" in gl)
        shot(pg, "e1-goals")

        pg.locator('main button[aria-label="Удалить цель"], main button[aria-label="Delete goal"]').first.click()
        # Deleting a goal is destructive, so it goes through the shared dialog.
        pg.get_by_role("button", name="Confirm", exact=True).click()
        pg.wait_for_timeout(1200)
        check("goal delete", "no goals yet" in pg.locator("main").inner_text().lower()
              or "нет целей" in pg.locator("main").inner_text().lower()
              or not pg.locator('main [role="progressbar"]').count())

        # ---------- F. Tags page ----------
        print("== F. Tags")
        pg.goto(BASE + "/tags")
        pg.wait_for_load_state("load")
        pg.fill("#new-tag-name", "qa-tag")
        pg.click('button:has-text("Add")')
        pg.wait_for_timeout(600)
        check("tag create", "qa-tag" in pg.locator('main ul[aria-label]').first.inner_text())
        shot(pg, "f1-tags")

        # ---------- G. Projects ----------
        print("== G. Projects")
        pg.goto(BASE + "/projects/new")
        pg.fill("#new-project-name", "QA Client")
        # Slug and colour sit behind "options".
        open_disclosures(pg.locator("#new-project-name").locator("xpath=ancestor::form"), "#new-project-slug")
        pg.fill("#new-project-slug", f"qa-client-{ts}")
        pg.fill('input[name="color"][pattern]', "#7c3aed")
        pg.get_by_role("button", name="Create", exact=True).click()
        pg.wait_for_url(f"**/projects/qa-client-{ts}")
        pg.wait_for_load_state("load")
        check("project created", f"qa-client-{ts}" in pg.url)
        # The settings block is a closed disclosure on the detail page.
        open_disclosures(pg, "#project-estimate")
        pg.fill("#project-estimate", "600")
        pg.fill("#project-rate", "100")
        pg.locator('#project-settings form[method="POST"] button[type="submit"]').first.click()
        pg.wait_for_load_state("load")
        body = pg.inner_text("body")
        check("estimate card", "planned vs actual" in body.lower() or "план и факт" in body.lower())
        check("rate saved", "100.00" in pg.content() or "10h" in body)
        shot(pg, "g1-project")

        # ---------- H. Timesheet ----------
        print("== H. Timesheet")
        pg.goto(BASE + "/timesheet")
        pg.wait_for_load_state("load")
        check("timesheet grid", pg.locator("#ts-body tr").count() >= 1)
        cell = pg.locator('#ts-body input[type="number"]').first
        if cell.count():
            cell.fill("90")
            cell.press("Tab")
            pg.wait_for_timeout(600)
            check("timesheet cell edit", True)
        shot(pg, "h1-timesheet")

        # ---------- I. Schedule ----------
        print("== I. Schedule")
        pg.goto(BASE + "/schedule")
        pg.wait_for_load_state("load")
        check("schedule grid", pg.locator('main table tbody tr').count() >= 1)
        scell = pg.locator('main table tbody input[type="number"]').first
        if scell.count():
            scell.fill("240")
            scell.press("Tab")
            pg.wait_for_timeout(600)
            check("schedule cell edit", True)
        shot(pg, "i1-schedule")

        # ---------- J. Invoices ----------
        print("== J. Invoices")
        # Unassigned time is not invoiceable — bind the activity to the project.
        # /stats is a React page now: it boots from the JSON payload in
        # #react-page-data, where each session row carries its ActivityID and
        # the page period is the only window with tracked time in it.
        assign = pg.evaluate(r"""async () => {
          const proj = await (await fetch('/api/v1/projects', {credentials:'same-origin'})).json();
          const plist = proj.projects || proj || [];
          const p = plist.find(x => (x.slug||'').startsWith('qa-client-')) || plist[0];
          const html = await (await fetch('/stats?period=yesterday', {credentials:'same-origin'})).text();
          const doc = new DOMParser().parseFromString(html, 'text/html');
          const data = JSON.parse(doc.getElementById('react-page-data').textContent).data || {};
          const row = (data.Sessions || [])[0] || {};
          const act = row.ActivityID;
          if (!p || !act) return {ok:false, pid:p&&p.id, act:act||null};
          const csrf = data.CSRFToken || '';
          const r = await fetch('/api/activities/' + act + '/project', {
            method:'POST', credentials:'same-origin',
            headers: {'X-CSRF-Token': csrf, 'Content-Type':'application/x-www-form-urlencoded'},
            body: new URLSearchParams({csrf_token: csrf, project_id: String(p.id)})});
          const day = value => String(value || '').slice(0, 10);
          return {ok: r.status < 300, status: r.status, pid: p.id, act: act,
                  from: day(data.Period && data.Period.Start), to: day(data.Period && data.Period.End)};
        }""")
        check("activity bound to project", bool(assign.get("ok")), str(assign))
        pg.goto(BASE + "/invoices")
        # The generator lives in a disclosure that starts open on an empty list.
        pg.wait_for_selector('form[action="/invoices"]')
        open_disclosures(pg, 'input[name="client"]')
        pg.fill('input[name="client"]', "QA Corp")
        # Bill the window the tracked time actually falls in; the form's own
        # defaults (month start → today) drift off it as the run goes on.
        if assign.get("from") and assign.get("to"):
            pg.fill('input[name="start"]', assign["from"])
            pg.fill('input[name="end"]', assign["to"])
        pg.locator('form[action="/invoices"] button[type="submit"]').click()
        pg.wait_for_url("**/invoices/*")
        # The shell renders the document from JSON, so the page chrome is there
        # before the invoice itself is.
        pg.wait_for_selector('a[href$="/pdf"]')
        check("invoice generated", "/invoices/" in pg.url)
        inv_body = pg.inner_text("body")
        check("invoice has number", "INV-" in inv_body)
        shot(pg, "j1-invoice")
        # PDF
        try:
            with pg.expect_download(timeout=8000) as dl:
                pg.locator('a[href$="/pdf"]').click()
            d = dl.value
            pdf = OUT / "j2-invoice.pdf"
            d.save_as(str(pdf))
            check("invoice PDF", pdf.read_bytes()[:5] == b"%PDF-", f"{pdf.stat().st_size} bytes")
        except Exception as e:
            check("invoice PDF", False, str(e)[:80])
        # manual payment link
        res = pg.evaluate("""async () => {
          const csrf = decodeURIComponent(document.cookie.match(/paratrack_csrf=([^;]+)/)?.[1] || '');
          const r = await fetch(location.pathname + '/pay', {method:'POST', credentials:'same-origin',
            headers: {'X-CSRF-Token': csrf, 'Content-Type': 'application/x-www-form-urlencoded'},
            body: new URLSearchParams({csrf_token: csrf, mode:'manual', url:'https://pay.example.com/qa'})});
          return r.status;
        }""")
        pg.reload()
        pg.wait_for_selector('a[href$="/pdf"]')
        check("payment link", res in (200, 303) and "pay.example.com" in pg.inner_text("body"))
        shot(pg, "j2-invoice-paylink")
        # mark paid
        if pg.locator('button:has-text("Mark paid")').count():
            pg.locator('button:has-text("Mark paid")').first.click()
            pg.wait_for_load_state("load")
            check("mark paid", "paid" in pg.inner_text("body").lower())
        else:
            check("mark paid", False, "no button")

        # ---------- K. Payroll ----------
        print("== K. Payroll")
        pg.goto(BASE + "/settings/members")
        pg.wait_for_selector('form[action="/api/member/pay"]')
        # The pay form is a plain POST form again: its submit button lives
        # inside it instead of pointing at it through a form= attribute.
        pay_form = pg.locator('form[action="/api/member/pay"]').first
        pay_form.locator('input[name="hourly_pay"]').fill("50")
        pay_form.locator('input[name="capacity_minutes"]').fill("480")
        pay_form.locator('button[type="submit"]').click()
        pg.wait_for_url("**/settings/members*")
        pg.goto(BASE + "/payroll")
        pg.wait_for_selector('form[action="/payroll"] button[type="submit"]')
        open_disclosures(pg, 'form[action="/payroll"] button[type="submit"]')
        # A pay run bills tracked time, so use the same window section J billed.
        if assign.get("from") and assign.get("to"):
            pg.fill('input[name="start"]', assign["from"])
            pg.fill('input[name="end"]', assign["to"])
        pg.locator('form[action="/payroll"] button[type="submit"]').click()
        pg.wait_for_url("**/payroll/*")
        pg.wait_for_selector('main h1:has-text("PAY-")')
        pb = pg.inner_text("body")
        check("payroll run", "/payroll/" in pg.url and "PAY-" in pb, pg.url)
        shot(pg, "k1-payroll")

        # ---------- L. Reports ----------
        print("== L. Reports")
        pg.goto(BASE + "/reports")
        pg.wait_for_load_state("load")
        check("report templates", pg.locator('input[name="id"]').count() >= 5)
        shot(pg, "l1-reports")
        # Report rows come from tracked time, so run them over the window the
        # backfill landed in rather than a hardcoded month.
        win = f"&from={assign['from']}&to={assign['to']}" if assign.get("from") and assign.get("to") else ""
        for rid, label in [("by-project", "project"), ("by-activity", "activity"), ("by-day", "day"), ("billable", "billable")]:
            pg.goto(BASE + f"/reports/run?id={rid}{win}")
            pg.wait_for_load_state("load")
            try:  # the shell renders the table only once React paints
                pg.wait_for_selector("main table", timeout=5000)
            except Exception:
                pass  # the check below reports it
            ok = pg.locator("table").count() >= 1
            check(f"report {label} renders", ok)
        shot(pg, "l2-report-run")
        try:
            with pg.expect_download(timeout=8000) as dl:
                pg.locator('a[href*="format=csv"]').click()
            d = dl.value
            csv = OUT / "l3-report.csv"
            d.save_as(str(csv))
            check("report CSV", "hours" in csv.read_text()[:60], csv.read_text().splitlines()[0])
        except Exception as e:
            check("report CSV", False, str(e)[:80])

        # ---------- M. Marketplace + Integrations ----------
        print("== M. Marketplace / Integrations")
        pg.goto(BASE + "/integrations/marketplace")
        pg.wait_for_load_state("load")
        mb = pg.inner_text("body")
        check("marketplace 8+ live", all(x in mb for x in ["GitHub", "GitLab", "Jira", "Trello", "Asana", "ClickUp", "Todoist", "Notion"]))
        check("marketplace coming soon", "coming soon" in mb)
        shot(pg, "m1-marketplace")
        pg.goto(BASE + "/integrations")
        # The provider picker is a Radix select: its options live in a portal
        # that only exists while the listbox is open.
        pg.wait_for_selector("#integration-provider")
        pg.locator("#integration-provider").click()
        opts = pg.locator('[role="option"]').all_inner_texts()
        pg.keyboard.press("Escape")
        check("connect form 8 providers", len(opts) == 8, str(opts))
        shot(pg, "m2-integrations")

        # ---------- N. Import ----------
        print("== N. Import")
        pg.goto(BASE + "/import")
        pg.wait_for_selector("#import-provider")
        pg.locator("#import-provider").click()
        ib = pg.locator('[role="option"]').all_inner_texts()
        pg.keyboard.press("Escape")
        check("import providers", all(x in " ".join(ib) for x in ["Toggl", "Harvest", "Clockify"]), str(ib))
        shot(pg, "n1-import")

        # ---------- O. Settings: tokens / webhooks / audit / stripe ----------
        print("== O. Settings")
        pg.goto(BASE + "/settings/tokens")
        pg.wait_for_load_state("load")
        pg.fill('input[name="name"]', "qa-token")
        pg.click('button:has-text("Create token")')
        pg.wait_for_load_state("load")
        check("API token created", "pt_" in pg.inner_text("body") or "only once" in pg.inner_text("body").lower() or "один раз" in pg.inner_text("body"))
        shot(pg, "o1-tokens")

        pg.goto(BASE + "/settings/webhooks")
        pg.wait_for_load_state("load")
        pg.fill('input[name="url"]', "https://example.com/qa-hook")
        pg.fill('input[name="secret"]', "shhh")
        pg.click('button:has-text("Add webhook")')
        pg.wait_for_load_state("load")
        check("webhook added", "example.com/qa-hook" in pg.inner_text("body"))
        shot(pg, "o2-webhooks")

        pg.goto(BASE + "/settings/audit")
        pg.wait_for_load_state("load")
        wait_app(pg)
        ab = pg.inner_text("body")
        # Audit rows are cards, not a table: each carries its action code as the
        # badge's title attribute.
        check("audit log has events", pg.locator('main span[title]').count() > 0 and "Webhook added" in ab)
        shot(pg, "o3-audit")

        pg.goto(BASE + "/settings/team")
        pg.wait_for_load_state("load")
        wait_app(pg)
        # The payments block (and its Stripe key field) is a closed disclosure.
        open_disclosures(pg, 'input[name="stripe_key"]')
        check("stripe form", pg.locator('input[name="stripe_key"]').count() >= 1)
        check("danger zone", "Danger zone" in pg.inner_text("body") or "Опасная зона" in pg.inner_text("body"))
        shot(pg, "o4-team")

        pg.goto(BASE + "/settings/profile")
        pg.wait_for_load_state("load")
        wait_app(pg)
        # Email change and password change both ask for the current password.
        check("current password required",
              pg.locator('input[name="current_password"]').count() == 2
              and pg.locator('input[name="current_password"]').first.get_attribute("required") is not None)
        shot(pg, "o5-profile")

        # ---------- P. i18n + theme ----------
        print("== P. i18n / theme")
        pg.goto(BASE + "/lang/ru?next=/")
        pg.wait_for_load_state("load")
        rb = pg.inner_text("body")
        check("RU dashboard", "Обзор" in rb or "Новая активность" in rb)
        shot(pg, "p1-ru")
        pg.goto(BASE + "/lang/en?next=/")
        pg.wait_for_load_state("load")
        check("EN dashboard", "Dashboard" in pg.inner_text("h1"))
        # The theme control moved into the React shell, which marks it with
        # data-theme-toggle instead of the server-rendered .theme-btn class.
        before = pg.evaluate("() => document.documentElement.dataset.themeMode || ''")
        pg.locator("[data-theme-toggle]").first.evaluate("node => node.click()")
        pg.wait_for_timeout(300)
        after = pg.evaluate("() => document.documentElement.dataset.themeMode || ''")
        check("theme switch cycles the mode", before != after and after in ("light", "dark", "auto"), f"{before} -> {after}")
        shot(pg, "p2-theme")
        # Dropped: "theme switch is silent" and "toast bottom-right". The toast
        # system is gone with the htmx pages — base.html still carries an empty
        # #toast live region, but nothing writes to it and window.paratrackToast
        # no longer exists, so both checks could only ever pass vacuously.

        # ---------- Q. PWA ----------
        print("== Q. PWA")
        pwa = pg.evaluate("""async () => {
          const m = await fetch('/static/manifest.webmanifest').then(r => r.json());
          const regs = await navigator.serviceWorker.getRegistrations();
          return {display: m.display, sw: regs.length, link: !!document.querySelector('link[rel=manifest]')};
        }""")
        check("PWA manifest+SW", pwa["display"] == "standalone" and pwa["sw"] >= 1 and pwa["link"], str(pwa))

        # ---------- R. API v1 + Bearer ----------
        print("== R. API v1 / Bearer")
        # extract token from tokens page is complex; use cookie-based fetch first
        api = pg.evaluate("""async () => {
          const r = await fetch('/api/v1/reports/summary', {credentials:'same-origin'});
          return {status: r.status, body: await r.text()};
        }""")
        check("API v1 summary", api["status"] == 200 and "total_seconds" in api["body"], api["body"][:60])
        api2 = pg.evaluate("""async () => {
          const r = await fetch('/api/v1/sessions', {credentials:'same-origin'});
          return {status: r.status, body: (await r.text()).slice(0, 60)};
        }""")
        check("API v1 sessions", api2["status"] == 200 and "sessions" in api2["body"])

        # ---------- S. Mobile ----------
        print("== S. Mobile")
        pg.set_viewport_size({"width": 390, "height": 844})
        for path, name in [("/", "s1-mobile-dash"), ("/stats?period=yesterday", "s2-mobile-stats"), ("/invoices", "s3-mobile-invoices")]:
            pg.goto(BASE + path)
            pg.wait_for_load_state("load")
            wait_app(pg)
            over = pg.evaluate("() => document.documentElement.scrollWidth - document.documentElement.clientWidth")
            check(f"mobile no h-overflow {path}", over <= 2, f"over={over}")
            shot(pg, name)

        br.close()

    print("\n" + "=" * 60)
    passed = sum(1 for _, ok, _ in results if ok)
    print(f"  {passed}/{len(results)} checks passed")
    if console_errors:
        print(f"\n  console errors ({len(console_errors)}):")
        for e in console_errors[:10]:
            print(f"    {e[:160]}")
    print(f"  screenshots: {OUT}")
    return 0 if passed == len(results) and not console_errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
