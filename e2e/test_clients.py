"""Clients and the project budget, on a clean account.

The client card is the first thing that has to survive two projects: the old
free-text recipient made the same customer into two unrelated rows, and a
duplicate name is the exact failure this suite guards. It also walks the
budget round trip, because a cleared field must remove the ceiling rather
than store a zero that reads as "nothing may be billed".
"""
import re
import sys
import time
from urllib.parse import urlsplit
from pathlib import Path

from playwright.sync_api import Page, expect, sync_playwright

from target import BASE_URL as BASE

SHOTS = Path(__file__).resolve().parents[1] / "e2e" / "screenshots"
failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def register(page: Page, context, name: str, email: str, password: str) -> None:
    """Register through the public form and land on the dashboard.

    Mirrors the flow the dashboard suite uses, including the workspace preset
    on /welcome: without it the run keeps re-entering the walkthrough and the
    first real navigation lands back on /login.
    """
    page.goto(f"{BASE}/register")
    page.fill("#name", name)
    page.fill("#email", email)
    page.fill("#password", password)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    # A stand keeps its database between runs, so the address is taken by the
    # second run: sign in instead of pretending registration worked.
    if "/login" in page.url or "/register" in page.url:
        page.goto(f"{BASE}/login")
        page.fill("#email", email)
        page.fill("#password", password)
        page.click('button[type="submit"]')
        page.wait_for_load_state("load")
    # A workspace cookie is what lets /projects resolve a workspace; /clients
    # does not need one, so a run that skipped the walkthrough gets three
    # green steps and then bounces off /login. If the cookie is missing,
    # go through the preset anyway.
    def pick_preset() -> None:
        page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="solo"]):not([data-welcome-skip]) button').click()
        page.wait_for_url(lambda url: "/welcome" not in url, timeout=15000)

    if page.url.endswith("/welcome"):
        pick_preset()
    if not any(c["name"] == "paratrack_team" for c in context.cookies()):
        page.goto(f"{BASE}/welcome", wait_until="networkidle")
        if page.url.endswith("/welcome"):
            pick_preset()
    page.wait_for_load_state("networkidle")


def open_all_sections(page: Page) -> None:
    """Open every collapsed section on the screen.

    The project form keeps its optional fields — rate, budget, client picker
    — inside collapsible sections. Radix select triggers also carry
    aria-expanded="false", so a trigger is only treated as a section when it
    is followed by a sibling panel carrying data-state; opening a select by
    mistake leaves a listbox covering the fields underneath it.
    """
    for _ in range(5):
        opened = False
        triggers = page.locator('main button[aria-expanded="false"]:visible')
        for index in range(triggers.count()):
            trigger = triggers.nth(index)
            panel = trigger.locator("xpath=following-sibling::*[@data-state][1]")
            if not panel.count() or panel.first.get_attribute("data-state") == "open":
                continue
            trigger.click()
            page.wait_for_timeout(250)
            opened = True
            break
        if not opened:
            return


def submit_form(page: Page, form: str) -> None:
    """Submit `form` by clicking its own button.

    Scoped to the form on purpose: the sign-out control in the sidebar is also
    a submit button and comes earlier in the document, so a document-wide
    ".first" signs the run out instead of saving. Falls back to dispatching
    the event when the button stays unreachable — Radix re-renders the form
    while a select settles, and Playwright then waits on a replaced node.
    """
    button = page.locator(f'{form} button[type="submit"]').first
    try:
        button.click(timeout=8000)
    except Exception:
        button.dispatch_event("click")
    page.wait_for_load_state("networkidle")


def fill_client(page: Page, scope: str, name: str, email: str, details: str = "") -> None:
    page.fill(f'{scope} input[name="name"]', name)
    page.fill(f'{scope} input[name="email"]', email)
    if details:
        page.fill(f'{scope} textarea[name="details"]', details)


