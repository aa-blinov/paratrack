"""An empty timer is answered by the application in the screen language, not by
the browser's English bubble.

Run after `cd web && npm run build:react`: python3 e2e/test_timer_activity_hint.py
No server, account, or database is required.
"""
import asyncio
import json
from urllib.parse import urlparse, parse_qs
from playwright.async_api import async_playwright
from test_react_navigation import document, STATIC


async def check(browser, width):
    context = await browser.new_context(viewport={'width': width, 'height': 720})
    page = await context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))
    html = document('/')
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    bootstrap = json.loads(html[start:end])
    payload = {}
    submitted = asyncio.Event()

    async def respond(route):
        path = urlparse(route.request.url).path
        if path.startswith('/static/'):
            await route.fulfill(path=str(STATIC / path.removeprefix('/static/')))
        elif path == '/api/start':
            payload.update(parse_qs(route.request.post_data, keep_blank_values=True))
            submitted.set()
            await route.fulfill(json={})
        elif path == '/api/dashboard':
            await route.fulfill(json={'data': bootstrap['data']})
        else:
            await route.fulfill(content_type='text/html', body=html)
    await context.route('**/*', respond)
    await page.goto('http://paratrack.test/')
    form = page.locator('#main form:has(#activity)')
    await form.wait_for()
    # Native validation would answer in the browser's language, which on a
    # Russian screen reads «Please fill out this field».
    assert await form.evaluate('node => node.noValidate'), 'Native validation must be off'
    assert await page.locator('#activity').evaluate('node => node.required'), 'The server contract stays'
    hint = page.locator('#activity-required')
    assert await hint.count() == 1
    assert await hint.evaluate('node => node.getAttribute("role")') == 'alert'
    assert (await hint.inner_text()).strip() == '', 'Nothing to announce before a mistake'

    await page.locator('#activity').click()
    await page.get_by_role('button', name='Старт', exact=True).click()
    await page.wait_for_timeout(200)
    assert not submitted.is_set(), 'An empty timer must not reach the server'
    assert (await hint.inner_text()).strip() != '', 'The hint must say something'
    assert await page.locator('#activity').evaluate('node => node.getAttribute("aria-invalid")') == 'true'
    assert await page.locator('#activity').evaluate('node => node.getAttribute("aria-describedby")') == 'activity-required'
    assert await page.evaluate('document.activeElement.id') == 'activity', 'Focus returns to the field'
    # The hint belongs to the page, not to a floating browser bubble.
    assert await hint.evaluate('node => !!node.closest("#main")')
    box = await hint.bounding_box()
    field = await page.locator('#activity').bounding_box()
    assert box and field and 0 <= box['y'] - (field['y'] + field['height']) < 60, 'The hint sits by the field'

    # Typing clears it, and Enter from the field starts the timer.
    await page.keyboard.type('Правка макета')
    await page.wait_for_timeout(100)
    assert (await hint.inner_text()).strip() == ''
    assert await page.locator('#activity').get_attribute('aria-invalid') is None
    await page.keyboard.press('Enter')
    await asyncio.wait_for(submitted.wait(), 5)
    assert payload['activity'] == ['Правка макета'], payload
    assert not errors, errors
    print(f'PASS {width}px: empty timer answered by the app, focus and Enter predictable')
    await context.close()


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()
        for width in [1440, 1280]:
            await check(browser, width)
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())