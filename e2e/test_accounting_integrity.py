"""Money and the timesheet tell the truth.

Three checks on a fresh account, each through the product's own forms:

1. A draft invoice notices that its sessions moved away from its lines, names both
   hours, and a rebuild brings the lines back to the sessions.
2. A finished week is approved, then the approval is taken back. Each step answers
   on the timesheet itself.
3. A week a pay run covers cannot be reopened, and the refusal says why.

Each run uses its own past week, so pay runs left by earlier runs do not interfere.
"""
import re
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


def post_form(page: Page, path: str, fields: dict[str, str], method: str = "POST") -> dict:
    """Send a form the way the app's own pages do, following redirects."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([path, fields, token, method]) => {
            const body = new URLSearchParams({ ...fields, csrf_token: token })
            const response = await fetch(path, {
                method, credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body,
            })
            return { status: response.status, url: response.url }
        }""",
        [path, fields, token, method],
    )


def page_data(page: Page) -> dict:
    """The data the server handed to the React page, as the page reads it."""
    return page.evaluate(
        """() => JSON.parse(document.getElementById("react-page-data").textContent).data"""
    )


def main() -> int:
    stamp = int(time.time())
    week_monday = date(2000, 1, 3) + timedelta(weeks=stamp % 1000)
    week_sunday = week_monday + timedelta(days=6)
    day = date.today()
    project_name = f"Счета {stamp}"
    activity = f"Правка счёта {stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Счета и табель", f"accounting{stamp}@example.com", "accounting-qa-pass-2026")
        post_form(page, "/api/team/modules", {"preset": "studio"})

        print("\n== 1. A draft invoice notices that its sessions moved, and a rebuild clears it")
        post_form(page, "/projects/new", {"name": project_name, "rate": "3000", "billable": "1"})
        page.goto(f"{BASE}/projects", wait_until="networkidle")
        project_id = next(row["ID"] for row in page_data(page)["Projects"] if row["Name"] == project_name)
        check("the project exists with a rate", project_id > 0, str(project_id))

        started = post_form(page, "/api/start", {"activity": activity, "project_id": str(project_id)})
        check("the timer starts on the project", started["status"] in (200, 204), str(started["status"]))
        post_form(page, "/api/active/stop-all", {})
        page.goto(f"{BASE}/stats", wait_until="networkidle")
        session_id = next(s["ID"] for s in page_data(page)["Sessions"] if s["ActivityName"] == activity)

        window = {"start_at": f"{day}T09:00", "end_at": f"{day}T11:00", "duration": "", "note": ""}
        post_form(page, f"/api/sessions/{session_id}", window, method="PATCH")
        created = post_form(page, "/invoices", {
            "project_id": str(project_id), "client": "Клиент счёта",
            "start": (day - timedelta(days=1)).isoformat(), "end": (day + timedelta(days=1)).isoformat(), "notes": "",
            "confirm_unapproved": "1",
        })
        invoice = re.search(r"/invoices/(\d+)", created["url"])
        check("the invoice is created as a draft", invoice is not None, created["url"])
        invoice_id = invoice.group(1) if invoice else "0"

        post_form(page, f"/api/sessions/{session_id}", {**window, "end_at": f"{day}T10:00"}, method="PATCH")
        page.goto(f"{BASE}/invoices/{invoice_id}", wait_until="networkidle")
        drift = page.get_by_role("status").filter(has_text="Табель изменился: в счёте 2,00 ч, учтено 1,00 ч.")
        expect(drift).to_be_visible()
        check("the draft names both hours", drift.count() == 1)

        post_form(page, f"/invoices/{invoice_id}/rebuild", {})
        page.goto(f"{BASE}/invoices/{invoice_id}", wait_until="networkidle")
        check("the rebuild clears the drift", page.get_by_role("status").filter(has_text="Табель изменился").count() == 0)

        print("\n== 2. A finished week is approved, then the approval is taken back")
        # Tracked time in the week, added before it is approved: a pay run needs it.
        post_form(page, "/api/sessions/backfill", {
            "activity": f"Выплата {stamp}", "start": f"{week_monday.isoformat()}T10:00",
            "end": f"{week_monday.isoformat()}T12:00", "note": "",
        })
        approve = post_form(page, "/api/timesheet/approve", {"start": week_monday.isoformat(), "end": week_sunday.isoformat()})
        check("the week is approved", approve["status"] in (200, 204), str(approve["status"]))
        page.goto(f"{BASE}/timesheet?date={week_monday.isoformat()}", wait_until="networkidle")
        page.get_by_role("button", name="Снять утверждение").first.click()
        page.wait_for_load_state("networkidle")
        answer = page.get_by_role("status").filter(has_text="Утверждение снято, неделю можно править.").first
        expect(answer).to_be_visible()
        check("the reopen is answered above the week", answer.count() == 1)

        print("\n== 3. A week a pay run covers cannot be reopened, and the refusal says why")
        post_form(page, "/api/timesheet/approve", {"start": week_monday.isoformat(), "end": week_sunday.isoformat()})
        # A pay run needs someone with an hourly rate: the owner gets one.
        page.goto(f"{BASE}/settings/members", wait_until="networkidle")
        owner_id = page_data(page)["User"]["ID"]
        post_form(page, "/api/member/pay", {"user_id": str(owner_id), "hourly_pay": "1000"})
        post_form(page, "/payroll", {"start": week_monday.isoformat(), "end": week_sunday.isoformat(), "notes": ""})
        page.goto(f"{BASE}/timesheet?date={week_monday.isoformat()}", wait_until="networkidle")
        page.get_by_role("button", name="Снять утверждение").first.click()
        page.wait_for_load_state("networkidle")
        refusal = page.get_by_role("alert").filter(has_text="По этой неделе уже есть выплата, утверждение снять нельзя.")
        expect(refusal).to_be_visible()
        check("the refusal names the pay run", refusal.count() == 1)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall accounting checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
