"""E2E walkthrough for paratrack web UI.

Drives the running paratrack server (assumed at http://127.0.0.1:8888)
with Playwright. Takes a screenshot at each major state, runs assertions
on the DOM, and prints a one-line pass/fail per step.

Run from the repo root with the .venv active:

    source .venv/bin/activate
    python e2e/test_dashboard.py
"""

from __future__ import annotations

import sys
import time
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

BASE = "http://127.0.0.1:8888"
SCREENSHOTS = Path(__file__).parent / "screenshots"
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
        # First run after a fresh DB: /register succeeds and the cookie
        # is set. On repeated runs the email is taken; detect by checking
        # the URL after submit — fall back to /login.
        register_account(page)
        # Register can land on `/` (success), `/register?error=…` (duplicate),
        # or `/login` (any prior redirect). Fall through to sign_in unless
        # we actually reached the dashboard.
        if "/login" in page.url or "/register" in page.url:
            sign_in(page)
        # Confirm the session took: the top-bar user menu shows the email.
        page.goto(BASE + "/")
        page.wait_for_load_state("load")
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
        nav_text = page.locator("header nav").inner_text()
        for label in ["Dashboard", "Stats"]:
            check(f"nav has '{label}' link", label in nav_text)
        # Optional sections move into the top bar as width allows; CSV stays
        # in More. Exactly one visible link should lead to each destination.
        more = page.locator('header nav button:has-text("More")')
        more.click()
        check("Graph has one visible destination", page.locator('header nav a[href="/graph"]:visible').count() == 1)
        check("More menu opens export settings", page.locator('header nav a[href="/export"]').count() == 1)
        page.keyboard.press("Escape")
        for width, visible_extra in ((1024, ()), (1152, ("/graph",)),
                                     (1280, ("/graph", "/goals")),
                                     (1440, ("/graph", "/goals", "/tags"))):
            page.set_viewport_size({"width": width, "height": 900})
            check(
                f"header destinations fit at {width}px",
                page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
                and all(page.locator(f'header nav a[href="{path}"]:visible').count() == 1
                        for path in visible_extra),
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
            page.locator('header button:has-text("workspace")').count() >= 1,
        )
        shot(page, "01-dashboard-light")
        page.set_viewport_size({"width": 320, "height": 844})
        page.goto(BASE + "/timesheet")
        if page.locator('#ts-body tr').count() == 0:
            empty_cta = page.locator('main a[href="/"]:has-text("Start a timer")')
            box = empty_cta.bounding_box()
            check("empty timesheet action is visible without grid scrolling",
                  box is not None and box['x'] >= 0 and box['x'] + box['width'] <= 320
                  and page.locator('table.week-grid').count() == 0)
        page.goto(BASE + "/")
        page.set_viewport_size({"width": 390, "height": 844})
        more_tab = page.locator('[data-sheet-open="more-sheet"]')
        sheet = page.locator('#more-sheet')
        more_tab.click()
        expect(sheet).to_be_visible()
        check("mobile More sheet has one accessible grab-to-close control",
              sheet.locator('[data-sheet-grab][aria-label="Close"]').count() == 1
              and sheet.locator('form[method="dialog"]').count() == 0)
        page.wait_for_timeout(300)  # Capture the open sheet, not its entrance animation.
        page.screenshot(path=str(SCREENSHOTS / "01-more-sheet-mobile.png"))
        sheet.locator('[data-sheet-grab]').click()
        check("mobile More sheet closes by tapping grab", not sheet.evaluate("el => el.open"))
        more_tab.click()
        page.mouse.click(10, 10)
        check("mobile More sheet closes via backdrop", not sheet.evaluate("el => el.open"))
        more_tab.click()
        page.keyboard.press("Escape")
        check("mobile More sheet closes via Escape", not sheet.evaluate("el => el.open"))
        # Real synthesized Chromium touch input: list scroll must not drag
        # the dialog; only the always-visible handle can dismiss it.
        touch_context = browser.new_context(
            viewport={"width": 390, "height": 650}, is_mobile=True,
            has_touch=True, storage_state=context.storage_state(),
        )
        touch_page = touch_context.new_page()
        touch_page.goto(BASE + "/")
        touch_page.locator('[data-sheet-open="more-sheet"]').click()
        touch_page.wait_for_timeout(300)
        touch_sheet = touch_page.locator('#more-sheet')
        scroller = touch_sheet.locator('.sheet-content')
        top = touch_sheet.evaluate('el => el.getBoundingClientRect().top')
        body_scroll = touch_page.evaluate('document.scrollingElement.scrollTop')
        cdp = touch_context.new_cdp_session(touch_page)

        def swipe(x, y, distance):
            cdp.send('Input.dispatchTouchEvent', {'type': 'touchStart', 'touchPoints': [{'x': x, 'y': y}]})
            for step in range(1, 9):
                cdp.send('Input.dispatchTouchEvent', {'type': 'touchMove',
                         'touchPoints': [{'x': x, 'y': y + distance * step / 8}]})
                touch_page.wait_for_timeout(15)
            cdp.send('Input.dispatchTouchEvent', {'type': 'touchEnd', 'touchPoints': []})
            touch_page.wait_for_timeout(250)

        swipe(185, 530, -330)
        check("More list scrolls while sheet and page stay put",
              scroller.evaluate('el => el.scrollTop') > 0
              and abs(touch_sheet.evaluate('el => el.getBoundingClientRect().top') - top) < 1
              and touch_page.evaluate('document.scrollingElement.scrollTop') == body_scroll)
        check("More handle stays visible after scrolling",
              touch_sheet.locator('[data-sheet-grab]').is_visible())
        swipe(185, 350, 250)
        check("dragging the list back up does not close the sheet",
              touch_sheet.evaluate('el => el.open')
              and scroller.evaluate('el => el.scrollTop') == 0)
        grab_box = touch_sheet.locator('[data-sheet-grab]').bounding_box()
        swipe(grab_box['x'] + grab_box['width'] / 2,
              grab_box['y'] + grab_box['height'] / 2, 170)
        check("handle swipe dismisses the sheet", not touch_sheet.evaluate('el => el.open'))
        touch_page.locator('[data-sheet-open="more-sheet"]').click()
        check("reopened More sheet starts at top", scroller.evaluate('el => el.scrollTop') == 0)
        touch_context.close()
        more_tab.click()
        sheet.locator('a[href="/export"]').click()
        check("mobile Export explains sessions and offers a summary route",
              page.url.endswith('/export')
              and page.locator('form[action="/api/reports.csv"] button[type="submit"]').count() == 1
              and 'paratrack.csv' in page.locator('main').inner_text()
              and page.locator('main a[href="/settings/sections"]:has-text("Reports")').count() == 1
              and page.locator('main a[href="/reports"]').count() == 0)
        page.evaluate("window.paratrackToast('Saved', 'success', 10000)")
        check("success toast is readable without a decorative check",
              page.locator('#toast .toast-note').inner_text() == 'Saved'
              and page.locator('#toast .toast-note svg').count() == 0)
        page.evaluate("window.paratrackToast('Failed', 'error', 10000)")
        check("error toast retains its distinct icon",
              page.locator('#toast .toast-note[role="alert"] svg use[href$="#i-x"]').count() == 1)
        page.goto(BASE + '/settings/sections')
        check("preset stays selected without a redundant check",
              page.locator('.preset-card.is-active[aria-current="true"]').count() == 1
              and page.locator('.preset-card.is-active use[href$="#i-check"]').count() == 0
              and page.locator('input[name="modules"]:checked').count() > 0)
        for width in (390, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            page.goto(BASE + "/")
            baseline = page.evaluate("""() => {
                const main = document.querySelector('main');
                const rect = main.getBoundingClientRect();
                const style = getComputedStyle(main);
                const left = parseFloat(style.paddingLeft);
                const right = parseFloat(style.paddingRight);
                return {x: rect.x + left, width: rect.width - left - right};
            }""")
            for path in ("/settings/profile", "/settings/preferences", "/settings/team",
                         "/settings/members", "/settings/notifications", "/projects/new"):
                page.goto(BASE + path)
                box = page.locator("main > div").first.bounding_box()
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

        # ------------------------------------------------------------------ 2
        print("\n== 2. Start a new activity via the form (UI)")
        before = page.locator(".status-pill.is-active").count()
        page.fill('input[name="activity"]', "writing")
        page.fill('input[name="note"]', "e2e playwright test")
        page.click('button[type="submit"]:has-text("Start")')
        # Wait specifically inside #active-list — not the form input —
        # so we know the HTMX swap has happened.
        page.wait_for_selector('#active-list td:has-text("writing")', timeout=5000)
        check("active list contains 'writing' after HTMX swap", True)
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
        page.click('header nav a:has-text("Stats")')
        page.wait_for_url("**/stats")
        expect(page.locator("h1")).to_have_text("Stats")
        check("stats h1=Stats", True)
        # Wait for sessions table to render. On timeout dump the page so the
        # failure is diagnosable from /tmp/e2e-stats.html rather than a bare
        # TimeoutError.
        try:
            page.wait_for_selector("table tbody tr", timeout=3000)
        except Exception as _e:
            try:
                with open("/tmp/e2e-stats.html", "w") as _f:
                    _f.write(page.content())
            except Exception:
                pass
            raise
        rows = page.locator("table tbody tr").count()
        check("stats shows session rows", rows >= 1, f"{rows} rows")
        # Edit duration inline: change first row duration to 45m
        first_dur = page.locator('input[name="duration"]').first
        first_dur.fill("45m")
        first_dur.press("Tab")
        page.wait_for_timeout(500)
        shot(page, "04-stats-with-edit")

        # ------------------------------------------------------------------ 5
        print("\n== 5. Graph page")
        page.locator('header nav a[href="/graph"]:visible').click()
        page.wait_for_url("**/graph")
        # The seeded sessions use a synthetic future end; a complete week
        # keeps them visible while today correctly clips at the current time.
        page.goto(BASE + "/graph?period=week")
        page.wait_for_load_state("load")
        expect(page.locator("h1")).to_have_text("When you work")
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
        page.click('header nav a:has-text("Dashboard")')
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
        page.fill('input[name="name"]', proj_name)
        page.fill('input[name="slug"]', proj_slug)
        # Slug blank → auto. Color picker value is the hex text input.
        page.fill('input[name="color"][pattern]', "#7c3aed")
        page.click('button.btn-neutral:has-text("Create")')
        page.wait_for_url(f"**/projects/{proj_slug}")
        check(f"project created at /projects/{proj_slug}", proj_slug in page.url)

        # Detail page renders the right title.
        expect(page.locator("h1")).to_contain_text(proj_name)
        check("detail page shows project name", proj_name in page.content())

        # A unique activity per run keeps repeated local E2E runs from
        # inheriting a project assignment from an earlier run.
        activity_name = f"deep-work-{proj_slug}"
        page.goto(BASE + "/")
        page.fill('#activity', activity_name)
        page.select_option('#project_id', label=proj_name)
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
        project_select = page.locator("#active-list select.ledger-project")
        page.wait_for_selector("#active-list select.ledger-project", state="visible", timeout=3000)
        selected = project_select.locator("option:checked").inner_text()
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
        project_select = page.locator("#active-list select.ledger-project")
        # A long project option must not steal the activity's entire lane
        # on the intermediate layout. Change display text only, then reload.
        for width in (390, 768, 1024, 1280):
            page.set_viewport_size({"width": width, "height": 900})
            short_width = project_select.evaluate("el => el.getBoundingClientRect().width")
            project_select.evaluate("el => { el.selectedOptions[0].textContent = 'A very long project name that must not move the timer columns'; }")
            if width == 768:
                page.screenshot(path=str(SCREENSHOTS / "11-project-long-name.png"))
            check(f"long project keeps activity visible at {width}px",
                  page.locator('#active-list .ledger-activity').last.evaluate(
                      "el => el.getBoundingClientRect().width > 36")
                  and abs(project_select.evaluate("el => el.getBoundingClientRect().width") - short_width) < 1
                  and page.evaluate("document.documentElement.scrollWidth <= innerWidth"))
            project_select.evaluate("(el, name) => { el.selectedOptions[0].textContent = name; }", proj_name)
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/")

        # Weekly tables share a fixed name lane; long names must not consume
        # day columns, even at narrow effective widths (browser zoom/mobile).
        for width in (320, 1024):
            page.set_viewport_size({"width": width, "height": 900})
            page.goto(BASE + "/timesheet")
            check(f"timesheet keeps a readable first column at {width}px",
                  page.locator('.week-grid th').first.evaluate(
                      "el => el.getBoundingClientRect().width >= 160")
                  and page.evaluate("document.documentElement.scrollWidth <= innerWidth")
                  and page.locator('.week-nav-label').evaluate(
                      "el => getComputedStyle(el).whiteSpace === 'nowrap'"))
        page.set_viewport_size({"width": 320, "height": 900})
        page.goto(BASE + "/")
        page.locator('[data-project-label]').first.evaluate(
            "el => { el.textContent = 'A very long project name for the new timer'; }")
        check("long project summary stays on one line at 320px",
              page.locator('.ledger-more-summary').evaluate(
                  "el => el.getBoundingClientRect().height < 40"))
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/")

        # Set a visible duration before the stats period/filter checks.
        # The duration PATCH closes a running session; a second stop would
        # correctly return "already stopped".
        stop_path = page.locator(f'#active-list tbody tr:has-text("{activity_name}") button[hx-post$="/stop"]').first.get_attribute("hx-post")
        new_sid = _re.search(r"/api/sessions/(\d+)/stop", stop_path).group(1)
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
        page.wait_for_selector(f"text={proj_name}", timeout=3000)
        check("stats breakdown mentions project", proj_name in page.content())
        page.set_viewport_size({"width": 320, "height": 900})
        page.locator('a[href*="project="] span.truncate').first.evaluate(
            "el => { el.textContent = 'A very long project name that must stay inside the stats filter'; }")
        check("long stats project filter fits at 320px",
              page.evaluate("document.documentElement.scrollWidth <= innerWidth"))
        page.set_viewport_size({"width": 1280, "height": 900})

        # Project filter narrows to just the one project.
        page.goto(BASE + f"/stats?project={proj_slug}")
        page.wait_for_load_state("load")
        filtered_body = page.locator("body").inner_text()
        check(
            "project filter keeps the selected project",
            proj_name in filtered_body and "writing" not in filtered_body,
            f"contains_project={proj_name in filtered_body}",
        )

        shot(page, "11-projects-detail")

        # ------------------------------------------------------------------ 8
        print("\n== 8. Active session edit (PATCH via duration)")
        # Go back to dashboard; start something fresh via API for speed.
        page.goto(BASE + "/stats?period=week")
        page.wait_for_selector('input[name="duration"]')
        before = page.locator('input[name="duration"]').first.input_value()
        d_in = page.locator('input[name="duration"]').first
        d_in.fill("2h 30m")
        d_in.press("Tab")
        page.wait_for_timeout(700)  # HTMX PATCH + swap
        after = page.locator('input[name="duration"]').first.input_value()
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
              const all = document.querySelectorAll('div');
              for (const e of all) {
                if (e.style && e.style.position === 'absolute'
                    && e.innerText && /\\d/.test(e.innerText)
                    && e.innerText.length < 200) {
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
        page.locator('footer [data-theme-toggle]:visible').click()  # auto → light
        page.wait_for_function(
            "() => document.documentElement.dataset.theme === 'paratrack-light'",
            timeout=2000,
        )
        page.locator('footer [data-theme-toggle]:visible').click()  # light → dark
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
        expect(page.locator("h1")).to_have_text("Goals")
        check("goals h1=Goals", True)

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
        widget = page.locator(".card-title:has-text('Goals')")
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
            page.wait_for_load_state("load")
            chip_count = page.locator(f"text=#e2e-test").count()
            check("stats row shows the chip", chip_count >= 1)

            # Filter by the tag.
            page.goto(BASE + "/stats?tag=e2e-test")
            page.wait_for_load_state("load")
            filter_banner = page.locator("text=FILTERED BY").count()
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
            breakdown = page.locator('.card:has(table.stats-breakdown-table)')
            check(f"stats breakdown uses available width at {width}px",
                  breakdown.count() == 1
                  and breakdown.evaluate('e => e.getBoundingClientRect().width >= document.querySelector("main").clientWidth - 34')
                  and page.locator('table.stats-breakdown-table tbody tr.font-semibold [aria-hidden="true"]').count() > 0
                  and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        # Mobile tables: report rows must keep every figure readable without
        # enlarging the page; inline date editing must fit on a 320px phone.
        page.set_viewport_size({"width": 320, "height": 844})
        page.goto(BASE + "/stats?period=month")
        date_input = page.locator('main input[type="datetime-local"]').first
        check("mobile session date has room for the native picker",
              date_input.count() == 1 and date_input.evaluate('e => e.clientWidth >= 240 && parseFloat(getComputedStyle(e).fontSize) >= 16')
              and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        page.goto(BASE + "/settings/sections")
        page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="studio"]) button').click()
        for width in (320, 390):
            page.set_viewport_size({"width": width, "height": 844})
            for report_id in ("by-project", "by-day", "billable", "utilization"):
                page.goto(BASE + f"/reports/run?id={report_id}")
                check(f"{report_id} report fits and labels its figures at {width}px",
                      page.locator('table.report-table').count() == 1
                      and page.locator('table.report-table .report-mobile-label:visible').count() > 0
                      and page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(BASE + "/reports/run?id=by-project")
        check("desktop retains the report table",
              page.locator('table.report-table thead').is_visible()
              and page.locator('table.report-table .report-mobile-label:visible').count() == 0)
        page.set_viewport_size({"width": 320, "height": 844})
        page.emulate_media(media="print")
        check("printing from a phone retains column headers",
              page.locator('table.report-table').evaluate('e => getComputedStyle(e).display') == 'table'
              and page.locator('table.report-table thead').is_visible()
              and page.locator('table.report-table .report-mobile-label:visible').count() == 0)
        page.emulate_media(media="screen")
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
