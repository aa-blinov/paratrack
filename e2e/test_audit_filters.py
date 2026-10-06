"""Audit-log filters: the trail narrows by period, actor and action, and the
state lives in the address.

The suite registers its own workspace and fills it with more than one window of
events, so nothing in an existing workspace is read or changed. Point
PARATRACK_BASE at a server running the current build:

    PARATRACK_BASE=http://127.0.0.1:8899 .venv/bin/python e2e/test_audit_filters.py
"""
from __future__ import annotations

import re
import time
from datetime import date, timedelta
from pathlib import Path

import requests
from playwright.sync_api import sync_playwright

from target import BASE_URL as BASE

PASSWORD = "longenoughpw"
# Enough start/stop pairs to push the trail past the 100-row window.
CYCLES = 55
SCREENSHOT = Path("/tmp/paratrack-audit-filters.png")

FAILURES: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if detail else ""))
    if not ok:
        FAILURES.append(name)


def csrf_of(session: requests.Session, path: str) -> str:
    page = session.get(f"{BASE}{path}")
    token = re.search(r'"csrfToken":"([^"]+)"', page.text)
    return token.group(1) if token else session.cookies.get("paratrack_csrf", "")


def build_workspace() -> requests.Session:
    """Register a throwaway account and leave more than one window of events."""
    session = requests.Session()
    session.headers["Accept-Language"] = "ru"
    token = csrf_of(session, "/login")
    email = f"auditfilters{int(time.time())}@x.test"
    response = session.post(
        f"{BASE}/api/register",
        data={"name": "Фильтры журнала", "email": email, "password": PASSWORD, "csrf_token": token},
        headers={"X-CSRF-Token": token},
        allow_redirects=False,
    )
    if response.status_code not in (200, 303):
        raise SystemExit(f"register failed: {response.status_code} {response.text[:200]}")
    for index in range(CYCLES):
        for path, body in (
            ("/api/start", {"activity": f"Проверка {index}"}),
            ("/api/active/stop-all", {}),
        ):
            session.post(
                f"{BASE}{path}",
                data={**body, "csrf_token": session.cookies.get("paratrack_csrf", "")},
                headers={"X-CSRF-Token": session.cookies.get("paratrack_csrf", "")},
                allow_redirects=False,
            )
    return session


def cookies_for(session: requests.Session) -> list[dict]:
    return [{"name": c.name, "value": c.value, "domain": "127.0.0.1", "path": "/"} for c in session.cookies]


