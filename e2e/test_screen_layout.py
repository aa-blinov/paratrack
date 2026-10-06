"""Built UI checks for document history, disclosures and form payloads. No real writes."""
import asyncio,json
from urllib.parse import urlparse,parse_qs
from playwright.async_api import async_playwright
from test_react_navigation import document,STATIC


def invoice_document():
    html=document('/invoices')
    start=html.index('<div id="react-page-data" hidden>')+len('<div id="react-page-data" hidden>')
    end=html.index('</div>',start)
    bootstrap=json.loads(html[start:end])
    bootstrap['shell'].update(active='invoices',requestPath='/invoices',title='Счета')
    bootstrap['data']={'InvoicesReact':True,'Lang':'ru','CSRFToken':'test','Billable':True,'Items':[{'ID':1,'Number':'INV-2026-001','Client':'Клиент','Status':'draft','Period':'1–5 октября','Hours':'2','Total':'5000 ₽'}],'Projects':[],'Prefill':{'ClientName':'','ClientEmail':'','ClientDetails':''},'Unbilled':[],'Unassigned':[],'DefStart':'2026-10-01','DefEnd':'2026-10-05','Flash':'','FlashOK':True}
    return html[:start]+json.dumps(bootstrap)+html[end:]


async def check(browser,width):
    context=await browser.new_context(viewport={'width':width,'height':900})
    page=await context.new_page();errors=[];page.on('pageerror',lambda e: errors.append(str(e)))
    submitted=asyncio.Event();fields={}
    async def respond(route):
        path=urlparse(route.request.url).path
        if path.startswith('/static/'):
            await route.fulfill(path=str(STATIC/path.removeprefix('/static/')))
        elif route.request.method=='POST':
            fields.update(parse_qs(route.request.post_data,keep_blank_values=True));submitted.set()
            await route.fulfill(status=204)
        else:
            await route.fulfill(content_type='text/html',body=invoice_document())
    await context.route('**/*',respond)
    await page.goto('http://paratrack.test/invoices')
    form=page.locator('#new')
    await form.wait_for()
    assert await form.get_attribute('data-state') == 'closed', 'History comes first for returning users'
    assert await page.get_by_role('link',name='INV-2026-001').is_visible()
    await page.get_by_role('link',name='Новый счёт',exact=True).click()
    assert await form.get_attribute('data-state') == 'open'
    await page.locator('#inv-client').fill('Новый клиент')
    optional=form.locator('[data-disclosure]')
    assert await optional.get_attribute('data-state') == 'closed'
    await optional.locator(':scope > button').click()
    await page.locator('#inv-email').fill('invalid')
    await optional.locator(':scope > button').click()
    await form.get_by_role('button',name='Сформировать',exact=True).click()
    assert await page.locator('#inv-email').is_visible(), 'Invalid hidden fields must be revealed'
    assert not submitted.is_set()
    await page.locator('#inv-email').fill('client@example.test')
    await page.locator('#inv-client-details').fill('Реквизиты клиента')
    await page.locator('#inv-notes').fill('Комментарий')
    await optional.locator(':scope > button').click()
    await form.get_by_role('button',name='Сформировать',exact=True).click()
    await asyncio.wait_for(submitted.wait(),5)
    assert fields['client']==['Новый клиент']
    assert fields['client_email']==['client@example.test']
    assert fields['client_details']==['Реквизиты клиента']
    assert fields['notes']==['Комментарий']
    assert fields['start']==['2026-10-01'] and fields['end']==['2026-10-05']
    assert not errors,errors
    assert await page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    print(f'PASS {width}px: history visible, new invoice reachable, hidden validation, complete form payload')
    await context.close()


async def check_timer(browser, width):
    context = await browser.new_context(viewport={"width": width, "height": 900})
    page = await context.new_page()
    html = document("/")
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    bootstrap = json.loads(html[start:end])
    bootstrap['data']['Projects'] = [{'ID': 77, 'Name': 'Client project'}]
    html = html[:start] + json.dumps(bootstrap) + html[end:]
    payload = {}; submitted = asyncio.Event()
    async def respond(route):
        path = urlparse(route.request.url).path
        if path.startswith('/static/'):
            await route.fulfill(path=str(STATIC / path.removeprefix('/static/')))
        elif path == '/api/start':
            payload.update(parse_qs(route.request.post_data)); submitted.set()
            await route.fulfill(json={})
        elif path == '/api/dashboard':
            await route.fulfill(json={'data': bootstrap['data']})
        else:
            await route.fulfill(content_type='text/html', body=html)
    await context.route('**/*', respond)
    await page.goto('http://paratrack.test/')
    await page.locator('#activity').fill('Work')
    details = page.locator('#main form [data-disclosure]')
    await details.locator(':scope > button').click()
    await page.locator('#project_id').click()
    await page.get_by_role('option', name='Client project', exact=True).click()
    await page.locator('#timer-note').fill('Meeting notes')
    await details.locator(':scope > button').click()
    await page.get_by_role('button', name='Старт', exact=True).click()
    await asyncio.wait_for(submitted.wait(), 5)
    assert payload['activity'] == ['Work']
    assert payload['project_id'] == ['77']
    assert payload['note'] == ['Meeting notes']
    print(f'PASS {width}px: timer submits selected project and note with optional fields collapsed')
    await context.close()


async def main():
    async with async_playwright() as p:
        browser=await p.chromium.launch()
        for width in [1440,820,390]:
            await check(browser,width)
            await check_timer(browser,width)
        await browser.close()

if __name__=='__main__':asyncio.run(main())
