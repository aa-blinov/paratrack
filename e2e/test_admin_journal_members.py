"""The journal tells the story of a member: joined, changed role, left, still filterable.

Two accounts on a fresh workspace, each through the product's own forms and endpoints:

1. The owner invites by link; a second account accepts it and creates an API token.
2. The owner changes that member's role to admin, then removes them.
3. The journal names the member on the role change, the removal, and the token row,
   with the role spelled out; and the filter "Кто" still offers the removed member,
   narrowing the list to their own rows.

Checked against the page's data, not the rendered text, so a label change does not
silently pass.
"""
import sys
import time
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import Page, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
OWNER = "Журнал Владелец"
MEMBER = "Журнал Участник"
PASSWORD = "journal-members-qa-pass-2026"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def send(page: Page, method: str, path: str, fields: dict[str, str]) -> dict:
    """Send a form the way the app's own pages do; report the final URL after redirects."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([method, path, fields, token]) => {
            const response = await fetch(path, {
                method, credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body: new URLSearchParams({ ...fields, csrf_token: token }),
            })
            return { status: response.status, url: response.url, text: await response.text() }
        }""",
        [method, path, fields, token],
    )


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def main() -> int:
    stamp = int(time.time())
    token_name = f"member-token-{stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        owner_ctx = browser.new_context(viewport={"width": 1280, "height": 900})
        owner = owner_ctx.new_page()
        register(owner, owner_ctx, OWNER, f"journal-owner{stamp}@example.com", PASSWORD)
        send(owner, "POST", "/api/team/modules", {"preset": "studio"})

        print("\n== 1. The owner invites; a second account joins and creates a token")
        invited = send(owner, "POST", "/api/team/invites", {})
        link = parse_qs(urlparse(invited["url"]).query).get("invite", [""])[0]
        check("an invitation link is issued", "/invites/" in link, invited["url"])
        token = link.rsplit("/", 1)[-1]
        # The link leads to the accept form; the account is fresh, so it registers first.
        member_ctx = browser.new_context(viewport={"width": 1280, "height": 900})
        member = member_ctx.new_page()
        register(member, member_ctx, MEMBER, f"journal-member{stamp}@example.com", PASSWORD)
        joined = send(member, "POST", f"/api/invites/{token}/accept", {})
        check("the member joins by the link", joined["status"] in (200, 204) and "/invites/" not in joined["url"], joined["url"])
        created = send(member, "POST", "/api/tokens", {"name": token_name, "read_only": "", "expires": ""})
        check("the member creates a token", created["status"] in (200, 204), str(created["status"]))

        owner.goto(f"{BASE}/settings/members", wait_until="networkidle")
        member_id = next((m["UserID"] for m in page_data(owner)["Members"] if m["Name"] == MEMBER), 0)
        check("the owner sees the member", member_id > 0)

        print("\n== 2. The owner changes the role, then removes the member")
        changed = send(owner, "POST", f"/api/team/members/{member_id}/role", {"role": "admin"})
        check("the role is changed", changed["status"] in (200, 204), str(changed["status"]))
        removed = send(owner, "POST", f"/api/team/members/{member_id}/remove", {})
        check("the member is removed", removed["status"] in (200, 204), str(removed["status"]))

        print("\n== 3. The journal tells the story and the filter keeps the former member")
        owner.goto(f"{BASE}/settings/audit", wait_until="networkidle")
        items = page_data(owner)["Items"]
        role_rows = [row for row in items if row["Action"] == "member.role"]
        remove_rows = [row for row in items if row["Action"] == "member.remove"]
        token_rows = [row for row in items if row["Action"] == "auth.api_token_create" and row["Target"] == token_name]
        check("the role change names the member", any(r["Target"] == MEMBER and r["Detail"] == "администратор" and r["Actor"] == OWNER for r in role_rows), str(role_rows))
        check("the removal names the member and the owner as author", any(r["Target"] == MEMBER and r["Actor"] == OWNER for r in remove_rows), str(remove_rows))
        check("the member's token row names the member as author", any(r["Actor"] == MEMBER for r in token_rows), str(token_rows))

        people = page_data(owner)["People"]
        check("the filter offers the former member", any(person["ID"] == member_id and person["Name"] == MEMBER for person in people))

        owner.goto(f"{BASE}/settings/audit?user={member_id}", wait_until="networkidle")
        narrowed = page_data(owner)["Items"]
        check("the filter narrows to the member's own rows", len(narrowed) >= 1 and all(row["Actor"] == MEMBER for row in narrowed), str(narrowed))
        check("the owner's rows are left out by the filter", all(row["Actor"] != OWNER for row in narrowed))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall member journal checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
