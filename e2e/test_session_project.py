"""Correcting one session's project in the stats editor touches that entry only.

Two finished entries sit on one activity. The editor changes the project of the
first one, after the user confirms it; the second keeps the activity's project,
and the page reloads with the new project on the first entry alone. Refusing the
confirmation keeps the stored project.
"""
import sys
import time

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def post_api(page: Page, path: str, fields: dict[str, str]) -> int:
    """POST a form the way the dashboard does, with the CSRF cookie as the token."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([path, fields, token]) => {
            const body = new URLSearchParams({ ...fields, csrf_token: token })
            const response = await fetch(path, {
                method: "POST", credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body,
            })
            return response.status
        }""",
        [path, fields, token],
    )


def main() -> int:
    stamp = int(time.time())
    activity = f"Правка проекта {stamp}"
    second = f"Вторая {stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Редактор проекта", f"session-project{stamp}@example.com", "project-qa-pass-2026")

        print("\n== 1. Two finished entries on one activity")
        page.goto(f"{BASE}/projects/new", wait_until="networkidle")
        page.fill('form[action="/projects/new"] input[name="name"]', second)
        page.locator('form[action="/projects/new"] button[type="submit"]').first.click()
        page.wait_for_load_state("networkidle")
        for _ in range(2):
            status = post_api(page, "/api/start", {"activity": activity})
            check("the timer starts", status in (200, 204), str(status))
            post_api(page, "/api/active/stop-all", {})

        print("\n== 2. The editor asks before it changes a project")
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        selects = page.get_by_label(f"Проект: {activity}")
        check("both entries have a project field", selects.count() == 2, str(selects.count()))
        selects.first.click()
        page.get_by_role("option", name=second).click()
        dialog = page.get_by_role("alertdialog")
        expect(dialog).to_be_visible()
        check("the confirmation names the activity and says only this entry changes",
              "Изменится только эта запись" in dialog.inner_text() and activity in dialog.inner_text())
        dialog.locator("button").last.click()
        page.wait_for_load_state("networkidle")

        print("\n== 3. Only the corrected entry carries the new project")
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        moved = page.get_by_label(f"Проект: {activity}").filter(has_text=second)
        check("one entry shows the new project after reload", page.get_by_label(f"Проект: {activity}").count() == 2 and moved.count() == 1,
              str(moved.count()))

        print("\n== 4. A refused confirmation keeps the stored project")
        untouched = page.get_by_label(f"Проект: {activity}").filter(has_not_text=second)
        check("the other entry still has no project", untouched.count() == 1, str(untouched.count()))
        untouched.first.click()
        page.get_by_role("option", name=second).click()
        expect(page.get_by_role("alertdialog")).to_be_visible()
        page.get_by_role("alertdialog").locator("button").first.click()
        page.wait_for_timeout(300)
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        check("the refused change leaves the stored project in place",
              page.get_by_label(f"Проект: {activity}").filter(has_text=second).count() == 1)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall session project checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
