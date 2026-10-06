"""QA pass for the member (participant) profile against a live stand.

Walkthrough order: the owner invites member2@x.test, Masha accepts the link,
then we record what she can reach from the menu, what the management screens
answer her, and what she can and cannot do with time data.

One browser context per account, one sign-in each — the login rate limit is
ten attempts a minute and repeated sign-ins push the account to the form.

Run:  PARATRACK_BASE=http://127.0.0.1:8895 .venv/bin/python e2e/qa_profiles.py
"""
from __future__ import annotations

import json
import os
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8895")
OWNER = "owner2@x.test"
MEMBER = "member2@x.test"
PW = "longenoughpw"
ROOT = "#paratrack-react-root"
OUT = Path(__file__).parent / "screenshots" / "qa-profiles"
OUT.mkdir(parents=True, exist_ok=True)

notes: list[str] = []
page_dump: dict[str, str] = {}
logout_hits: list[str] = []


def note(section: str, text: str) -> None:
    notes.append(f"[{section}] {text}")
    print(f"[{section}] {text}")


def shot(page, name: str) -> str:
    try:
        page.screenshot(path=str(OUT / f"{name}.png"), full_page=True)
        return name
    except Exception as exc:  # noqa: BLE001 - screenshots are diagnostics
        print(f"        shot failed {name}: {exc}")
        return ""


def sign_in(page, email: str) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", email)
    page.fill("#password", PW)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url:
        raise SystemExit(f"sign-in failed for {email}: still on {page.url}")


def wait_app(page) -> None:
    try:
        page.wait_for_selector(f"{ROOT} .app-boot", state="detached", timeout=15000)
    except Exception:
        pass


def body_text(page, limit: int = 900) -> str:
    try:
        return " ".join(page.locator("body").inner_text(timeout=5000).split())[:limit]
    except Exception as exc:  # noqa: BLE001
        return f"<no text: {exc}>"


def dump(page, key: str, label: str = "") -> str:
    text = body_text(page)
    page_dump[key] = text
    if label:
        print(f"        [{key}] {text[:220]}")
    return text


def nav_items(page) -> list[dict[str, str]]:
    out = []
    for loc in page.locator(f'{ROOT} nav[aria-label] a, {ROOT} aside a').all():
        try:
            out.append({"text": " ".join(loc.inner_text().split()), "href": loc.get_attribute("href") or ""})
        except Exception:
            pass
    return out


def settings_tabs(page) -> list[str]:
    return [ " ".join(a.inner_text().split()) for a in page.locator(f'{ROOT} nav a[href^="/settings/"]').all() ]


def probe(page, path: str, name: str, section: str) -> dict:
    """Open a path straight from the address bar and describe the answer."""
    rec: dict = {"path": path}
    try:
        resp = page.goto(BASE + path, wait_until="domcontentloaded")
        wait_app(page)
        page.wait_for_timeout(400)
        rec["status"] = resp.status if resp else None
        rec["final_url"] = page.url.replace(BASE, "") or "/"
        rec["login_form"] = bool(page.locator('form[action="/api/login"], #email').count())
        rec["h1"] = " ".join((page.locator("h1, h2").first.inner_text(timeout=3000) or "").split()) if page.locator("h1, h2").count() else ""
        rec["text"] = body_text(page, 500)
        rec["shot"] = shot(page, name)
        rec["inputs"] = page.locator(f"{ROOT} input:not([type=hidden]), {ROOT} textarea, {ROOT} select").count()
        rec["tables"] = page.locator(f"{ROOT} table").count()
        rec["alert"] = " ".join(page.locator(f'{ROOT} [role="alert"]').first.inner_text().split()) if page.locator(f'{ROOT} [role="alert"]').count() else ""
    except Exception as exc:  # noqa: BLE001
        rec["error"] = str(exc)[:200]
    note(section, f"{path} → {rec.get('status')} final={rec.get('final_url')} h1={rec.get('h1','')!r} inputs={rec.get('inputs')} tables={rec.get('tables')} :: {rec.get('text', rec.get('error', ''))[:260]}")
    return rec


