import {test,expect,type Page} from '@playwright/test'
import {calendar,groupJourney,type JourneyDay,type JourneyHistory} from '../src/services/journey'
const now='2026-11-11T08:00:00Z'
const observation=(data:unknown,status='available')=>({data,status,source:'synthetic-journey-fixture',observedAt:now,refreshSeconds:30,verified:false})
function entries(from='2026-11-05',to='2026-11-11'){
 return calendar(from,to,[]).map((day,index)=>{const recent=day.date>='2026-11-05',offset=recent?Math.round((Date.parse(day.date)-Date.parse('2026-11-05'))/86400000):-1,observed=recent&&offset!==1&&offset!==3;return {...day,observed,distanceKm:observed?(offset===0?0:14.65):null,drivingSeconds:observed?(offset===0?0:900):null,sampleCount:observed?8:0}})
}
function period(days:JourneyDay[]){const observed=days.filter(day=>day.observed);return {distanceKm:observed.length?observed.reduce((sum,day)=>sum+day.distanceKm!,0):null,drivingSeconds:observed.length?observed.reduce((sum,day)=>sum+day.drivingSeconds!,0):null,hasData:observed.length>0,validObservedDays:observed.length,sampleCount:observed.length*8,days}}
function report(query:URLSearchParams,empty=false):JourneyHistory{
 const preset=query.get('period')||'7',to=query.get('to')||'2026-11-11',from=preset==='all'?'2026-10-31':query.get('from')||new Date(Date.parse(to)-(Number(preset)-1)*86400000).toISOString().slice(0,10),days=empty?calendar(from,to,[]):entries(from,to)
 return {range:{period:preset,from,to,sourceFrom:empty?undefined:'2026-11-05',sourceTo:empty?undefined:'2026-11-11'},days,period:period(days),trips:empty?[]:[{id:'synthetic-one',start:'2026-11-11T00:00:00Z',end:'2026-11-11T00:15:00Z',distanceKm:14.65,drivingSeconds:900,observedSeconds:900,sampleCount:8}],totalTrips:empty?0:1,detailsTruncated:false,sourceTruncated:false,sourceFiles:2,unreadableFiles:0,noFixSentences:empty?180:4,badChecksums:0,indexedAt:now,source:'stock-recording-nmea',status:empty?'unknown':'available',reason:empty?'有効な連続測位データがありません':undefined}
}
async function fixture(page:Page,theme='light',empty=false){
 await page.clock.install({time:new Date(now)})
 const queries:URLSearchParams[]=[]
 await page.route('https://tile.openstreetmap.org/**',route=>route.abort())
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let data:unknown={}
  if(path.endsWith('/auth/session'))data={csrf:'synthetic-test'}
  else if(path.endsWith('/settings'))data={revision:1,theme,locale:'ja',favorites:[]}
  else if(path.endsWith('/capabilities'))data=[]
  else if(path.endsWith('/system/summary'))data={tripStats:observation({periods:Object.fromEntries([7,30,90,365].map(count=>{const from=new Date(Date.parse('2026-11-11')-(count-1)*86400000).toISOString().slice(0,10);return [count,period(empty?calendar(from,'2026-11-11',[]):entries(from,'2026-11-11'))]}))}),usage:observation(null),gps:observation({fix:false})}
  else if(path.endsWith('/trips')){queries.push(url.searchParams);data=observation(report(url.searchParams,empty),empty?'unknown':'available')}
  else if(path.endsWith('/radio/history'))data={unit:'dBm',points:[],cells:[],total:0,unplaced:0,accuracyExcluded:0,missing:0,outOfService:0,carriers:[],bands:[],truncated:false}
  else if(path.endsWith('/radio/heatmap/legend'))data={unit:'dBm',note:'',thresholds:[]}
  await route.fulfill({contentType:'application/json',body:JSON.stringify(data)})
 })
 return queries
}
test('calendar grouping preserves missing values, actual zero and partial totals',()=>{
 const daily=groupJourney(entries(),'day')
 expect(daily[0]?.distance).toBe(0);expect(daily[1]?.distance).toBe(null);expect(daily[1]?.cumulativeDistance).toBe(null);expect(daily[2]?.cumulativeDistance).toBe(14.65)
 const weeks=groupJourney(entries(),'week')
 expect(weeks).toHaveLength(2);expect(weeks[0]?.dayCount).toBe(4);expect(weeks[0]?.observedDays).toBe(2);expect(weeks[1]?.dayCount).toBe(3)
 expect(groupJourney(entries(),'month')[0]?.distance).toBeCloseTo(58.6)
})
for(const width of [1440,320])for(const theme of ['light','dark','system'])test(`paired journey chart remains usable at ${width}px in ${theme}`,async({page},testInfo)=>{
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 await page.setViewportSize({width,height:width===320?780:1100});await page.emulateMedia({colorScheme:'dark'});const queries=await fixture(page,theme);await page.goto('/#dashboard')
 await expect(page.getByTestId('trip-distance')).toHaveText('58.6km');await expect(page.getByTestId('trip-time')).toHaveText('1時間 0分')
 await expect(page.locator('.jc-bucket')).toHaveCount(7);await expect(page.locator('.jc-bucket.zero')).toHaveCount(1);await expect(page.locator('.jc-bucket.missing')).toHaveCount(2)
 await expect(page.locator('.jc-distance-bar')).toHaveCount(4);await expect(page.locator('.jc-time-bar')).toHaveCount(4)
 await expect(page.locator('.jc-left .jc-time').nth(1)).toHaveText('0.15 h')
 const contrast=await page.evaluate(()=>{
  const luminance=(rgb:string)=>{const [r,g,b]=rgb.match(/[\d.]+/g)!.slice(0,3).map(Number).map(value=>{value/=255;return value<=.04045?value/12.92:((value+.055)/1.055)**2.4});return r!*.2126+g!*.7152+b!*.0722}
  const background=luminance(getComputedStyle(document.querySelector('.jc-panel')!).backgroundColor)
  return ['.jc-legend .jc-distance','.jc-legend .jc-time'].map(selector=>{const foreground=luminance(getComputedStyle(document.querySelector(selector)!).color);return (Math.max(background,foreground)+.05)/(Math.min(background,foreground)+.05)})
 })
 contrast.forEach(ratio=>expect(ratio).toBeGreaterThan(4.5))
 expect((await page.locator('.jc-distance-line').getAttribute('d'))!.match(/M/g)).toHaveLength(3)
 await expect(page.getByLabel('左軸: 各区間の距離kmと時間h')).toBeVisible();await expect(page.getByLabel('右軸: 計測分累計の距離kmと時間h')).toBeVisible()
 await page.locator('.jc-bucket').first().focus();await page.keyboard.press('Enter');await expect(page.locator('.jc-detail')).toContainText('距離 0 km')
 await page.keyboard.press('ArrowRight');await expect(page.locator('.jc-bucket').nth(1)).toBeFocused();await expect(page.locator('.jc-detail')).toContainText('距離 — km')
 await page.getByLabel('走行グラフの集約').selectOption('week');await expect(page.locator('.jc-bucket')).toHaveCount(2)
 await page.getByLabel('走行グラフの集約').selectOption('month');await expect(page.locator('.jc-bucket')).toHaveCount(1)
 await page.getByRole('button',{name:'全期間',exact:true}).click();await expect.poll(()=>queries.some(query=>query.get('period')==='all')).toBe(true)
 await page.getByRole('button',{name:'期間指定',exact:true}).click();await page.getByLabel('開始日').fill('2026-11-07');await page.getByLabel('終了日').fill('2026-11-10');await page.getByRole('button',{name:'表示',exact:true}).click()
 await expect.poll(()=>queries.some(query=>query.get('from')==='2026-11-07'&&query.get('to')==='2026-11-10')).toBe(true)
 await expect(page.locator('.jc-foot')).toContainText('2026-11-07〜2026-11-10')
 await expect.poll(()=>page.locator('.jc-scroll').evaluate(element=>Number(element.querySelector('svg')!.getAttribute('width'))>=element.clientWidth)).toBe(true)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([])
 await page.screenshot({path:testInfo.outputPath(`journey-${theme}-${width}.png`),fullPage:true})
})
for(const empty of [false,true])test(`drive history shows ${empty?'no-fix evidence':'daily trip details'} at 320px`,async({page},testInfo)=>{
 await page.setViewportSize({width:320,height:780});await fixture(page,'dark',empty);await page.goto('/#trips')
 await expect(page.getByRole('heading',{name:'日別の詳細',exact:true})).toBeVisible()
 if(empty){await expect(page.getByTestId('trip-distance')).toHaveText('—km');await expect(page.getByText('距離・走行時間は未計測です')).toBeVisible();await expect(page.getByText('未測位 180 件',{exact:false})).toBeVisible();await expect(page.locator('.dh-trip-list')).toHaveCount(0)}
 else {await expect(page.locator('.dh-trip-list>li')).toHaveCount(1);await expect(page.locator('.dh-trip-list')).toContainText('09:00:00');await expect(page.locator('.dh-trip-list')).toContainText('0時間 15分');await page.getByLabel('詳細を表示する日付').fill('2026-11-06');await expect(page.getByText('この日に有効な連続測位の記録はありません。')).toBeVisible()}
 await expect(page.getByRole('heading',{name:'電波マップ',exact:true})).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:testInfo.outputPath(`drive-history-${empty?'empty':'populated'}-320.png`),fullPage:true})
})
