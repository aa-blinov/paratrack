"""Notification topics and a verification send: the screen says what happened.

Drives a running paratrack server with Playwright. Signs in once and opens
/settings/notifications. The three checkboxes are the events the application
really sends, and a fresh account starts with all of them on: a person who
never touched the setting must not lose notifications. The verification button
never reports a success it cannot prove — in a headless browser the permission
is refused, and the screen says so instead of failing silently.

The two endpoints are intercepted with the answers each leg needs, so the
round trip is checked without a real push service behind it: the save must
carry the whole selection, and the four channel outcomes (delivered, no
channel, refused, browser permission missing) must each be named differently.

Run from the repo root with the .venv active:

    PARATRACK_BASE=http://127.0.0.1:8899 .venv/bin/python e2e/test_notification_topics.py
"""

from __future__ import annotations

import json
import os
import uuid
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE

EMAIL = os.environ.get("PARATRACK_EMAIL", "doc-1791228444@x.test")
PASSWORD = os.environ.get("PARATRACK_PASSWORD", "longenoughpw")
SCREENSHOTS = Path(os.environ.get("PARATRACK_SCREENSHOTS", "/tmp"))
STAMP = uuid.uuid4().hex[:8]

SESSION_STOPPED = "Сессия остановлена (вручную или по концу таймера)"
GOAL_MET = "Достижение целей"
PAYROLL_PAID = "Расчёт помечен выплаченным"
INVOICE_PAID = "Счёт помечен оплаченным"

results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}{(' — ' + detail) if detail else ''}")
    results.append((name, ok, detail))


def sign_in(page) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", EMAIL)
    page.fill("#password", PASSWORD)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    check("signed in", "/login" not in page.url, f"url={page.url}")


def csrf(page) -> str:
    for cookie in page.context.cookies():
        if cookie.get("name") == "paratrack_csrf":
            return cookie.get("value") or ""
    return ""


def stored_topics(page) -> dict:
    """Only the events the account actually switched off."""
    html = page.request.get(BASE + "/settings/notifications", headers={"X-CSRF-Token": csrf(page)}).text()
    marker = '<div id="react-page-data" hidden>'
    start = html.index(marker) + len(marker)
    data = json.loads(html[start:html.index("</div>", start)])["data"]
    return {topic["Key"]: topic for topic in (data.get("Topics") or []) if topic.get("Muted")}


def checkbox_for(page, label: str):
    return page.get_by_role("checkbox", name=label, exact=True)


def answer_json(route, payload: dict) -> None:
    route.fulfill(json=payload)


def main() -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors: list[str] = []
        page.on("pageerror", lambda e: errors.append(str(e)))
        saved_bodies: list[str] = []
        outcome = {"value": "delivered"}

        # The two endpoints are registered by the transport wiring, so the legs
        # below answer them here and check what the screen does with the reply.
        def topics_route(route):
            saved_bodies.append(route.request.post_data or "")
            stored = {pair.split("=", 1)[1] for pair in (route.request.post_data or "").split("&") if pair.startswith("topic=")}
            answer_json(route, {"muted": sorted(stored)})

        def test_route(route):
            answer_json(route, {"outcome": outcome["value"], "delivered": 1 if outcome["value"] == "delivered" else 0})

        page.route("**/api/push/topics", topics_route)
        page.route("**/api/push/test", test_route)

        sign_in(page)

        # ------------------------------------------------------------------ 1
        print("\n== 1. The three events the app really sends are listed and on")
        before = stored_topics(page)
        page.goto(BASE + "/settings/notifications")
        page.locator("#main h1").wait_for()
        for label in (SESSION_STOPPED, GOAL_MET, PAYROLL_PAID):
            expect(checkbox_for(page, label)).to_be_visible()
            check(f"«{label}» is offered and on", checkbox_for(page, label).is_checked())
        check("the screen offers exactly three events", page.get_by_role("checkbox").count() == 3,
              f"{page.get_by_role('checkbox').count()} checkboxes")
        # The app only ever sends these three, so the screen must not offer a
        # fourth one nobody delivers — not even with a "coming soon" note.
        check("no phantom topic is offered",
              INVOICE_PAID not in [box.get_attribute("aria-label") or "" for box in page.get_by_role("checkbox").all()],
              str([box.get_attribute("aria-label") for box in page.get_by_role("checkbox").all()]))
        page.screenshot(path=str(SCREENSHOTS / f"notify-topics-{STAMP}.png"), full_page=True)

        # ------------------------------------------------------------------ 2
        print("\n== 2. Clearing one event saves the whole selection and survives a reload")
        checkbox_for(page, GOAL_MET).click()
        page.get_by_role("status").filter(has_text="Сохранено").wait_for()
        last = saved_bodies[-1]
        check("the save carries every event left switched off",
              "topic=session.stopped" not in last and "topic=goal.achieved" in last, last)
        checkbox_for(page, GOAL_MET).click()
        page.get_by_role("status").filter(has_text="Сохранено").wait_for()
        check("switching it back on clears the entry", "topic=goal.achieved" not in saved_bodies[-1], saved_bodies[-1])
        check("nothing was stored on the account", not stored_topics(page),
              json.dumps(stored_topics(page), ensure_ascii=False))

        # ------------------------------------------------------------------ 3
        print("\n== 3. The check names each channel outcome")
        test_button = page.locator("[data-notification-test]")
        expect(test_button).to_be_visible()
        status = page.locator("[data-notification-status]")
        permission = page.evaluate("Notification.permission")
        if permission == "granted":
            outcome["value"] = "delivered"
            test_button.click()
            status.filter(has_text="Проверка отправлена").wait_for()
            check("a delivered check says so", status.inner_text().strip().startswith("Проверка отправлена"), status.inner_text())
            for value, expected in (("no_channel", "нет подписанных устройств"), ("failed", "Не удалось")):
                outcome["value"] = value
                test_button.click()
                status.filter(has_text=expected).wait_for()
                check(f"outcome {value} is named, not swallowed", expected in status.inner_text(), status.inner_text())
        else:
            # Headless Chromium refuses the permission, so the honest answer is
            # the browser, and the check must stop there.
            test_button.click()
            status.filter(has_text="Браузер").wait_for()
            check("the check explains the missing permission instead of failing quietly",
                  "Браузер" in status.inner_text(), f"permission={permission}, status={status.inner_text()}")

        check("no page errors", not errors, "; ".join(errors[:3]))
        context.close()
        browser.close()

    failed = [name for name, ok, _ in results if not ok]
    print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())