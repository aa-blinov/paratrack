"""A session longer than ten hours asks before it is saved (TIME-18).

One finished entry on a fresh account, edited in the stats editor:

1. A duration of 10:30 opens the confirmation and sends nothing.
2. Refusing keeps the stored duration.
3. Confirming saves the duration.

Checked against the rendered page: the save is counted from the PATCH requests
the page sends, and the stored value is read back from the page data.
"""
import re
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


def post_api(page: Page, path: str, fields: dict[str, str]) -> int:
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


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def stored_duration(page: Page, activity: str) -> str:
    session = next(s for s in page_data(page)["Sessions"] if s["ActivityName"] == activity)
    return session["DurationInput"].replace(" ", " ")


def local_stamp(seconds_ago: int) -> str:
    moment = time.localtime(time.time() - seconds_ago)
    return time.strftime("%Y-%m-%d %H:%M:%S", moment)


def main() -> int:
    stamp = int(time.time())
    activity = f"Длинная сессия {stamp}"
    saves: list[str] = []
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        page.on("request", lambda r: saves.append(r.post_data or "") if r.method == "PATCH" and "/api/sessions/" in r.url else None)
        register(page, context, "Длинная сессия", f"long-session{stamp}@example.com", "long-session-qa-pass-2026")

        print("\n== 1. One finished entry")
        check("the timer starts", post_api(page, "/api/start", {"activity": activity}) in (200, 204))
        post_api(page, "/api/active/stop-all", {})
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        duration = page.get_by_label(re.compile(rf"Длительность.*{re.escape(activity)}"))
        before_value = stored_duration(page, activity)

        print("\n== 2. A duration over ten hours asks first")
        before = len(saves)
        duration.fill("10:30")
        duration.press("Enter")
        dialog = page.get_by_role("alertdialog")
        expect(dialog).to_be_visible()
        check("the confirmation says the session is longer than ten hours",
              "больше 10 часов" in dialog.inner_text())
        check("nothing is sent before the answer", len(saves) == before)

        print("\n== 3. Refusing keeps the stored duration")
        dialog.locator("button").first.click()
        page.wait_for_timeout(500)
        check("refusing sends nothing", len(saves) == before)
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        check("the stored duration is unchanged", stored_duration(page, activity) == before_value,
              stored_duration(page, activity))

        print("\n== 4. Confirming saves it")
        duration = page.get_by_label(re.compile(rf"Длительность.*{re.escape(activity)}"))
        duration.fill("10:30")
        duration.press("Enter")
        expect(page.get_by_role("alertdialog")).to_be_visible()
        page.get_by_role("alertdialog").locator("button").last.click()
        page.wait_for_load_state("networkidle")
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        check("the stored duration is 10:30", stored_duration(page, activity) == "10:30",
              stored_duration(page, activity))

        print("\n== 5. A manual entry of more than ten hours asks before it saves")
        page.goto(f"{BASE}/", wait_until="networkidle")
        manual = f"Вручную {stamp}"
        page.get_by_text("Добавить прошедшую сессию").click()
        page.fill("#b-activity", manual)
        page.fill("#b-start", local_stamp(30 * 3600))
        page.fill("#b-end", local_stamp(18 * 3600))
        # No project: the first save only asks about it (TIME-12); the second one
        # reaches the server, which asks about the length.
        page.locator('form:has(#b-activity) button[type="submit"]').first.click()
        page.wait_for_timeout(300)
        page.locator('form:has(#b-activity) button[type="submit"]').first.click()
        expect(page.get_by_role("alertdialog")).to_be_visible()
        check("the manual entry asks about the long session",
              "больше 10 часов" in page.get_by_role("alertdialog").inner_text())
        page.get_by_role("alertdialog").locator("button").last.click()
        page.wait_for_function("document.querySelector('#b-activity').value === ''")
        page.goto(f"{BASE}/stats?period=week", wait_until="networkidle")
        check("the confirmed manual entry is saved",
              any(s["ActivityName"] == manual for s in page_data(page)["Sessions"]))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall long-session checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
