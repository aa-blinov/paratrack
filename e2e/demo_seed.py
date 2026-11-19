"""Seed a demo account with thirty days of life.

Creates a fresh account (studio preset, every section), two clients, three
projects with rates and colours, a month of weekday sessions in two shifts
with notes and tags, goals, board tasks, a plan for the current week, an
approved completed week, one paid invoice over that week and one paid payout
run. Prints the credentials and a short summary at the end.

Run:  python e2e/demo_seed.py [email-suffix]
BASE: PARATRACK_BASE, default http://127.0.0.1:8888.

The style follows qa_logic.py: one requests.Session, forms with csrf_token
and the X-CSRF-Token header, no browser. Idempotency by day does not matter —
every run registers a new account, and every date is computed from today.
"""
from __future__ import annotations

import datetime as dt
import os
import random
import re
import sys
import time

import requests

BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8888").rstrip("/")
PASSWORD = "paratrack-demo"

PASS = FAIL = 0


def check(name: str, ok: bool, detail: str = ""):
    global PASS, FAIL
    if ok:
        PASS += 1
        print(f"  [PASS] {name}" + (f" — {detail}" if detail else ""))
    else:
        FAIL += 1
        print(f"  [FAIL] {name}" + (f" — {detail}" if detail else ""))
    return ok


# ---------------------------------------------------------------------------
# Dates: everything hangs off today, UTC, the way qa_logic.py computes its
# windows. The completed week is the one before the week that is running now:
# its hours can be approved, billed and paid while today is still being worked.
now = dt.datetime.now(dt.timezone.utc)
today = now.date()
week_start = today - dt.timedelta(days=today.weekday())            # Monday
last_monday = week_start - dt.timedelta(days=7)
LW_FROM = str(last_monday)
LW_TO = str(last_monday + dt.timedelta(days=6))                    # last Sunday


def shift(day: dt.date, start: str, end: str) -> tuple[str, str]:
    """The wall-clock moments of one work shift on a day, ISO with a Z."""
    return f"{day}T{start}:00Z", f"{day}T{end}:00Z"


# The work itself: activity name, project slug, and the note a person would
# leave. The pool keeps the month readable without a hundred activities.
WORK = [
    ("Разработка", "client-portal", "Форма заявки и валидация"),
    ("Разработка", "client-portal", "Личный кабинет: загрузка документов"),
    ("Дизайн", "logo-master", "Три варианта логотипа, шрифт и палитра"),
    ("Правки", "logo-master", "Правки по ревью: сетка и отступы"),
    ("Планирование", "internal", "Оценка задач на неделю"),
    ("Созвоны", "internal", "Еженедельный созвон команды"),
]

TAGS = ["дедлайн", "ревью", "созвон"]


