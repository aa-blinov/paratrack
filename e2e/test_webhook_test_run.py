"""Webhook test run on /settings/webhooks.

Covers the two things the screen used to make impossible: pressing one button
sends a signed event to the chosen address and shows the answer right there, and
the request body behind a delivery is readable.

Drives a running paratrack server with Playwright. The base URL comes from
target.py and can be overridden with PARATRACK_BASE. Signs in once (the login
rate limit is ten attempts a minute) and deletes every endpoint it creates.

The address it registers points back at the server itself, so the outbound
delivery is refused by network policy. That is the case worth automating: the
result must still be visible on the same screen, and the history must render the
request body without a response body next to it. A 2xx answer needs a real
public receiver.

    PARATRACK_BASE=http://127.0.0.1:8899 .venv/bin/python e2e/test_webhook_test_run.py
"""

from __future__ import annotations

import os
import sys
import time

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE

OWNER_EMAIL = os.environ.get("PARATRACK_OWNER_EMAIL", "doc-1791228444@x.test")
OWNER_PASSWORD = os.environ.get("PARATRACK_OWNER_PASSWORD", "longenoughpw")
PROBE_PATH = "/__webhook_test_run_probe"

ROOT = "#paratrack-react-root"
TEST_BUTTON = "Отправить тест"
results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    results.append((name, ok, detail))
    print(f"{'PASS' if ok else 'FAIL'}  {name}{'' if ok else ' — ' + detail}")


def sign_in(page) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", OWNER_EMAIL)
    page.fill("#password", OWNER_PASSWORD)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url:
        raise SystemExit(f"sign-in failed: still on {page.url}")


def open_webhooks(page) -> None:
    page.goto(BASE + "/settings/webhooks")
    page.locator(f"{ROOT} main").wait_for()


def endpoint_cards(page):
    return page.locator(f"{ROOT} article")


def card_for(page, url: str):
    return endpoint_cards(page).filter(has_text=url)


def add_endpoint(page, url: str, secret: str, all_events: bool = False) -> None:
    page.fill("#webhook-url", url)
    page.fill("#webhook-secret", secret)
    box = page.get_by_role("checkbox", name="Все события")
    if all_events and box.count() == 1:
        box.check()
    page.get_by_role("button", name="Добавить вебхук").click()
    page.wait_for_load_state("load")
    expect(card_for(page, url)).to_have_count(1)


def run_test(page, url: str) -> None:
    card = card_for(page, url)
    card.locator(f'form[action$="/test"] button').click()
    page.wait_for_load_state("load")
    expect(card_for(page, url)).to_have_count(1)


def delete_endpoint(page, url: str) -> None:
    card = card_for(page, url)
    if card.count() == 0:
        return
    card.get_by_role("button", name="Удалить вебхук").click()
    page.wait_for_load_state("load")


def run(browser) -> None:
    context = browser.new_context()
    page = context.new_page()
    errors: list[str] = []
    page.on("pageerror", lambda error: errors.append(str(error)))

    sign_in(page)
    open_webhooks(page)
    cards_before = endpoint_cards(page).count()

    url = f"{BASE}{PROBE_PATH}"
    add_endpoint(page, url, "webhook-test-run-secret")
    check("a new endpoint card offers a test button", card_for(page, url).locator(f'form[action$="/test"] button').count() == 1)
    check(
        "the test button is labelled for the action it performs",
        TEST_BUTTON in card_for(page, url).locator(f'form[action$="/test"] button').inner_text(),
        card_for(page, url).locator(f'form[action$="/test"] button').inner_text(),
    )

    run_test(page, url)
    check("the test run comes back to the same screen with the endpoint marked", f"test=" in page.url, page.url)
    check("the outcome is announced on the screen", page.locator(f'{ROOT} [role="status"], {ROOT} [role="alert"]').count() >= 1, page.url)

    card = card_for(page, url)
    delivery = card.locator("li").first
    text = delivery.inner_text()
    check("the delivery history has an attempt right away", "Последние доставки" in card.inner_text(), card.inner_text()[:200])
    check("the attempt is marked as a test run", "тест" in text.lower(), text)
    check(
        "the sent request body is readable",
        '"action":"test"' in text and '"event":"session.stopped"' in text,
        text,
    )
    check(
        "a failed delivery explains itself instead of showing an empty code",
        "destination is not allowed" in text or "delivery failed" in text,
        text,
    )
    details = delivery.locator("details")
    check("the body sits behind a details block that opens on the test row", details.count() == 1 and details.is_visible(), text)
    if details.count() == 1:
        opened = details.evaluate("node => node.open")
        if not opened:
            details.locator("summary").click()
        check(
            "the details block reveals the stored body",
            '"action":"test"' in details.inner_text(),
            details.inner_text()[:200],
        )
    check(
        "a delivery without a response body leaves the page intact",
        card.locator("details").count() == 1 and not errors,
        f"details={card.locator('details').count()} errors={errors}",
    )

    open_webhooks(page)
    wildcard_url = f"{BASE}{PROBE_PATH}_star"
    add_endpoint(page, wildcard_url, "webhook-test-run-secret", all_events=True)
    check(
        "one box subscribes the endpoint to every event",
        "Все события" in card_for(page, wildcard_url).inner_text(),
        card_for(page, wildcard_url).inner_text()[:200],
    )

    delete_endpoint(page, wildcard_url)
    delete_endpoint(page, url)
    open_webhooks(page)
    check(
        "cleanup restored the endpoint list",
        endpoint_cards(page).count() == cards_before,
        f"cards before={cards_before} after={endpoint_cards(page).count()}",
    )
    check("no page errors on the webhooks screen", not errors, str(errors))
    context.close()


def main() -> int:
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        try:
            run(browser)
        finally:
            browser.close()
    failed = [name for name, ok, _ in results if not ok]
    print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())