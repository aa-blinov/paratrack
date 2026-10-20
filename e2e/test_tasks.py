"""The whole task cycle on a clean account.

A task is created (an unreadable due date blocks it), takes a second activity,
has time logged to its activities, changes column, is dragged across the board,
is read back in the table, works on a phone without sideways scroll, and is
archived. The time must show on the task, and a due date that cannot be read
must never clear a saved one.
"""
import re
import sys
import time
from datetime import date, timedelta
from pathlib import Path
from urllib.parse import urlsplit

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register, submit_form

SHOTS = Path(__file__).resolve().parents[1] / "e2e" / "screenshots"
failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def text_of(locator) -> str:
    """Visible text with the no-break spaces the formatters use turned into plain ones."""
    return locator.inner_text().replace("\u00a0", " ")


def no_sideways_scroll(page: Page) -> bool:
    return page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")


def log_time(page: Page, activity: str, day: str, start: str, end: str) -> int:
    """Log a finished session on an activity by name, the way the backfill form does."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([activity, start, end, token]) => {
            const body = new URLSearchParams({ activity, start, end, csrf_token: token })
            const response = await fetch("/api/sessions/backfill", {
                method: "POST", credentials: "same-origin",
                headers: { "X-CSRF-Token": token, Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body,
            })
            return response.status
        }""",
        [activity, f"{day} {start}", f"{day} {end}", token],
    )


def create_task(page: Page, title: str, estimate: str = "", due: str = "") -> None:
    page.goto(f"{BASE}/tasks", wait_until="networkidle")
    page.fill("#new-task-title", title)
    if estimate:
        page.fill("#new-task-estimate", estimate)
    if due:
        page.fill("#new-task-due", due)
    page.locator('form[aria-label="Новая задача"] button[type="submit"]').click()


