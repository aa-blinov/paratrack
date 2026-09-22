#!/usr/bin/env python3
"""slice_screenshots.py — нарезает UI каждой страницы paratrack на секции.

Для каждой комбинации (page, theme, optional state) запускает Playwright,
открывает соответствующий URL, ждёт пока Alpine/HTMX/ECharts отрендерят,
и сохраняет отдельные PNG:

    screenshots/<combo>/header.png        # topbar
    screenshots/<combo>/footer.png        # footer (если есть)
    screenshots/<combo>/main.png          # <main class="container">
    screenshots/<combo>/card-1-…png       # по card-селектору

Сервер paratrack web должен быть запущен на $ADDR (по умолчанию
127.0.0.1:8888). Если недоступен — скрипт падает.

Существующий каталог screenshots/ очищается перед запуском, чтобы
не плодить старые артефакты.

Пример:

    make web &
    .venv/bin/python scripts/slice_screenshots.py
"""

import os
import sys
import shutil
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "screenshots"
ADDR = os.environ.get("ADDR", "127.0.0.1:8888")
BASE = f"http://{ADDR}"

# Каждая комбинация: (slug, url, theme, [actions]). slug — это
# имя подкаталога в screenshots/. theme — "light" | "dark".
# actions — список кортежей (label, fn) для перерендеринга состояния
# (start session, pause, и т.д.) перед скриншотом. label добавляется
# к slug если не None.
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
    import urllib.request
    import urllib.error
    deadline = time.time() + 10
    while time.time() < deadline:
        try:
            urllib.request.urlopen(BASE, timeout=1).read()
            return
        except (urllib.error.URLError, ConnectionResetError, OSError):
            time.sleep(0.3)
    sys.exit(f"server at {BASE} is not up; start it with 'make web' first")


def set_theme(page, theme):
    """Set the page theme via paratrack-theme localStorage key, then reload."""
    page.goto(BASE + "/", wait_until="domcontentloaded")
    page.evaluate(f"localStorage.setItem('paratrack-theme', '{theme}')")


def screenshot_section(page, selector, out_path):
    """Take a screenshot of one DOM element. Returns False if missing."""
    try:
        el = page.locator(selector).first
        el.wait_for(state="visible", timeout=2000)
        el.screenshot(path=str(out_path))
        return True
    except Exception as e:
        print(f"  skip {selector}: {e}", file=sys.stderr)
        return False


def screenshot_card(page, index, out_path, desc):
    """Take the index-th .card inside <main class='container'>. Used to slice
    per-page sections regardless of whether they're nested in grid wrappers.
    """
    try:
        el = page.locator("main.container .card").nth(index)
        el.wait_for(state="visible", timeout=2000)
        el.screenshot(path=str(out_path))
        return True
    except Exception as e:
        print(f"  skip card[{index}] ({desc}): {e}", file=sys.stderr)
        return False


# Селекторы секций для каждой страницы. Ключ — это slug из COMBOS;
# значение — список (filename, selector/index, desc).
# Для card-* используется индекс в main.container .card, не CSS-селектор,
# чтобы не зависеть от того, обёрнуты ли карточки в grid-*.
SECTIONS = {
    "default": [
        ("header.png", "header.topbar", "topbar"),
        ("main.png", "main.container", "main content"),
        ("footer.png", "footer.footer", "footer"),
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
    """Mutate server state for a combo (start/pause a session, etc.)."""
    if not actions:
        return
    if "active" in actions or "paused" in actions:
        # Start a session via the API.
        page.request.post(BASE + "/api/start", form={"activity": "reading", "note": "slice-demo"})
        # Wait for the active-list to refresh.
        page.wait_for_timeout(400)
        if "paused" in actions:
            # Find the active session id and pause it.
            ids = page.evaluate("""
                () => Array.from(document.querySelectorAll('[data-session-id]'))
                          .map(e => e.dataset.sessionId)
            """)
            ids += page.evaluate("""
                () => Array.from(document.querySelectorAll('a[href*="/api/sessions/"]'))
                          .map(a => (a.href.match(/sessions\/(\\d+)/) || [])[1])
                          .filter(Boolean)
            """)
            ids = [int(x) for x in ids if x and x.isdigit()]
            if ids:
                page.request.post(BASE + f"/api/sessions/{ids[0]}/stop")
                page.request.post(BASE + "/api/start", form={"activity": "writing", "note": "slice-demo"})
                page.wait_for_timeout(400)
                ids2 = page.evaluate("""
                    () => Array.from(document.querySelectorAll('a[href*="/api/sessions/"]'))
                              .map(a => (a.href.match(/sessions\/(\\d+)/) || [])[1])
                              .filter(Boolean)
                """)
                ids2 = [int(x) for x in ids2 if x and x.isdigit()]
                new_id = max(set(ids2) - set(ids), default=None)
                if new_id:
                    page.request.post(BASE + f"/api/sessions/{new_id}/pause")
                    page.wait_for_timeout(300)


def cleanup_actions(page):
    """Stop any active sessions we created so the user's DB stays clean."""
    try:
        ids = page.evaluate("""
            () => Array.from(document.querySelectorAll('a[href*="/api/sessions/"]'))
                      .map(a => (a.href.match(/sessions\/(\\d+)/) || [])[1])
                      .filter(Boolean)
        """)
        for sid in sorted(set(int(x) for x in ids if x and x.isdigit()), reverse=True):
            page.request.post(BASE + f"/api/sessions/{sid}/stop")
    except Exception:
        pass


def slice_combo(browser, slug, url, theme, actions):
    out_dir = OUT / slug
    out_dir.mkdir(parents=True, exist_ok=True)

    # URL для выбора секций — это path-only, без query string.
    from urllib.parse import urlparse
    path = urlparse(url).path

    context = browser.new_context(viewport={"width": 1280, "height": 900})
    page = context.new_page()

    set_theme(page, theme)
    page.goto(BASE + url, wait_until="domcontentloaded")
    apply_actions(context, page, actions, slug)
    page.goto(BASE + url, wait_until="networkidle")
    # Give Alpine/HTMX/ECharts a moment.
    page.wait_for_timeout(600)

    # Top-level sections.
    for fname, selector, desc in SECTIONS["default"]:
        screenshot_section(page, selector, out_dir / fname)
        print(f"  {slug}: {fname} ({desc})")

    # Page-specific cards — match by either the full URL (with query) or path.
    sections = SECTIONS.get(url) or SECTIONS.get(path, [])
    for fname, index, desc in sections:
        screenshot_card(page, index, out_dir / fname, desc)
        print(f"  {slug}: {fname} ({desc})")

    cleanup_actions(page)
    context.close()


def main():
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir(parents=True)

    wait_server()

    with sync_playwright() as p:
        browser = p.chromium.launch()
        for slug, url, theme, actions in COMBOS:
            print(f"slicing {slug} ({url}, {theme}, {actions or 'idle'})…")
            try:
                slice_combo(browser, slug, url, theme, actions)
            except Exception as e:
                print(f"  ! {slug}: {e}", file=sys.stderr)
        browser.close()

    combos_done = sum(1 for _ in OUT.iterdir() if _.is_dir())
    print(f"\ndone — {combos_done} combos in {OUT.relative_to(ROOT)}/")


if __name__ == "__main__":
    main()