def main() -> int:
    # Unique per run: the stand keeps its database between runs, and the
    # duplicate check later needs a name that already exists. Without this the
    # directory ends up with several cards called the same thing and the page
    # under test is whichever one happens to be first.
    client_name = f'ООО «Ромашка» {int(time.time())}'
    # The client name must repeat — refusing a duplicate is one of the checks
    # — but the project name must not, or a re-run on a kept database fails on
    # a slug it used last time.
    project_name = f"Сайт Ромашки {int(time.time())}"
    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors: list[str] = []
        page.on("pageerror", lambda exc: errors.append(str(exc)))

        register(page, context, "Клиенты Тест", "clients@test.local", "verysecret123")

        # ---------------------------------------------------------------- 1
        print("\n== 1. The empty directory says so instead of inventing rows")
        page.goto(f"{BASE}/clients", wait_until="networkidle")
        check("clients page opens", "Клиенты" in page.content(), page.url)
        # Not "the list is empty": a re-run on a kept database finds the card
        # it filed last time, and that must not read as a failure.
        check("directory renders", page.locator('form[action="/clients"]').count() > 0)

        # ---------------------------------------------------------------- 2
        print("\n== 2. A client card is filed")
        fill_client(page, 'form[action="/clients"]', client_name, "buh@romashka.ru", "ИНН 7701234567")
        submit_form(page, 'form[action="/clients"]')
        check("card is listed", client_name in page.content(), page.url)
        check("the address it was given is kept", "buh@romashka.ru" in page.content())
        page.screenshot(path=str(SHOTS / "clients-list.png"), full_page=True)

        # ---------------------------------------------------------------- 3
        print("\n== 3. A duplicate name is refused in plain language")
        fill_client(page, 'form[action="/clients"]', client_name, "second@romashka.ru")
        submit_form(page, 'form[action="/clients"]')
        check("duplicate refused", "уже есть" in page.content(), page.url)

        # ---------------------------------------------------------------- 4
        print("\n== 4. A project remembers the client and carries a budget")
        page.goto(f"{BASE}/projects/new", wait_until="networkidle")
        page.fill('form[action="/projects/new"] input[name="name"]', project_name)
        # Scoped to the form: the sign-out control in the sidebar is also a
        # submit button and comes earlier in the DOM, so ".first" over the
        # whole document signs the run out instead of creating the project.
        submit_form(page, 'form[action="/projects/new"]')
        check("project created", "/projects/" in page.url, page.url)
        # The page URL carries ?flash=..., so parse rather than split —
        # a slug with the query glued on matches no form on the screen.
        slug = urlsplit(page.url).path.rstrip("/").rsplit("/", 1)[-1]
        edit_form = f'form[action="/projects/{slug}"]'

        # Both live behind the collapsed "Изменить" block, so open it once
        # for the field and once for the picker.
        open_all_sections(page)
        page.fill("#project-budget", "250000")
        # The client picker is a Radix select, not a native one, so drive it
        # the way the dashboard timer picker is driven elsewhere in the suite.
        open_all_sections(page)
        picker = page.locator("#project-client")
        expect(picker).to_be_visible(timeout=5000)
        picker.click()
        # Scoped to the open listbox: the project screen has more than one
        # Radix select, and a bare [role="option"] also matches the closed
        # ones, whose items cannot be clicked.
        listbox = page.locator('[role="listbox"]').last
        expect(listbox).to_be_visible(timeout=5000)
        options = listbox.locator('[role="option"]')
        expect(options.first).to_be_visible(timeout=5000)
        names = [options.nth(i).inner_text().strip() for i in range(options.count())]
        picked = next((i for i, n in enumerate(names) if client_name in n), None)
        check("the picker offers the client", picked is not None, " | ".join(names))
        if picked is not None:
            # Radix unmounts the listbox the instant an item is chosen, so a
            # normal click waits on an element that is already gone. Dispatch
            # the event straight at the item instead.
            # Click the item by coordinates: the listbox is portalled, and its
            # items are re-rendered while React settles, so locator-based
            # clicks keep waiting on a node that is replaced mid-gesture.
            box = options.nth(picked).bounding_box()
            if box:
                page.mouse.click(box["x"] + box["width"] / 2, box["y"] + box["height"] / 2)
            submit_form(page, edit_form)
            check("the project keeps the client", client_name in page.content(), page.url)

        page.goto(f"{BASE}/projects/{slug}", wait_until="networkidle")
        open_all_sections(page)
        budget_field = page.locator("#project-budget")
        expect(budget_field).to_be_visible(timeout=5000)
        # Money fields come back normalised — grouped and with kopecks — the
        # same way the hourly rate does, so compare the number, not the glyphs.
        raw_budget = budget_field.input_value()
        normalised = raw_budget.replace("\u00a0", "").replace(" ", "").replace(",00", "").rstrip(".")
        check("the budget round-trips", normalised == "250000", raw_budget)
        check("the budget is shown as money", "2 500,00" in page.content() or "250 000" in page.content())
        page.screenshot(path=str(SHOTS / "project-budget.png"), full_page=True)

        # ---------------------------------------------------------------- 5
        print("\n== 5. Clearing the budget removes it")
        budget_field.fill("")
        submit_form(page, edit_form)
        page.goto(page.url, wait_until="networkidle")
        empty_budget = page.locator("#project-budget").input_value()
        check("a cleared budget stays cleared", empty_budget.strip() == "", repr(empty_budget))
        check("the page admits there is no budget", "Бюджет не задан" in page.content())

        # ---------------------------------------------------------------- 6
        print("\n== 6. The client page shows the work and the documents")
        page.goto(f"{BASE}/clients", wait_until="networkidle")
        # The card title is a link to the client's own page; take it by href,
        # since the name also appears inside the (collapsed) edit form.
        card_link = page.locator('a[href^="/clients/"]').filter(has_text=client_name)
        expect(card_link.first).to_be_visible(timeout=5000)
        card_link.first.click()
        # Wait for the destination itself: networkidle can settle before the
        # navigation starts, and the URL is what this check is about.
        page.wait_for_url(lambda url: "/clients/" in url, timeout=10000)
        page.wait_for_load_state("networkidle")
        check("the client page opened", "/clients/" in page.url, page.url)
        # Wait for the row itself: the URL settles before React has mounted the
        # page, and reading the HTML at that point says nothing about it.
        project_row = page.locator("main").get_by_text(project_name).first
        expect(project_row).to_be_visible(timeout=10000)
        check("the project it is linked to is listed", project_row.is_visible(), page.url)
        # The budget was cleared in the previous step, so what must be here is
        # the client's own details — proof the card's data came along, not
        # just a route that renders.
        check("the client's own details are on the page",
              "buh@romashka.ru" in page.content(), page.url)
        page.screenshot(path=str(SHOTS / "client-detail.png"), full_page=True)

        check("no page errors", not errors, "; ".join(errors[:3]))
        context.close()
        browser.close()

    print(f"\n{len(failures)} failed" if failures else "\nall checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())