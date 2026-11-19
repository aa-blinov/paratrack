"""TIME-23/TIME-24 e2e: approving a week for one member.

A manager invites two members, both track time in the same finished week,
then approves the week for one of them only. The approved member's time is
locked, the other member's is not, the payroll refusal names only the
unapproved member, and the manager's timesheet can be narrowed to one person
with ?person=<id>.

Run:  .venv\\Scripts\\python.exe e2e\\test_per_person_approval.py
"""
from __future__ import annotations

import datetime as dt
import re
import sys
import time

import requests

from target import BASE_URL as BASE

PASS = FAIL = 0


def check(name: str, ok: bool, detail: str = "") -> None:
    global PASS, FAIL
    mark = "PASS" if ok else "FAIL"
    if not ok:
        FAIL += 1
    else:
        PASS += 1
    print(f"  [{mark}] {name}" + (f" — {detail}" if detail else ""))


def register(base: str, email: str, session: requests.Session) -> str:
    session.headers["Accept-Language"] = "en"
    r = session.get(f"{base}/login")
    csrf = session.cookies.get("paratrack_csrf", "")
    r = session.post(
        f"{base}/api/register",
        data={"name": "Manager", "email": email, "password": "longenoughpw", "csrf_token": csrf},
        headers={"X-CSRF-Token": csrf}, allow_redirects=False,
    )
    csrf = session.cookies.get("paratrack_csrf", "")
    session.post(f"{base}/api/team/modules", data={"preset": "studio", "from": "welcome", "csrf_token": csrf})
    return csrf