def csrf(page) -> str:
    for c in page.context.cookies():
        if c.get("name") == "paratrack_csrf":
            return c.get("value") or ""
    return ""


def api_probe(page, method: str, path: str, body: str = "") -> dict:
    """Fire one write request as the signed-in member and report the status."""
    try:
        r = getattr(page.request, method)(
            BASE + path,
            headers={"X-CSRF-Token": csrf(page), "Content-Type": "application/x-www-form-urlencoded"},
            data=body or None,
            max_redirects=0,
        )
        text = " ".join(r.text().split())[:220]
        note("запись-API", f"{method} {path} → {r.status} :: {text or '<пусто>'}")
        return {"status": r.status, "body": text}
    except Exception as exc:  # noqa: BLE001
        note("запись-API", f"{method} {path} → ошибка запроса: {str(exc)[:160]}")
        return {"status": None, "body": str(exc)[:160]}


def track_logouts(page, who: str) -> None:
    def on_response(r):
        if "/api/logout" in r.url:
            logout_hits.append(f"{time.strftime('%H:%M:%S')} {who} {r.request.method} {r.url} → {r.status}")
    page.on("response", on_response)


def accept_invite(page, link: str) -> dict:
    rec: dict = {"link": link}
    page.goto(link)
    wait_app(page)
    page.wait_for_timeout(300)
    rec["landing_text"] = body_text(page, 400)
    rec["landing_shot"] = shot(page, "m2-invite-landing")
    buttons = page.locator(f"{ROOT} form button, {ROOT} button")
    rec["buttons"] = [" ".join(b.inner_text().split()) for b in buttons.all()][:10]
    # One click on the accept button is the whole join.
    accept = page.locator(f'{ROOT} form[action*="/accept"] button').first
    if accept.count():
        rec["clicks"] = 1
        accept.click()
        page.wait_for_load_state("load")
        wait_app(page)
        page.wait_for_timeout(500)
        rec["after"] = page.url.replace(BASE, "")
        rec["after_text"] = body_text(page, 400)
        rec["after_shot"] = shot(page, "m2-after-accept")
    else:
        rec["clicks"] = 0
        rec["error"] = "кнопка принятия не найдена"
    return rec


