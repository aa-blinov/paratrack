"""What each role sees on screen: the remove button and the locked-week sentence.

Three accounts on one workspace, each in its own browser context:

1. The owner invites two people by link; both accept. The second one is made an admin.
2. The owner approves a past week on the timesheet.
3. The admin reads the members list: the owner's row has no remove button, an ordinary
   member's row has one. The admin and the member both read the approved week on the
   timesheet and each gets the sentence that fits their rights.

Checked against the rendered page: the remove button is looked for inside the member's
own row, so a button elsewhere cannot make the check pass.
"""
import sys
import time
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import Page, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
PASSWORD = "member-roles-qa-pass-2026"
OWNER = "Роли Владелец"
ADMIN = "Роли Администратор"
MEMBER = "Роли Участник"
WEEK_START, WEEK_END = "2020-03-02", "2020-03-08"
LOCKED_MEMBER = "Неделя утверждена, правки закрыты. Попросите владельца или администратора снять утверждение."
LOCKED_MANAGER = "Неделя утверждена. Чтобы править, сначала снимите утверждение."


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


def new_account(browser, name: str, email: str):
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    page = context.new_page()
    register(page, context, name, email, PASSWORD)
    return context, page


def main() -> int:
    stamp = int(time.time())
    with sync_playwright() as p:
        browser = launch(p)
        owner_ctx, owner = new_account(browser, OWNER, f"roles-owner{stamp}@example.com")
        send(owner, "POST", "/api/team/modules", {"preset": "studio"})

        print("\n== 1. Two people join; the second one becomes an admin")
        admin_token = invite_token(owner)
        member_token = invite_token(owner)
        admin_ctx, admin = new_account(browser, ADMIN, f"roles-admin{stamp}@example.com")
        check("the admin joins", send(admin, "POST", f"/api/invites/{admin_token}/accept", {})["status"] in (200, 204))
        member_ctx, member = new_account(browser, MEMBER, f"roles-member{stamp}@example.com")
        check("the member joins", send(member, "POST", f"/api/invites/{member_token}/accept", {})["status"] in (200, 204))

        owner.goto(f"{BASE}/settings/members", wait_until="networkidle")
        people = {m["Name"]: m["UserID"] for m in page_data(owner)["Members"]}
        admin_id, member_id = people.get(ADMIN, 0), people.get(MEMBER, 0)
        check("the owner sees both people", admin_id > 0 and member_id > 0, str(people))
        changed = send(owner, "POST", f"/api/team/members/{admin_id}/role", {"role": "admin"})
        check("the second person becomes an admin", changed["status"] in (200, 204), str(changed["status"]))

        print("\n== 2. The owner approves a past week")
        approved = send(owner, "POST", "/api/timesheet/approve", {"start": WEEK_START, "end": WEEK_END})
        check("the week is approved", approved["status"] in (200, 204), str(approved["status"]))

        print("\n== 3. The remove button follows the rights")
        admin.goto(f"{BASE}/settings/members", wait_until="networkidle")
        rows = admin.locator("article")
        owner_row = rows.filter(has_text=OWNER)
        member_row = rows.filter(has_text=MEMBER)
        check("the admin sees the owner's row", owner_row.count() == 1, str(owner_row.count()))
        check("the admin sees the member's row", member_row.count() == 1, str(member_row.count()))
        check("the owner's row has no remove button for the admin",
              owner_row.count() == 1 and owner_row.get_by_role("button", name="Убрать").count() == 0)
        check("an ordinary member's row has a remove button for the admin",
              member_row.count() == 1 and member_row.get_by_role("button", name="Убрать").count() == 1)

        print("\n== 4. Each role reads the approved week its own way")
        member.goto(f"{BASE}/timesheet?date={WEEK_START}", wait_until="networkidle")
        check("a member reads the sentence for members", LOCKED_MEMBER in member.inner_text("body"))
        admin.goto(f"{BASE}/timesheet?date={WEEK_START}", wait_until="networkidle")
        check("an admin reads the sentence for managers", LOCKED_MANAGER in admin.inner_text("body"))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall role checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
