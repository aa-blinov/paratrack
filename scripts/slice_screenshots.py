#!/usr/bin/env python3
"""slice_screenshots.py — нарезает UI каждой страницы paratrack на секции.

Под капотом: поднимает свой paratrack web с изолированной HOME
(чтобы не трогать ~/.track/track.db), сидит его демо-данными через
sqlite3, прогоняет Playwright по всем (page, theme, state) комбинациям
и сохраняет PNG в screenshots/<combo>/{header,main,footer,card-N}.png.
Сервер изолирован — реальные данные пользователя не затрагиваются.

Использование:

    .venv/bin/python scripts/slice_screenshots.py
"""

import os
import sys
import shutil
import subprocess
import tempfile
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "screenshots"
ADDR = os.environ.get("ADDR", "127.0.0.1:8888")
BASE = f"http://{ADDR}"
SLICE_BIN = ROOT / "paratrack"

# (slug, url, theme, actions). actions — ["active"|"paused"] to mutate
# the server state before the screenshot.
COMBOS = [
    ("dashboard-light", "/", "light", []),
    ("dashboard-light-active", "/", "light", ["active"]),
    ("dashboard-light-paused", "/", "light", ["paused"]),
    ("dashboard-dark", "/", "dark", []),
    ("dashboard-dark-active", "/", "dark", ["active"]),
    ("dashboard-dark-paused", "/", "dark", ["paused"]),
    ("stats-light", "/stats", "light", []),
    ("stats-dark", "/stats", "dark", []),
    ("stats-light-tag", "/stats?tag=morning", "light", []),
    ("stats-dark-tag", "/stats?tag=morning", "dark", []),
    ("graph-light", "/graph", "light", []),
    ("graph-dark", "/graph", "dark", []),
    ("goals-light", "/goals", "light", []),
    ("goals-dark", "/goals", "dark", []),
    ("tags-light", "/tags", "light", []),
    ("tags-dark", "/tags", "dark", []),
]


def wait_server():
    if server_up():
        return
    sys.exit(f"server at {BASE} is not up; start it with 'make web' first")


def server_up():
    import urllib.request
    try:
        urllib.request.urlopen(BASE, timeout=1).read()
        return True
    except Exception:
        return False


def start_own_server():
    """Run our own `paratrack web` against a fresh TRACK_HOME so we never
    touch the user's real ~/.track/track.db. Returns (process, tmpdir)."""
    if not SLICE_BIN.exists():
        sys.exit(f"{SLICE_BIN} not built; run `make build` first")
    tmp_home = Path(tempfile.mkdtemp(prefix="paratrack-slice-"))
    log = open("/tmp/paratrack-slice.log", "w")
    proc = subprocess.Popen(
        [str(SLICE_BIN), "web", "--addr", ADDR],
        env={**os.environ, "HOME": str(tmp_home)},
        stdout=log, stderr=log,
    )
    import urllib.request
    deadline = time.time() + 10
    while time.time() < deadline:
        try:
            urllib.request.urlopen(BASE, timeout=1).read()
            print(f"  (started own paratrack in {tmp_home}, log: /tmp/paratrack-slice.log)")
            return proc, tmp_home
        except Exception:
            time.sleep(0.3)
    proc.terminate()
    sys.exit(f"paratrack failed to start on {BASE} — see /tmp/paratrack-slice.log")


