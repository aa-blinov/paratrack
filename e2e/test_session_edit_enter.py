"""Enter saves the session editor's duration and note, as leaving the field does.

One finished entry on a fresh account:

1. Enter in the duration sends one save, and the page reloads with the new duration.
2. Enter with nothing changed sends nothing.
3. Enter in the note saves the note.
4. The editor's hint names both ways to save (on a phone, once the editor is open).
5. An entry in an approved past week: an Enter that the server refuses shows the
   refusal under the entry, and the stored duration stays.

Checked against the rendered page: the fields are found by their label, and the
saves are counted from the requests the page sends. Composition by an input method
is not emulated here; the editor reads the composition flag in code.
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


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def session_of(page: Page, activity: str) -> dict:
    return next(s for s in page_data(page)["Sessions"] if s["ActivityName"] == activity)


def shown_duration(page: Page, activity: str) -> str:
    # The page writes no-break spaces into the duration; the check reads plain ones.
    return session_of(page, activity)["DurationInput"].replace(" ", " ")


def main() -> int:
    stamp = int(time.time())
    activity = f"Enter в редакторе {stamp}"
    saves: list[str] = []
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        page.on("request", lambda r: saves.append(r.url) if r.method == "PATCH" and "/api/sessions/" in r.url else None)
        register(page, context, "Редактор Enter", f"session-enter{stamp}@example.com", "session-enter-qa-pass-2026")

        print("\n== 1. One finished entry")
        check("the timer starts", post_api(page, "/api/start", {"activity": activity}) in (200, 204))
        post_api(page, "/api/active/stop-all", {})
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        duration = page.get_by_label(re.compile(rf"Длительность.*{re.escape(activity)}"))
        note =page.get_by_label(re.compile(rf"Заметка.*{re.escape(activity)}"))
        check("the duration field is there", duration.count() == 1, str(duration.count()))

        print("\n== 2. Enter in the duration saves once")
        before = len(saves)
        duration.fill("1ч 30м")
        with page.expect_navigation(wait_until="networkidle"):
            duration.press("Enter")
        check("Enter sends one save", len(saves) - before == 1, str(len(saves) - before))
        check("the saved session carries 1 ч 30 мин", shown_duration(page, activity) == "1 ч 30 мин",
              shown_duration(page, activity))

        print("\n== 3. Enter with nothing changed sends nothing")
        before = len(saves)
        duration.focus()
        duration.press("Enter")
        page.wait_for_timeout(800)
        check("Enter without a change sends no save", len(saves) == before, str(len(saves) - before))

        print("\n== 4. Enter in the note saves the note")
        before = len(saves)
        note.fill("Сохранено по Enter")
        with page.expect_navigation(wait_until="networkidle"):
            note.press("Enter")
        check("Enter in the note sends one save", len(saves) - before == 1, str(len(saves) - before))
        check("the note is stored", session_of(page, activity)["Note"] == "Сохранено по Enter",
              str(session_of(page, activity)["Note"]))

        print("\n== 5. The hint names both ways to save")
        # The hint is shown once the editor is open, which on a phone is the "Показать" toggle.
        page.set_viewport_size({"width": 375, "height": 800})
        page.get_by_role("button", name="Показать", exact=True).click()
        check("the editor hint says Enter or leaving the field", "по Enter или после выхода из поля" in page.inner_text("body"))
        page.set_viewport_size({"width": 1280, "height": 900})

        print("\n== 6. A refused save shows the refusal under the entry")
        # Only a week that has ended can be approved, so the entry sits in a past week.
        past = f"Прошлая неделя {stamp}"
        past_url = f"{BASE}/stats?period=custom&start=2020-03-01T00:00&end=2020-03-09T00:00"
        post_api(page, "/api/sessions/backfill", {
            "activity": past, "start": "2020-03-03T10:00", "end": "2020-03-03T11:00", "note": "",
        })
        page.goto(past_url, wait_until="networkidle")
        check("the past entry is there", any(s["ActivityName"] == past for s in page_data(page)["Sessions"]))
        check("the past week is approved", post_api(page, "/api/timesheet/approve", {
            "start": "2020-03-02", "end": "2020-03-08",
        }) in (200, 204))
        page.goto(past_url, wait_until="networkidle")
        past_duration = page.get_by_label(re.compile(rf"Длительность.*{re.escape(past)}"))
        before = len(saves)
        past_duration.fill("2ч")
        past_duration.press("Enter")
        refusal = page.get_by_role("alert").filter(has_text="утверждена")
        expect(refusal).to_be_visible()
        check("the refused Enter reaches the server", len(saves) - before == 1, str(len(saves) - before))
        check("the refusal says the week is approved", refusal.count() == 1, str(refusal.count()))
        page.goto(past_url, wait_until="networkidle")
        stored = next(s for s in page_data(page)["Sessions"] if s["ActivityName"] == past)["DurationInput"]
        check("the stored duration stays 1 ч", stored.replace(" ", " ") == "1 ч", stored)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall Enter save checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
