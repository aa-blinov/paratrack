"""The navigation list must admit it continues instead of cutting a row in half.

Run after `cd web && npm run build:react`: python3 e2e/test_sidebar_overflow.py
No server, account, or database is required.
"""
import asyncio
from playwright.async_api import async_playwright
from test_react_navigation import document, STATIC


async def serve(context, page):
    async def respond(route):
        from urllib.parse import urlparse
        path = urlparse(route.request.url).path
        if path.startswith('/static/'):
            await route.fulfill(path=str(STATIC / path.removeprefix('/static/')))
        else:
            await route.fulfill(content_type='text/html', body=document('/'))
    await context.route('**/*', respond)


async def check(browser, width, height):
    context = await browser.new_context(viewport={'width': width, 'height': height})
    page = await context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))
    await serve(context, page)
    await page.goto('http://paratrack.test/')
    nav = page.locator('nav.app-shell-desktop-nav')
    await nav.wait_for()
    scroller = page.locator('[data-slot="sidebar-content"]')
    measure = """() => {
      const nav = document.querySelector('nav.app-shell-desktop-nav');
      const box = nav.closest('[data-slot="sidebar-content"]');
      const style = getComputedStyle(box);
      return {
        links: nav.querySelectorAll('a').length,
        client: box.clientHeight, scroll: box.scrollHeight,
        scrollbarWidth: style.scrollbarWidth,
        webkit: getComputedStyle(box, '::-webkit-scrollbar').width,
      };
    }"""
    state = await page.evaluate(measure)
    assert state['scroll'] > state['client'], f'{width}x{height} is expected to overflow the sidebar'
    # An overlay scrollbar takes no space and stays invisible until you scroll,
    # which is what left the last row looking like a broken layout.
    assert state['scrollbarWidth'] == 'thin', state
    assert state['webkit'] not in ('', 'auto'), state
    more = page.locator('[data-nav-more="below"]')
    assert await more.count() == 1, 'A cut-off list must say it continues'
    assert await more.is_visible()
    await scroller.evaluate('box => box.scrollTo(0, box.scrollHeight)')
    await page.wait_for_timeout(150)
    assert await more.count() == 0, 'Nothing is cut off at the bottom, so nothing is announced'
    await scroller.evaluate('box => box.scrollTo(0, 0)')
    await page.wait_for_timeout(150)
    assert await more.count() == 1
    # The sidebar keeps its <nav aria-label>: the keyboard shortcuts look the
    # links up through it.
    assert await page.locator('nav[aria-label="Меню"]').count() == 1
    await page.keyboard.press('s')
    await page.wait_for_timeout(400)
    assert not errors, errors
    print(f'PASS {width}x{height}: thin scrollbar, continuation marker, keyboard shortcuts intact')
    await context.close()


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()
        for width, height in [(1280, 720), (1366, 768), (1440, 900)]:
            await check(browser, width, height)
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())