import { test, expect, type Page, type Route } from '@playwright/test'
import {calendar} from '../src/services/journey'
const reviewURL=process.env.NCC_UI_REVIEW_URL
// These synthetic values exist only in the browser test and are never written to the device.
const observation=(data:any,status='available')=>({status,data,source:'usability-test-fixture',observedAt:'2026-11-11T08:00:00Z',refreshSeconds:10,verified:false})
function fixture(populated=false){
 const days=Array.from({length:7},(_,i)=>({date:`2026-11-${String(i+5).padStart(2,'0')}`,distanceKm:i===1||i===3?null:i===0?0:14.65,drivingSeconds:i===1||i===3?null:i===0?0:900,observed:i!==1&&i!==3,sampleCount:8}))
 const period={distanceKm:populated?58.6:null,drivingSeconds:populated?3600:null,validObservedDays:populated?5:0,hasData:populated,days:populated?days:[]}
 return {tripStats:observation({periods:Object.fromEntries([7,30,90,365].map(d=>[d,period]))}),usage:observation(populated?{todayRX:'1073741824',todayTX:'536870912',monthRX:'2147483648',monthTX:'1073741824',startedAt:'2026-11-11T01:00:00Z',sampledAt:'2026-11-11T08:00:00Z',wanType:'Wi-Fi'}:null),modem:observation({registered:false}),hotspot:observation(populated?{enabled:true,ssid:'Test-network-with-a-long-display-name',clientCount:0,mode:'always-on'}:{}),wifi:observation(populated?{ssid:'Example-WiFi',wpa_state:'COMPLETED',key_mgmt:'WPA2',freq:2412}:{}),tailscale:observation({online:true,backendState:'Running',ips:['100.64.0.10'],activePeerConnections:[]}),gps:observation({fix:false,satellites:0}),cpu:observation(populated?{usedPercent:19}:null),ram:observation(populated?{usedPercent:49,usedBytes:'536870912',totalBytes:'1073741824'}:null),sd:observation(populated?{freeBytes:'68719476736',totalBytes:'137438953472',usedPercent:50}:null,populated?'available':'unknown'),internalStorage:observation(populated?{freeBytes:'4294967296'}:null),temperatures:observation(populated?[{name:'cpu',celsius:42.4}]:[])}
}
async function open(page:Page,summary:any,ledHandler?:(route:Route)=>Promise<void>){
 await page.clock.install({time:new Date('2026-11-11T08:00:00Z')})
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname
  if(path.endsWith('/device/led')&&ledHandler)return ledHandler(route)
  if(path.endsWith('/events'))return route.fulfill({status:200,contentType:'text/event-stream',body:': test fixture\n\n'})
  let data:any={}
  if(path.endsWith('/auth/session'))data={csrf:'test-fixture'}
  if(path.endsWith('/system/summary'))data=summary
  if(path.endsWith('/trips')){
   const kind=url.searchParams.get('period')||'7',to=url.searchParams.get('to')||'2026-11-11',from=url.searchParams.get('from')||new Date(Date.parse(to)-(Number(kind)-1)*86400000).toISOString().slice(0,10),known=summary.tripStats.data.periods[kind],days=calendar(from,to,known?.days||[]),observed=days.filter(day=>day.observed)
   const period={days,hasData:observed.length>0,distanceKm:observed.length?observed.reduce((sum,day)=>sum+day.distanceKm!,0):null,drivingSeconds:observed.length?observed.reduce((sum,day)=>sum+day.drivingSeconds!,0):null,validObservedDays:observed.length,sampleCount:observed.reduce((sum,day)=>sum+day.sampleCount,0)}
   data=observation({range:{period:kind,from,to},days,period,trips:[],totalTrips:0,detailsTruncated:false,sourceTruncated:false,sourceFiles:2,unreadableFiles:0,noFixSentences:observed.length?0:120,badChecksums:0,indexedAt:'2026-11-11T08:00:00Z',source:'ncc-retained-measured-intervals',status:period.hasData?'available':'unknown'},period.hasData?'available':'unknown')
  }
  if(path.endsWith('/settings'))data={revision:1,theme:'system',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))data=[]
  if(path.endsWith('/voicevox'))data={endpoint:'https://voice.example.invalid',defaultEndpoint:'https://voice.example.invalid',keyConfigured:false}
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(data)})
 })
  await page.goto(reviewURL!+(ledHandler?'#system/led':''))
  await expect(page.getByRole('heading',{name:ledHandler?'前面LED':'走行の記録',exact:true})).toBeVisible()
}
async function noOverflow(page:Page){expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
test.describe('user-focused dashboard review (synthetic browser fixtures)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to the local Vite UI to run isolated usability fixtures.')
 test('mobile missing measurements stay unknown and preserve useful connections',async({page},testInfo)=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
  await page.setViewportSize({width:390,height:844});await open(page,fixture())
  await expect(page.getByTestId('trip-distance')).toHaveText('—km');await expect(page.getByTestId('trip-time')).toHaveText('—')
  await expect(page.getByTestId('traffic-total')).toHaveText('—')
  await expect(page.getByText('この期間の走行記録はありません')).toBeVisible()
  await expect(page.locator('.traffic-usage').getByText('未登録',{exact:true})).toBeVisible()
  await expect(page.locator('.connection-card').first().getByText('取得待ち',{exact:true})).toBeVisible()
  await expect(page.getByText('管理ネットワークに接続')).toBeVisible()
  await expect(page.getByText('遠隔アクセス利用可能')).toHaveCount(0)
  await expect(page.locator('.device-meter progress')).toHaveCount(0)
  await noOverflow(page);expect(errors).toEqual([])
  await page.screenshot({path:testInfo.outputPath('dashboard-empty-mobile.png'),fullPage:true})
 })
 test('calendar-complete graph distinguishes zero and missing, then aggregates long periods',async({page},testInfo)=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
  await page.setViewportSize({width:1440,height:1100});await open(page,fixture(true))
  await expect(page.getByTestId('trip-distance')).toHaveText('58.6km');await expect(page.getByTestId('trip-time')).toHaveText('1時間 0分')
  await expect(page.locator('.jc-bucket')).toHaveCount(7);await expect(page.locator('.jc-bucket.zero')).toHaveCount(1);await expect(page.locator('.jc-bucket.missing')).toHaveCount(2)
  await page.locator('.jc-bucket.zero').click();await expect(page.locator('.chart-detail')).toContainText('0 km')
  await page.screenshot({path:testInfo.outputPath('dashboard-populated-desktop.png'),fullPage:true})
  await page.getByRole('button',{name:'90日',exact:true}).click();await expect(page.locator('.jc-bucket')).toHaveCount(14)
  await page.getByRole('button',{name:'365日',exact:true}).click();await expect(page.locator('.jc-bucket')).toHaveCount(13)
  await page.getByLabel('走行グラフの集約').selectOption('day');await expect(page.locator('.jc-bucket')).toHaveCount(365);await noOverflow(page)
  await page.emulateMedia({colorScheme:'dark'})
  const rgb=await page.evaluate(()=>getComputedStyle(document.documentElement).getPropertyValue('--bg').trim());expect(rgb).toBe('#111318')
  await page.screenshot({path:testInfo.outputPath('dashboard-dark-desktop.png'),fullPage:true});expect(errors).toEqual([])
  await page.setViewportSize({width:390,height:844})
  await page.waitForFunction(()=>document.querySelector('.sidebar')!.getBoundingClientRect().right<=0)
  await page.getByRole('button',{name:'メニューを開く'}).click()
  await expect(page.getByRole('link',{name:'ダッシュボード',exact:true})).toBeVisible()
  await page.getByRole('link',{name:'ダッシュボード',exact:true}).click()
  await page.waitForFunction(()=>document.querySelector('.sidebar')!.getBoundingClientRect().right<=0)
  await noOverflow(page)
  await page.screenshot({path:testInfo.outputPath('dashboard-dark-mobile.png'),fullPage:true})
 })
 test('320px long names, stale usage and partial counters do not overflow or crash',async({page},testInfo)=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));const summary=fixture(true)
  summary.usage=observation({todayRX:'9007199254740993',todayTX:'1',monthRX:null,monthTX:null,wanType:'Wi-Fi',stale:true},'stale')
  await page.setViewportSize({width:320,height:780});await open(page,summary)
  await expect(page.getByText('更新が止まっています')).toBeVisible();await expect(page.locator('.usage-month strong')).toHaveText('—')
  await page.getByRole('button',{name:'30日',exact:true}).click();await expect(page.locator('.jc-bucket')).toHaveCount(30)
  await noOverflow(page);expect(errors).toEqual([])
  await page.screenshot({path:testInfo.outputPath('dashboard-narrow-mobile.png'),fullPage:true})
 })
 test('LED waits for settings, blocks concurrent changes and recovers from conflicts',async({page},testInfo)=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
  let available=false,puts=0,release:(()=>void)|undefined
  const settings={revision:2,mode:'off',brightness:180,speed:240,fps:15,rgb:[255,255,255],autostart:false}
  await open(page,fixture(),async route=>{
   if(route.request().method()==='PUT'){
    puts++;if(puts===1){await new Promise<void>(resolve=>release=resolve);await route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({reason:'REVISION_CONFLICT'})});return}
    expect(route.request().postDataJSON().expectedRevision).toBe(2)
   }
   await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(available?observation({settings,serviceState:'inactive'}):observation(null,'unknown'))})
  })
  await page.getByRole('link',{name:'システム',exact:true}).click();await page.getByRole('button',{name:'LED / 音声',exact:true}).click()
  const apply=page.getByRole('button',{name:'点灯',exact:true})
  await expect(apply).toBeDisabled();await expect(page.locator('.led-panel').getByRole('alert')).toContainText('設定を取得できません')
  available=true;await page.locator('.led-panel').getByRole('button',{name:'状態を更新',exact:true}).click();await expect(apply).toBeEnabled()
  await page.getByLabel('表示モード').selectOption('rainbow')
  await page.getByLabel('点灯し続ける',{exact:true}).check()
  await apply.click();await expect(page.getByRole('button',{name:'実行中…',exact:true})).toBeDisabled();await expect(page.locator('.led-panel').getByRole('button',{name:'状態を更新',exact:true})).toBeDisabled()
  await page.getByRole('button',{name:'実行中…',exact:true}).evaluate((el:HTMLButtonElement)=>el.click());expect(puts).toBe(1);release!()
  await expect(page.locator('.led-panel').getByRole('alert')).toContainText('別の操作で設定が変わりました');await expect(apply).toBeDisabled()
  await page.locator('.led-panel').getByRole('button',{name:'状態を更新',exact:true}).click();await expect(apply).toBeEnabled();await apply.click()
  await expect(page.locator('.led-panel p[role=status]')).toContainText('消灯するまで点灯を続けます')
  await page.setViewportSize({width:390,height:844});await page.waitForFunction(()=>document.querySelector('.sidebar')!.getBoundingClientRect().right<=0)
  await noOverflow(page);await page.screenshot({path:testInfo.outputPath('led-mobile.png'),fullPage:true});expect(errors).toEqual([])
 })
})