def main() -> int:
    stamp = int(time.time())
    email = f"tasks{stamp}@example.com"
    project_name = f"Бриф для студии {stamp}"
    brief = "Собрать бриф"
    layout = "Макет главной"
    day = (date.today() - timedelta(days=2)).isoformat()

    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Тест задач", email, "tasks-qa-pass-2026")

        print("\n== 1. A project to hang tasks on")
        page.goto(f"{BASE}/projects/new", wait_until="networkidle")
        page.fill('form[action="/projects/new"] input[name="name"]', project_name)
        submit_form(page, 'form[action="/projects/new"]')
        check("project created", "/projects/" in page.url, page.url)

        print("\n== 2. An empty board, then the first task")
        page.goto(f"{BASE}/tasks", wait_until="networkidle")
        check("empty board says so", page.get_by_text("Задач пока нет").count() > 0)
        page.screenshot(path=str(SHOTS / "tasks-empty-desktop.png"), full_page=True)

        create_task(page, brief, estimate="240")
        page.wait_for_url(lambda url: re.search(r"/tasks/\d+$", urlsplit(url).path) is not None, timeout=15000)
        task_path = urlsplit(page.url).path
        check("creating a task opens its card", True)
        expect(page.get_by_role("heading", level=1)).to_have_text(brief)
        activities = page.locator('section[aria-label="Активности"] li')
        check("the first activity is named after the task",
              activities.count() == 1 and brief in activities.first.inner_text(), str(activities.all_inner_texts()))

        print("\n== 3. A second activity, and time logged to both")
        page.fill("#task-activity-name", "Ревью")
        page.locator('form:has(#task-activity-name) button[type="submit"]').click()
        expect(activities).to_have_count(2)
        check("a second activity joins the task", activities.count() == 2)
        check("time on the task's own activity is accepted", log_time(page, brief, day, "09:00", "09:30") in (200, 204))
        check("time on the second activity is accepted", log_time(page, "Ревью", day, "10:00", "10:15") in (200, 204))
        page.reload(wait_until="networkidle")
        tracked = page.get_by_text("Учтено:").locator("..")
        check("the card shows 45 minutes from both activities", "45 мин" in text_of(tracked), text_of(tracked))
        check("the estimate is kept on the card", page.locator("#task-estimate").input_value() == "240")

        print("\n== 4. A status change, kept after a reload")
        page.click("#task-status")
        page.get_by_role("option", name="В работе").click()
        page.wait_for_timeout(500)
        page.reload(wait_until="networkidle")
        check("the card keeps the new status", "В работе" in page.locator("#task-status").inner_text())
        page.screenshot(path=str(SHOTS / "tasks-card-desktop.png"), full_page=True)

        print("\n== 5. An unreadable due date blocks the create, and never clears a saved one")
        create_task(page, layout, due="31.02.2026")
        alert = page.get_by_role("alert")
        check("the create form refuses 31.02.2026", alert.count() > 0 and "не распознан" in alert.first.inner_text(),
              str(alert.all_inner_texts()))
        check("the refused create stays on the list", urlsplit(page.url).path == "/tasks")
        page.screenshot(path=str(SHOTS / "tasks-create-refused-desktop.png"), full_page=True)
        page.fill("#new-task-due", "30.10.2026")
        page.fill("#new-task-estimate", "120")
        page.locator('form[aria-label="Новая задача"] button[type="submit"]').click()
        page.wait_for_url(lambda url: re.search(r"/tasks/\d+$", urlsplit(url).path) is not None, timeout=15000)
        check("a readable date creates the second task", True)
        expect(page.get_by_role("heading", level=1)).to_have_text(layout)
        check("the due date is kept", page.locator("#task-due").input_value() == "30.10.2026",
              page.locator("#task-due").input_value())
        page.fill("#task-due", "31.02.2026")
        page.locator('form[aria-label="Задача"] button[type="submit"]').click()
        expect(page.get_by_role("alert")).to_contain_text("не распознан")
        page.reload(wait_until="networkidle")
        check("a refused save leaves the saved due date", page.locator("#task-due").input_value() == "30.10.2026",
              page.locator("#task-due").input_value())

        print("\n== 6. The board: columns, and a drag across them")
        page.goto(f"{BASE}/tasks?view=board", wait_until="networkidle")
        check("the first task sits in «В работе»",
              page.locator('section[aria-label="В работе"]').locator("a", has_text=brief).count() == 1)
        check("the second task sits in «К работе»",
              page.locator('section[aria-label="К работе"]').locator("a", has_text=layout).count() == 1)
        page.locator("article", has_text=layout).drag_to(page.locator('section[aria-label="Готово"]'))
        page.wait_for_load_state("networkidle")
        page.reload(wait_until="networkidle")
        check("a drag moves the task to «Готово»",
              page.locator('section[aria-label="Готово"]').locator("a", has_text=layout).count() == 1)
        page.screenshot(path=str(SHOTS / "tasks-board-desktop.png"), full_page=True)

        print("\n== 7. The table reads the same tasks")
        page.goto(f"{BASE}/tasks", wait_until="networkidle")
        check("the table lists both tasks", page.locator("tbody tr").count() == 2)
        brief_row = page.locator("tbody tr", has_text=brief)
        check("the table shows the tracked time", "45 мин" in text_of(brief_row), text_of(brief_row))
        check("the table shows the due date", re.search(r"30 \S+ 2026", text_of(page.locator("tbody tr", has_text=layout))) is not None,
              text_of(page.locator("tbody tr", has_text=layout)))
        page.screenshot(path=str(SHOTS / "tasks-table-desktop.png"), full_page=True)

        print("\n== 8. Phone: stacked cards, the board stacks, no sideways scroll")
        page.set_viewport_size({"width": 390, "height": 844})
        page.goto(f"{BASE}/tasks", wait_until="networkidle")
        check("no sideways scroll on the task list", no_sideways_scroll(page))
        form_on_phone = page.locator("#new-task-form")
        check("on the phone the create form waits behind its button", not form_on_phone.is_visible())
        page.click('button[aria-controls="new-task-form"]')
        check("the button opens the create form", form_on_phone.is_visible())
        page.click('button[aria-controls="new-task-form"]')
        check("the tasks are cards on the phone", page.locator('main a[href^="/tasks/"]:visible').count() >= 2)
        page.screenshot(path=str(SHOTS / "tasks-table-phone.png"), full_page=True)
        page.goto(f"{BASE}/tasks?view=board", wait_until="networkidle")
        check("no sideways scroll on the board", no_sideways_scroll(page))
        page.screenshot(path=str(SHOTS / "tasks-board-phone.png"), full_page=True)
        page.goto(f"{BASE}{task_path}", wait_until="networkidle")
        check("no sideways scroll on the card", no_sideways_scroll(page))
        page.screenshot(path=str(SHOTS / "tasks-card-phone.png"), full_page=True)

        print("\n== 9. Archive takes tasks off the board")
        page.set_viewport_size({"width": 1280, "height": 900})
        page.goto(f"{BASE}/tasks", wait_until="networkidle")
        page.locator("tbody tr", has_text=layout).get_by_role("button", name="Убрать с доски").click()
        page.wait_for_load_state("networkidle")
        page.goto(f"{BASE}/tasks", wait_until="networkidle")
        check("an archived task leaves the table", page.locator("tbody tr", has_text=layout).count() == 0)
        check("the other task stays", page.locator("tbody tr", has_text=brief).count() == 1)
        page.locator("tbody tr", has_text=brief).get_by_role("button", name="Убрать с доски").click()
        page.wait_for_load_state("networkidle")
        page.goto(f"{BASE}/tasks", wait_until="networkidle")
        check("with both archived the board is empty again", page.get_by_text("Задач пока нет").count() > 0)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall tasks checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
