"""Third QA pass: the same questions, this time inside Петр's studio.

Pass 2 signed Masha in from a clean context and landed her in her own
personal space, so the timer, timesheet and export answers belonged to the
wrong workspace. Here she switches to the studio first, and the permission
probes are issued with lowercase verbs (`APIRequestContext.post`, not `.POST`).

Run:  PARATRACK_BASE=http://127.0.0.1:8895 .venv/bin/python e2e/qa_profiles3.py
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
dump: dict[str, str] = {}
logout_hits: list[str] = []


def note(section: str, text: str) -> None:
    notes.append(f"[{section}] {text}")
    print(f"[{section}] {text}")


def shot(page, name: str) -> str:
    try:
        page.screenshot(path=str(OUT / f"{name}.png"), full_page=True)
        return name
    except Exception as exc:  # noqa: BLE001
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


def body(page, limit: int = 900) -> str:
    try:
        return " ".join(page.locator("body").inner_text(timeout=5000).split())[:limit]
    except Exception as exc:  # noqa: BLE001
        return f"<no text: {exc}>"


def keep(page, key: str, label: str = "") -> str:
    text = body(page)
    dump[key] = text
    if label:
        print(f"        [{key}] {text[:240]}")
    return text


def csrf(page) -> str:
    for c in page.context.cookies():
        if c.get("name") == "paratrack_csrf":
            return c.get("value") or ""
    return ""


def api(page, method: str, path: str, body_str: str = "") -> dict:
    verb = method.lower()
    try:
        r = getattr(page.request, verb)(
            BASE + path,
            headers={"X-CSRF-Token": csrf(page), "Content-Type": "application/x-www-form-urlencoded"},
            data=body_str or None,
            max_redirects=0,
        )
        text = " ".join(r.text().split())[:240]
        note("запись-API", f"{method} {path} → {r.status} :: {text or '<пусто>'}")
        return {"status": r.status, "body": text}
    except Exception as exc:  # noqa: BLE001
        note("запись-API", f"{method} {path} → исключение: {str(exc)[:150]}")
        return {"status": None, "body": str(exc)[:150]}


def sidebar(page) -> list[tuple[str, str]]:
    out, seen = [], set()
    for loc in page.locator(f"{ROOT} nav.app-shell-desktop-nav a, {ROOT} nav.app-shell-mobile-nav a").all():
        try:
            href = loc.get_attribute("href") or ""
            if href and href not in seen:
                seen.add(href)
                out.append((" ".join(loc.inner_text().split()), href))
        except Exception:
            pass
    return out


def settings_tabs(page) -> list[str]:
    return [" ".join(a.inner_text().split()) for a in page.locator(f'{ROOT} nav[aria-label] a[href^="/settings/"]').all()]


def switch_workspace(page, needle: str) -> int:
    """Click the workspace switcher and pick the entry that contains `needle`."""
    clicks = 0
    trigger = page.locator(f'{ROOT} button:has(.app-shell-workspace), {ROOT} button.app-shell-workspace, {ROOT} button:has-text("Пространство")').first
    trigger.click()
    clicks += 1
    page.wait_for_timeout(500)
    target = page.locator(f'{ROOT} [role="menuitem"]:has-text("{needle}")').first
    target.click()
    clicks += 1
    page.wait_for_load_state("load")
    wait_app(page)
    page.wait_for_timeout(600)
    return clicks


def main() -> int:
    report: dict = {}
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx_m = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU", accept_downloads=True)
        ctx_o = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU", accept_downloads=True)
        masha = ctx_m.new_page()
        owner = ctx_o.new_page()
        js_errors: list[str] = []
        for pg, who in ((masha, "member"), (owner, "owner")):
            pg.on("pageerror", lambda e, w=who: js_errors.append(f"{w}: {e}"))
            pg.on("response", lambda r, w=who: logout_hits.append(f"{time.strftime('%H:%M:%S')} {w} {r.request.method} {r.url} → {r.status}") if "/api/logout" in r.url else None)

        # ------------------------------------------------- Пётр: эталон меню
        print("\n== Пётр: эталон меню")
        sign_in(owner, OWNER)
        wait_app(owner)
        report["owner_nav"] = sidebar(owner)
        note("меню-петра", f"{len(report['owner_nav'])}: " + ", ".join(f"{t}→{h}" for t, h in report["owner_nav"]))
        owner.goto(BASE + "/settings/profile")
        wait_app(owner)
        report["owner_tabs"] = settings_tabs(owner)
        note("вкладки-петра", ", ".join(report["owner_tabs"]))

        # Петр запускает таймер, чтобы проверить, видит ли Маша чужое время
        owner.goto(BASE + "/")
        wait_app(owner)
        owner.locator(f'{ROOT} input[type="text"], {ROOT} input[placeholder]').first.fill("созвон по Nordwind (Пётр)")
        owner.get_by_role("button", name="Старт", exact=True).first.click()
        owner.wait_for_timeout(1200)
        note("петр-таймер", f"запущен: {body(owner, 400)[300:500]}")

        # ------------------------------------------------- Маша в студии
        print("\n== Маша: вход и переключение пространства")
        sign_in(masha, MEMBER)
        wait_app(masha)
        where_before = body(masha, 400)
        keep(masha, "o1-after-login", label="куда попала после входа")
        note("вход", f"после входа в шапке: {where_before[:160]}")
        shot(masha, "o1-masha-after-login")

        clicks = switch_workspace(masha, "Пётр")
        where_after = body(masha, 300)
        keep(masha, "o2-studio", label="после переключения")
        note("переключение", f"кликов: {clicks}, шапка: {where_after[:140]}")
        shot(masha, "o2-masha-studio")

        report["member_nav"] = sidebar(masha)
        note("меню-маши", f"{len(report['member_nav'])}: " + ", ".join(f"{t}→{h}" for t, h in report["member_nav"]))
        masha.goto(BASE + "/settings/profile")
        wait_app(masha)
        report["member_tabs"] = settings_tabs(masha)
        note("вкладки-маши", ", ".join(report["member_tabs"]))
        shot(masha, "o3-masha-settings")

        # ------------------------------------ чужие данные сразу после входа
        print("\n== Маша: видно ли чужое время на дашборде студии")
        masha.goto(BASE + "/")
        wait_app(masha)
        home = keep(masha, "o4-home", label="дашборд студии")
        note("дашборд", f"совпадения {[w for w in ('Пётр', 'созвон по Nordwind', 'ставк', '140', '160') if w.lower() in home.lower()]}")
        note("дашборд-хвост", home[300:1000])
        shot(masha, "o4-masha-home")

        for path, key, shot_name in [("/stats", "o5-stats", "o5-masha-stats"), ("/timesheet", "o6-timesheet", "o6-masha-timesheet"), ("/projects", "o7-projects", "o7-masha-projects"), ("/schedule", "o8-schedule", "o8-masha-schedule")]:
            masha.goto(BASE + path)
            wait_app(masha)
            txt = keep(masha, key)
            note(f"экран {path}", f"маркеры {[w for w in ('Пётр', 'Ставка', '140', '160', '₽') if w.lower() in txt.lower()]} :: {txt[200:750]}")
            shot(masha, shot_name)

        # ------------------------------------------- табель: сохранилось ли
        masha.goto(BASE + "/timesheet")
        wait_app(masha)
        cells = masha.locator(f'{ROOT} input[type="number"]')
        vals = []
        for i in range(cells.count()):
            try:
                vals.append(cells.nth(i).input_value())
            except Exception:
                vals.append("?")
        report["studio_timesheet_cells"] = vals
        note("табель-студия", f"полей: {cells.count()}, значения: {vals}")
        row_labels = [" ".join(r.inner_text().split())[:80] for r in masha.locator(f"{ROOT} table tbody tr").all()]
        report["studio_timesheet_rows"] = row_labels
        note("табель-строки", f"{row_labels}")
        shot(masha, "o9-masha-timesheet-cells")

        # --------------------------------------------- таймер в студии
        print("\n== Маша: таймер в студии")
        masha.goto(BASE + "/")
        wait_app(masha)
        field = masha.locator(f'{ROOT} input[type="text"], {ROOT} input[placeholder]').first
        field.click()
        field.fill("макет главной Nordwind")
        masha.get_by_role("button", name="Старт", exact=True).first.click()
        masha.wait_for_timeout(1500)
        running = body(masha, 1500)
        keep(masha, "o10-running", label="таймер идёт")
        note("таймер", f"после старта: {running[300:900]}")
        shot(masha, "o10-masha-timer-running")
        masha.locator(f'{ROOT} main button:has-text("Стоп")').first.click()
        masha.wait_for_timeout(1500)
        stopped = keep(masha, "o11-stopped", label="таймер остановлен")
        note("таймер-стоп", stopped[300:1000])
        shot(masha, "o11-masha-timer-stopped")

        # --------------------------------------------- график по проектам
        print("\n== Маша: расписание по проектам студии")
        masha.goto(BASE + "/schedule")
        wait_app(masha)
        trigger = masha.locator(f'{ROOT} [role="combobox"]').first
        options = []
        if trigger.count():
            trigger.click()
            masha.wait_for_timeout(500)
            for opt in masha.locator(f'[role="option"]').all():
                options.append(" ".join(opt.inner_text().split()))
            if options:
                for index, label in enumerate(options):
                    masha.locator(f'[role="option"]').nth(index).click()
                    masha.wait_for_timeout(1000)
                    txt = keep(masha, f"o12-schedule-{index}")
                    note("расписание", f"[{label}] вводов: {masha.locator(f'{ROOT} input').count()} :: {txt[250:900]}")
                    shot(masha, f"o12-masha-schedule-{index}")
                    if index + 1 < len(options):
                        masha.locator(f'{ROOT} [role="combobox"]').first.click()
                        masha.wait_for_timeout(400)
        report["schedule_options"] = options
        note("расписание-опции", f"{options}")

        # --------------------------------------------- экспорт из студии
        print("\n== Маша: экспорт из студии")
        masha.goto(BASE + "/export")
        wait_app(masha)
        try:
            with masha.expect_download(timeout=15000) as dl:
                masha.get_by_role("button", name="Скачать CSV").click()
            path = OUT / "o13-masha-studio-export.csv"
            dl.value.save_as(str(path))
            lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
            report["member_studio_csv"] = lines
            note("CSV-маши-студия", f"{len(lines)} строк: {lines[:8]}")
        except Exception as exc:  # noqa: BLE001
            note("CSV-маши-студия", f"не скачался: {str(exc)[:180]}")

        # --------------------------------------------- границы прав
        print("\n== Маша: границы прав (запись)")
        w = {}
        w["invite"] = api(masha, "POST", "/api/team/invites", f"csrf_token={csrf(masha)}")
        w["timesheet_foreign"] = api(masha, "POST", "/api/timesheet/cell", f"csrf_token={csrf(masha)}&activity_id=3&date=2019-03-04&minutes=17")
        w["schedule_foreign"] = api(masha, "POST", "/api/schedule/cell", f"csrf_token={csrf(masha)}&activity_id=3&date=2019-03-04&minutes=17")
        w["payroll"] = api(masha, "POST", "/api/payroll", f"csrf_token={csrf(masha)}&user_id=1&amount=100")
        w["invoice"] = api(masha, "POST", "/api/invoices", f"csrf_token={csrf(masha)}&client=QA&total=100")
        w["member_promote"] = api(masha, "POST", "/api/team/members/1", f"csrf_token={csrf(masha)}&role=owner")
        w["team_modules"] = api(masha, "POST", "/api/team/modules", f"csrf_token={csrf(masha)}&preset=studio")
        w["webhook"] = api(masha, "POST", "/api/team/webhooks", f"csrf_token={csrf(masha)}&url=https://example.com/hook")
        w["cleanup"] = api(masha, "POST", "/api/timesheet/row/clear", f"csrf_token={csrf(masha)}&activity_id=3")
        report["writes"] = w

        # --------------------------------------------- Пётр: что он видит
        print("\n== Пётр: контрольный замер")
        owner.goto(BASE + "/timesheet")
        wait_app(owner)
        owner_cells = [owner.locator(f'{ROOT} input[type="number"]').nth(i).input_value() for i in range(owner.locator(f'{ROOT} input[type="number"]').count())]
        report["owner_timesheet_cells"] = owner_cells
        note("табель-петра", f"значения: {owner_cells}")
        shot(owner, "p6-owner-timesheet")

        owner.goto(BASE + "/schedule")
        wait_app(owner)
        keep(owner, "p7-owner-schedule", label="график у Петра")
        note("график-петра", body(owner, 1000)[250:900])
        shot(owner, "p7-owner-schedule")

        owner.goto(BASE + "/")
        wait_app(owner)
        alive = "/login" not in owner.url
        note("владелец", f"{'на дашборде' if alive else 'выброшен на ' + owner.url}")
        report["owner_alive"] = alive
        shot(owner, "p6-owner-dashboard")

        report["js_errors"] = js_errors[:20]
        report["logout_hits"] = logout_hits
        note("консоль", f"ошибок JS: {len(js_errors)}" + (f" → {js_errors[:3]}" if js_errors else ""))
        note("выходы", f"POST /api/logout из браузера: {len(logout_hits)}" + (f" → {logout_hits}" if logout_hits else ""))
        report["notes"] = notes
        report["dump"] = dump
        (OUT / "report3.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        browser.close()

    print("\n===== ЗАМЕТКИ =====")
    for line in notes:
        print(line)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())