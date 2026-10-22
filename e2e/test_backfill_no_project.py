"""A session added without a project asks once before it is saved (TIME-12).

One account on the dashboard:

1. The first save without a project creates nothing and shows the warning; the
   button changes to "save without project".
2. The second save creates the session, and the statistics list it.
3. Choosing a project after the warning clears the confirmation.

Checked against the rendered page: the session is looked up in the statistics
data the page renders, so a visible toast cannot make a check pass.
"""
import sys
import time

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
WARNING = "Без проекта сессия не попадёт в отчёт по проекту"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def stamp(seconds_ago: int) -> str:
    moment = time.localtime(time.time() - seconds_ago)
    return time.strftime("%Y-%m-%d %H:%M:%S", moment)


def main() -> int:
    stamp_id = int(time.time())
    activity = f"Без проекта {stamp_id}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Без проекта", f"no-project{stamp_id}@example.com", "no-project-qa-pass-2026")
        page.goto(f"{BASE}/", wait_until="networkidle")

        print("\n== 1. The first save asks")
        page.get_by_text("Добавить прошедшую сессию").click()
        page.fill("#b-activity", activity)
        page.fill("#b-start", stamp(3 * 3600))
        page.fill("#b-end", stamp(2 * 3600))
        submit = page.locator('form:has(#b-activity) button[type="submit"]').first
        submit.click()
        expect(page.get_by_role("alert").filter(has_text=WARNING)).to_be_visible()
        check("the warning appears", page.get_by_role("alert").filter(has_text=WARNING).count() == 1)
        check("the button now says save without project", "Сохранить без проекта" in submit.inner_text())
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        check("nothing is saved yet", not any(s["ActivityName"] == activity for s in page_data(page)["Sessions"]))

        print("\n== 2. The second save creates the session")
        page.goto(f"{BASE}/", wait_until="networkidle")
        page.get_by_text("Добавить прошедшую сессию").click()
        page.fill("#b-activity", activity)
        page.fill("#b-start", stamp(3 * 3600))
        page.fill("#b-end", stamp(2 * 3600))
        page.locator('form:has(#b-activity) button[type="submit"]').first.click()
        page.wait_for_timeout(300)
        page.locator('form:has(#b-activity) button[type="submit"]').first.click()
        page.wait_for_function("document.querySelector('#b-activity').value === ''")
        page.goto(f"{BASE}/stats?period=week", wait_until="networkidle")
        check("the session is saved after the second save", any(s["ActivityName"] == activity for s in page_data(page)["Sessions"]))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall backfill checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
