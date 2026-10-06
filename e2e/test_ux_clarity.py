"""Scenario checks for timesheet editing without writing to a real workspace."""
import asyncio,json
from urllib.parse import urlparse
from playwright.async_api import async_playwright
from test_react_navigation import document,STATIC

def timesheet_document():
 html=document('/timesheet')
 start=html.index('<div id="react-page-data" hidden>')+len('<div id="react-page-data" hidden>');end=html.index('</div>',start)
 boot=json.loads(html[start:end]);days=[{'ISO':f'2026-10-{5+i:02d}','Label':str(i+1),'Date':str(5+i),'IsToday':i==0} for i in range(7)]
 rows=[{'ActivityID':i,'ActivityName':'Одинаковая активность','ProjectID':i,'Color':'#123456','RowTotalLabel':'0 мин','Cells':[{'Index':j,'ISO':d['ISO'],'Min':0,'Secs':0,'IsToday':j==0} for j,d in enumerate(days)]} for i in [1,2]]
 boot['data']={'Lang':'ru','Active':'timesheet','TimesheetReact':True,'CSRFToken':'test','Rows':rows,'ProjectNames':{'1':'Первый проект','2':'Второй проект'},'Days':days,'DayTotalLabels':['0 мин']*7,'Others':[],'Added':[],'DateISO':'2026-10-05','WeekLabel':'5–11 октября','PrevWeek':'2026-09-28','NextWeek':'2026-10-12','GrandTotalLabel':'0 мин'}
 boot['shell']['active']='timesheet'
 return html[:start]+json.dumps(boot,ensure_ascii=False)+html[end:]

async def main():
 async with async_playwright() as p:
  browser=await p.chromium.launch()
  for width in [1440,390]:
   ctx=await browser.new_context(viewport={'width':width,'height':900});page=await ctx.new_page();errors=[];page.on('pageerror',lambda e: errors.append(str(e)))
   pending=asyncio.Event();release=asyncio.Event()
   async def respond(route):
    path=urlparse(route.request.url).path
    if path.startswith('/static/'):
     await route.fulfill(path=str(STATIC/path.removeprefix('/static/')))
    elif path=='/api/timesheet/cell':
     if 'minutes=50' in route.request.post_data:
      await route.fulfill(status=500,body='Не удалось сохранить',content_type='text/plain; charset=utf-8');return
     assert 'minutes=45' in route.request.post_data
     pending.set();await release.wait()
     await route.fulfill(json={'activityId':1,'activityName':'Одинаковая активность','color':'#123456','cells':[{'iso':f'2026-10-{5+i:02d}','secs':2700 if i==0 else 0,'min':45 if i==0 else 0} for i in range(7)],'rowTotal':2700,'rowTotalLabel':'45 мин','dayTotals':[{'secs':2700 if i==0 else 0,'total':'45 мин' if i==0 else '0 мин'} for i in range(7)],'grandTotal':2700,'grandTotalLabel':'45 мин'})
    else:await route.fulfill(content_type='text/html; charset=utf-8',body=timesheet_document())
   await ctx.route('**/*',respond);await page.goto('http://paratrack.test/timesheet')
   await page.locator('#ts-body').wait_for()
   for name in ['Первый проект','Второй проект']:assert await page.locator('#ts-body th').filter(has_text=name).count()==1
   field=page.get_by_role('spinbutton',name='Одинаковая активность — Первый проект — 2026-10-05',exact=True)
   await field.fill('45');await field.press('Enter');await asyncio.wait_for(pending.wait(),5)
   assert await page.get_by_role('status').inner_text()=='Сохраняем правку…'
   release.set();await page.get_by_role('status').filter(has_text='Сохранено:').wait_for()
   assert await field.input_value()=='45';assert await page.locator('#ts-row-1 th').inner_text()=='Одинаковая активность\nПервый проект'
   await field.fill('50');await field.press('Enter')
   await page.get_by_role('alert').filter(has_text='Не удалось сохранить').wait_for()
   assert await field.input_value()=='50', 'Failed save must preserve typed minutes'
   assert not errors,errors
   assert await page.evaluate('document.documentElement.scrollWidth <= innerWidth')
   await page.screenshot(path=f'/tmp/paratrack-ux-timesheet-{width}.png',full_page=True)
   print(f'PASS {width}px: project identity, Enter saves, pending/success, totals, no page overflow')
   await ctx.close()
  await browser.close()
asyncio.run(main())
