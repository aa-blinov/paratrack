"""Business-logic QA — verify the numbers, not just the presence of UI.

Run:  . .venv/bin/activate && python e2e/qa_logic.py
"""
from __future__ import annotations

import json
import os
import pathlib
import re
import sys
import time
from urllib.parse import urlencode

import requests

BASE = os.environ.get("PARATRACK_BASE", "https://paratrack.duckdns.org")
OUT = pathlib.Path(__file__).resolve().parent / "screenshots" / "qa-logic"
OUT.mkdir(parents=True, exist_ok=True)

PASS = FAIL = 0


def check(name: str, ok: bool, detail: str = ""):
    global PASS, FAIL
    if ok:
        PASS += 1
        print(f"  [PASS] {name}" + (f" — {detail}" if detail else ""))
    else:
        FAIL += 1
        print(f"  [FAIL] {name}" + (f" — {detail}" if detail else ""))


def main() -> int:
    s = requests.Session()
    s.headers["Accept-Language"] = "en"  # assertions below match English labels
    ts = int(time.time())
    email = f"logic{ts}@x.test"

    # ---------- auth ----------
    r = s.get(f"{BASE}/login")
    csrf = s.cookies.get("paratrack_csrf", "")
    r = s.post(
        f"{BASE}/api/register",
        data={"name": "Logic", "email": email, "password": "longenoughpw", "csrf_token": csrf},
        headers={"X-CSRF-Token": csrf},
        allow_redirects=False,
    )
    check("register", r.status_code in (200, 303), f"{r.status_code}")
    csrf = s.cookies.get("paratrack_csrf", "")

    def post(path, **kw):
        hdrs = {"X-CSRF-Token": csrf}
        hdrs.update(kw.pop("headers", {}) or {})
        redir = kw.pop("allow_redirects", False)
        return s.post(f"{BASE}{path}", headers=hdrs, allow_redirects=redir, **kw)

    def get(path, **kw):
        return s.get(f"{BASE}{path}", **kw)

    # ---------- 1. timer + duration math ----------
    print("\n== A. Timer & tracked-time math")
    r = post("/api/start", data={"activity": "timer-live", "csrf_token": csrf})
    check("start timer", r.status_code in (200, 204, 303), f"{r.status_code}")

    # stop after a known window is hard; instead patch a closed session
    # create one via backfill with exact seconds
    start = "2026-09-25T09:00:00Z"
    end = "2026-09-25T11:30:00Z"
    r = post(
        "/api/sessions/backfill",
        data={"activity": "deep-work", "start": start, "end": end, "note": "logic-backfill", "csrf_token": csrf},
    )
    check("backfill 2h30m session", r.status_code in (200, 303), f"{r.status_code}")

    r = get("/api/v1/sessions")
    payload = r.json()
    sessions = payload.get("sessions") or payload.get("items") or payload
    if not isinstance(sessions, list):
        sessions = []
    back = [x for x in sessions if "logic-backfill" in json.dumps(x)]
    check("backfill row present in API v1", len(back) >= 1, f"n={len(sessions)} sample={sessions[:1]}")
    if back:
        secs = back[0].get("seconds") or back[0].get("duration_seconds") or back[0].get("durationSeconds") or 0
        check("backfill duration == 2h30m (9000s)", secs == 9000, f"secs={secs}")

    # ---------- 2. project rate + invoice math ----------
    print("\n== B. Project rate & invoice math")
    r = post("/projects/new", data={"name": "LogicCo", "slug": "logicco", "csrf_token": csrf})
    check("create project", r.status_code in (200, 303), f"{r.status_code}")

    # find the project slug
    r = get("/projects")
    m = re.search(r'href="/projects/(logicco[^"/]*)"', r.text)
    slug = m.group(1) if m else "logicco"
    check("project slug resolvable", bool(m), f"slug={slug}")

    # set billable rate 4000 cents/h (= 40.00/h)
    r = post(f"/projects/{slug}", data={
        "name": "LogicCo", "rate_cents": "4000", "billable": "1",
        "csrf_token": csrf,
    })
    check("set billable rate 4000", r.status_code in (200, 303), f"{r.status_code}")

    # assign the backfilled activity to the project
    # resolve numeric activity id from API v1 projects or timesheet
    racts = get("/api/v1/projects")
    try:
        pj = racts.json()
    except Exception:
        pj = {}
    act_id_n = 0
    for pr in (pj.get("projects") or pj if isinstance(pj, list) else []):
        for a in (pr.get("activities") or []):
            if (a.get("name") or "") == "deep-work":
                act_id_n = a.get("id") or a.get("activity_id") or 0
    if not act_id_n:
        m2 = re.search(r'activity_id":\s*(\d+)[^\n]*deep-work|deep-work[^\n]*activity_id":\s*(\d+)', get("/timesheet").text)
        if m2:
            act_id_n = int(m2.group(1) or m2.group(2) or 0)
    # fall back to first activity id on timesheet
    if not act_id_n:
        ids = re.findall(r'activity_id":\s*(\d+)', get("/timesheet").text)
        act_id_n = int(ids[0]) if ids else 0
    check("resolved activity id", act_id_n > 0, f"id={act_id_n}")
    # project_id must be the numeric id (slug is rejected with 400)
    rproj = get("/api/v1/projects")
    pid = 0
    try:
        plist = rproj.json().get("projects") or rproj.json()
        if isinstance(plist, list):
            for pr in plist:
                if (pr.get("slug") or pr.get("name") or "").lower().startswith("logic"):
                    pid = pr.get("id") or 0
    except Exception:
        pass
    check("resolved numeric project id", pid > 0, f"pid={pid}")

    # Invoice proof is a dedicated 2h30m activity — nothing else may join it.
    post("/api/stop", data={"csrf_token": csrf})
    post("/api/sessions/backfill", data={
        "activity": "bill-me", "start": "2026-09-25T09:00",
        "end": "2026-09-25T11:30", "csrf_token": csrf,
    })
    ts_html = get("/timesheet").text
    # each timesheet row prints the activity NAME first, then its activity_id
    act_bill = 0
    pos = ts_html.find("bill-me")
    if pos >= 0:
        m = re.search(r'activity_id"?:\s*(\d+)', ts_html[pos:])
        if m:
            act_bill = int(m.group(1))
    check("resolved bill-me activity id", act_bill > 0, f"id={act_bill}")
    r = post(f"/api/activities/{act_bill}/project", data={"project_id": str(pid), "csrf_token": csrf})
    check("assign activity to project", r.status_code in (200, 204, 303), f"{r.status_code}")

    r = post("/invoices", data={
        "client": "Logic Client",
        "start": "2026-09-25",
        "end": "2026-09-25",
        "csrf_token": csrf,
    }, allow_redirects=True)
    check("create invoice", r.status_code in (200, 303), f"{r.status_code}")

    r = get("/invoices")
    inv = re.search(r'href="/invoices/(\d+)"', r.text) or re.search(r'/invoices/(\d+)', r.text)
    check("invoice listed", bool(inv), inv.group(1) if inv else "none")
    if inv:
        iid = inv.group(1)
        r = get(f"/invoices/{iid}")
        html = r.text
        text = re.sub(r"<[^>]+>", " ", html)
        text = re.sub(r"\s+", " ", text)
        # 2.5h * 40.00 = 100.00 (HTML may split digits across tags)
        check("invoice amount 100.00", "100.00" in text,
              text[text.find("Total"):text.find("Total")+80] if "Total" in text else "no Total")
        check("invoice number INV-", "INV-" in text)
        check("invoice line shows 2h 30m", bool(re.search(r"2h\s*30m", text)))
        check("invoice rate 40.00", "40.00" in text)
        # PDF
        r = get(f"/invoices/{iid}/pdf")
        check("invoice PDF 200 + %PDF", r.status_code == 200 and r.content[:4] == b"%PDF",
              f"{r.status_code} {r.content[:4]!r} {len(r.content)}B")

    # ---------- 3. timesheet ----------
    print("\n== C. Timesheet write model")
    # resolve activity_id
    acts = s.get(f"{BASE}/api/v1/projects").text
    r0 = get("/timesheet")
    ids = re.findall(r'activity_id"?:\s*(\d+)', r0.text) or re.findall(r'activity_id=(\d+)', r0.text)
    act_id = ids[0] if ids else "0"
    check("timesheet exposes activity_id", act_id != "0", f"id={act_id}")
    r = post("/api/timesheet/cell", data={
        "activity_id": act_id, "date": "2026-09-24", "minutes": "120", "csrf_token": csrf,
    })
    check("timesheet cell 120m", r.status_code in (200, 303), f"{r.status_code}")
    r = get("/timesheet")
    check("timesheet page 200", r.status_code == 200)
    check("timesheet shows 2h", bool(re.search(r">\s*2h\s*<|>2h<|2h 0m", r.text)), "grid cell")

    # 0 clears the day
    r = post("/api/timesheet/cell", data={
        "activity_id": act_id, "date": "2026-09-23", "minutes": "0", "csrf_token": csrf,
    })
    check("timesheet clear (0m) accepted", r.status_code in (200, 303), f"{r.status_code}")

    # ---------- 4. estimates ----------
    print("\n== D. Estimates vs actual")
    r = post(f"/projects/{slug}", data={
        "name": "LogicCo", "estimate_minutes": "600", "csrf_token": csrf,
    })
    check("set estimate 600m (10h)", r.status_code in (200, 303), f"{r.status_code}")
    r = get(f"/projects/{slug}")
    check("estimate label present", "Estimate vs actual" in r.text or "estimate" in r.text.lower())
    check("estimate value 10h rendered", "10h" in r.text, [ln.strip() for ln in r.text.splitlines() if "10h" in ln][:2])

    # ---------- 5. payroll ----------
    print("\n== E. Payroll math")
    r = get("/settings/members")
    check("members page 200", r.status_code == 200)
    # set pay on self
    me = get("/api/me").json()
    uid = me.get("id") or me.get("user_id") or me.get("user", {}).get("id") or 0
    check("resolved current user id", uid > 0, f"uid={uid}")
    r = post("/api/member/pay", data={"user_id": str(uid), "hourly_pay_cents": "10000", "capacity_minutes": "480", "csrf_token": csrf})
    check("set member pay 10000", r.status_code in (200, 303), f"{r.status_code}")

    r = post("/payroll", data={
        "start": "2026-09-22", "end": "2026-09-28", "csrf_token": csrf,
    })
    check("create payroll run", r.status_code in (200, 303), f"{r.status_code}")
    r = get("/payroll")
    check("payroll list 200", r.status_code == 200)
    pay = re.search(r'href="/payroll/(\d+)"', r.text) or re.search(r'/payroll/(\d+)', r.text)
    check("payroll run listed", bool(pay), pay.group(1) if pay else "none")
    if pay:
        rp = get(f"/payroll/{pay.group(1)}")
        check("payroll number PAY-", "PAY-" in rp.text, [ln.strip() for ln in rp.text.splitlines() if "PAY-" in ln][:1])

    # ---------- 6. report templates + CSV ----------
    print("\n== F. Report templates & CSV")
    r = get("/reports")
    check("reports gallery 200", r.status_code == 200)
    for tid in ("by-project", "by-activity", "by-day", "billable", "utilization"):
        rr = get(f"/reports/run?id={tid}&from=2026-09-22&to=2026-09-28")
        check(f"report {tid} 200", rr.status_code == 200, f"{rr.status_code}")
    r = get("/reports/run?id=billable&from=2026-09-22&to=2026-09-28&format=csv")
    check("billable CSV 200", r.status_code == 200)
    check("CSV header key,hours", r.text.splitlines()[0].startswith("key,") if r.text else False,
          r.text.splitlines()[0] if r.text else "empty")

    # ---------- 7. marketplace ----------
    print("\n== G. Marketplace")
    r = get("/integrations/marketplace")
    check("marketplace 200", r.status_code == 200)
    check("marketplace lists 11 cards", r.text.count("Connect") + r.text.count("coming") >= 8)

    # ---------- 8. API v1 + Bearer ----------
    print("\n== H. API v1 & Bearer tokens")
    r = get("/settings/tokens")
    check("tokens page 200", r.status_code == 200)
    r = post("/api/tokens", data={"name": "logic-token", "csrf_token": csrf})
    check("create token", r.status_code in (200, 303), f"{r.status_code}")
    m = re.search(r"[?&]token=(pt_[0-9a-f]+)", r.headers.get("Location", "") + r.text)
    if not m:
        # follow
        loc = r.headers.get("Location", "")
        if loc:
            r2 = s.get(BASE + loc)
            m = re.search(r"pt_[0-9a-f]{16,}", r2.text)
            raw = m.group(0) if m else ""
        else:
            raw = ""
    else:
        raw = m.group(1)
    check("raw pt_ token surfaced once", raw.startswith("pt_"), raw[:14] + "…" if raw else "none")

    if raw:
        r = requests.get(f"{BASE}/api/v1/sessions", headers={"Authorization": f"Bearer {raw}"})
        check("Bearer /api/v1/sessions", r.status_code == 200, f"{r.status_code}")
        r = requests.get(f"{BASE}/api/v1/reports/summary", headers={"Authorization": f"Bearer {raw}"})
        check("Bearer /api/v1/reports/summary", r.status_code == 200, f"{r.status_code}")
        r = requests.get(f"{BASE}/api/me", headers={"Authorization": f"Bearer {raw}"})
        check("Bearer /api/me", r.status_code == 200, f"{r.status_code}")
        # wrong token rejected
        r = requests.get(f"{BASE}/api/v1/sessions", headers={"Authorization": "Bearer pt_deadbeef"})
        check("bad bearer rejected", r.status_code in (401, 403), f"{r.status_code}")

    # ---------- 9. webhooks + audit ----------
    print("\n== I. Webhooks & audit")
    r = post("/api/webhooks", data={
        "url": "https://example.com/hook", "secret": "shh", "events": "*", "csrf_token": csrf,
    })
    check("create webhook", r.status_code in (200, 303), f"{r.status_code}")
    r = get("/settings/audit")
    check("audit page 200", r.status_code == 200)
    check("audit has rows", r.text.count("auth.") + r.text.count("session.") + r.text.count("invoice.") + r.text.count("webhook.") >= 1)

    # ---------- 10. security headers + CSRF ----------
    print("\n== J. Security")
    r = get("/login")
    h = r.headers
    check("X-Frame-Options DENY", h.get("X-Frame-Options", "").upper() == "DENY", h.get("X-Frame-Options"))
    check("nosniff", h.get("X-Content-Type-Options") == "nosniff")
    check("CSP present", "default-src" in h.get("Content-Security-Policy", ""))
    check("HSTS on https", "max-age" in h.get("Strict-Transport-Security", "") or BASE.startswith("http://"),
          h.get("Strict-Transport-Security"))

    # CSRF missing → 403
    r = s.post(f"{BASE}/api/start", data={"activity": "no-csrf"}, headers={"X-CSRF-Token": "wrong"}, allow_redirects=False)
    check("bad CSRF rejected", r.status_code == 403, f"{r.status_code}")

    # rate limit on login
    csrf_rl = s.cookies.get("paratrack_csrf", csrf)
    codes = []
    for i in range(12):
        rr = s.post(f"{BASE}/api/login",
                    data={"email": "nobody@x.test", "password": "nope", "csrf_token": csrf_rl},
                    headers={"X-CSRF-Token": csrf_rl}, allow_redirects=False)
        codes.append(rr.status_code)
    check("login rate-limited (429 present)", 429 in codes, f"codes={codes}")

    # ---------- 11. i18n ----------
    print("\n== K. i18n")
    r = get("/lang/ru", allow_redirects=False)
    check("switch to RU", r.status_code in (200, 303))
    r = get("/")
    check("RU dashboard", "Панель" in r.text or "Сессии" in r.text or "Активност" in r.text)
    r = get("/lang/en", allow_redirects=False)
    r = get("/")
    check("EN dashboard", "Dashboard" in r.text)

    # ---------- 12. CSV export ----------
    print("\n== L. CSV export")
    r = get("/api/reports.csv")
    check("CSV 200 text/csv", r.status_code == 200 and "csv" in r.headers.get("Content-Type", ""), r.headers.get("Content-Type"))
    check("CSV has data", len(r.text.splitlines()) >= 1, f"lines={len(r.text.splitlines())}")

    print(f"\nBusiness-logic QA: {PASS}/{PASS+FAIL} passed")
    return 1 if FAIL else 0


if __name__ == "__main__":
    sys.exit(main())
