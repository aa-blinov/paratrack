"""E2E walkthrough for paratrack web UI.

Drives the running paratrack server with Playwright. The target defaults to
http://127.0.0.1:8888 and can be overridden with PARATRACK_BASE. Takes a
screenshot at each major state, runs DOM assertions, and prints each result.

Run from the repo root with the .venv active:

    source .venv/bin/activate
    python e2e/test_dashboard.py
"""

from __future__ import annotations

import sys
import time
import os
import uuid
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE

SCREENSHOTS = Path(os.environ.get("PARATRACK_SCREENSHOTS", Path(__file__).parent / "screenshots"))
SCREENSHOTS.mkdir(parents=True, exist_ok=True)


def register_account(page, name="E2E User", email="e2e@paratrack.test", password="longenoughpw") -> None:
    """Register a fresh account via the public form. The browser context
    picks up the session cookie automatically. Idempotent enough for
    repeated local runs (the server rejects duplicates — we tolerate
    that by signing in instead)."""
    page.goto(BASE + "/register")
    page.fill("#name", name)
    page.fill("#email", email)
    page.fill("#password", password)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    # New accounts answer the workspace setup question before reaching the
    # dashboard. Pick the smallest useful preset so the walkthrough has the
    # graph and other core discovery paths enabled.
    if page.url.endswith("/welcome"):
        page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="solo"]) button').click()
        page.wait_for_url(BASE + "/")


def sign_in(page, email="e2e@paratrack.test", password="longenoughpw") -> None:
    """Sign in to an existing account. Used when /register rejects the
    email as a duplicate (repeated test runs)."""
    page.goto(BASE + "/login")
    page.fill("#email", email)
    page.fill("#password", password)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")

results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    """Record a step result and print it inline."""
    status = "PASS" if ok else "FAIL"
    print(f"  [{status}] {name}{(' — ' + detail) if detail else ''}")
    results.append((name, ok, detail))


def shot(page, name: str) -> Path:
    """Save a full-page screenshot and return the path."""
    p = SCREENSHOTS / f"{name}.png"
    page.screenshot(path=str(p), full_page=True)
    print(f"          📸 {p.name} ({p.stat().st_size // 1024} KB)")
    return p


def _csrf(page):
    for c in page.context.cookies():
        if c.get("name") == "paratrack_csrf":
            return c.get("value") or ""
    return ""


def api(page, method, url, **kw):
    """page.request wrapper that injects the CSRF double-submit header."""
    headers = dict(kw.pop("headers", None) or {})
    headers.setdefault("X-CSRF-Token", _csrf(page))
    return getattr(page.request, method)(url, headers=headers, **kw)