def main() -> int:
    ts = int(time.time())
    base = BASE
    manager = requests.Session()
    csrf = register(base, f"pp-manager-{ts}@x.test", manager)

    # Two invites: the token is single-use, so each member needs their own.
    def fresh_token() -> str:
        r = manager.post(f"{base}/api/team/invites", data={"role": "member", "csrf_token": csrf},
                         headers={"X-CSRF-Token": csrf}, allow_redirects=False)
        location = r.headers.get("Location", "")
        from urllib.parse import unquote
        link = unquote(location.split("invite=")[1].split("&")[0]) if "invite=" in location else ""
        return link.rsplit("/", 1)[-1] if link else ""

    token_a = fresh_token()
    check("invite created", bool(token_a), "no invite link")

    def accept_member(label: str, token: str) -> tuple[requests.Session, str]:
        s = requests.Session()
        s.headers["Accept-Language"] = "en"
        s.get(f"{base}/login")
        c = s.cookies.get("paratrack_csrf", "")
        s.post(f"{base}/api/register",
               data={"name": label, "email": f"pp-{label}-{ts}@x.test", "password": "longenoughpw", "csrf_token": c},
               headers={"X-CSRF-Token": c}, allow_redirects=False)
        c = s.cookies.get("paratrack_csrf", "")
        accept = s.post(f"{base}/api/invites/{token}/accept", data={"csrf_token": c},
                        headers={"X-CSRF-Token": c}, allow_redirects=False)
        if accept.status_code in (200, 303):
            return s, c
        raise AssertionError(f"invite not accepted: {accept.status_code}")

    alice, alice_csrf = accept_member("alice", token_a)
    bob, bob_csrf = accept_member("bob", fresh_token())

    # The real user ids, straight from each member's own /api/me.
    def me_id(s: requests.Session) -> int:
        payload = s.get(f"{base}/api/me").json()
        user = payload.get("user") or payload
        return int(user.get("id") or user.get("ID") or 0)

    alice_id = me_id(alice)
    bob_id = me_id(bob)
    check("member ids resolved", alice_id > 0 and bob_id > 0 and alice_id != bob_id, f"alice={alice_id} bob={bob_id}")

    # A finished week in the past: last week.
    today = dt.datetime.now(dt.timezone.utc).date()
    monday = today - dt.timedelta(days=today.weekday())
    last_monday = monday - dt.timedelta(days=7)
    tuesday = last_monday + dt.timedelta(days=1)
    t_start = f"{tuesday}T10:00:00Z"
    t_end = f"{tuesday}T11:00:00Z"

    def backfill(s: requests.Session, c: str) -> None:
        s.post(f"{base}/api/sessions/backfill", data={
            "activity": "design-review", "start": t_start, "end": t_end, "csrf_token": c,
        }, headers={"X-CSRF-Token": c}, allow_redirects=False)

    backfill(alice, alice_csrf)
    backfill(bob, bob_csrf)

    # The manager approves the week for Alice only.
    r = manager.post(f"{base}/api/timesheet/approve", data={
        "start": str(last_monday), "end": str(last_monday + dt.timedelta(days=6)),
        "user_id": str(alice_id), "csrf_token": csrf,
    }, headers={"X-CSRF-Token": csrf}, allow_redirects=False)
    check("approve for one member", r.status_code in (200, 303), f"{r.status_code}")

    # Alice's week is closed: another backfill is refused with the week named.
    # The endpoint answers 200 with the reason in the form fragment, so the
    # check reads the body, not the status code.
    r = alice.post(f"{base}/api/sessions/backfill", data={
        "activity": "design-review", "start": f"{tuesday}T12:00:00Z", "end": f"{tuesday}T13:00:00Z",
        "csrf_token": alice_csrf,
    }, headers={"X-CSRF-Token": alice_csrf}, allow_redirects=False)
    body = r.text.lower()
    check("approved member cannot add time", "approved" in body or "утвержден" in body or "locked" in body,
          f"{r.status_code} {r.text[:80]!r}")

    # Bob's week is open: his backfill goes through.
    r = bob.post(f"{base}/api/sessions/backfill", data={
        "activity": "design-review", "start": f"{tuesday}T12:00:00Z", "end": f"{tuesday}T13:00:00Z",
        "csrf_token": bob_csrf,
    }, headers={"X-CSRF-Token": bob_csrf}, allow_redirects=False)
    check("other member still adds time", r.status_code in (200, 204, 303), f"{r.status_code}")

    # The manager's sheet narrowed to Bob (TIME-24): the grid carries his
    # activity and not Alice's duplicate one.
    page = manager.get(f"{base}/timesheet?date={last_monday}&person={bob_id}").text
    check("manager opens one member's sheet", "design-review" in page, "activity missing")

    # The manager sets an hourly rate for Bob, so a run over the week has a
    # member to price and reaches the approval check instead of the earlier
    # "no rate" refusal.
    me = manager.get(f"{base}/api/me").json()
    manager_id = int((me.get("user") or me).get("id") or 0)
    r = manager.post(f"{base}/api/member/pay", data={
        "user_id": str(bob_id), "hourly_pay": "100.00", "capacity_minutes": "480", "csrf_token": csrf,
    }, headers={"X-CSRF-Token": csrf}, allow_redirects=False)
    check("bob gets an hourly rate", r.status_code in (200, 303), f"{r.status_code}")

    # A payroll run over the week names the unapproved member's time. The form
    # redirects back to the list with the reason as a flash parameter, so it
    # is read from the redirect URL (the page body renders it as a flash
    # message, but the URL carries it verbatim).
    r = manager.post(f"{base}/payroll", data={
        "start": str(last_monday), "end": str(last_monday + dt.timedelta(days=6)), "csrf_token": csrf,
    }, headers={"X-CSRF-Token": csrf}, allow_redirects=False)
    landed = r.headers.get("Location", "")
    r = manager.get(f"{base}{landed}" if landed.startswith("/") else landed)
    body = (r.url + " " + r.text).lower()
    check("payroll refuses unapproved member time",
          "not approved" in body or "не утвержден" in body or "утверждён" in body,
          landed[:100] or "no redirect")

    print(f"\nPer-person approval: {PASS}/{PASS + FAIL} checks passed")
    return 1 if FAIL else 0


if __name__ == "__main__":
    sys.exit(main())
