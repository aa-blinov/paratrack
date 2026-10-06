"""Built UI regression checks, with deterministic HTML and delayed page chunks.

Run after `cd web && npm run build:react`: python3 e2e/test_react_navigation.py
No server, account, or database is required.
"""
import asyncio
import json
from pathlib import Path
from urllib.parse import urlparse

from playwright.async_api import async_playwright

STATIC = Path(__file__).resolve().parents[1] / "internal/web/static"


def document(path, lang="ru"):
    active = "projects" if path == "/projects" else "dashboard"
    data = {"Lang": lang, "Active": active, "CSRFToken": "test",
            "Activities": [], "Projects": [], "ActiveSessions": [], "Recent": [],
            "Goals": [], "Unbilled": [], "Widgets": {}, "Mods": {},
            "TodayTotal": "0 ч", "HasSession": True, "HasProject": True}
    if path == "/projects":
        data["ShowArchived"] = False
    shell = {"title": "Проекты" if active == "projects" else "Обзор",
             "active": active, "lang": lang, "user": {"id": 1, "name": "Тест", "email": "test@example.test"},
             "team": {"id": 1, "name": "Моя команда"}, "userTeams": [],
             "requestPath": path, "csrfToken": "test", "canManage": True, "mods": None, "tabs": []}
    if lang == "en":
        shell["title"] = "Projects" if active == "projects" else "Dashboard"
    payload = json.dumps({"data": data, "shell": shell})
    return f'''<!doctype html><html lang="{lang}" data-theme="paratrack-light"><head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width,initial-scale=1">
    <title>{shell['title']}</title><link rel="stylesheet" href="/static/css/paratrack.css">
    <link rel="stylesheet" href="/static/ui/app.css">
    <script type="module" blocking="render" src="/static/ui/app.js"></script></head><body data-i18n-undo="{'Undo' if lang == 'en' else 'Отменить'}">
    <div id="react-page-data" hidden>{payload}</div><div id="paratrack-react-root"><div class="app-boot"><h1>Загрузка…</h1></div></div>
    </body></html>'''


