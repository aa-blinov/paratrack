"""The schedule names an idle person and an overloaded one, on each person's line.

Two accounts on one workspace, each person with 40 available hours a week
(480 minutes a day, five days):

1. The owner plans 15 hours: under half of 40, so the line says "простой".
2. The member plans 45 hours: over 40, so the line says "перегрузка" and never "простой".

Checked on the rendered schedule, on the line of each person: the plan and the
available time are part of the text that the label sits beside.
"""
import re
import sys
import time
from datetime import date, timedelta
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
OWNER = "Расписание Владелец"
MEMBER = "Расписание Участник"
PASSWORD = "schedule-idle-qa-pass-2026"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def send(page: Page, method: str, path: str, fields: dict[str, str]) -> dict:
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([method, path, fields, token]) => {
            const response = await fetch(path, {
                method, credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body: new URLSearchParams({ ...fields, csrf_token: token }),
            })
            return { status: response.status, url: response.url }
        }""",
        [method, path, fields, token],
    )


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def invite_token(owner: Page) -> str:
    invited = send(owner, "POST", "/api/team/invites", {})
    link = parse_qs(urlparse(invited["url"]).query).get("invite", [""])[0]
    return link.rsplit("/", 1)[-1]


def main() -> int:
    stamp = int(time.time())
    project_name = f"Простой {stamp}"
    monday = date.today() - timedelta(days=date.today().weekday())
    weekdays = [(monday + timedelta(days=i)).isoformat() for i in range(5)]
    with sync_playwright() as p:
        browser = launch(p)
        owner_ctx = browser.new_context(viewport={"width": 1280, "height": 900})
        owner = owner_ctx.new_page()
        register(owner, owner_ctx, OWNER, f"schedule-owner{stamp}@example.com", PASSWORD)
        send(owner, "POST", "/api/team/modules", {"preset": "studio"})
        send(owner, "POST", "/projects/new", {"name": project_name, "rate": "3000", "billable": "1"})
        owner.goto(f"{BASE}/projects", wait_until="networkidle")
        project_id = next(row["ID"] for row in page_data(owner)["Projects"] if row["Name"] == project_name)

        token = invite_token(owner)
        member_ctx = browser.new_context(viewport={"width": 1280, "height": 900})
        member = member_ctx.new_page()
        register(member, member_ctx, MEMBER, f"schedule-member{stamp}@example.com", PASSWORD)
        joined = send(member, "POST", f"/api/invites/{token}/accept", {})
        check("the member joins by the link", joined["status"] in (200, 204) and "/invites/" not in joined["url"], joined["url"])

        owner.goto(f"{BASE}/settings/members", wait_until="networkidle")
        people = {m["Name"]: m["UserID"] for m in page_data(owner)["Members"]}
        owner_id, member_id = people.get(OWNER, 0), people.get(MEMBER, 0)
        check("the owner sees both people", owner_id > 0 and member_id > 0, str(people))

        print("\n== Capacity: 480 minutes a day for each person")
        for user_id in (owner_id, member_id):
            saved = send(owner, "POST", "/api/member/pay", {"user_id": str(user_id), "hourly_pay": "3000", "capacity_minutes": "480"})
            check(f"capacity is set for user {user_id}", saved["status"] in (200, 204), str(saved["status"]))

        print("\n== Plans: the owner 15 hours, the member 45 hours")
        for day in weekdays[:3]:
            send(owner, "POST", "/api/schedule/cell", {"user_id": str(owner_id), "project_id": str(project_id), "date": day, "minutes": "300"})
        for day in weekdays:
            send(owner, "POST", "/api/schedule/cell", {"user_id": str(member_id), "project_id": str(project_id), "date": day, "minutes": "540"})

        owner.goto(f"{BASE}/schedule", wait_until="networkidle")
        # The desktop table is the visible one; the stacked list repeats the text hidden.
        owner_line = owner.get_by_role("row", name=re.compile(r"план 15 ч из 40 ч")).first
        member_line = owner.get_by_role("row", name=re.compile(r"план 45 ч из 40 ч")).first
        expect(owner_line).to_be_visible()
        expect(member_line).to_be_visible()
        owner_text = owner_line.inner_text()
        member_text = member_line.inner_text()
        print(f"  owner line: {owner_text!r}")
        print(f"  member line: {member_text!r}")
        check("the owner's line shows the idle word and 37%", "простой" in owner_text and "37%" in owner_text and "перегрузка" not in owner_text, owner_text)
        check("the member's line shows the overload word and no idle word", "перегрузка" in member_text and "простой" not in member_text, member_text)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall schedule label checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
