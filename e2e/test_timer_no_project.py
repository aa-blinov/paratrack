"""A dashboard timer with no project asks once (TIME-12).

One account on the dashboard:

1. Starting a timer with no project opens the question and starts nothing.
2. Refusing leaves no timer running.
3. Starting again and confirming runs the timer.

Checked against the rendered page: running timers are counted in the active list
the page draws, so a dialog alone cannot make a check pass.
"""
import sys
import time

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def running(page: Page) -> int:
    return page.locator("#active-list [data-session-id]").count()


def start(page: Page, activity: str) -> None:
    page.fill("#activity", activity)
    page.locator("button[data-action-primary]").first.click()


def main() -> int:
    stamp = int(time.time())
    activity = f"Без проекта {stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Таймер без проекта", f"timer-no-project{stamp}@example.com", "timer-no-project-qa-pass-2026")
        page.goto(f"{BASE}/", wait_until="networkidle")

        print("\n== 1. Starting asks first and starts nothing")
        start(page, activity)
        expect(page.get_by_role("alertdialog")).to_be_visible()
        check("the question names the missing project",
              "Таймер без проекта" in page.get_by_role("alertdialog").inner_text())
        check("nothing runs before the answer", running(page) == 0, str(running(page)))

        print("\n== 2. Refusing runs nothing")
        page.get_by_role("alertdialog").locator("button").first.click()
        page.wait_for_timeout(500)
        check("refusing leaves no timer running", running(page) == 0, str(running(page)))

        print("\n== 3. Confirming runs the timer")
        start(page, activity)
        expect(page.get_by_role("alertdialog")).to_be_visible()
        page.get_by_role("alertdialog").locator("button").last.click()
        page.wait_for_timeout(800)
        page.goto(f"{BASE}/", wait_until="networkidle")
        check("the confirmed timer runs", running(page) == 1, str(running(page)))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall timer checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
