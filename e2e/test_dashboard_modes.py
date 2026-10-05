"""Mode-specific dashboard priorities with deterministic data and no server writes."""
import asyncio,json
from urllib.parse import urlparse
from playwright.async_api import async_playwright
from test_react_navigation import document,STATIC


def mode_document(mode,manager=True):
    html=document('/')
    start=html.index('<div id="react-page-data" hidden>')+len('<div id="react-page-data" hidden>');end=html.index('</div>',start)
    bootstrap=json.loads(html[start:end])
    mods={'goals':True,'invoices':mode!='solo','schedule':mode=='studio','payroll':mode=='studio'}
    bootstrap['shell'].update(mods=mods,canManage=manager)
    bootstrap['data'].update(Mode=mode,CanManage=manager,RunningCount=0,PausedCount=0,Mods=mods,Widgets={'goals':True,'unbilled':True},Goals=[{'ID':1,'ActivityName':'Reading','PeriodRangeLabel':'this week','AchievedLabel':'1h','TargetLabel':'3h','Percent':33}],Unbilled=[{'ProjectID':77,'ProjectName':'Client work','Slug':'client','Hours':'2.5','Amount':'5000 ₽','SinceISO':'2026-10-01'}])
    return html[:start]+json.dumps(bootstrap)+html[end:]


def project_document(mode):
    html=document('/projects/new')
    start=html.index('<div id="react-page-data" hidden>')+len('<div id="react-page-data" hidden>');end=html.index('</div>',start)
    bootstrap=json.loads(html[start:end])
    bootstrap['data']={'NewProject':True,'Lang':'ru','CSRFToken':'test','Mods':{'invoices':mode!='solo'},'Currencies':[{'Code':'RUB','Label':'RUB'}],'TeamCurrency':'RUB'}
    return html[:start]+json.dumps(bootstrap)+html[end:]


async def main():
    async with async_playwright() as p:
        browser=await p.chromium.launch()
        for width in [1440,390]:
            for mode,manager in [('solo',True),('freelance',True),('studio',True),('custom',True),('studio',False)]:
                ctx=await browser.new_context(viewport={'width':width,'height':900});page=await ctx.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
                async def respond(route):
                    path=urlparse(route.request.url).path
                    if path.startswith('/static/'):await route.fulfill(path=str(STATIC/path.removeprefix('/static/')))
                    else:await route.fulfill(content_type='text/html',body=project_document(mode) if path=='/projects/new' else mode_document(mode,manager))
                await ctx.route('**/*',respond);await page.goto('http://paratrack.test/')
                await page.locator('[data-dashboard-widgets]').wait_for()
                titles=await page.locator('[data-dashboard-widgets] [data-slot="card-title"]').all_text_contents()
                assert titles==(['Цели'] if mode=='solo' or not manager else ['Не выставлено','Цели'] if mode=='freelance' else ['Цели','Не выставлено']),titles
                team=page.locator('#main nav[aria-label="Работа с командой"]')
                assert await team.count()==int(mode=='studio')
                if mode=='studio':
                    expected=['/schedule','/settings/members','/settings/invites','/payroll'] if manager else ['/schedule']
                    assert await team.locator('a').evaluate_all('links=>links.map(a=>a.getAttribute("href"))')==expected
                if manager:assert await page.locator('#main header a[href="/settings/sections"]').count()==1
                else:assert await page.locator('#main header a[href="/settings/sections"]').count()==0
                assert not errors,errors
                assert await page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                await page.screenshot(path=f'/tmp/paratrack-mode-{width}-{mode}-{manager}.png',full_page=True)
                if manager:
                    await page.goto('http://paratrack.test/projects/new')
                    await page.locator('#new-project-name').wait_for()
                    assert await page.locator('#new-project-rate').is_visible()==(mode!='solo')
                    if mode=='solo':
                        await page.locator('#main form [data-disclosure] > button').click()
                        assert await page.locator('#new-project-rate').is_visible()
                if manager:
                    await page.locator('#new-project-name').fill('Project')
                    await page.locator('#new-project-slug').fill('BAD URL')
                    await page.locator('#main form [data-disclosure] > button').click()
                    await page.get_by_role('button',name='Создать',exact=True).click()
                    assert await page.locator('#new-project-slug').is_visible(), 'Invalid project URL must become visible'
                print(f'PASS {width}px {mode} manager={manager}: priorities and permissions')
                await ctx.close()
        await browser.close()

if __name__=='__main__':asyncio.run(main())