def main() -> int:
    session = build_workspace()
    today = date.today().isoformat()
    tomorrow = (date.today() + timedelta(days=1)).isoformat()

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        for width in (1440, 390):
            context = browser.new_context(viewport={"width": width, "height": 900})
            context.add_cookies(cookies_for(session))
            page = context.new_page()
            errors: list[str] = []
            page.on("pageerror", lambda error: errors.append(str(error)))
            form = page.locator("form[action='/settings/audit']")

            page.goto(f"{BASE}/settings/audit")
            page.locator("#audit-from").wait_for()
            controls = page.locator("main input, main select")
            check(f"{width}px: period, actor and action filters exist", controls.count() == 4,
                  f"{controls.count()} controls")
            check(f"{width}px: every filter has a label",
                  page.locator("main label[for=audit-from], main label[for=audit-to], "
                               "main label[for=audit-user], main label[for=audit-action]").count() == 4)
            check(f"{width}px: filters start empty", page.locator("#audit-from").input_value() == ""
                  and page.locator("#audit-user").input_value() == "")
            members = [value for value in page.locator("#audit-user option").evaluate_all(
                "nodes => nodes.map(n => n.value)") if value]
            check(f"{width}px: the actor filter lists workspace members", len(members) == 1, str(members))
            member = members[0] if members else ""

            # An unfiltered trail is longer than the window, so the screen has to
            # admit the cut and offer a way past it.
            check(f"{width}px: a cut list widens the window", form.count() == 1 and
                  page.locator("main a[href*='size=200']").count() == 1)
            check(f"{width}px: the action filter offers recorded codes",
                  page.locator("#audit-action option").evaluate_all("nodes => nodes.map(n => n.value)")
                  == ["", "auth.register", "session.start", "session.stop"])

            # Filtering by action: every row must be that action.
            page.goto(f"{BASE}/settings/audit?event=session.start")
            page.locator("#audit-from").wait_for()
            codes = page.locator("main [title]").evaluate_all("nodes => nodes.map(n => n.title)")
            check(f"{width}px: action filter keeps one event type",
                  bool(codes) and set(codes) == {"session.start"}, f"{len(codes)} rows {sorted(set(codes))}")
            check(f"{width}px: the active action stays in the address",
                  "event=session.start" in page.url and page.locator("#audit-action").input_value() == "session.start")

            # Filtering by actor keeps the state in the controls, not only in colour.
            page.goto(f"{BASE}/settings/audit?user=999999")
            page.locator("#audit-from").wait_for()
            check(f"{width}px: an unknown actor is ignored", page.locator("#audit-user").input_value() == "")
            page.goto(f"{BASE}/settings/audit?event=nope.unknown")
            page.locator("#audit-from").wait_for()
            check(f"{width}px: an unknown action is ignored", page.locator("#audit-action").input_value() == "")

            # Period + actor + action together, arriving as one link.
            page.goto(f"{BASE}/settings/audit?from={today}&to={today}&user={member}&event=session.stop&size=200")
            page.locator("#audit-from").wait_for()
            codes = page.locator("main [title]").evaluate_all("nodes => nodes.map(n => n.title)")
            check(f"{width}px: period, actor and action combine",
                  bool(codes) and set(codes) == {"session.stop"},
                  f"{len(codes)} rows {sorted(set(codes))}")
            check(f"{width}px: the wider window survives a reload",
                  page.locator("input[name=size]").input_value() == "200")

            # A period with nothing in it is a state, not a blank page.
            page.goto(f"{BASE}/settings/audit?from={tomorrow}&to={tomorrow}")
            page.locator("#audit-from").wait_for()
            check(f"{width}px: an empty filtered result explains itself",
                  page.locator("main a[href='/settings/audit']").count() >= 1
                  and page.locator("main [title]").count() == 0)
            page.screenshot(path=str(SCREENSHOT.with_name(f"{SCREENSHOT.stem}-{width}{SCREENSHOT.suffix}")), full_page=True)

            # Submitting the form lands on the journal itself. A control named
            # after an HTMLFormElement property used to redirect the form to the
            # control itself, so the destination is part of what this checks.
            page.goto(f"{BASE}/settings/audit")
            page.locator("#audit-from").wait_for()
            page.locator("#audit-action").select_option("session.start")
            with page.expect_navigation():
                page.locator("form[action='/settings/audit'] button[type=submit]").click()
            page.locator("#audit-from").wait_for()
            check(f"{width}px: the form submits to the journal with its filters",
                  "/settings/audit?" in page.url and "event=session.start" in page.url, page.url)

            # The form works from the keyboard alone: pick a period and an action, then
            # submit with Enter instead of the mouse.
            page.goto(f"{BASE}/settings/audit")
            page.locator("#audit-from").wait_for()
            page.locator("#audit-from").fill(today)
            page.locator("#audit-action").select_option("session.stop")
            with page.expect_navigation():
                page.locator("#audit-from").press("Enter")
            page.locator("#audit-from").wait_for()
            check(f"{width}px: submitting from the keyboard writes the filters into the address",
                  f"from={today}" in page.url and "event=session.stop" in page.url
                  and page.locator("#audit-from").input_value() == today,
                  page.url)

            check(f"{width}px: no script errors", not errors, str(errors))
            check(f"{width}px: no horizontal overflow",
                  page.evaluate("document.documentElement.scrollWidth <= innerWidth"))
            context.close()
        browser.close()
    return 1 if FAILURES else 0


if __name__ == "__main__":
    raise SystemExit(main())