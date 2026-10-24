"""Seeds the two accounts test_login_returns_workspace signs in with.

owner2 creates a studio workspace and invites member2 by address. member2
registers, accepts the invitation and so ends up in two workspaces: the
personal one and the studio. Run it against a stand before the login test:

    PARATRACK_BASE=http://127.0.0.1:8890 .venv/bin/python e2e/seed_member2.py

Running it twice is safe: an account that already exists is signed into
instead of registered again, and an invitation to a workspace the address
already belongs to is simply not needed the second time.
"""

from __future__ import annotations

from urllib.parse import parse_qs, urlparse

from playwright.sync_api import sync_playwright

from target import BASE_URL as BASE

PASSWORD = "longenoughpw"
import os

OWNER = os.environ.get("PARATRACK_SEED_OWNER", "owner2@x.test")
MEMBER = os.environ.get("PARATRACK_SEED_MEMBER", "member2@x.test")
STUDIO = "Студия member2"


def csrf(page) -> str:
    for cookie in page.context.cookies():
        if cookie.get("name") == "paratrack_csrf":
            return cookie.get("value") or ""
    return ""


def register_or_sign_in(page, name: str, email: str) -> None:
    page.goto(BASE + "/register")
    page.fill("#name", name)
    page.fill("#email", email)
    page.fill("#password", PASSWORD)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/register" not in page.url and "/login" not in page.url:
        return
    page.goto(BASE + "/login")
    page.fill("#email", email)
    page.fill("#password", PASSWORD)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url:
        raise SystemExit(f"could not sign in as {email}: still on {page.url}")


def choose_set(page) -> None:
    # A new workspace opens on the welcome screen until a set of sections is picked.
    post(page, "/api/team/modules", {"preset": "studio", "from": "welcome"})


def post(page, path: str, form: dict, max_redirects: int = 0):
    headers = {"X-CSRF-Token": csrf(page)}
    return page.request.post(BASE + path, form=form, headers=headers, max_redirects=max_redirects)


def main() -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch()

        owner_ctx = browser.new_context()
        owner = owner_ctx.new_page()
        register_or_sign_in(owner, "Владелец member2", OWNER)
        choose_set(owner)
        created = post(owner, "/api/team/create", {"name": STUDIO})
        print(f"studio created: status={created.status}")
        choose_set(owner)

        invited = post(owner, "/api/team/invites", {"email": MEMBER})
        location = invited.headers.get("location", "")
        link = (parse_qs(urlparse(location).query).get("invite") or [""])[0]
        if not link:
            raise SystemExit(f"no invitation link in the answer: status={invited.status} location={location!r}")
        token = link.rsplit("/", 1)[-1]
        print(f"invitation issued for {MEMBER}")

        member_ctx = browser.new_context()
        member = member_ctx.new_page()
        register_or_sign_in(member, "Masha", MEMBER)
        choose_set(member)
        accepted = post(member, f"/api/invites/{token}/accept", {})
        print(f"invitation accepted: status={accepted.status}")
        browser.close()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