def main() -> int:
    suffix = sys.argv[1] if len(sys.argv) > 1 else str(int(time.time()))
    email = f"demo-{suffix}@x.test"

    s = requests.Session()
    r = s.get(f"{BASE}/login")
    csrf = s.cookies.get("paratrack_csrf", "")

    def post(path, **kw):
        nonlocal csrf
        hdrs = {"X-CSRF-Token": csrf}
        hdrs.update(kw.pop("headers", {}) or {})
        data = kw.pop("data", {}) or {}
        data.setdefault("csrf_token", csrf)
        redir = kw.pop("allow_redirects", True)
        resp = s.post(f"{BASE}{path}", data=data, headers=hdrs, allow_redirects=redir, **kw)
        csrf = s.cookies.get("paratrack_csrf", csrf)
        return resp

    def get(path, **kw):
        return s.get(f"{BASE}{path}", **kw)

    print(f"== demo_seed: {email} against {BASE}")

    # ---------- account ----------
    r = post("/api/register", data={"name": "Демо Студия", "email": email, "password": PASSWORD},
             allow_redirects=False)
    check("register", r.status_code in (200, 303), f"{r.status_code}")
    r = post("/api/team/modules", data={"preset": "studio", "from": "welcome"}, allow_redirects=False)
    check("choose sections (studio preset)", r.status_code in (200, 303), f"{r.status_code}")

    # ---------- clients ----------
    print("\n== clients")
    for name, mail, details, notes in [
        ("ООО Ромашка", "buh@romashka.ru", "ИНН 7707083893, КПП 770701001, 127006, г. Москва, ул. Тверская, 1",
         "Оплата по договору №12, оплачивает бухгалтерия"),
        ("ИП Мастер", "master@maket.ru", "ИНН 781012345678, 197022, г. Санкт-Петербург, наб. Реки Карповки, 5",
         "Предоплата 50%, остальное по акту"),
    ]:
        r = post("/clients", data={"name": name, "email": mail, "details": details, "notes": notes})
        check(f"client {name}", r.status_code == 200, f"{r.status_code}")
    # Client ids: the projects reference them, so read them off the directory.
    clients_html = get("/clients").text
    client_ids = {name: int(cid) for cid, name in re.findall(r'"ID":\s*(\d+),\s*"Name":\s*"([^"]+)"', clients_html)}
    romashka = int(client_ids.get("ООО Ромашка", 0))
    master = int(client_ids.get("ИП Мастер", 0))
    check("client ids resolved", romashka > 0 and master > 0, f"ромашка={romashka} мастер={master}")

    # ---------- projects ----------
    print("\n== projects")
    r = post("/projects/new", data={
        "name": "Клиентский портал", "slug": "client-portal", "color": "#2563eb",
        "rate": "2800", "client_id": str(romashka),
    })
    check("project Клиентский портал (2800/h)", r.status_code == 200, f"{r.status_code}")
    r = post("/projects/new", data={
        "name": "Логотип для Мастера", "slug": "logo-master", "color": "#f59e0b",
        "rate": "1500", "client_id": str(master),
    })
    check("project Логотип для Мастера (1500/h)", r.status_code == 200, f"{r.status_code}")
    r = post("/projects/new", data={"name": "Внутренние задачи", "slug": "internal", "color": "#64748b"})
    check("project Внутренние задачи (no rate)", r.status_code == 200, f"{r.status_code}")
    # The update form always writes the billable flag, so the internal project
    # is switched off for invoicing explicitly, the way the screen does.
    r = post("/projects/internal", data={"name": "Внутренние задачи"})
    check("project Внутренние задачи billable=0", r.status_code == 200, f"{r.status_code}")

    # Numeric ids: the backfill, tasks and schedule all take them, not slugs.
    projects = {}
    rj = get("/api/v1/projects").json()
    plist = rj.get("projects") if isinstance(rj, dict) else rj
    for pr in plist or []:
        projects[pr.get("slug")] = pr.get("id")
    check("project ids resolved",
          all(projects.get(x, 0) > 0 for x in ("client-portal", "logo-master", "internal")),
          f"{projects}")

    # ---------- thirty days of sessions ----------
    print("\n== backfill")
    rng = random.Random(30)
    days = [today - dt.timedelta(days=n) for n in range(1, 31)]
    weekdays = [d for d in days if d.weekday() < 5]
    # Two shifts on most weekdays; a few days stay empty the way real weeks do.
    picked = [d for d in weekdays if rng.random() < 0.8] or weekdays[:8]
    shifts = [("09:00", "13:00"), ("14:00", "18:00")]
    made = 0
    last_week_days = {last_monday + dt.timedelta(days=n) for n in range(7)}
    planned = [(d, sh) for d in picked for sh in shifts]
    rng.shuffle(planned)
    for (day, (start, end)), (activity, slug, note) in zip(planned, [WORK[i % len(WORK)] for i in range(len(planned))]):
        t0, t1 = shift(day, start, end)
        r = post("/api/sessions/backfill", data={
            "activity": activity, "start": t0, "end": t1, "note": note,
            "project_id": str(projects.get(slug, 0)),
        }, allow_redirects=False)
        if r.status_code in (200, 204, 303):
            made += 1
        else:
            check(f"backfill {day} {start}", False, f"{r.status_code} {r.text[:120]}")
    # The threshold sits on the expected value (weekdays × 0.8 × 2 ≈ 35),
    # so a wide range keeps the check green on any run day without lying
    # about an empty month.
    check("backfilled sessions in range", 20 <= made <= 45, f"n={made}")
    # The money story needs billable hours inside the completed week, and the
    # week must carry some, so two guaranteed shifts are added on its Thursday.
    bw = last_monday + dt.timedelta(days=3)
    for (start, end), (activity, slug, note) in zip(shifts, WORK[:2]):
        t0, t1 = shift(bw, start, end)
        r = post("/api/sessions/backfill", data={
            "activity": activity, "start": t0, "end": t1, "note": note,
            "project_id": str(projects.get(slug, 0)),
        }, allow_redirects=False)
        check(f"billable shift {bw} {start}", r.status_code in (200, 204, 303), f"{r.status_code}")
        made += 1

    # ---------- tags ----------
    print("\n== tags")
    rj = get("/api/v1/sessions").json()
    sessions = rj.get("sessions") or []
    # The list is windowed to recent days, so it can hold fewer than the whole
    # month of backfills; what matters is that the seeded shifts are visible
    # and the API answers.
    check("sessions readable via /api/v1/sessions", len(sessions) >= 20, f"n={len(sessions)}")
    tagged = 0
    for i, sess in enumerate(sessions[:9]):
        r = post(f"/api/sessions/{sess['id']}/tags", data={"name": TAGS[i % len(TAGS)]}, allow_redirects=False)
        if r.status_code in (200, 204):
            tagged += 1
    check("tagged sessions", tagged >= 5, f"n={tagged}")

    # ---------- goals ----------
    print("\n== goals")
    for activity, period, minutes in [("Разработка", "daily", 240), ("Дизайн", "daily", 120), ("Созвоны", "weekly", 60)]:
        r = post("/api/goals", data={"activity": activity, "period": period, "minutes": str(minutes)})
        check(f"goal {activity}/{period}", r.status_code in (200, 303), f"{r.status_code}")

    # ---------- tasks ----------
    print("\n== tasks")
    tasks = [
        ("Собрать форму заявки", "client-portal", "doing"),
        ("Кабинет: загрузка документов", "client-portal", "todo"),
        ("Вариант логотипа №2", "logo-master", "done"),
        ("Презентация палитры", "logo-master", "todo"),
        ("Оценка задач на декабрь", "internal", "todo"),
    ]
    for title, slug, status in tasks:
        r = post("/api/tasks", data={"project_id": str(projects.get(slug, 0)), "title": title, "status": status})
        check(f"task «{title}» ({status})", r.status_code in (200, 204, 303), f"{r.status_code}")

    # ---------- schedule for the running week ----------
    print("\n== schedule")
    me = get("/api/me").json()
    uid = me.get("id") or me.get("user_id") or (me.get("user") or {}).get("id") or 0
    check("current user id", uid > 0, f"uid={uid}")
    cells = 0
    plan = [(0, "client-portal", 180), (1, "client-portal", 120), (1, "logo-master", 240),
            (2, "logo-master", 180), (3, "internal", 120)]
    for offset, slug, minutes in plan:
        day = week_start + dt.timedelta(days=offset)
        r = post("/api/schedule/cell", data={
            "user_id": str(uid), "project_id": str(projects.get(slug, 0)),
            "date": str(day), "minutes": str(minutes),
        }, allow_redirects=False)
        if r.status_code in (200, 204):
            cells += 1
        else:
            check(f"schedule cell {day}", False, f"{r.status_code} {r.text[:120]}")
    check("schedule cells on the running week", cells == len(plan), f"n={cells}")

    # ---------- approve the completed week ----------
    print("\n== approval, invoice, payout")
    r = post("/api/timesheet/approve", data={"start": LW_FROM, "end": LW_TO})
    check("approve the completed week", r.status_code == 200, f"{r.status_code}")

    # Invoice over the same week, for the portal project and its client.
    r = post("/invoices", data={
        "client": "ООО Ромашка", "start": LW_FROM, "end": LW_TO,
        "project_id": str(projects.get("client-portal", 0)),
    })
    inv = re.search(r"/invoices/(\d+)$", r.url)
    if not inv:
        lst = get("/invoices").text
        inv = re.search(r'"ID":\s*(\d+)[^}]*?"Number":\s*"INV-', lst) or re.search(
            r'"Number":\s*"INV-[^"]*"[^}]*?"ID":\s*(\d+)', lst)
    check("invoice created", bool(inv), inv.group(1) if inv else "none")
    iid = int(inv.group(1)) if inv else 0
    r = post(f"/invoices/{iid}/status", data={"status": "sent"})
    check("invoice sent", r.status_code == 200, f"{r.status_code}")
    r = post(f"/invoices/{iid}/paid")
    check("invoice paid", r.status_code == 200, f"{r.status_code}")
    page = get(f"/invoices/{iid}").text
    number = (re.search(r'"Number":\s*"(INV-[^"]+)"', page) or [None, ""])[1]
    total = int((re.search(r'"TotalCents":\s*(\d+)', page) or [None, 0])[1])
    check("invoice total read", total > 0, f"{number}: {total / 100:.2f}")

    # Payout for the same approved week.
    r = post("/api/member/pay", data={
        "user_id": str(uid), "hourly_pay_cents": "120000", "capacity_minutes": "480",
    })
    check("member pay 1200/h", r.status_code == 200, f"{r.status_code}")
    r = post("/payroll", data={"start": LW_FROM, "end": LW_TO})
    run = re.search(r"/payroll/(\d+)$", r.url)
    if not run:
        lst = get("/payroll").text
        run = re.search(r'"ID":\s*(\d+)[^}]*?"Number":\s*"PAY-', lst) or re.search(
            r'"Number":\s*"PAY-[^"]*"[^}]*?"ID":\s*(\d+)', lst)
    check("payout run created", bool(run), run.group(1) if run else "none")
    rid = int(run.group(1)) if run else 0
    r = post(f"/payroll/{rid}/paid")
    check("payout run paid", r.status_code == 200, f"{r.status_code}")
    page = get(f"/payroll/{rid}").text
    pay_number = (re.search(r'"Number":\s*"(PAY-[^"]+)"', page) or [None, ""])[1]
    pay_total = int((re.search(r'"TotalCents":\s*(\d+)', page) or [None, 0])[1])
    check("payout total read", pay_total > 0, f"{pay_number}: {pay_total / 100:.2f}")

    # ---------- summary ----------
    print(f"""
== demo account ready
email:    {email}
password: {PASSWORD}
workspace: studio preset (every section)
clients:  2   projects: 3 (portal 2800/h, logo 1500/h, internal billable=0)
sessions: {made} weekday shifts over the last 30 days, {tagged} tagged
goals:    3   tasks: {len(tasks)}   schedule cells: {cells}
invoice:  {number or '?'} over {LW_FROM}..{LW_TO} — paid, {total / 100:.2f}
payout:   {pay_number or '?'} for the same week — paid, {pay_total / 100:.2f}
""")
    print(f"demo_seed: {PASS}/{PASS + FAIL} checks passed")
    return 1 if FAIL else 0


if __name__ == "__main__":
    sys.exit(main())
