#!/usr/bin/env python3
"""Creates the persona accounts, companies and sample data on a running stand.

    PARATRACK_BASE=http://127.0.0.1:8890 python3 e2e/personas/seed.py

Reproducible: the same personas.json gives the same companies, clients,
projects, sessions and approved weeks every time. A persona whose account
already exists is signed into and its data is left as it is, so a second run
does not duplicate anything. Standard library only.
"""

from __future__ import annotations

import http.cookiejar
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent
BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8890").rstrip("/")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Keep 303 answers visible: the app signals success with a redirect."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Session:
    def __init__(self) -> None:
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.jar), NoRedirect()
        )

    def csrf(self) -> str:
        self.get("/login")
        for cookie in self.jar:
            if cookie.name == "paratrack_csrf":
                return urllib.parse.unquote(cookie.value)
        raise RuntimeError("the stand did not set a CSRF cookie")

    def get(self, path: str) -> tuple[int, str]:
        try:
            with self.opener.open(BASE + path) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as err:
            return err.code, err.read().decode("utf-8", "replace")

    def post(self, path: str, form: dict[str, str], token: str) -> tuple[int, str, str]:
        data = urllib.parse.urlencode({**form, "csrf_token": token}).encode()
        req = urllib.request.Request(
            BASE + path,
            data=data,
            method="POST",
            headers={
                "Content-Type": "application/x-www-form-urlencoded",
                "X-CSRF-Token": token,
                "HX-Request": "true",
            },
        )
        try:
            with self.opener.open(req) as resp:
                return resp.status, resp.headers.get("Location", ""), resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as err:
            return err.code, err.headers.get("Location", ""), err.read().decode("utf-8", "replace")


def ensure_account(session: Session, token: str, persona: dict, password: str) -> str:
    """Registers the persona, or signs in when the account already exists."""
    status, location, body = session.post(
        "/api/register",
        {"name": persona["name"], "email": persona["email"], "password": password},
        token,
    )
    if status == 303 and location.startswith("/welcome"):
        return "registered"
    status, location, body = session.post(
        "/api/login", {"email": persona["email"], "password": password}, token
    )
    if status == 303 and not location.startswith("/login"):
        return "signed in"
    raise RuntimeError(f"{persona['email']}: cannot register or sign in ({status}): {body[:200]}")


def list_projects(session: Session) -> list[dict]:
    status, body = session.get("/api/projects")
    if status != 200:
        raise RuntimeError(f"cannot list projects ({status})")
    return json.loads(body).get("projects", [])


def seed_data(session: Session, token: str, persona: dict) -> None:
    existing = {p["name"] for p in list_projects(session)}
    if any(p["name"] in existing for p in persona["projects"]):
        return  # Already seeded: a second run changes nothing.

    session.post("/api/team/modules", {"preset": persona["preset"]}, token)
    session.post("/api/team/rename", {"name": persona["company"]}, token)
    for client in persona["clients"]:
        session.post("/clients", {"name": client}, token)
    for project in persona["projects"]:
        session.post("/projects/new", {"name": project["name"]}, token)
    slugs = {p["name"]: p["slug"] for p in list_projects(session)}
    for project in persona["projects"]:
        slug = slugs.get(project["name"], "")
        if slug and project["billable"]:
            session.post(
                f"/projects/{slug}/rate",
                {"slug": slug, "rate": str(project["rate_rub"]), "billable": "1"},
                token,
            )

    ids = {p["name"]: p["id"] for p in list_projects(session)}
    for entry in persona["sessions"]:
        form = {
            "activity": entry["activity"],
            "start": entry["start"],
            "end": entry["end"],
            "note": entry["note"],
        }
        pid = ids.get(entry["project"])
        if pid is None:
            raise RuntimeError(f"project {entry['project']!r} is missing for {persona['email']}")
        form["project_id"] = str(pid)
        session.post("/api/sessions/backfill", form, token)

    for start, end in persona["approved_weeks"]:
        session.post("/api/timesheet/approve", {"start": start, "end": end}, token)


def main() -> int:
    sys.stdout.reconfigure(encoding="utf-8")
    data = json.loads((HERE / "personas.json").read_text(encoding="utf-8"))
    password = data["password"]
    report = []
    for persona in data["personas"]:
        session = Session()
        token = session.csrf()
        state = ensure_account(session, token, persona, password)
        seed_data(session, token, persona)
        report.append(f"{persona['id']:<10} {state:<10} {persona['email']:<24} {persona['company']}")
    print("\n".join(report))
    return 0


if __name__ == "__main__":
    sys.exit(main())
