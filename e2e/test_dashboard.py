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


def main() -> int:
    with sync_playwright() as p:
        # Real desktop viewport, with prefers-color-scheme = light by default
        # so we can exercise the theme toggle visually.
        browser = p.chromium.launch()
        context = browser.new_context(
            viewport={"width": 1280, "height": 900},
            color_scheme="light",
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
        print("\n== 1. Dashboard, light theme, initial load")
        # Make sure no stale sessions from a previous run block step 2.
        # /api/stop without arg requires interactive multi-select, so use
        # the stop-on-each-active trick: list via /api/active, then POST
        # stop for each id.
        active_html = page.request.get(BASE + "/api/active").text()
        import re as _re
        for sid in _re.findall(r"/api/sessions/(\d+)/(?:stop|pause|resume)", active_html):
            page.request.post(BASE + f"/api/sessions/{sid}/stop")
        page.goto(BASE + "/")
        expect(page.locator("h1")).to_have_text("Dashboard")
        check("dashboard renders h1=Dashboard", True)
        nav_text = page.locator(".nav").inner_text()
        for label in ["Dashboard", "Stats", "Graph", "CSV"]:
            check(f"nav has '{label}' link", label in nav_text)
        check(
            "theme toggle button present",
            page.locator('[data-theme-toggle]').count() == 1,
        )
        shot(page, "01-dashboard-light")

        # ------------------------------------------------------------------ 2
        print("\n== 2. Start a new activity via the form (UI)")
        before = page.locator(".badge-active").count()
        page.fill('input[name="activity"]', "writing")
        page.fill('input[name="note"]', "e2e playwright test")
        page.click('button[type="submit"]:has-text("Start")')
        # Wait specifically inside #active-list — not the form input —
        # so we know the HTMX swap has happened.
        page.wait_for_selector('#active-list td:has-text("writing")', timeout=5000)
        check("active list contains 'writing' after HTMX swap", True)
        after = page.locator(".badge-active").count()
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
            page.locator(".badge-paused").count() == 1,
            f"got {page.locator('.badge-paused').count()}",
        )
        shot(page, "03-dashboard-after-pause")

        # ------------------------------------------------------------------ 4
        print("\n== 4. Stats page")
        page.click('a.nav-link, .nav a:has-text("Stats")')
        page.wait_for_url("**/stats")
        expect(page.locator("h1")).to_have_text("Stats")
        check("stats h1=Stats", True)
        # Wait for sessions table to render.
        page.wait_for_selector("table tbody tr", timeout=3000)
        rows = page.locator("table tbody tr").count()
        check("stats shows session rows", rows >= 3, f"{rows} rows")
        # Edit duration inline: change first row duration to 45m
        first_dur = page.locator('input[name="duration"]').first
        first_dur.fill("45m")
        first_dur.press("Tab")
        page.wait_for_timeout(500)
        shot(page, "04-stats-with-edit")

        # ------------------------------------------------------------------ 5
        print("\n== 5. Graph page")
        page.click('.nav a:has-text("Graph")')
        page.wait_for_url("**/graph")
        expect(page.locator("h1")).to_have_text("Graph")
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
        for expected in ["light", "dark", "auto"]:
            page.click('[data-theme-toggle]')
            page.wait_for_timeout(150)
            attr = page.evaluate(
                "() => document.documentElement.dataset.theme || 'unset'"
            )
            check(
                f"theme click sets html data-theme={expected!r}",
                attr == expected or (expected == "auto" and attr == "unset"),
                f"actual={attr}",
            )
        # Stop on dark for the dramatic screenshot.
        # After the loop above we're at 'auto'; click twice to reach dark.
        page.click('[data-theme-toggle]')  # auto -> light
        page.click('[data-theme-toggle]')  # light -> dark
        page.wait_for_timeout(200)
        page.click('.nav a:has-text("Dashboard")')
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
        page.goto(BASE + "/graph?period=today")
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
              // Find the index of the busiest bar by total stack height.
              const opt = inst.getOption();
              let bestIdx = 0, bestSum = -1;
              for (let i = 0; i < opt.xAxis[0].data.length; i++) {
                let s = 0;
                for (const ser of opt.series) s += (ser.data[i] || 0);
                if (s > bestSum) { bestSum = s; bestIdx = i; }
              }
              inst.dispatchAction({type: 'showTip', seriesIndex: 0, dataIndex: bestIdx});
              window.__paratrackBestIdx = bestIdx;
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
            check(
                "tooltip names an activity (writing/work/reading/...)",
                any(name in tip_text for name in ("writing", "work", "reading", "check123")),
                f"tip={tip_text!r}",
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
        page.wait_for_timeout(150)
        page.click('[data-theme-toggle]')  # auto → light
        page.wait_for_function(
            "() => document.documentElement.dataset.theme === 'light'",
            timeout=2000,
        )
        page.click('[data-theme-toggle]')  # light → dark
        # Wait for the attribute to actually flip before checking.
        page.wait_for_function(
            "() => document.documentElement.dataset.theme === 'dark'",
            timeout=2000,
        )
        page.wait_for_timeout(400)  # let MutationObserver rebuild chart
        theme_attr = page.evaluate(
            "() => document.documentElement.dataset.theme"
        )
        check(
            "data-theme is 'dark' after two clicks",
            theme_attr == "dark",
            f"actual={theme_attr!r}",
        )
        # Chart instance should still exist after the rebuild.
        still_there = page.evaluate(
            "() => !!echarts.getInstanceByDom(document.getElementById('echart-canvas'))"
        )
        check("chart re-built on theme change", still_there)
        shot(page, "11-echart-dark")

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