def main() -> int:
    started = time.time()
    report: dict = {"started": started}

    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx_o = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU", device_scale_factor=1)
        ctx_m = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU", device_scale_factor=1)
        owner = ctx_o.new_page()
        masha = ctx_m.new_page()
        errors: list[str] = []
        for pg, who in ((owner, "owner"), (masha, "member")):
            pg.on("pageerror", lambda e, w=who: errors.append(f"{w}: {e}"))
            track_logouts(pg, who)

        # ---------------------------------------------------------- owner
        print("\n== Пётр: вход и приглашение")
        sign_in(owner, OWNER)
        wait_app(owner)
        report["owner_nav"] = nav_items(owner)
        note("меню-петра", f"{len(report['owner_nav'])} пунктов: " + ", ".join(f"{i['text']}" for i in report["owner_nav"]))
        shot(owner, "p1-owner-dashboard")

        owner.goto(BASE + "/settings/invites")
        wait_app(owner)
        report["owner_settings_tabs"] = settings_tabs(owner)
        note("вкладки-петра", ", ".join(report["owner_settings_tabs"]))
        shot(owner, "p2-owner-invites")

        owner.get_by_role("button", name="Создать ссылку-приглашение").click()
        owner.wait_for_load_state("load")
        wait_app(owner)
        link = owner.locator(f"{ROOT} a[href^='http']").first.get_attribute("href") or ""
        note("приглашение", f"ссылка создана: {link}")
        report["invite_link"] = link
        shot(owner, "p3-owner-invite-created")

        # ---------------------------------------------------------- Маша
        print("\n== Маша: вход, личное пространство")
        sign_in(masha, MEMBER)
        wait_app(masha)
        report["member_nav_before"] = nav_items(masha)
        note("меню-маши-до", f"{len(report['member_nav_before'])} пунктов: " + ", ".join(i["text"] for i in report["member_nav_before"]))
        dump(masha, "m1-personal-dashboard", label="личное пространство")
        shot(masha, "m1-masha-personal")

        print("\n== Маша: принимает приглашение")
        report["accept"] = accept_invite(masha, link)
        note("вступление", f"кнопок на странице: {report['accept'].get('buttons')} → кликов: {report['accept'].get('clicks')} → {report['accept'].get('after')}")

        print("\n== Маша: меню и вкладки после вступления")
        report["member_nav"] = nav_items(masha)
        note("меню-маши-после", f"{len(report['member_nav'])} пунктов: " + ", ".join(i["text"] for i in report["member_nav"]))
        dump(masha, "m2-studio-dashboard", label="дашборд студии")
        shot(masha, "m2-masha-studio-dashboard")

        masha.goto(BASE + "/settings/profile")
        wait_app(masha)
        report["member_settings_tabs"] = settings_tabs(masha)
        note("вкладки-маши", ", ".join(report["member_settings_tabs"]) or "(пусто)")
        shot(masha, "m3-masha-settings-profile")

        print("\n== Маша: прямой заход на управленческие экраны")
        admin_paths = [
            ("/settings/team", "m4-member-team"),
            ("/settings/sections", "m4-member-sections"),
            ("/settings/members", "m4-member-members"),
            ("/settings/invites", "m4-member-invites"),
            ("/settings/webhooks", "m4-member-webhooks"),
            ("/settings/audit", "m4-member-audit"),
            ("/settings/tokens", "m4-member-tokens"),
            ("/settings/email-preview", "m4-member-email-preview"),
            ("/invoices", "m4-member-invoices"),
            ("/payroll", "m4-member-payroll"),
            ("/reports", "m4-member-reports"),
            ("/reports/run", "m4-member-report-run"),
            ("/export", "m4-member-export"),
        ]
        report["admin_probes"] = [probe(masha, path, shot_name, "доступ-участницы") for path, shot_name in admin_paths]

        print("\n== Маша: свои рабочие экраны")
        work_paths = [
            ("/", "m5-member-dashboard"),
            ("/timesheet", "m5-member-timesheet"),
            ("/stats", "m5-member-stats"),
            ("/graph", "m5-member-graph"),
            ("/schedule", "m5-member-schedule"),
            ("/projects", "m5-member-projects"),
        ]
        report["work_probes"] = [probe(masha, path, shot_name, "свои-экраны") for path, shot_name in work_paths]

        # ---------------------------------------------------- таймер
        print("\n== Маша: таймер")
        masha.goto(BASE + "/")
        wait_app(masha)
        before = body_text(masha, 400)
        page_dump["m5-dashboard-before-timer"] = before
        start = masha.get_by_role("button", name="Старт", exact=True).first
        timer = {"start_clicks": 0}
        if start.count():
            start.click()
            masha.wait_for_timeout(1200)
            timer["start_clicks"] = 1
            timer["after_start"] = body_text(masha, 500)
            page_dump["m5-dashboard-after-start"] = timer["after_start"]
            shot(masha, "m5-member-timer-running")
            stop = masha.get_by_role("button", name="Стоп", exact=True).first
            if stop.count():
                stop.click()
                masha.wait_for_timeout(1200)
                timer["stop_clicks"] = 2
                timer["after_stop"] = body_text(masha, 500)
                page_dump["m5-dashboard-after-stop"] = timer["after_stop"]
                shot(masha, "m5-member-timer-stopped")
        else:
            timer["error"] = "кнопка «Старт» не найдена на дашборде"
        note("таймер", f"кликов до старта: {timer.get('start_clicks')} (с дашборда), стоп: клик {timer.get('stop_clicks')}; {timer.get('error','')}")
        note("таймер-текст", f"после старта: {timer.get('after_start','')[:300]}")

        # -------------------------------------------------- табель
        print("\n== Маша: запись в табель")
        masha.goto(BASE + "/timesheet")
        wait_app(masha)
        dump(masha, "m6-timesheet-open", label="табель открыт")
        cell = masha.locator(f'{ROOT} input[type="number"]').first
        ts = {"inputs": masha.locator(f'{ROOT} input[type="number"]').count()}
        if cell.count():
            cell.click()
            cell.fill("45")
            cell.press("Enter")
            masha.wait_for_timeout(1500)
            ts["value"] = cell.input_value()
            ts["clicks"] = 2
            ts["after"] = body_text(masha, 400)
            page_dump["m6-timesheet-after"] = ts["after"]
            shot(masha, "m6-member-timesheet-filled")
            note("табель", f"ячеек в строке: {ts['inputs']}, ввёл 45 мин, осталось в поле: {ts.get('value')!r}")
        else:
            ts["error"] = "в табеле нет числовых полей"
            note("табель", ts["error"])
        shot(masha, "m6-member-timesheet")
        report["timesheet"] = ts

        # ------------------------------------------- границы прав
        print("\n== Маша: границы прав (запись в чужое)")
        masha.goto(BASE + "/stats")
        wait_app(masha)
        stats = body_text(masha, 700)
        page_dump["m7-member-stats"] = stats
        note("статистика", stats[:300])
        shot(masha, "m7-member-stats")

        masha.goto(BASE + "/schedule")
        wait_app(masha)
        sched = body_text(masha, 700)
        page_dump["m8-member-schedule"] = sched
        note("график", sched[:400])
        report["schedule_inputs"] = masha.locator(f"{ROOT} input, {ROOT} select, {ROOT} button[data-*]").count()
        report["schedule_disabled"] = masha.locator(f'{ROOT} input[disabled]').count()
        note("график-поля", f"полей ввода: {report['schedule_inputs']}, из них disabled: {report['schedule_disabled']}")
        shot(masha, "m8-member-schedule")

        api_probe(masha, "POST", "/api/team/invites", "csrf_token=" + csrf(masha))
        api_probe(masha, "POST", "/api/schedule/cell", "csrf_token=" + csrf(masha) + "&activity_id=1&date=2026-10-06&minutes=60")
        api_probe(masha, "POST", "/api/timesheet/cell", "csrf_token=" + csrf(masha) + "&activity_id=1&date=2026-10-06&minutes=60")
        api_probe(masha, "POST", "/api/payroll", "csrf_token=" + csrf(masha) + "&user_id=1&amount=100")
        api_probe(masha, "POST", "/api/invoices", "csrf_token=" + csrf(masha) + "&client=Test&amount=100")

        # -------------------------------------- чужие данные на экранах
        print("\n== Маша: видны ли чужие данные")
        for path, key, label in [("/", "m9-member-home-others", "дашборд"), ("/projects", "m9-member-projects-others", "проекты"), ("/export", "m9-member-export-others", "экспорт")]:
            masha.goto(BASE + path)
            wait_app(masha)
            txt = body_text(masha, 1200)
            page_dump[key] = txt
            hits = [w for w in ("Пётр", "owner2", "140", "160", "Ставк", "₽") if w in txt]
            note("чужие-данные", f"{path}: совпадения {hits} :: {txt[:220]}")

        # ------------------------------------------- Пётр после прогона
        print("\n== Пётр: не вылетел ли после работы Маши")
        owner.goto(BASE + "/")
        wait_app(owner)
        owner_alive = "/login" not in owner.url
        note("владелец", f"после прогона участницы: {'на дашборде' if owner_alive else 'выброшен на ' + owner.url}")
        report["owner_alive_after"] = owner_alive
        shot(owner, "p4-owner-after")

        report["console_errors"] = errors[:20]
        report["logout_hits"] = logout_hits
        note("консоль", f"ошибок JS: {len(errors)}" + (f" → {errors[:3]}" if errors else ""))
        note("выходы", f"POST /api/logout за прогон: {len(logout_hits)}" + (f" → {logout_hits}" if logout_hits else ""))

        report["notes"] = notes
        report["page_dump"] = page_dump
        (OUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        browser.close()

    print("\n===== ЗАМЕТКИ =====")
    for line in notes:
        print(line)
    print(f"\nJSON: {OUT / 'report.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())