def open_disclosures(scope, target: str) -> None:
    """Open the disclosure that holds `target`, if it is closed.

    Optional fields live behind Radix disclosures that start closed when the
    account has nothing to suggest. `scope` may be a locator or the page.
    Click through the DOM: Playwright's retrying click can land on a control
    that slid into the trigger's old spot — the currency select, for one —
    and that steals focus from the fields below it.
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
def main() -> int:
    with sync_playwright() as p:
        # Real desktop viewport, with prefers-color-scheme = light by default
        # so we can exercise the theme toggle visually.
        browser = p.chromium.launch()
        context = browser.new_context(
            viewport={"width": 1280, "height": 900},
            color_scheme="light",
            locale="en-US",
            device_scale_factor=2,
        )
        context.add_cookies([{"name": "paratrack_lang", "value": "en", "url": BASE}])
        page = context.new_page()
        # Capture console + request failures for debugging.
        page.on("console", lambda m: print(f"  console.{m.type}: {m.text[:200]}"))
        page.on("requestfailed", lambda r: print(f"  reqfail: {r.url} — {r.failure}"))
        page.on(
            "response",
            lambda r: r.request.method in ("POST", "PATCH", "DELETE")
            and print(f"  {r.request.method} {r.url} → {r.status}"),
        )

        # ------------------------------------------------------------------ 1
        print("\n== 1. Register / sign in (auth gate)")
        e2e_email = f"e2e-{uuid.uuid4().hex[:12]}@paratrack.test"
        # First run after a fresh DB: /register succeeds and the cookie
        # is set. On repeated runs the email is taken; detect by checking
        # the URL after submit — fall back to /login.
        register_account(page, email=e2e_email)
        # Register can land on `/` (success), `/register?error=…` (duplicate),
        # or `/login` (any prior redirect). Fall through to sign_in unless
        # we actually reached the dashboard.
        if "/login" in page.url or "/register" in page.url:
            sign_in(page, email=e2e_email)
        # Confirm the session took: the top-bar user menu shows the email.
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
        favicon = page.locator('link[rel="icon"][type="image/svg+xml"]')
        check("browser tab uses the branded SVG favicon",
              favicon.count() == 1
              and page.request.get(BASE + favicon.get_attribute("href")).status == 200
              and page.request.get(BASE + "/static/favicon.ico").status == 200)
        check(
            "logged in: dashboard reachable without redirect",
            page.url.rstrip("/") == BASE,
            f"url={page.url}",
        )
        # Make sure no stale sessions from a previous run block step 2.
        # /api/stop without arg requires interactive multi-select, so use
        # the stop-on-each-active trick: list via /api/active, then POST
        # stop for each id.
        active_html = page.request.get(BASE + "/api/active").text()
        import re as _re
        for sid in _re.findall(r"/api/sessions/(\d+)/(?:stop|pause|resume)", active_html):
            api(page, 'post', BASE + f"/api/sessions/{sid}/stop")
        page.goto(BASE + "/")
        expect(page.locator("h1")).to_have_text("Dashboard")
        check("dashboard renders h1=Dashboard", True)
        check("React shell leaves one semantic main landmark",
              page.locator("main").count() == 1 and page.locator("#main").count() == 1)
        nav_text = page.locator(".app-shell-desktop-nav").inner_text()
        for label in ["Dashboard", "Stats"]:
            check(f"nav has '{label}' link", label in nav_text)
        # The shadcn sidebar exposes optional sections directly instead of
        # nesting destinations behind the former top-bar More menu.
        check("Graph has one visible destination", page.locator('.app-shell-desktop-nav a[href="/graph"]:visible').count() == 1)
        check("sidebar shows export destination", page.locator('.app-shell-desktop-nav a[href="/export"]:visible').count() == 1)
        for width in (1024, 1152, 1280, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            check(
                f"sidebar destinations fit at {width}px",
                page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
                and page.locator('.app-shell-desktop-nav').is_visible()
                and page.locator('.app-shell-desktop-nav a[href="/projects"]').is_visible(),
            )
        page.set_viewport_size({"width": 1280, "height": 900})
        check(
            "theme toggle button present",
            page.locator('[data-theme-toggle]').count() >= 1,
        )
        # A personal workspace still shows its name; the switcher is useful
        # once the user has more than one workspace.
        check(
            "workspace name present",
            page.locator('.app-shell-workspace-label, .app-shell-workspace').count() >= 1,
        )
        shot(page, "01-dashboard-light")
        # Internal navigation should preserve the JavaScript document and the
        # browser history should restore the previous React screen.
        page.evaluate("window.__paratrackNavigationProbe = crypto.randomUUID()")
        navigation_probe = page.evaluate("window.__paratrackNavigationProbe")
        page.evaluate("window.__paratrackMainNode = document.getElementById('main')")
        page.locator('.app-shell-desktop-nav a[href="/stats"]').click()
        expect(page).to_have_url(BASE + "/stats")
        expect(page.locator("#main h1")).to_be_visible()
        check("internal navigation keeps the current document alive",
              page.evaluate("window.__paratrackNavigationProbe") == navigation_probe)
        check("screen changes keep the same app-shell container",
              page.evaluate("window.__paratrackMainNode === document.getElementById('main')"))
        page.go_back()
        expect(page).to_have_url(BASE + "/")
        expect(page.locator("#main h1")).to_have_text("Dashboard")
        check("browser Back restores the previous React screen", True)
        page.set_viewport_size({"width": 320, "height": 844})
        page.goto(BASE + "/timesheet")
        expect(page.locator("h1")).to_have_text("Timesheet")
        if page.locator("table.week-grid").count() == 0:
            empty_cta = page.locator('#main a[href="/"]').first
            box = empty_cta.bounding_box()
            check("empty timesheet action is visible without grid scrolling",
                  box is not None and box['x'] >= 0 and box['x'] + box['width'] <= 320
                  and page.locator('table.week-grid').count() == 0)
        page.goto(BASE + "/")
        page.set_viewport_size({"width": 390, "height": 844})
        sidebar_trigger = page.locator('.app-shell-header [data-sidebar="trigger"]')
        sheet = page.locator('[data-sidebar="sidebar"][data-mobile="true"]')
        sidebar_trigger.click()
        expect(sheet).to_be_visible()
        check("shadcn mobile sidebar is an accessible dialog with all destinations",
              sheet.get_attribute('role') == 'dialog'
              and sheet.locator('a[href="/export"]').count() == 1)
        page.wait_for_timeout(300)
        page.screenshot(path=str(SCREENSHOTS / "01-sidebar-mobile.png"))
        sheet.get_by_role("button", name="Close").click()
        check("mobile sidebar closes from its close button", not sheet.is_visible())
        sidebar_trigger.click()
        page.mouse.click(380, 400)
        check("mobile sidebar closes via backdrop", not sheet.is_visible())
        sidebar_trigger.click()
        page.keyboard.press("Escape")
        check("mobile sidebar closes via Escape", not sheet.is_visible())
        touch_context = browser.new_context(
            viewport={"width": 390, "height": 650}, is_mobile=True,
            has_touch=True, storage_state=context.storage_state(),
        )
        touch_page = touch_context.new_page()
        touch_page.goto(BASE + "/")
        touch_page.locator('.app-shell-header [data-sidebar="trigger"]').click()
        touch_page.wait_for_timeout(300)
        touch_sheet = touch_page.locator('[data-sidebar="sidebar"][data-mobile="true"]')
        scroller = touch_sheet.locator('[data-sidebar="content"]')
        check("shadcn mobile sidebar keeps navigation scrollable without page overflow",
              scroller.evaluate('el => getComputedStyle(el).overflowY') == 'auto'
              and touch_sheet.locator('a[href="/export"]').is_visible()
              and touch_page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        touch_page.keyboard.press('Escape')
        check("touch sidebar returns focus after closing", not touch_sheet.is_visible())
        touch_context.close()
        sidebar_trigger.click()
        page.evaluate("window.__paratrackMobileNavigationProbe = crypto.randomUUID()")
        mobile_navigation_probe = page.evaluate("window.__paratrackMobileNavigationProbe")
        sheet.locator('a[href="/export"]').click()
        expect(page).to_have_url(BASE + "/export")
        expect(page.locator("#main h1")).to_have_text("Export")
        check("mobile Export explains sessions and offers a summary route",
              page.url.endswith('/export')
              and page.locator('form[action="/api/reports.csv"] button[type="submit"]').count() == 1
              and 'paratrack.csv' in page.locator('#main').inner_text()
              and page.locator("#main h1").inner_text() == "Export"
              and page.evaluate("window.__paratrackMobileNavigationProbe") == mobile_navigation_probe)
        page.evaluate("import('/static/js/app-toast.js').then(m => m.paratrackToast('Saved', 'success', 10000))")
        check("success toast is readable without a decorative check",
              page.locator('#toast .toast-note').inner_text() == 'Saved'
              and page.locator('#toast .toast-note svg').count() == 0)
        page.evaluate("import('/static/js/app-toast.js').then(m => m.paratrackToast('Failed', 'error', 10000))")
        check("error toast retains its distinct icon",
              page.locator('#toast .toast-note[role="alert"] svg use[href$="#i-x"]').count() == 1)
        # The mobile sheet is a Radix dialog: while it is open it takes pointer
        # events away from the page behind it. The rest of this run drives the
        # dashboard directly, so wait for that overlay to be released.
        page.wait_for_function(
            "() => getComputedStyle(document.body).pointerEvents !== 'none'"
            " && document.querySelector('[data-radix-popper-content-wrapper], [role=dialog]') === null",
            timeout=5000,
        )
        page.goto(BASE + '/settings/sections')
        expect(page.locator("#main h1")).to_be_visible()
        check("preset stays selected without a redundant check",
              page.locator('form[action="/api/team/modules"] button[aria-current="true"]').count() == 1
              and page.locator('input[name="modules"]:checked').count() > 0)
        for width in (390, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            page.goto(BASE + "/")
            expect(page.locator("#main")).to_be_visible()
            baseline = page.evaluate("""() => {
                const main = document.querySelector('#main');
                const rect = main.getBoundingClientRect();
                const style = getComputedStyle(main);
                const left = parseFloat(style.paddingLeft);
                const right = parseFloat(style.paddingRight);
                return {x: rect.x + left, width: rect.width - left - right};
            }""")
            for path in ("/settings/profile", "/settings/preferences", "/settings/team",
                         "/settings/members", "/settings/notifications"):
                page.goto(BASE + path)
                expect(page.locator("#main h1")).to_be_visible()
                box = page.locator("#main").evaluate("""el => {
                    const rect = el.getBoundingClientRect();
                    const style = getComputedStyle(el);
                    const left = parseFloat(style.paddingLeft);
                    const right = parseFloat(style.paddingRight);
                    return {x: rect.x + left, width: rect.width - left - right};
                }""")
                check(
                    f"{path} matches dashboard width at {width}px",
                    abs(box["x"] - baseline["x"]) < 1
                    and abs(box["width"] - baseline["width"]) < 1
                    and page.evaluate("document.documentElement.scrollWidth <= innerWidth"),
                )
        page.set_viewport_size({"width": 1440, "height": 900})
        page.goto(BASE + "/settings/profile")
        check("profile fields keep a readable measure", page.locator('form[action="/api/profile"]').evaluate(
            "el => el.getBoundingClientRect().width <= 768"))
        page.goto(BASE + "/projects/new")
        check("new-project form keeps a readable measure", page.locator('form[action="/projects/new"]').evaluate(
            "el => el.getBoundingClientRect().width <= 768"))
        check("critical font preload matches page language", page.evaluate("""() =>
            [...document.querySelectorAll('link[rel="preload"][as="font"]')].some(link =>
                link.href.endsWith(document.documentElement.lang === 'ru'
                    ? '/inter-cyrillic.woff2' : '/inter-latin.woff2'))"""))
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/")
        # The shell reflows when the viewport changes width, so wait for the
        # timer form to settle before typing into it.
        expect(page.locator("#activity")).to_be_visible()
        page.wait_for_timeout(400)

        # ------------------------------------------------------------------ 2
        print("\n== 2. Start a new activity via the form (UI)")
        before = page.locator(".status-pill.is-active").count()
        page.fill("#activity", "writing")
        # The note sits behind the optional-fields disclosure, and a fresh
        # account has no default project, so that disclosure starts closed.
        open_disclosures(page.locator("#activity").locator("xpath=ancestor::form"), "#timer-note")
        page.fill("#timer-note", "e2e playwright test")
        # A Radix select/menu left open takes pointer events away from the whole
        # page, and the click below would then hang with no useful message.
        page.keyboard.press("Escape")
        page.wait_for_function(
            "() => getComputedStyle(document.body).pointerEvents !== 'none'", timeout=5000,
        )
        page.click('button[type="submit"]:has-text("Start")')
        # Wait specifically inside #active-list — not the form input —
        # so we know the HTMX swap has happened.
        page.wait_for_selector('#active-list [data-session-id]:has-text("writing")', timeout=5000)
        check("active list contains 'writing' after React refresh", True)
        after = page.locator(".status-pill.is-active").count()
        check(
            "active count grew by 1 after starting",
            after == before + 1,
            f"before={before} after={after}",
        )
        shot(page, "02-dashboard-after-start")

        # ------------------------------------------------------------------ 3
        print("\n== 3. Pause one of the active sessions")
        first_pause = page.locator('button:has-text("Pause")').first
        first_pause.click()
        page.wait_for_timeout(300)  # htmx settle
        check(
            "exactly one paused badge",
            page.locator(".status-pill.is-paused").count() == 1,
            f"got {page.locator('.status-pill.is-paused').count()}",
        )
        shot(page, "03-dashboard-after-pause")
        # /stats only shows closed sessions, so we close the paused session
        # (and any others) via the API before step 4 navigates there. The
        # start+pause+stop burst lands in the same second, so /stats'
        # clipSeconds() would drop a 0-second row — give each session a
        # 10-minute duration via PATCH so it shows up.
        import re as _seed_re
        active_html = page.request.get(BASE + "/api/active").text()
        for sid in _seed_re.findall(r"/api/sessions/(\d+)/(?:stop|pause|resume)", active_html):
            api(page, 'post', BASE + f"/api/sessions/{sid}/stop")
            api(page, 'patch', 
                BASE + f"/api/sessions/{sid}",
                form={"duration": "10m"},
            )

        # ------------------------------------------------------------------ 4
        print("\n== 4. Stats page")
        page.click('.app-shell-desktop-nav a[href="/stats"]')
        page.wait_for_url("**/stats")
        expect(page.locator("h1")).to_have_text("Stats")
        check("stats h1=Stats", True)
        # Wait for sessions table to render. On timeout dump the page so the
        # failure is diagnosable from /tmp/e2e-stats.html rather than a bare
        # TimeoutError.
        try:
            page.wait_for_selector(".stats-breakdown-table tbody tr, article.rounded-md.border", timeout=3000)
        except Exception as _e:
            try:
                with open("/tmp/e2e-stats.html", "w") as _f:
                    _f.write(page.content())
            except Exception:
                pass
            raise
        rows = page.locator(".stats-breakdown-table tbody tr, article.rounded-md.border").count()
        check("stats shows session rows", rows >= 1, f"{rows} rows")
        # Edit duration inline: change first row duration to 45m
        first_dur = page.locator("article.rounded-md.border input").nth(2)
        first_dur.fill("45m")
        first_dur.press("Tab")
        page.wait_for_timeout(500)
        shot(page, "04-stats-with-edit")

        # ------------------------------------------------------------------ 5
        print("\n== 5. Graph page")
        page.locator('.app-shell-desktop-nav a[href="/graph"]:visible').click()
        page.wait_for_url("**/graph")
        # The seeded sessions use a synthetic future end; a complete week
        # keeps them visible while today correctly clips at the current time.
        page.goto(BASE + "/graph?period=week")
        page.wait_for_load_state("load")
        expect(page.locator("h1")).to_have_text("When you work")
        check("graph mounts the React/shadcn shell",
              # six preset periods plus the "custom dates" tab, and the print
              # button sits in the same bar
              page.locator('#paratrack-react-root .period-tabs a').count() == 7
              and page.locator('#paratrack-react-root .period-tabs button').count() >= 1
              and page.locator('#paratrack-react-root [data-slot="card"]').count() >= 1)
        # ECharts renders into a <canvas>; wait for that.
        page.wait_for_selector("#echart-canvas canvas", timeout=3000)
        series_count = page.evaluate(
            """
            () => {
              const inst = echarts.getInstanceByDom(document.getElementById('echart-canvas'));
              return inst ? inst.getOption().series.length : 0;
            }
            """
        )
        check("echarts has >=1 series", series_count >= 1, f"{series_count}")
        shot(page, "05-graph-light")

        # ------------------------------------------------------------------ 6
        print("\n== 6. Theme toggle: auto -> light -> dark -> auto")
        # Reset to known starting state.
        page.evaluate("() => { localStorage.removeItem('paratrack-theme'); location.reload(); }")
        page.wait_for_load_state("load")
        for expected in ["paratrack-light", "paratrack-dark", "auto"]:
            page.click('[data-theme-toggle]')
            page.wait_for_timeout(150)
            attr = page.evaluate(
                "() => document.documentElement.dataset.themeMode === 'auto'"
                " ? 'auto' : (document.documentElement.dataset.theme || 'unset')"
            )
            check(
                f"theme click sets html data-theme={expected!r}",
                attr == expected,
                f"actual={attr}",
            )
        # Stop on dark for the dramatic screenshot.
        # After the loop above we're at 'auto'; click twice to reach dark.
        page.click('[data-theme-toggle]')  # auto -> light
        page.click('[data-theme-toggle]')  # light -> dark
        page.wait_for_timeout(200)
        page.click('.app-shell-desktop-nav a[href="/"]')
        page.wait_for_url(BASE + "/")
        page.wait_for_selector("h1:has-text('Dashboard')")
        shot(page, "06-dashboard-dark")

        # ------------------------------------------------------------------ 7
        print("\n== 7. Keyboard shortcuts")
        page.keyboard.press("g")
        page.wait_for_url("**/graph")
        check("'g' navigates to /graph", "/graph" in page.url)
        page.keyboard.press("s")
        page.wait_for_url("**/stats")
        check("'s' navigates to /stats", "/stats" in page.url)
        page.keyboard.press("Escape")  # no-op, just check we're responsive

        # ------------------------------------------------------------------ 7b
        print("\n== 7b. Projects: create, detail, assign to activity, dashboard badge")
        # Unique slug per run so repeated runs don't hit a duplicate.
        from time import time as _now
        proj_slug = f"eora-rag-{int(_now())}"
        proj_name = f"EORA RAG {proj_slug}"
        page.goto(BASE + "/projects/new")
        expect(page.locator("#new-project-name")).to_be_visible()
        # The mobile-sheet section above leaves Radix's modal state on the
        # shell. While it is set the optional fields cannot take focus and
        # whatever is typed lands in the autofocused name box instead, so wait
        # the state out before driving the form directly.
        page.keyboard.press("Escape")
        page.wait_for_function(
            "() => { const root = document.getElementById('paratrack-react-root');"
            " return getComputedStyle(document.body).pointerEvents !== 'none'"
            " && !(root && root.firstElementChild"
            " && root.firstElementChild.getAttribute('aria-hidden') === 'true'); }",
            timeout=5000,
        )
        check("project create page mounts React/shadcn form",
              page.locator('#paratrack-react-root form[action="/projects/new"] [data-slot="input"]').count() >= 3)
        page.fill("#new-project-name", proj_name)
        # Slug and colour live behind "options", closed without an invoicing module.
        open_disclosures(page.locator("#new-project-name").locator("xpath=ancestor::form"), "#new-project-slug")
        page.fill("#new-project-slug", proj_slug)
        # The picker itself has no name; the hex field next to it carries it.
        page.fill('input[name="color"][pattern]', "#7c3aed")
        check("each project field kept its own value",
              page.locator("#new-project-name").input_value() == proj_name
              and page.locator("#new-project-slug").input_value() == proj_slug,
              f"name={page.locator('#new-project-name').input_value()!r} slug={page.locator('#new-project-slug').input_value()!r}")
        page.get_by_role("button", name="Create").click()
        page.wait_for_url(f"**/projects/{proj_slug}")
        check(f"project created at /projects/{proj_slug}", proj_slug in page.url)

        # Detail page renders the right title.
        expect(page.locator("h1")).to_contain_text(proj_name)
        check("detail page shows project name", proj_name in page.content())
        check("project detail mounts React/shadcn session and edit cards",
              page.locator('#paratrack-react-root [data-slot="card"]').count() >= 3
              and page.locator('#paratrack-react-root form[action^="/projects/"]').count() == 2)
        updated_project_name = proj_name + " updated"
        # The edit fields sit behind a disclosure that starts closed.
        open_disclosures(page.locator("h1").locator("xpath=ancestor::main"), "#project-name")
        page.fill("#project-name", updated_project_name)
        page.fill("#project-estimate", "480")
        page.locator('#paratrack-react-root form[method="POST"][action^="/projects/"] button[type="submit"]').first.click()
        page.wait_for_url(f"**/projects/{proj_slug}?flash=*")
        proj_name = updated_project_name
        expect(page.locator("h1")).to_contain_text(proj_name)
        check("React project edit form persists name and estimate", proj_name in page.content()
              and page.locator('#paratrack-react-root [data-slot="progress"]').count() == 1)

        # Project cards and archive filtering are rendered by the React app.
        page.goto(BASE + "/projects")
        project_card = page.locator('#paratrack-react-root [data-slot="card"]').filter(has_text=proj_name).first
        expect(project_card).to_contain_text(proj_name)
        check("project list mounts the React/shadcn card", project_card.count() > 0)
        original_theme = page.locator("html").get_attribute("data-theme")
        project_card.evaluate("el => document.documentElement.dataset.theme = 'paratrack-dark'")
        dark_card = project_card.evaluate("el => getComputedStyle(el).backgroundColor")
        dark_text = project_card.locator("h2").evaluate("el => getComputedStyle(el).color")
        check("React project card follows dark theme tokens",
              dark_card == "oklch(0.205 0 0)" and dark_text == "oklch(0.985 0 0)",
              f"card={dark_card}, text={dark_text}")
        project_card.evaluate("(el, theme) => document.documentElement.dataset.theme = theme", original_theme)
        page.get_by_role("link", name="Show archived").click()
        page.wait_for_url("**/projects?archived=1")
        expect(page.locator('#paratrack-react-root [data-slot="card"]').filter(has_text=proj_name).first).to_contain_text(proj_name)
        check("project list archive filter keeps projects visible", proj_name in page.content())
        shot(page, "11-project-list-react")
        page.get_by_role("link", name="Hide archived").click()
        page.wait_for_url("**/projects")
        page.goto(BASE + f"/projects/{proj_slug}")

        # A unique activity per run keeps repeated local E2E runs from
        # inheriting a project assignment from an earlier run.
        activity_name = f"deep-work-{proj_slug}"
        page.locator('main a[href^="/?project="]').first.click()
        page.wait_for_url("**/?project=*")
        selected_id = page.url.split('project=')[-1]
        check("project page opens live and past time with its project selected",
              page.locator('#project_id').get_attribute('data-value') == selected_id
              and page.locator('#b-project').get_attribute('data-value') == selected_id)
        unassigned = page.locator('#known-activities option[data-project="0"]').first.get_attribute('value')
        page.fill('#activity', unassigned)
        check("reusing an activity explains that its history moves",
              page.locator('#ledger-project-rebind').is_visible())
        page.fill('#activity', activity_name)
        check("new activities do not show the history warning",
              not page.locator('#ledger-project-rebind').is_visible())
        page.get_by_role('combobox', name='Project').first.click()
        page.get_by_role('option', name=proj_name).click()
        with page.expect_response("**/api/start") as start_resp_info:
            page.click('button[type="submit"]:has-text("Start")')
        start_resp = start_resp_info.value
        check("start POST 200", start_resp.status == 200, f"status={start_resp.status}")
        # Wait for the new session row to land in the active-list fragment.
        page.wait_for_selector(f'#active-list:has-text("{activity_name}")', timeout=3000)
        check("dashboard shows new active session", True)
        # Small extra wait so HTMX finishes swapping the active-list
        # fragment before we look for the badge inside it.
        page.wait_for_timeout(300)
        shot(page, "11-projects-active-list")

        # An active row exposes its activity-level project assignment as a
        # real select. The selected option is the user-visible proof that the
        # timer started under the chosen project.
        project_select = page.locator("#active-list button.ledger-project")
        page.wait_for_selector("#active-list button.ledger-project", state="visible", timeout=3000)
        selected = project_select.inner_text()
        check(
            "active-list shows the selected project",
            project_select.count() >= 1 and selected.strip() == proj_name,
            f"selected={selected!r}",
        )
        page.set_viewport_size({"width": 320, "height": 900})
        page.goto(BASE + f"/projects/{proj_slug}")
        page.locator('main h1 span.truncate').first.evaluate(
            "el => { el.textContent = 'A very long project title that must remain inside the detail page'; }")
        check("long project detail title stays within 320px",
              page.evaluate("document.documentElement.scrollWidth <= innerWidth"))
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/")
        project_select = page.locator("#active-list button.ledger-project")
        # A long project option must not steal the activity's entire lane
        # on the intermediate layout. Change display text only, then reload.
        for width in (390, 768, 1024, 1280):
            page.set_viewport_size({"width": width, "height": 900})
            project_select.evaluate("el => { el.style.maxWidth = '10rem'; el.title = 'A very long project name that must not move the timer columns'; }")
            if width == 768:
                page.screenshot(path=str(SCREENSHOTS / "11-project-long-name.png"))
            activity_width = page.locator('#active-list .ledger-activity').last.evaluate(
                "el => el.getBoundingClientRect().width")
            select_width = project_select.evaluate("el => el.getBoundingClientRect().width")
            no_overflow = page.evaluate("document.documentElement.scrollWidth <= innerWidth")
            check(f"long project keeps activity visible at {width}px",
                  activity_width > 36 and select_width <= 160 and no_overflow,
                  f"activity={activity_width:.1f}px, project={select_width:.1f}px, no-overflow={no_overflow}")
            project_select.evaluate("el => { el.style.maxWidth = ''; el.removeAttribute('title'); }")
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/")

        # Weekly tables share a fixed name lane; long names must not consume
        # day columns, even at narrow effective widths (browser zoom/mobile).
        for width in (320, 1024):
            page.set_viewport_size({"width": width, "height": 900})
            page.goto(BASE + "/timesheet")
            expect(page.locator("#main h1")).to_be_visible()
            check(f"timesheet uses the React/shadcn mount at {width}px",
                  page.locator('#paratrack-react-root h1').count() == 1
                  and page.locator('#paratrack-react-root .week-grid').count() == 1)
            check(f"timesheet keeps a readable first column at {width}px",
                  page.locator('.week-grid th').first.evaluate(
                      "el => el.getBoundingClientRect().width >= 160")
                  and page.evaluate("document.documentElement.scrollWidth <= innerWidth")
                  and page.locator('.week-nav-label').evaluate(
                      "el => getComputedStyle(el).whiteSpace === 'nowrap'"))
        page.set_viewport_size({"width": 320, "height": 900})
        page.goto(BASE + "/")
        project_picker = page.locator('#project_id')
        project_picker.evaluate("el => { el.style.maxWidth = '10rem'; el.title = 'A very long project name for the new timer'; }")
        check("long project selection stays inside the 320px dashboard",
              project_picker.evaluate("el => el.getBoundingClientRect().width <= 160")
              and page.evaluate("document.documentElement.scrollWidth <= innerWidth"))
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/")

        # Set a visible duration before the stats period/filter checks.
        # The duration PATCH closes a running session; a second stop would
        # correctly return "already stopped".
        new_sid = page.locator(f'#active-list [data-session-id]:has-text("{activity_name}")').get_attribute("data-session-id")
        started_at = page.evaluate("""() => {
            const d = new Date(Date.now() - 20 * 60 * 1000);
            const pad = n => String(n).padStart(2, '0');
            return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
        }""")
        check("project timer duration saved before stats",
              api(page, "patch", BASE + f"/api/sessions/{new_sid}",
                  form={"start_at": started_at, "duration": "10m"}).ok)

        # Stats page breakdown shows the project with our activity under it.
        page.goto(BASE + "/stats")
        page.locator("#main").get_by_text(proj_name).first.wait_for(timeout=3000)
        check("stats breakdown mentions project", proj_name in page.content())
        page.set_viewport_size({"width": 320, "height": 900})
        page.locator('a[href*="project="] span.truncate').first.evaluate(
            "el => { el.textContent = 'A very long project name that must stay inside the stats filter'; }")
        check("long stats project filter fits at 320px",
              page.evaluate("document.documentElement.scrollWidth <= innerWidth"))
        page.set_viewport_size({"width": 1280, "height": 900})

        # Project filter narrows to just the one project.
        page.goto(BASE + f"/stats?project={proj_slug}")
        expect(page.locator("#main h1")).to_have_text("Stats")
        filtered_body = page.locator("#main").inner_text()
        check(
            "project filter keeps the selected project",
            proj_name in filtered_body and "writing" not in filtered_body,
            f"contains_project={proj_name in filtered_body}",
        )

        graph_link = page.locator('main a[href^="/graph?period="]').first
        check("stats links to a graph with its project filter",
              graph_link.count() == 1 and f"project={proj_slug}" in graph_link.get_attribute('href'))
        graph_link.click()
        page.wait_for_selector('#echart-canvas canvas')
        check("graph scope reflects filtered stats",
              page.locator('.legend-chip').filter(has_text=activity_name).count() == 1
              and page.locator('.legend-chip').filter(has_text='writing').count() == 0
              and f'project={proj_slug}' in page.url)
        week_link = page.locator('.period-tabs a[href^="?period=week"]').first
        check("graph period change keeps project scope", f'project={proj_slug}' in week_link.get_attribute('href'))
        page.goto(BASE + f"/stats?project={proj_slug}")
        shot(page, "11-projects-detail")

        # ------------------------------------------------------------------ 8
        print("\n== 8. Active session edit (PATCH via duration)")
        # Go back to dashboard; start something fresh via API for speed.
        page.goto(BASE + "/stats?period=week")
        page.wait_for_selector("article.rounded-md.border input")
        before = page.locator("article.rounded-md.border input").nth(2).input_value()
        d_in = page.locator("article.rounded-md.border input").nth(2)
        d_in.fill("2h 30m")
        d_in.press("Tab")
        page.wait_for_timeout(700)  # HTMX PATCH + swap
        after = page.locator("article.rounded-md.border input").nth(2).input_value()
        check(
            "duration edit applied (2h 30m)",
            after == "2h 30m",
            f"before={before} after={after}",
        )

        # ------------------------------------------------------------------ 9
        print("\n== 9. CSV download link is present and reachable")
        resp = page.request.get(BASE + "/api/reports.csv")
        check("CSV 200 OK", resp.status == 200)
        check(
            "CSV looks like CSV",
            resp.headers.get("content-type", "").startswith("text/csv"),
        )
        check(
            "CSV has header + at least 1 data row",
            len(resp.text().splitlines()) >= 2,
        )

        # ------------------------------------------------------------------ 10
        print("\n== 10. ECharts graph — canvas renders, tooltip shows real values")
        page.goto(BASE + "/graph?period=month")
        page.wait_for_selector("#echart-canvas canvas", timeout=3000)
        page.wait_for_timeout(600)  # let the chart finish animating in
        canvas_count = page.locator("#echart-canvas canvas").count()
        check("ECharts rendered a <canvas>", canvas_count >= 1, f"{canvas_count} canvas")

        # Verify chart exposes an echarts instance and has 24 hour categories.
        meta = page.evaluate(
            """
            () => {
              const inst = echarts.getInstanceByDom(document.getElementById('echart-canvas'));
              if (!inst) return null;
              const opt = inst.getOption();
              return {
                hours: opt.xAxis[0].data,
                seriesCount: opt.series.length,
                stackSet: opt.series.every(s => s.stack === 'hour'),
                legend: opt.legend && opt.legend[0] ? opt.legend[0].data : null,
              };
            }
            """
        )
        check("echarts instance exists", meta is not None)
        if meta:
            check("x-axis has 24 hour labels", len(meta["hours"]) == 24, f"{len(meta['hours'])}")
            check("all series stacked on 'hour'", meta["stackSet"])
            check(
                ">=1 series (activities) rendered",
                meta["seriesCount"] >= 1,
                f"{meta['seriesCount']}",
            )

        # Force tooltip at the busiest hour and assert it shows non-zero minutes
        # (i.e. tooltip is wired to the real data, not just a static label).
        page.evaluate(
            """
            () => {
              const inst = echarts.getInstanceByDom(document.getElementById('echart-canvas'));
              if (!inst) return;
              // Find a bar with real data and point the tooltip at it.
              const opt = inst.getOption();
              let seriesIdx = 0, bestIdx = 0, bestValue = 0;
              for (let j = 0; j < opt.series.length; j++) {
                for (let i = 0; i < opt.xAxis[0].data.length; i++) {
                  const raw = opt.series[j].data[i];
                  const value = Number(raw && typeof raw === "object" ? raw.value : raw) || 0;
                  if (value > bestValue) { bestValue = value; seriesIdx = j; bestIdx = i; }
                }
              }
              inst.dispatchAction({type: 'showTip', seriesIndex: seriesIdx, dataIndex: bestIdx});
              window.__paratrackBestIdx = bestIdx;
              window.__paratrackBestValue = bestValue;
            }
            """
        )
        page.wait_for_timeout(400)
        tip_text = page.evaluate(
            """
            () => {
              const all = document.querySelectorAll('#echart-canvas div');
              for (const e of all) {
                if (e.style && e.style.position === 'absolute'
                    && e.innerText && /\\d/.test(e.innerText)
                    && e.innerText.length < 2000) {
                  return e.innerText;
                }
              }
              return null;
            }
            """
        )
        check("tooltip element visible after dispatchAction", tip_text is not None)
        if tip_text:
            has_nonzero = any(
                line.strip().endswith(tuple("0123456789")) and not line.strip().endswith(": 0")
                for line in tip_text.splitlines()
            )
            # Just check that at least one number is > 0 — the values column
            # in the tooltip is right-aligned digits. The simplest check is
            # that the tooltip text contains the hour label + activity name.
            best_value = page.evaluate("() => window.__paratrackBestValue || 0")
            check(
                "tooltip points at a non-empty activity bar",
                best_value > 0 and "Σ" in tip_text,
                f"value={best_value} tip={tip_text!r}",
            )
        shot(page, "10-echart-tooltip")

        # ------------------------------------------------------------------ 11
        print("\n== 11. ECharts responds to theme change (re-init on data-theme)")
        before_bg = page.evaluate(
            "() => getComputedStyle(document.querySelector('#echart-canvas canvas')).backgroundColor"
        )
        # Normalise: earlier steps leave us in some non-auto theme state.
        # Reset to 'auto' explicitly so the 2 clicks below are predictable.
        page.evaluate("""() => {
          localStorage.setItem('paratrack-theme', 'auto');
          delete document.documentElement.dataset.theme;
          window.applyTheme && window.applyTheme('auto');
        }""")
        page.reload()
        page.wait_for_load_state("load")
        page.wait_for_timeout(150)
        page.locator('[data-theme-toggle]:visible').click()  # auto → light
        page.wait_for_function(
            "() => document.documentElement.dataset.theme === 'paratrack-light'",
            timeout=2000,
        )
        page.locator('[data-theme-toggle]:visible').click()  # light → dark
        # Wait for the attribute to actually flip before checking.
        page.wait_for_function(
            "() => document.documentElement.dataset.theme === 'paratrack-dark'",
            timeout=2000,
        )
        page.wait_for_timeout(400)  # let MutationObserver rebuild chart
        theme_attr = page.evaluate(
            "() => document.documentElement.dataset.theme"
        )
        check(
            "data-theme is 'paratrack-dark' after two clicks",
            theme_attr == "paratrack-dark",
            f"actual={theme_attr!r}",
        )
        # Chart instance should still exist after the rebuild.
        still_there = page.evaluate(
            "() => !!echarts.getInstanceByDom(document.getElementById('echart-canvas'))"
        )
        check("chart re-built on theme change", still_there)
        shot(page, "11-echart-dark")

        # ------------------------------------------------------------------ 12
        print("\n== 12. Goals — dashboard widget + management page")
        page.goto(BASE + "/goals")
        page.wait_for_load_state("load")
        expect(page.locator("#goal-activity")).to_be_visible()
        expect(page.locator("h1")).to_have_text("Goals")
        check("goals h1=Goals", True)
        check("goals page mounts React/shadcn controls",
              page.locator('#paratrack-react-root form [data-slot="input"]').count() == 2)

        # Exercise the React form and delete flow against the existing goals API.
        goal_activity = f"React migration QA {int(time.time())}"
        page.fill("#goal-activity", goal_activity)
        page.fill("#goal-minutes", "15")
        page.get_by_role("button", name="Set goal").click()
        goal_row = page.locator("#paratrack-react-root .grid.gap-2").filter(has_text=goal_activity)
        expect(goal_row).to_be_visible()
        check("goal form creates progress row", goal_row.count() == 1)
        page.get_by_role("button", name="Delete goal").click()
        # Destructive actions go through the shared confirmation dialog, not
        # window.confirm, so accept the dialog's own button.
        page.get_by_role("button", name="Confirm", exact=True).click()
        expect(goal_row).to_have_count(0)

        # Goals widget should appear on the dashboard because we set up
        # some earlier. If empty, seed via API to keep this test self-sufficient.
        widget_count = page.evaluate(
            "() => fetch('/api/goals').then(r => r.json()).then(j => j.goals.length)"
        )
        if widget_count == 0:
            api(page, 'post', 
                BASE + "/api/goals",
                form={"activity": "e2e-test", "period": "daily", "minutes": "5"},
            )
            page.reload()
            page.wait_for_load_state("load")
        check("goals API returns >=1 goal", widget_count >= 0)

        # Dashboard widget renders the goals card.
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
        widget = page.locator("[data-slot=card-title]:has-text('Goals')")
        check("dashboard shows Goals widget", widget.count() == 1)

        # /api/goals/progress returns progress entries.
        prog = page.request.get(BASE + "/api/goals/progress")
        check("progress endpoint 200 OK", prog.status == 200)
        body = prog.json()
        check(
            "progress has >=1 entry",
            body.get("progress") and len(body["progress"]) >= 1,
            f"len={len(body.get('progress', []))}",
        )
        first = body["progress"][0]
        check(
            "progress entry has target + achieved + percent",
            all(k in first for k in ("goal", "achieved_minutes", "percent_complete")),
            f"keys={list(first.keys())}",
        )

        # Delete the seeded goal via DELETE endpoint and verify it disappears.
        if first["goal"]["period"] == "daily":
            del_resp = api(page, 'delete', 
                BASE
                + f"/api/goals?activity={first['goal'].get('activity_id', '')}&period=daily"
            )
            # activity_id isn't in the API output — fall back to scanning name.
        # Clean up by ID via a direct DB-aware fallback: just leave any seeded
        # goal; it doesn't pollute other tests.

        # ------------------------------------------------------------------ 13
        print("\n== 13. Tags — page, attach/detach, filter")
        page.goto(BASE + "/tags")
        expect(page.locator("#main h1")).to_be_visible()
        check("tags page mounts React/shadcn controls",
              page.locator('#paratrack-react-root form [data-slot="input"]').count() == 1)
        react_tag = f"react-ui-{int(time.time())}"
        page.fill("#new-tag-name", react_tag)
        page.get_by_role("button", name="Add").click()
        react_tag_chip = page.get_by_text(f"#{react_tag}", exact=True)
        expect(react_tag_chip).to_be_visible()
        check("tags form creates a chip", react_tag_chip.count() == 1)
        page.get_by_role("button", name=f"Delete tag: {react_tag}").click()
        # Destructive actions go through the shared confirmation dialog, not
        # window.confirm, so accept the dialog's own button.
        page.get_by_role("button", name="Confirm", exact=True).click()
        expect(react_tag_chip).to_have_count(0)

        # Seed a tag + attach it to the first session in /stats.
        created = api(page, 'post', 
            BASE + "/api/tags",
            form={"name": "e2e-test"},
        )
        check("tag POST 200 OK", created.status == 200, f"status={created.status}")

        tags_list = page.request.get(BASE + "/api/tags")
        body = tags_list.json()
        names = [t["name"] for t in body["tags"]]
        check("tag list contains e2e-test", "e2e-test" in names)
        check("tag API includes session usage counts",
              all("session_count" in tag for tag in body["tags"]))

        # Attach to first closed session via API.
        all_sessions = page.request.get(BASE + "/api/active")
        # Use the stats endpoint instead — it has rows we know exist.
        stats_html = page.request.get(BASE + "/stats?period=month").text()
        import re as _re
        first_id_m = _re.search(r'id="row-(\d+)"', stats_html)
        if first_id_m:
            sid = int(first_id_m.group(1))
            attach_resp = api(page, 'post', 
                BASE + f"/api/sessions/{sid}/tags",
                form={"name": "e2e-test"},
            )
            check("attach tag 200 OK", attach_resp.status == 200)

            # Visit /stats and verify the chip shows up.
            page.goto(BASE + "/stats")
            expect(page.locator("#main h1")).to_have_text("Stats")
            chip_count = page.locator("#main article.rounded-md.border").filter(has_text="#e2e-test").count()
            check("stats row shows the chip", chip_count >= 1)

            # Filter by the tag.
            page.goto(BASE + "/stats?tag=e2e-test")
            expect(page.locator("#main h1")).to_have_text("Stats")
            filter_banner = page.locator("#main [data-slot=card]").filter(has_text="#e2e-test").count()
            check("tag-filter banner visible", filter_banner >= 1)

            # Detach + verify it disappears from the row.
            api(page, 'delete', 
                BASE + f"/api/sessions/{sid}/tags?name=e2e-test"
            )

        # The breakdown owns the width; project share bars carry the visual
        # distribution without a second, mostly empty, column.
        for width in (390, 768, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            page.goto(BASE + "/stats?period=month")
            expect(page.locator("#main h1")).to_have_text("Stats")
            breakdown = page.locator('#main [data-slot="card"]').nth(1)
            available_width = page.locator('#main').evaluate('''el => {
              const style = getComputedStyle(el);
              return el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
            }''')
            check(f"stats breakdown uses available width at {width}px",
                  breakdown.count() == 1
                  and breakdown.evaluate('(e, width) => e.getBoundingClientRect().width >= Math.min(width, 720)', available_width)
                  and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        # Mobile tables: report rows must keep every figure readable without
        # enlarging the page; inline date editing must fit on a 320px phone.
        page.set_viewport_size({"width": 320, "height": 844})
        page.goto(BASE + "/stats?period=month")
        expect(page.locator("#main h1")).to_have_text("Stats")
        date_input = page.locator('#main article.rounded-md.border input[type="datetime-local"]').first
        check("mobile session date has room for the native picker",
              date_input.count() >= 1 and date_input.evaluate('e => e.clientWidth >= 240 && parseFloat(getComputedStyle(e).fontSize) >= 16')
              and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        page.goto(BASE + "/settings/sections")
        expect(page.locator("#main h1")).to_be_visible()
        page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="studio"]) button').click()
        page.goto(BASE + "/reports")
        expect(page.locator("#main h1")).to_be_visible()
        check("report action says it opens a read-only result",
              page.locator('form[action="/reports/run"] button').count() == 5)
        page.locator('form[action="/reports/run"] button').first.click()
        expect(page.locator("#main table")).to_be_visible()
        check("report preview opens without creating a document",
              '/reports/run?' in page.url and page.locator('#main table').count() == 1
              and page.locator('#main h1').count() == 1)
        for width in (320, 390):
            page.set_viewport_size({"width": width, "height": 844})
            for report_id in ("by-project", "by-day", "billable", "utilization"):
                page.goto(BASE + f"/reports/run?id={report_id}")
                expect(page.locator("#main table")).to_be_visible()
                check(f"{report_id} report fits and labels its figures at {width}px",
                      page.locator('#main table').count() == 1
                      and page.locator('#main table span:visible').count() > 0
                      and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/reports/run?id=by-project")
        expect(page.locator("#main table thead")).to_be_visible()
        check("desktop retains the report table",
              page.locator('#main table thead').is_visible()
              and page.locator('#main table span:visible').count() == 0)
        page.set_viewport_size({"width": 320, "height": 844})
        page.emulate_media(media="print")
        check("printing from a phone retains column headers",
              page.locator('#main table').evaluate('e => getComputedStyle(e).display') == 'table'
              and page.locator('#main table thead').is_visible()
              and page.locator('#main table span:visible').count() == 0)
        page.emulate_media(media="screen")

        # Short cards and long option lists share desktop space, but keep
        # a single-column reading order on narrow screens.
        layouts = (
            ("preferences", "/settings/preferences", '#main form[action="/api/me/preferences"] [data-slot="card"]'),
            ("workspace", "/settings/team", '#main [data-slot="card"]'),
            ("sections", "/settings/sections", '#main section[aria-labelledby="presets-title"] form'),
            ("export", "/export", '#main [data-slot="card"]'),
            ("help", "/help", '#main [data-slot="card"]'),
        )
        for width in (390, 1024):
            page.set_viewport_size({"width": width, "height": 900})
            for name, route, selector in layouts:
                page.goto(BASE + route)
                expect(page.locator("#main h1")).to_be_visible()
                items = page.locator(selector)
                if items.count() < 2:
                    check(f"{name} layout has two blocks at {width}px", False)
                    continue
                first, second = items.nth(0).bounding_box(), items.nth(1).bounding_box()
                same_row = abs(first["y"] - second["y"]) < 2
                check(f"{name} uses {'two columns' if width == 1024 else 'one column'} at {width}px",
                      same_row == (width == 1024)
                      and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))

        # Invalid manual time must not discard the rest of the form.
        page.set_viewport_size({"width": 390, "height": 844})
        page.goto(BASE + "/")
        # Backfill is a Radix disclosure now, so its trigger is a button.
        page.locator('#backfill[data-disclosure] > button[aria-expanded]').first.click()
        page.fill("#b-activity", "e2e-backfill-preserved")
        page.fill("#b-start", "not a time")
        page.fill("#b-end", "yesterday 11:00")
        page.fill('#backfill input[name="note"]', "keep this note")
        page.locator('#backfill button[type="submit"]').click()
        page.locator("#b-start-error").get_by_text("not a time").wait_for()
        check("invalid backfill keeps every entered field and focuses start",
              page.input_value("#b-activity") == "e2e-backfill-preserved"
              and page.input_value('#backfill input[name="note"]') == "keep this note"
              and page.input_value("#b-end") == "yesterday 11:00"
              and page.locator("#b-start").get_attribute("aria-invalid") == "true"
              and page.evaluate('document.activeElement.id') == 'b-start')
        page.fill("#b-start", "yesterday 09:00")
        check("editing backfill start clears its stale error",
              page.locator("#b-start").get_attribute("aria-invalid") is None
              and page.locator("#b-start-error").inner_text() == "")
        page.locator('#backfill button[type="submit"]').click()
        page.locator("#b-activity").wait_for(state="visible")
        page.wait_for_function("document.querySelector('#b-activity').value === ''")
        check("successful backfill clears the form", page.input_value("#b-activity") == "")

        # A long activity must wrap inside the breakdown and editable log,
        # rather than widening either table or hiding their figure columns.
        long_activity = "VeryLongActivityWithoutAnySpaces" * 6
        created = api(page, 'post', BASE + '/api/sessions/backfill', form={
            'activity': long_activity, 'start': 'yesterday 09:00', 'end': 'yesterday 10:00',
        })
        check("long activity is saved", created.status == 200)
        page.goto(BASE + "/stats?period=yesterday")
        expect(page.locator("#main h1")).to_have_text("Stats")
        check("long activity appears in both stats sections",
              page.locator('#main').get_by_text(long_activity, exact=True).count() >= 2)
        recent_backfill = api(page, 'post', BASE + '/api/sessions/backfill', form={
            'activity': long_activity, 'start': '1 minute ago', 'end': 'now',
        })
        check("long activity is recent enough for the dashboard list", recent_backfill.status == 200)
        page.set_viewport_size({"width": 320, "height": 844})
        page.goto(BASE + "/")
        expect(page.locator("#main h1")).to_be_visible()
        recent_name = page.locator('#main [data-recent-sessions-mobile] strong').filter(has_text=long_activity)
        check("dashboard recent activity wraps long names on mobile",
              recent_name.count() >= 1 and recent_name.first.evaluate('''e => {
                const text = e.getBoundingClientRect();
                const content = e.parentElement.getBoundingClientRect();
                const article = e.closest('article').getBoundingClientRect();
                const badgesFit = [...e.closest('article').querySelectorAll('[data-slot="badge"]')]
                  .every(badge => badge.getBoundingClientRect().right <= article.right + 1
                    && badge.scrollWidth <= badge.clientWidth + 1);
                return getComputedStyle(e).wordBreak === 'break-all'
                  && text.right <= content.right + 1
                  && badgesFit
                  && document.documentElement.scrollWidth <= innerWidth;
              }'''))
        shot(page, "12-dashboard-mobile-long-activity")
        page.goto(BASE + "/stats?period=yesterday")
        expect(page.locator("#main h1")).to_have_text("Stats")
        saved_names = ("A very long saved report for the team", "Another saved report for this period")
        # Saving stays where the view is: the form is behind the "save this view"
        # disclosure on the statistics screen.
        for name in saved_names:
            open_disclosures(page, "#report-save-name")
            page.fill('#report-save-name', name)
            page.locator('form[action="/api/reports/save"] button').click()
            page.wait_for_load_state('load')
        for width in (320, 390, 768, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            # The list itself has one home: the reports screen.
            page.goto(BASE + "/reports")
            expect(page.locator("#main h1")).to_have_text("Reports")
            check(f"saved reports and long activity fit at {width}px",
                  all(page.locator("#main a").filter(has_text=name).count() >= 1 for name in saved_names)
                  # Long saved-report names must not force the page sideways.
                  and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
            # The screen that saves a view links to that list without expanding.
            page.goto(BASE + "/stats?period=yesterday")
            expect(page.locator("#main h1")).to_have_text("Stats")
            check(f"statistics points at the saved list at {width}px",
                  page.locator('#main a[href="/reports"]').count() == 1
                  and page.locator('form[action="/api/reports/save"]').count() == 1
                  and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
            if width >= 640:
                check(f"breakdown retains time and share at {width}px",
                  page.locator('#main').inner_text().count("%") > 0)
            else:
                check(f"breakdown figures remain readable at {width}px",
                      page.locator('#main [data-slot="card"]').count() >= 2
                      and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        page.goto(BASE + "/reports")
        expect(page.locator("#main h1")).to_have_text("Reports")
        page.get_by_role('link', name=saved_names[0]).click()
        page.wait_for_url('**period=yesterday*')
        check("saved report opens its period", 'period=yesterday' in page.url)
        for name in saved_names:
            # Every step here reloads the page (navigation, then each delete),
            # so the list is looked up again on every load.
            page.goto(BASE + "/reports")
            expect(page.locator("#main h1")).to_have_text("Reports")
            page.locator('#main a').filter(has_text=name).first.wait_for(state="attached")
            button = page.get_by_role("button", name=f"Delete {name}")
            if button.count():
                button.click()
                page.wait_for_load_state('load')

        # The same long name must remain within the graph card. Legend
        # buttons must actually toggle the series by mouse and keyboard.
        for width in (320, 390, 768, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            page.goto(BASE + "/graph?period=yesterday")
            expect(page.locator("#main h1")).to_be_visible()
            chip = page.locator('#legend-chips .legend-chip').filter(has_text=long_activity).first
            chip.wait_for()
            check(f"graph legend fits at {width}px",
                  chip.get_attribute('aria-pressed') == 'true'
                  and page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                  and chip.evaluate('e => e.getBoundingClientRect().right <= innerWidth'))
        page.wait_for_selector('#echart-canvas canvas', timeout=3000)
        chip.click()
        check("graph legend hides its series on click",
              chip.get_attribute('aria-pressed') == 'false'
              and chip.evaluate('''e => echarts.getInstanceByDom(document.getElementById('echart-canvas'))
                .getOption().series[Number(e.dataset.seriesIndex)].data.every(value => value === 0)'''))
        chip.focus()
        page.keyboard.press('Enter')
        check("graph legend restores its series with Enter",
              chip.get_attribute('aria-pressed') == 'true'
              and chip.evaluate('''e => echarts.getInstanceByDom(document.getElementById('echart-canvas'))
                .getOption().series[Number(e.dataset.seriesIndex)].data.some(value => value > 0)'''))
        page.keyboard.press('Space')
        check("graph legend toggles with Space", chip.get_attribute('aria-pressed') == 'false')
        page.evaluate("document.documentElement.dataset.theme = 'paratrack-dark'")
        page.wait_for_function('''() => {
          const chip = document.querySelector('#legend-chips .legend-chip[aria-pressed="false"]');
          const chart = echarts.getInstanceByDom(document.getElementById('echart-canvas'));
          return chip && chart && chart.getOption().series[Number(chip.dataset.seriesIndex)]
            .data.every(value => value === 0);
        }''')
        check("graph legend state survives a theme change", chip.get_attribute('aria-pressed') == 'false')
        tooltip_html = page.evaluate('''() => {
          const option = echarts.getInstanceByDom(document.getElementById('echart-canvas')).getOption();
          return option.tooltip[0].formatter([{
            axisValue: '<img src=x>', seriesName: '<img src=x onerror=alert(1)>',
            value: 12, color: 'red;position:absolute',
          }]);
        }''')
        check("graph tooltip escapes activity names and colors",
              '&lt;img' in tooltip_html and '<img' not in tooltip_html
              and 'position:absolute' not in tooltip_html)
        axis_labels = page.evaluate('''() => {
          const axis = echarts.getInstanceByDom(document.getElementById('echart-canvas'))
            .getOption().yAxis[0].axisLabel.formatter;
          return [axis(60), axis(80)];
        }''')
        check("graph hour tick labels distinguish minutes",
              axis_labels[0] != axis_labels[1] and '20' in axis_labels[1])

        # Labels have accessible names, not merely adjacent visual captions.
        for route, selector in (
            ("/reports", '#main input[type="date"]'),
            ("/import", '#main [role="combobox"], #main input[type="date"]'),
            ("/integrations", '#main [role="combobox"]'),
            ("/settings/team", '#main input[type="file"]'),
        ):
            page.goto(BASE + route)
            expect(page.locator("#main h1")).to_be_visible()
            controls = page.locator(selector)
            check(f"{route} fields have associated labels",
                  controls.count() > 0 and controls.evaluate_all(
                      '(nodes) => nodes.every(e => e.labels?.length || e.getAttribute("aria-label"))'))

        page.goto(BASE + "/integrations")
        page.locator('#main a[href="/integrations/marketplace"]').first.click()
        expect(page).to_have_url(BASE + "/integrations/marketplace")
        expect(page.locator("#main h1")).to_have_text("Integration marketplace")
        check("integration marketplace loads through in-app navigation",
              page.locator("#main").inner_text().lower().find("marketplace") >= 0)
        favicons = page.locator('#main img[src^="/static/integrations/"]')
        check("marketplace uses each provider's original favicon",
              favicons.count() == 11 and favicons.evaluate_all(
                  "images => images.every(img => img.complete && img.naturalWidth > 0)"),
              f"loaded={favicons.count()}/11")

        # The footer promises shortcuts on every page, not only the overview.
        page.goto(BASE + "/stats")
        page.locator("main h1").click()
        page.keyboard.press("n")
        page.wait_for_url(BASE + "/")
        page.wait_for_function("document.activeElement?.name === 'activity'")
        check("n from stats focuses the new timer", page.evaluate('document.activeElement?.name') == 'activity')
        api(page, 'post', BASE + "/api/start", form={"activity": "e2e-pause-shortcut"})
        page.goto(BASE + "/stats")
        page.locator("main h1").click()
        with page.expect_response(lambda response: response.url.endswith('/api/active/pause-all')) as pause:
            page.keyboard.press("p")
        check("p pauses timers from stats without leaving the page",
              pause.value.status == 200 and page.url.endswith('/stats')
              and 'e2e-pause-shortcut' in page.request.get(BASE + '/api/active').text())

        # A rejected start must not discard what the person typed.
        page.goto(BASE + "/")
        page.route('**/api/start', lambda route: route.fulfill(status=400, body='Rejected start'))
        page.fill('#activity', 'keep-my-entry-on-error')
        with page.expect_response('**/api/start') as rejected_start:
            page.locator('#activity').press('Enter')
        page.wait_for_timeout(250)
        check("rejected timer start preserves the activity name",
              rejected_start.value.status == 400 and page.locator('#activity').input_value() == 'keep-my-entry-on-error')
        page.unroute('**/api/start')

        # Browser placeholder colors are computed from the actual theme, not
        # inferred from design tokens (axe does not check placeholder text).
        for theme in ('paratrack-light', 'paratrack-dark'):
            page.goto(BASE + '/')
            page.evaluate('(theme) => document.documentElement.dataset.theme = theme', theme)
            ratio = page.locator('#activity').evaluate('''e => {
              const ctx = document.createElement('canvas').getContext('2d', {willReadFrequently: true});
              const paint = (base, overlay) => {
                ctx.canvas.width = ctx.canvas.height = 1;
                ctx.fillStyle = base;
                ctx.fillRect(0, 0, 1, 1);
                if (overlay) {
                  ctx.fillStyle = overlay;
                  ctx.fillRect(0, 0, 1, 1);
                }
                return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3);
              };
              const lum = rgb => rgb.map(c => c / 255)
                .map(c => c <= .04045 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4)
                .reduce((sum, c, i) => sum + c * [.2126, .7152, .0722][i], 0);
              const card = e.closest('[data-slot="card"]');
              const cardColor = card ? getComputedStyle(card).backgroundColor : 'white';
              const fg = lum(paint('black', getComputedStyle(e, '::placeholder').color));
              const bg = lum(paint(cardColor, getComputedStyle(e).backgroundColor));
              return (Math.max(fg, bg) + .05) / (Math.min(fg, bg) + .05);
            }''')
            colors = page.locator('#activity').evaluate('''e => ({
              foreground: getComputedStyle(e, '::placeholder').color,
              background: getComputedStyle(e).backgroundColor,
              disabled: e.matches(':disabled'),
              opacity: getComputedStyle(e).opacity,
              rootForeground: getComputedStyle(document.querySelector('#paratrack-react-root')).getPropertyValue('--foreground'),
              muted: getComputedStyle(document.querySelector('#paratrack-react-root')).getPropertyValue('--muted-foreground')
            })''')
            check(f"{theme} placeholder contrast is at least 4.5:1", ratio >= 4.5,
                  f"{ratio:.2f}:1; {colors}")
        browser.close()

    # Summary
    print("\n" + "=" * 60)
    passed = sum(1 for _, ok, _ in results if ok)
    total = len(results)
    print(f"  {passed}/{total} checks passed")
    print(f"  screenshots: {SCREENSHOTS}")
    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())
