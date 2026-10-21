"""A closed week says which week it is, and the team load report reads plan against time.

Two checks on a fresh account, each through the product's own endpoints and screens:

1. Writing into an approved week (backfill and the editor) names the week and says who can
   reopen it, instead of the generic failure.
2. The team load report over the current week counts the whole week's plan, including the
   days still to come, and shows the load of the row as a share of that plan.

Run from Tuesday on: the load check backfills a session on this week's Monday.
"""
import sys
import time
from datetime import date, timedelta

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def send(page: Page, method: str, path: str, fields: dict[str, str]) -> dict:
    """Send a form the way the app's own pages do, and return the answer as the page gets it."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([method, path, fields, token]) => {
            const response = await fetch(path, {
                method, credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body: new URLSearchParams({ ...fields, csrf_token: token }),
            })
            return { status: response.status, text: await response.text() }
        }""",
        [method, path, fields, token],
    )


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def main() -> int:
    stamp = int(time.time())
    today = date.today()
    monday = today - timedelta(days=today.weekday())
    project_name = f"Загрузка {stamp}"
    activity = f"Неделя {stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Неделя и загрузка", f"weeklock{stamp}@example.com", "week-lock-qa-pass-2026")
        send(page, "POST", "/api/team/modules", {"preset": "studio"})

        print("\n== 1. A closed week names itself and the next step")
        approve_form = {"start": "2020-03-02", "end": "2020-03-08"}
        check("the week is approved", send(page, "POST", "/api/timesheet/approve", approve_form)["status"] in (200, 204))
        into = send(page, "POST", "/api/sessions/backfill", {
            "activity": activity, "start": "2020-03-03T10:00", "end": "2020-03-03T11:00", "note": "",
        })
        want = "Неделя с 2 мар по 8 мар утверждена, правки закрыты. Попросите владельца или администратора снять утверждение."
        check("backfill names the approved week and the next step", want in into["text"], into["text"][:200])
        check("backfill no longer shows the generic failure", "Что-то пошло не так" not in into["text"])

        print("\n== 2. The team load report reads the week's plan against time")
        post = send(page, "POST", "/projects/new", {"name": project_name, "rate": "3000", "billable": "1"})
        check("the project is created", post["status"] in (200, 204), str(post["status"]))
        page.goto(f"{BASE}/settings/members", wait_until="networkidle")
        owner_id = next(m["UserID"] for m in page_data(page)["Members"] if m["Role"] == "owner")
        page.goto(f"{BASE}/projects", wait_until="networkidle")
        project_id = next(row["ID"] for row in page_data(page)["Projects"] if row["Name"] == project_name)

        friday = monday + timedelta(days=4)
        for day, minutes in ((monday, "600"), (friday, "1200")):
            cell = send(page, "POST", "/api/schedule/cell", {
                "user_id": str(owner_id), "project_id": str(project_id), "date": day.isoformat(), "minutes": minutes,
            })
            check(f"the plan of {day.isoformat()} is saved", cell["status"] in (200, 204), str(cell["status"]))

        tracked = send(page, "POST", "/api/sessions/backfill", {
            "activity": activity, "start": f"{monday.isoformat()}T08:00", "end": f"{monday.isoformat()}T20:00",
            "note": "", "project_id": str(project_id),
        })
        check("12 hours are tracked on Monday", "Что-то пошло" not in tracked["text"], tracked["text"][:200])

        page.goto(f"{BASE}/reports/run?id=utilization", wait_until="networkidle")
        load_header = page.get_by_role("columnheader", name="Загрузка")
        check("the report shows the load column", load_header.count() == 1)
        check("the report has no share column for the plan", page.get_by_role("columnheader", name="Доля").count() == 0)
        load_cell = page.get_by_role("row").filter(has_text="40%")
        expect(load_cell.first).to_be_visible()
        check("12 tracked hours against a 30 hour plan read 40%", load_cell.count() >= 1)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall week lock and load checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