def seed_demo_data():
    """Plant a handful of activities / sessions / tags / goals in the DB
    directly via sqlite3 so the pages have something to render."""
    import datetime, subprocess
    db = Path(os.environ["HOME"]) / ".track" / "track.db"
    print(f"  seeding {db}", flush=True)
    if not db.exists():
        print(f"  ! db missing, skipping seed", flush=True)
        return
    # Use timezone-aware UTC instead of deprecated utcnow() so we
    # don't accidentally emit naive datetimes that the template
    # formatter then reads back in a different timezone.
    now = datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)
    # RFC 3339 with microseconds: 2026-09-22T13:10:43.072300Z.
    # strftime("%f") returns 6 digits without a leading dot, so we
    # splice it back in ourselves.
    iso = lambda t: t.strftime("%Y-%m-%dT%H:%M:%S.") + f"{t.microsecond:06d}Z"
    print(f"  now={now.isoformat()}", flush=True)

    def at(seconds_ago):
        return iso(now - datetime.timedelta(seconds=seconds_ago))

    rows = []
    for name in ["reading", "work", "writing", "exercise"]:
        rows.append(
            f"INSERT INTO activities (name, created_at, updated_at) "
            f"VALUES ('{name}', '{at(86400)}', '{at(86400)}');"
        )
    sessions = [
        (1, 5 * 3600, 30 * 60, "Designing the new look"),
        (2, 6 * 3600, 150 * 60, ""),
        (3, 7 * 3600, 45 * 60, "morning"),
        (4, 4 * 3600, 60 * 60, "afternoon"),
        (1, 3 * 3600, 25 * 60, ""),
        (2, 2 * 3600, 90 * 60, ""),
        (3, 1 * 3600, 15 * 60, "test"),
    ]
    for aid, ago, secs, note in sessions:
        start = at(ago)
        end = at(ago - secs)
        rows.append(
            f"INSERT INTO sessions (activity_id, start_at, end_at, note, paused, "
            f"accumulated_seconds, created_at, updated_at) "
            f"VALUES ({aid}, '{start}', '{end}', '{note}', 0, 0, "
            f"'{start}', '{end}');"
        )
    rows.append(f"INSERT INTO goals (activity_id, period, target_minutes, created_at, updated_at) VALUES (1, 'daily', 120, '{at(86400)}', '{at(86400)}');")
    rows.append(f"INSERT INTO goals (activity_id, period, target_minutes, created_at, updated_at) VALUES (2, 'daily', 90, '{at(86400)}', '{at(86400)}');")
    rows.append(f"INSERT INTO goals (activity_id, period, target_minutes, created_at, updated_at) VALUES (3, 'weekly', 480, '{at(86400)}', '{at(86400)}');")
    for name in ["deep-work", "morning", "weekend", "study"]:
        rows.append(f"INSERT INTO tags (name, created_at) VALUES ('{name}', '{at(86400)}');")

    sql = "\n".join(rows)
    subprocess.run(
        ["sqlite3", str(db), sql],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def cleanup_actions(page):
    """Stop any active sessions we created. Scans both <a href> and
    [hx-post] / [hx-delete] attributes since we use button-based actions,
    not links.
    """
    try:
        ids = page.evaluate(r"""
            () => {
              const out = new Set();
              for (const a of document.querySelectorAll('a[href*="/api/sessions/"]')) {
                const m = a.href.match(/sessions\/(\d+)/);
                if (m) out.add(m[1]);
              }
              for (const el of document.querySelectorAll('[hx-post*="/api/sessions/"]')) {
                const m = (el.getAttribute('hx-post') || '').match(/sessions\/(\d+)/);
                if (m) out.add(m[1]);
              }
              for (const el of document.querySelectorAll('[hx-delete*="/api/sessions/"]')) {
                const m = (el.getAttribute('hx-delete') || '').match(/sessions\/(\d+)/);
                if (m) out.add(m[1]);
              }
              return Array.from(out);
            }
        """)
        for sid in sorted({int(x) for x in ids if x and x.isdigit()}, reverse=True):
            try:
                page.request.post(BASE + f"/api/sessions/{sid}/stop")
            except Exception:
                pass
    except Exception:
        pass


def set_theme(page, theme):
    """Set the page theme via paratrack-theme localStorage key, then reload."""
    page.goto(BASE + "/", wait_until="domcontentloaded")
    page.evaluate(f"localStorage.setItem('paratrack-theme', '{theme}')")


def screenshot_section(page, selector, out_path):
    try:
        el = page.locator(selector).first
        el.wait_for(state="visible", timeout=2000)
        el.screenshot(path=str(out_path))
        return True
    except Exception as e:
        print(f"  skip {selector}: {e}", file=sys.stderr)
        return False


def screenshot_card(page, index, out_path, desc):
    try:
        el = page.locator("main .card").nth(index)
        el.wait_for(state="visible", timeout=2000)
        el.screenshot(path=str(out_path))
        return True
    except Exception as e:
        print(f"  skip card[{index}] ({desc}): {e}", file=sys.stderr)
        return False


# Sections per URL path. card-N entries use index in main .card, not
# CSS selector, so they don't break when cards are wrapped in grid-* layouts.
SECTIONS = {
    "default": [
        ("header.png", "header", "topbar"),
        ("main.png", "main", "main content"),
        ("footer.png", "footer", "footer"),
    ],
    "/": [
        ("card-1-start.png", 0, "start form"),
        ("card-2-today.png", 1, "today"),
        ("card-3-active.png", 2, "active sessions"),
        ("card-4-recent.png", 3, "recent sessions"),
        ("card-5-goals.png", 4, "goals widget"),
    ],
    "/stats": [
        ("card-1-breakdown.png", 0, "breakdown"),
        ("card-2-distribution.png", 1, "distribution"),
        ("card-3-sessions.png", 2, "sessions list"),
    ],
    "/graph": [
        ("card-1-chart.png", 0, "echart"),
        ("card-2-legend.png", 1, "legend chips"),
    ],
    "/goals": [
        ("card-1-new-goal.png", 0, "new goal form"),
        ("card-2-current.png", 1, "current goals"),
        ("card-3-cli.png", 2, "CLI hints"),
    ],
    "/tags": [
        ("card-1-new-tag.png", 0, "new tag form"),
        ("card-2-all.png", 1, "all tags"),
        ("card-3-cli.png", 2, "CLI hints"),
    ],
    "/stats?tag=morning": [
        ("card-1-filter.png", 0, "tag-filter banner"),
        ("card-2-breakdown.png", 1, "breakdown"),
        ("card-3-distribution.png", 2, "distribution"),
        ("card-4-sessions.png", 3, "sessions list"),
    ],
}


def apply_actions(context, page, actions, slug):
    """Mutate server state for a combo (start/pause a session)."""
    if not actions:
        return
    if "active" in actions or "paused" in actions:
        # Stop any pre-existing session so the start below can't 409.
        import urllib.request, re as _re
        body = urllib.request.urlopen(BASE + "/api/active", timeout=2).read().decode()
        for sid in _re.findall(r'/api/sessions/(\d+)/stop', body):
            urllib.request.urlopen(
                urllib.request.Request(
                    BASE + f"/api/sessions/{sid}/stop", method="POST"),
                timeout=2,
            ).read()
        page.request.post(BASE + "/api/start", form={"activity": "reading", "note": "slice-demo"})
        page.wait_for_timeout(400)
        if "paused" in actions:
            # Stop the active one, start fresh, pause that — guarantees
            # the dashboard renders a paused row.
            body = urllib.request.urlopen(BASE + "/api/active", timeout=2).read().decode()
            ids = [int(x) for x in _re.findall(r'/api/sessions/(\d+)/', body) if x.isdigit()]
            if ids:
                urllib.request.urlopen(
                    urllib.request.Request(
                        BASE + f"/api/sessions/{ids[0]}/stop", method="POST"),
                    timeout=2,
                ).read()
                page.request.post(BASE + "/api/start", form={"activity": "writing", "note": "slice-demo"})
                page.wait_for_timeout(400)
                body = urllib.request.urlopen(BASE + "/api/active", timeout=2).read().decode()
                ids2 = [int(x) for x in _re.findall(r'/api/sessions/(\d+)/', body) if x.isdigit()]
                new_id = max(set(ids2) - set(ids), default=None)
                if new_id:
                    page.request.post(BASE + f"/api/sessions/{new_id}/pause")
                    page.wait_for_timeout(300)


def slice_combo(browser, slug, url, theme, actions):
    print(f"  [{slug}] starting", flush=True)
    out_dir = OUT / slug
    out_dir.mkdir(parents=True, exist_ok=True)

    # URL для выбора секций — это path-only, без query string.
    from urllib.parse import urlparse
    path = urlparse(url).path

    # Make sure no stale session is active before we start a new combo —
    # the previous run might have crashed mid-flow and left a session
    # open, which would 409 the /api/start we're about to issue.
    try:
        import urllib.request, re as _re
        body = urllib.request.urlopen(BASE + "/api/active", timeout=2).read().decode()
        for sid in _re.findall(r'/api/sessions/(\d+)/stop', body):
            urllib.request.urlopen(
                urllib.request.Request(
                    BASE + f"/api/sessions/{sid}/stop", method="POST"),
                timeout=2,
            ).read()
    except Exception:
        pass

    print(f"  [{slug}] creating context", flush=True)
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    page = context.new_page()
    # Hard cap on every wait_for, but page.goto keeps "load" — using
    # networkidle here would race against HTMX polling and ECharts
    # animations that never let the network go quiet.
    page.set_default_timeout(15000)

    print(f"  [{slug}] set_theme", flush=True)
    set_theme(page, theme)
    print(f"  [{slug}] goto url", flush=True)
    page.goto(BASE + url, wait_until="domcontentloaded")
    print(f"  [{slug}] apply_actions {actions}", flush=True)
    apply_actions(context, page, actions, slug)
    print(f"  [{slug}] reload", flush=True)
    page.goto(BASE + url, wait_until="load")
    # Give Alpine/HTMX/ECharts a moment.
    page.wait_for_timeout(600)

    # Top-level sections.
    for fname, selector, desc in SECTIONS["default"]:
        screenshot_section(page, selector, out_dir / fname)
        print(f"  {slug}: {fname} ({desc})", flush=True)

    # Page-specific cards — match by either the full URL (with query) or path.
    sections = SECTIONS.get(url) or SECTIONS.get(path, [])
    for fname, index, desc in sections:
        screenshot_card(page, index, out_dir / fname, desc)
        print(f"  {slug}: {fname} ({desc})", flush=True)

    cleanup_actions(page)
    context.close()
    print(f"  [{slug}] done", flush=True)


def main():
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir(parents=True)

    own_proc = None
    tmp_home = None
    saved_home = os.environ.get("HOME")
    # Playwright resolves its chromium cache as $HOME/Library/Caches/ms-playwright
    # on macOS. We override HOME for the paratrack subprocess so it
    # uses an isolated DB, but we don't want that override to nuke the
    # browser cache lookup for our own Playwright run. Pin the
    # browsers path explicitly so chromium is found regardless.
    pw_cache = Path.home() / "Library" / "Caches" / "ms-playwright"
    saved_pw = os.environ.get("PLAYWRIGHT_BROWSERS_PATH")
    os.environ["PLAYWRIGHT_BROWSERS_PATH"] = str(pw_cache)
    try:
        if not server_up():
            own_proc, tmp_home = start_own_server()
            # Re-point HOME at the temp dir so seed_demo_data writes
            # into the same DB the server is reading from.
            os.environ["HOME"] = str(tmp_home)

        # Seed the DB with a handful of activities / sessions / tags
        # / goals so the screenshots have something to render.
        seed_demo_data()

        with sync_playwright() as p:
            browser = p.chromium.launch()
            for slug, url, theme, actions in COMBOS:
                print(f"slicing {slug} ({url}, {theme}, {actions or 'idle'})…")
                try:
                    slice_combo(browser, slug, url, theme, actions)
                except Exception as e:
                    print(f"  ! {slug}: {e}", file=sys.stderr)
            browser.close()

        stop_all_active()
    finally:
        # Restore HOME and the Playwright cache lookup before we drop
        # the temp dir so subsequent calls (and the user's shell)
        # keep seeing the real ones.
        if saved_home is not None:
            os.environ["HOME"] = saved_home
        if saved_pw is not None:
            os.environ["PLAYWRIGHT_BROWSERS_PATH"] = saved_pw
        else:
            os.environ.pop("PLAYWRIGHT_BROWSERS_PATH", None)
        if own_proc is not None:
            try:
                own_proc.terminate()
                own_proc.wait(timeout=5)
            except Exception:
                try:
                    own_proc.kill()
                except Exception:
                    pass
        if tmp_home is not None:
            shutil.rmtree(tmp_home, ignore_errors=True)

    combos_done = sum(1 for _ in OUT.iterdir() if _.is_dir())
    print(f"\ndone — {combos_done} combos in {OUT.relative_to(ROOT)}/")


def stop_all_active():
    import urllib.request, re as _re
    try:
        body = urllib.request.urlopen(BASE + "/api/active", timeout=2).read().decode()
        for sid in _re.findall(r'/api/sessions/(\d+)/stop', body):
            try:
                urllib.request.urlopen(
                    urllib.request.Request(
                        BASE + f"/api/sessions/{sid}/stop", method="POST"),
                    timeout=2,
                ).read()
            except Exception:
                pass
    except Exception:
        pass


if __name__ == "__main__":
    main()