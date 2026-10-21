"""The project page shows the plan once, with its period.

Three accounts on one workspace, each in its own browser context:

1. The owner invites one person by link and the person joins.
2. The owner plans 10 h of the project for themselves and 5 h for the member.
3. The project page has no separate plan card. The total row under "По людям"
   reads 15 h, equal to the two rows above it. The owner also sees the tracked
   total in that row; the member does not see the tracked column at all.
4. On a 375 px phone the total row sits inside the screen, with no horizontal scroll.
5. A project with nobody planned shows the empty sentence and no total row.

Checked against the rendered page: the rows are found by the person's name, so a
number elsewhere on the page cannot make a check pass.
"""
import sys
import time
from datetime import date
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import Page, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
PASSWORD = "plan-single-qa-pass-2026"
OWNER = "План Владелец"
MEMBER = "План Участник"
PROJECT = "Один план"
EMPTY = "Пустой план"
CARD = "Спланировано за всё время"
TOTAL = "Итого за всё время"
NOBODY = "Пока никто не запланирован на этот проект."


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


def hours(text: str) -> float:
    # "10 ч" with a no-break space before the unit; the plan is whole hours here.
    return float(text.replace(" ", " ").split()[0])


def invite_token(owner: Page) -> str:
    invited = send(owner, "POST", "/api/team/invites", {})
    link = parse_qs(urlparse(invited["url"]).query).get("invite", [""])[0]
    return link.rsplit("/", 1)[-1]


def new_account(browser, name: str, email: str):
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    page = context.new_page()
    register(page, context, name, email, PASSWORD)
    return context, page


def main() -> int:
    stamp = int(time.time())
    today = date.today().isoformat()
    with sync_playwright() as p:
        browser = launch(p)
        owner_ctx, owner = new_account(browser, OWNER, f"plan-owner{stamp}@example.com")
        send(owner, "POST", "/api/team/modules", {"preset": "studio"})

        print("\n== 1. A second person joins")
        token = invite_token(owner)
        member_ctx, member = new_account(browser, MEMBER, f"plan-member{stamp}@example.com")
        check("the member joins", send(member, "POST", f"/api/invites/{token}/accept", {})["status"] in (200, 204))

        owner.goto(f"{BASE}/settings/members", wait_until="networkidle")
        people = {m["Name"]: m["UserID"] for m in page_data(owner)["Members"]}
        owner_id, member_id = people.get(OWNER, 0), people.get(MEMBER, 0)
        check("the owner sees both people", owner_id > 0 and member_id > 0, str(people))

        send(owner, "POST", "/projects/new", {"name": PROJECT})
        send(owner, "POST", "/projects/new", {"name": EMPTY})
        owner.goto(f"{BASE}/projects", wait_until="networkidle")
        projects = {row["Name"]: row for row in page_data(owner)["Projects"]}
        project = projects.get(PROJECT, {})
        empty = projects.get(EMPTY, {})
        check("both projects exist", bool(project.get("ID")) and bool(empty.get("ID")), str(list(projects)))

        print("\n== 2. The owner plans 10 h and the member 5 h on one project")
        for user_id, minutes in ((owner_id, "600"), (member_id, "300")):
            planned = send(owner, "POST", "/api/schedule/cell", {
                "user_id": str(user_id), "project_id": str(project["ID"]), "date": today, "minutes": minutes,
            })
            check(f"{minutes} minutes are planned", planned["status"] in (200, 204), str(planned["status"]))

        print("\n== 3. One plan, with its period, and a total that adds up")
        owner.goto(f"{BASE}/projects/{project['Slug']}", wait_until="networkidle")
        check("the separate plan card is gone", CARD not in owner.inner_text("body"))
        total = owner.get_by_role("row").filter(has_text=TOTAL)
        check("the total row is shown once", total.count() == 1, str(total.count()))
        owner_cells = owner.get_by_role("row").filter(has_text=OWNER).get_by_role("cell")
        member_cells = owner.get_by_role("row").filter(has_text=MEMBER).get_by_role("cell")
        total_cells = total.get_by_role("cell")
        if total.count() == 1 and owner_cells.count() and member_cells.count():
            planned_total = hours(total_cells.nth(1).inner_text())
            planned_rows = hours(owner_cells.nth(1).inner_text()) + hours(member_cells.nth(1).inner_text())
            check("the total plan is 15 h", planned_total == 15, str(planned_total))
            check("the total plan equals the people rows", planned_total == planned_rows,
                  f"total {planned_total}, rows {planned_rows}")
        check("a manager sees the tracked total in the total row", total_cells.count() == 3, str(total_cells.count()))

        member.goto(f"{BASE}/projects/{project['Slug']}", wait_until="networkidle")
        member_total = member.get_by_role("row").filter(has_text=TOTAL)
        check("a member sees the total plan", member_total.count() == 1 and "15" in member_total.inner_text())
        check("a member sees no tracked column in the total row", member_total.get_by_role("cell").count() == 2,
              str(member_total.get_by_role("cell").count()))

        print("\n== 4. The total row fits a phone")
        owner.set_viewport_size({"width": 375, "height": 800})
        owner.goto(f"{BASE}/projects/{project['Slug']}", wait_until="networkidle")
        phone_total = owner.get_by_role("row").filter(has_text=TOTAL)
        box = phone_total.bounding_box() if phone_total.count() == 1 else None
        check("the total row is on the phone screen", box is not None and box["x"] >= 0 and box["x"] + box["width"] <= 375,
              str(box))
        no_overflow = owner.evaluate("() => document.documentElement.scrollWidth <= window.innerWidth")
        check("the page has no horizontal scroll at 375 px", no_overflow)

        print("\n== 5. A project nobody planned has no total")
        owner.set_viewport_size({"width": 1280, "height": 900})
        owner.goto(f"{BASE}/projects/{empty['Slug']}", wait_until="networkidle")
        check("the empty project says nobody is planned", NOBODY in owner.inner_text("body"))
        check("the empty project has no total row", owner.get_by_role("row").filter(has_text=TOTAL).count() == 0)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall plan checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