async def check(browser, width):
    context = await browser.new_context(viewport={"width": width, "height": 900})
    page = await context.new_page()
    await page.add_init_script('''new PerformanceObserver(list => {
      for (const entry of list.getEntries()) {
        if (entry.name === 'first-contentful-paint') window.firstPaintHadPage = !!document.querySelector('#main h1');
      }
    }).observe({type:'paint', buffered:true});''')
    # Simulate the language endpoint redirect without contacting a real host.
    await page.add_init_script('''const nativeFetch = window.fetch;
      window.fetch = async (...args) => {
        const response = await nativeFetch(...args);
        if (new URL(args[0], location.href).pathname.startsWith("/lang/"))
          Object.defineProperty(response, "url", {value: location.origin + "/"});
        return response;
      };''')
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    chunk_started = asyncio.Event()
    release_chunk = asyncio.Event()

    language = "ru"

    async def respond(route):
        nonlocal language
        path = urlparse(route.request.url).path
        if path.startswith("/lang/"):
            language = path.rsplit("/", 1)[1]
            await route.fulfill(content_type="text/html", body=document("/", language))
        elif path.startswith("/static/"):
            if path == "/static/ui/app.js":
                await asyncio.sleep(0.4)
            if "project-list-" in path:
                chunk_started.set()
                await release_chunk.wait()
            file = STATIC / path.removeprefix("/static/")
            if file.is_file():
                await route.fulfill(path=str(file))
            else:
                await route.fulfill(status=404)
        else:
            await route.fulfill(content_type="text/html", body=document(path, language))

    await context.route("**/*", respond)
    await page.goto("http://paratrack.test/")
    await page.locator("#main h1").wait_for()
    await page.wait_for_function("window.firstPaintHadPage !== undefined")
    assert await page.evaluate("window.firstPaintHadPage"), "Browser painted before React was ready"
    if width < 1024:
        # Below 1024px the drawer is gone and the bar carries the navigation, so
        # the drawer's own checks belong to the widths that still have one.
        await open_more_sheet(page)
        assert await page.locator('[data-mobile-nav] a[aria-current="page"]').count() == 1, 'The bar marks where you are'
        assert await page.locator('[data-mobile-nav] a').evaluate_all(
            'items => items.slice(0,4).map(item => item.textContent)') == ['Обзор', 'Табель', 'Статистика', 'По часам']
        await page.keyboard.press('Escape')
        nav = page.locator('[data-mobile-nav]')
        # "Проекты" is not in the bar — on a phone it is one tap away in the
        # sheet, which is the path worth proving.
        target = ("/projects", "Проекты")
    else:
        nav = page.locator('[data-sidebar="sidebar"]:visible')
        assert await nav.locator('[data-slot="sidebar-menu-button"][data-active]').count() == 1
        assert await nav.locator('[data-slot="sidebar-menu-button"]').evaluate_all('items => items.slice(0,4).map(item => item.textContent)') == ['Обзор','Проекты','Табель','Статистика']
        assert await nav.locator('[data-slot="sidebar-group-label"]').all_text_contents() == ['Меню','Анализ','Деньги','Команда','Данные','Настройки']
        active = nav.locator('[aria-current="page"]')
        inactive = nav.locator('a[href="/projects"]')
        target = ("/projects", "Проекты")
    if width >= 1024:
        assert await active.evaluate("e => getComputedStyle(e).backgroundColor") != await inactive.evaluate("e => getComputedStyle(e).backgroundColor")
    await page.evaluate("""() => {
      window.shellBefore = document.querySelector('.app-shell-header');
      window.blankFrames = 0;
      window.checkFrames = true;
      function check() {
        if (!document.querySelector('#main h1')) window.blankFrames++;
        if (window.checkFrames) requestAnimationFrame(check);
      }
      requestAnimationFrame(check);
    }""")
    if width < 1024:
        await open_more_sheet(page)
        inactive = page.locator('[role="dialog"] a[href="/projects"]')
    await inactive.click()
    await asyncio.wait_for(chunk_started.wait(), 10)
    await page.wait_for_timeout(250)
    assert await page.locator("#main h1").inner_text() == "Обзор"
    assert urlparse(page.url).path == "/"
    if width < 768:
        await page.locator('[data-mobile="true"]').wait_for(state="hidden")
    release_chunk.set()
    await page.wait_for_url(f"**{target[0]}")
    await page.locator("#main h1").filter(has_text=target[1]).wait_for()
    await page.wait_for_timeout(100)
    assert await page.evaluate("window.blankFrames") == 0
    assert await page.evaluate("window.shellBefore === document.querySelector('.app-shell-header')")
    assert await page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    await page.evaluate("window.checkFrames = false")
    await page.screenshot(path=f"/tmp/paratrack-nav-{width}.png", full_page=True)
    await page.go_back()
    await page.locator("#main h1").filter(has_text="Обзор").wait_for()
    await page.evaluate("window.sameDocument = true")
    if width < 1024:
        await open_more_sheet(page)
    await page.locator('a[href^="/lang/en"]:visible').last.click()
    await page.locator("#main h1").filter(has_text="Dashboard").wait_for()
    assert await page.evaluate("window.sameDocument && document.documentElement.lang === 'en'")
    assert await page.locator("body").get_attribute("data-i18n-undo") == "Undo"
    if width < 1024:
        await open_more_sheet(page)
    await page.locator('a[href^="/lang/ru"]:visible').last.click()
    await page.locator("#main h1").filter(has_text="Обзор").wait_for()
    assert await page.evaluate("document.documentElement.lang === 'ru'")
    if width >= 1024:
        # A phone has no drawer to collapse; the state is a desktop one.
        await page.locator(".app-shell-sidebar-trigger").click()
        await page.reload()
        await page.locator('[data-slot="sidebar"][data-state="collapsed"]').wait_for()
    assert not errors, errors
    print(f"PASS {width}px: active item, delayed navigation, stable shell, back, overflow, console")
    await context.close()


async def open_more_sheet(page):
    """Below 1024px the bottom bar is the navigation; the drawer is not there.
    The bar's single button is the «Ещё» sheet — matched by position, not by
    its label, so this keeps working after a language switch."""
    await page.locator("[data-mobile-nav] button").click()
    await page.locator('[role="dialog"]').wait_for()


async def check_member_menu(browser):
    context = await browser.new_context(viewport={"width": 390, "height": 900})
    page = await context.new_page()
    async def respond(route):
        path = urlparse(route.request.url).path
        if path.startswith("/static/"):
            await route.fulfill(path=str(STATIC / path.removeprefix("/static/")))
        else:
            html = document(path).replace('"canManage": true', '"canManage": false').replace('"mods": null', '"mods": {"schedule": true}')
            await route.fulfill(content_type="text/html", body=html)
    await context.route("**/*", respond)
    await page.goto("http://paratrack.test/")
    await open_more_sheet(page)
    nav = page.locator('[role="dialog"]')
    # Four destinations are already in the bar; the sheet carries the rest, and
    # nothing a member cannot open.
    assert await nav.locator('a.mobile-nav-row').evaluate_all(
        'items => items.map(item => item.getAttribute("href"))') == [
            '/projects', '/schedule', '/export', '/settings/preferences', '/settings/profile',
            '/settings/tokens', '/help', '/lang/en?next=%2F']
    assert '/settings/team' not in await nav.locator('a.mobile-nav-row').evaluate_all(
        'items => items.map(item => item.getAttribute("href"))'), 'a member is not offered a screen that answers 403'
    print("PASS member: disabled modules, manager-only links, and empty groups hidden")
    await context.close()


async def main():
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch()
        for width in (1440, 820, 390):
            await check(browser, width)
        await check_member_menu(browser)
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
