import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL?.replace(/\/$/,'')
const stamp='2026-11-11T08:00:00Z'
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-shell-layout-fixture',observedAt:stamp,refreshSeconds:2,verified:false})
// No stored layout choice: these tests cover what a first-time browser gets.
test.use({storageState:{cookies:[],origins:[]}})
async function open(page:Page,route='dashboard',fix=false){
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 let writes=0
 const zooms:number[]=[],tile=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mN8+/r1fwAJTgPdt1gXJgAAAABJRU5ErkJggg==','base64')
 // Map tiles are never fetched from the network; the handler only notes which zoom level was asked for.
 await page.route('https://tile.openstreetmap.org/**',request=>{zooms.push(Number(new URL(request.request().url()).pathname.split('/')[1]));return request.fulfill({contentType:'image/png',body:tile})})
 await page.addInitScript(()=>{(window as any).EventSource=class extends EventTarget{onerror=null;close(){}}})
 await page.clock.install({time:new Date(stamp)})
 await page.route('**/api/v1/**',async request=>{
  const path=new URL(request.request().url()).pathname,method=request.request().method()
  if(path.endsWith('/camera/sessions')&&method==='POST')return request.fulfill({json:{id:'synthetic'}})
  if(path.includes('/camera/sessions/'))return request.fulfill({status:404,json:{}})
  if(method!=='GET'){writes++;return request.fulfill({status:409,json:{reason:'Layout review never changes device settings'}})}
  let data:unknown={}
  if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme:'light',locale:'ja',favorites:[],cellularLimitBytes:'30000000000'}
  if(path.endsWith('/capabilities'))data=[]
  if(path.endsWith('/system/summary'))data={modem:obs({registered:true,rat:'LTE',plmn:'44010',band:'B3',rsrp:-86}),wifi:obs({ssid:'Fixture Wi-Fi',wpa_state:'COMPLETED',freq:2437}),hotspot:obs({enabled:true,ssid:'Fixture AP',clientCount:2,mode:'always-on'}),tailscale:obs({online:true,backendState:'Running',ips:['100.64.0.10'],deviceName:'fixture-np1'}),gps:obs(fix?{fix:true,at:stamp,lat:35.681236,lon:139.767125,satellites:12,accuracyM:4}:{fix:false,satellites:0}),cpu:obs({usedPercent:23}),ram:obs({usedPercent:48,usedBytes:'1288490188',totalBytes:'2684354560'}),sd:obs({freeBytes:'3221225472',totalBytes:'32000000000',usedPercent:90}),internalStorage:obs({freeBytes:'1610612736',totalBytes:'4294967296',usedPercent:62.5}),temperatures:obs([{name:'cpu',celsius:48.2}]),recording:obs({writing:true,cameras:{front:true,in:true},writingObservedAt:stamp,finalizedClips:128}),uptime:obs(90061),load:obs('0.42 0.60 0.71 1/100 999'),routes:obs('default via 192.0.2.1 dev fixture0')}
  if(path.endsWith('/traffic/usage'))data=obs({networks:{cellular:{status:'available',active:true,todayRX:'280000000',todayTX:'40000000',monthRX:'21000000000',monthTX:'1600000000',speedStatus:'available',stale:false,rxBytesPerSecond:1400000,txBytesPerSecond:75000},wifi:{status:'available',active:true,todayRX:'3000000000',todayTX:'250000000',monthRX:'12000000000',monthTX:'1500000000',speedStatus:'available',stale:false,rxBytesPerSecond:400000,txBytesPerSecond:90000}},stale:false,sampledAt:stamp})
  if(path.endsWith('/trips'))data=obs({range:{period:'7',from:'2026-11-05',to:'2026-11-11'},days:[],period:{hasData:false,days:[],distanceKm:null,drivingSeconds:null,validObservedDays:0,sampleCount:0},trips:[],totalTrips:0,detailsTruncated:false,sourceTruncated:false,sourceFiles:1,unreadableFiles:0,noFixSentences:10,badChecksums:0,indexedAt:stamp,source:'synthetic',status:'unknown'})
  if(path.endsWith('/network/cellular/apn'))data={revision:0,profiles:[{id:'profile1',enabled:false,apn:'',auth:0},{id:'profile2',enabled:false,apn:'',auth:0}],applyAvailable:false,applyReason:'Synthetic apply unavailable'}
  if(path.endsWith('/network/wifi/networks'))data={status:{state:'COMPLETED',ssid:'Fixture Wi-Fi',networkID:1,ipv4:'192.0.2.2'},networks:[],saved:[],transaction:null}
  if(path.endsWith('/network/hotspot'))data=obs({settings:{revision:0,enabled:false,ssid:'Fixture AP',passphraseConfigured:false},enabled:false,dhcpReady:false,clientCount:null})
  if(path.endsWith('/recordings'))data=obs({clips:[],days:[],pendingFiles:0,unreadableFiles:0,truncated:false,timezone:'Asia/Tokyo',indexedAt:stamp})
  if(path.endsWith('/radio/history'))data={unit:'dBm',points:[],cells:[],total:0,unplaced:0,accuracyExcluded:0,missing:0,outOfService:0,carriers:[],bands:[],truncated:false}
  if(path.endsWith('/radio/heatmap/legend'))data={unit:'dBm',note:'',thresholds:[]}
  if(path.endsWith('/device/native-settings'))data=obs({items:[]})
  await request.fulfill({json:data})
 })
 await page.goto(url+'/#'+route);await expect(page.locator('main')).toBeVisible()
 return {errors,zooms,get writes(){return writes}}
}
const fits=(page:Page)=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)
test.describe('menu layouts and the overview dashboard (synthetic fixtures)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to the local Vite UI')
 test('PC defaults to a full-width floating menu and the map-backed overview',async({page})=>{
  await page.setViewportSize({width:1920,height:1000});const state=await open(page)
  const bar=page.locator('.topnav');await expect(bar).toBeVisible();await expect(page.locator('.sidebar,.tabbar')).toHaveCount(0)
  await expect(bar).toHaveCSS('position','sticky')
  // The bar spans the window with a margin, and with this much room every screen is one click away.
  const box=(await bar.boundingBox())!;expect(box.x).toBeLessThanOrEqual(24);expect(box.x+box.width).toBeGreaterThanOrEqual(1920-24)
  await expect(bar.locator('nav>a')).toHaveText(['ダッシュボード','4G','Wi-Fi / AP','Tailscale','ライブ','ドラレコ再生','走行履歴','ナンバー','純正設定','システム'])
  await expect(bar.locator('.topnav-group')).toHaveCount(0)
  await expect(bar.getByRole('link',{name:'ダッシュボード',exact:true})).toHaveAttribute('aria-current','page')
  // Overview: the map is the background of the whole window and the panels sit along its edges.
  const stage=page.locator('.dash-home'),map=(await page.locator('.dh-map').boundingBox())!
  expect(map.x).toBe(0);expect(map.y).toBe(0);expect(map.width).toBe(1920);expect(map.height).toBeGreaterThanOrEqual(1000)
  await expect(stage.getByRole('heading',{name:'走行の記録',exact:true})).toBeVisible()
  await expect(stage.locator('.dh-cams .dashboard-camera-pair figure')).toHaveCount(2)
  await expect(stage.locator('.dh-tile')).toHaveCount(4)
  const edge=async(selector:string)=>(await stage.locator(selector).boundingBox())!
  const journey=await edge('.dh-journey'),usage=await edge('.traffic-usage'),cameras=await edge('.dh-cams'),network=await edge('.network-summary'),resources=await edge('.resource-monitor'),device=await edge('.device-status')
  expect(journey.x).toBeLessThanOrEqual(24);expect(cameras.x+cameras.width).toBeGreaterThanOrEqual(1920-24);expect(Math.abs(journey.y-cameras.y)).toBeLessThanOrEqual(1)
  // Along the bottom edge: 4G, network, resources, device — one row, equal sizes, equal gaps.
  const row=[usage,network,resources,device],gaps=row.slice(1).map((panel,index)=>panel.x-row[index]!.x-row[index]!.width)
  expect(usage.x).toBeLessThanOrEqual(24);expect(device.x+device.width).toBeGreaterThanOrEqual(1920-24)
  for(const panel of row){expect(Math.abs(panel.width-usage.width)).toBeLessThanOrEqual(1);expect(Math.abs(panel.height-usage.height)).toBeLessThanOrEqual(1);expect(Math.abs(panel.y-usage.y)).toBeLessThanOrEqual(1);expect(panel.y+panel.height).toBeGreaterThanOrEqual(1000-40)}
  for(const gap of gaps){expect(gap).toBeGreaterThan(8);expect(Math.abs(gap-gaps[0]!)).toBeLessThanOrEqual(1)}
  // The row follows the window width.
  await page.setViewportSize({width:1500,height:1000})
  await expect.poll(async()=>Math.round((await edge('.device-status')).x+(await edge('.device-status')).width)).toBeGreaterThanOrEqual(1500-24)
  expect(Math.abs((await edge('.traffic-usage')).width-(await edge('.device-status')).width)).toBeLessThanOrEqual(1)
  await page.setViewportSize({width:1920,height:1000})
  // The trip record and the cameras end above the row, the version line is left out, and nothing needs scrolling at this size.
  expect(journey.y+journey.height).toBeLessThan(usage.y);expect(cameras.y+cameras.height).toBeLessThan(usage.y)
  await expect(page.locator('.page-footer')).toHaveCount(0)
  expect(await page.evaluate(()=>document.documentElement.scrollHeight<=innerHeight)).toBe(true)
  await expect(stage.getByRole('heading',{name:'4G データ通信',exact:true})).toBeVisible()
  const summary=stage.locator('.network-summary')
  await expect(summary.locator('[data-network=ap]')).toContainText('Fixture AP');await expect(summary.locator('[data-network=ap]')).toContainText('接続端末 2 台')
  await expect(summary.locator('[data-network=sta]')).toContainText('Fixture Wi-Fi');await expect(summary.locator('[data-network=vpn]')).toContainText('オンライン')
  await expect(stage.locator('.resource-monitor.compact [data-metric]')).toHaveCount(3)
  const status=stage.locator('.device-status')
  await expect(status).toContainText('1日 1時間 1分');await expect(status).toContainText('0.42');await expect(status.getByRole('progressbar')).toHaveCount(2)
  for(const [name,heading] of [['4G','APN接続設定'],['走行履歴','日別の詳細'],['ドラレコ再生','録画されている時間']] as const){await bar.getByRole('link',{name,exact:true}).click();await expect(page.getByRole('heading',{name:heading,exact:true})).toBeVisible()}
  await bar.getByRole('link',{name:'システム',exact:true}).click();await expect(page.getByRole('navigation',{name:'システム設定'})).toBeVisible()
  expect(await fits(page)).toBe(true);expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
 })
 test('the menu bar folds groups only as the window narrows',async({page})=>{
  await page.setViewportSize({width:1920,height:900});const state=await open(page)
  const bar=page.locator('.topnav'),inside=()=>bar.evaluate(element=>{const list=element.querySelector('nav')!,tools=element.querySelector('.topnav-tools')!;return list.lastElementChild!.getBoundingClientRect().right<=list.getBoundingClientRect().right+1&&tools.getBoundingClientRect().right<=element.getBoundingClientRect().right})
  const sizes:Record<string,number>={ネットワーク:3,ドラレコ:4,設定:2}
  await expect(bar.locator('nav>a')).toHaveCount(10)
  for(const width of [1600,1440,1280,1100,980,880]){
   await page.setViewportSize({width,height:900})
   await expect.poll(inside).toBe(true)
   // Whatever is folded, each of the ten screens is still offered exactly once.
   const flat=await bar.locator('nav>a').count(),folded=await bar.locator('.topnav-group>button').allTextContents()
   expect(flat+folded.reduce((sum,label)=>sum+(sizes[label]??0),0)).toBe(10)
   expect(await fits(page)).toBe(true)
  }
  // Narrow: every group is folded; it opens on click and each entry is one more click away.
  await expect(bar.locator('.topnav-group>button')).toHaveText(['ネットワーク','ドラレコ','設定'])
  await expect(bar.getByRole('link',{name:/4G/})).toHaveCount(0)
  await bar.getByRole('button',{name:'ネットワーク',exact:true}).click()
  await expect(bar.getByRole('link',{name:/Wi-Fi \/ AP/})).toBeVisible();await expect(bar.getByRole('link',{name:/Tailscale/})).toBeVisible()
  await bar.getByRole('link',{name:/4G/}).click()
  await expect(page.getByRole('heading',{name:'APN接続設定',exact:true})).toBeVisible();await expect(page.locator('.topnav-menu')).toHaveCount(0)
  await expect(bar.getByRole('button',{name:'ネットワーク',exact:true})).toHaveClass(/active/)
  await bar.getByRole('button',{name:'設定',exact:true}).click();await page.keyboard.press('Escape');await expect(page.locator('.topnav-menu')).toHaveCount(0)
  await bar.getByRole('button',{name:'設定',exact:true}).click();await page.locator('main').click({position:{x:5,y:5}});await expect(page.locator('.topnav-menu')).toHaveCount(0)
  await bar.getByRole('button',{name:'設定',exact:true}).click();await bar.getByRole('link',{name:/システム/}).click()
  await expect(page.getByRole('navigation',{name:'システム設定'})).toBeVisible()
  await bar.getByRole('button',{name:'ドラレコ',exact:true}).click();await bar.getByRole('link',{name:/走行履歴/}).click();await expect(page.getByRole('heading',{name:'日別の詳細',exact:true})).toBeVisible()
  // Widening again brings the entries back.
  await page.setViewportSize({width:1920,height:900});await expect(bar.locator('nav>a')).toHaveCount(10)
  await expect(bar.getByRole('link',{name:'走行履歴',exact:true})).toHaveAttribute('aria-current','page')
  expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
 })
 test('a narrower PC window keeps the map behind the panels and scrolls',async({page})=>{
  await page.setViewportSize({width:1000,height:700});const state=await open(page)
  const stage=page.locator('.dash-home'),map=(await page.locator('.dh-map').boundingBox())!
  expect(map.x).toBe(0);expect(map.width).toBe(1000)
  expect(Math.round(map.height)).toBe(await page.evaluate(()=>document.documentElement.scrollHeight))
  await expect(stage.locator('.dh-tile')).toHaveCount(4)
  const uncovered=(await stage.locator('.dh-window').boundingBox())!;expect(uncovered.height).toBeGreaterThanOrEqual(200)
  const box=async(selector:string)=>(await stage.locator(selector).boundingBox())!
  const journey=await box('.dh-journey'),cameras=await box('.dh-cams'),usage=await box('.traffic-usage'),network=await box('.network-summary'),resources=await box('.resource-monitor'),device=await box('.device-status')
  expect(journey.y).toBeGreaterThanOrEqual(uncovered.y+uncovered.height)
  // Two equal columns: the trip record beside the cameras at the same height, and the tiles two by two under them.
  const close=(a:number,b:number)=>expect(Math.abs(a-b)).toBeLessThanOrEqual(1)
  close(journey.width,cameras.width);close(journey.y,cameras.y);close(journey.height,cameras.height)
  for(const left of [usage,resources]){close(left.x,journey.x);close(left.width,journey.width)}
  for(const right of [network,device]){close(right.x,cameras.x);close(right.width,cameras.width)}
  close(usage.y,network.y);close(usage.height,network.height);close(resources.y,device.y);close(resources.height,device.height)
  expect(resources.y).toBeGreaterThan(usage.y+usage.height)
  // The two camera pictures sit side by side.
  const pictures=await stage.locator('.dh-cams figure').all(),front=(await pictures[0]!.boundingBox())!,cabin=(await pictures[1]!.boundingBox())!
  close(front.y,cabin.y);expect(cabin.x).toBeGreaterThanOrEqual(front.x+front.width-1);expect(front.height).toBeGreaterThanOrEqual(front.width*9/16-1)
  expect(await fits(page)).toBe(true);expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
 })
 test('the detailed dashboard and the sidebar are selectable and remembered in this browser',async({page})=>{
  await page.setViewportSize({width:1440,height:1000});const state=await open(page)
  // The dashboard switch sits next to the menu-position switch.
  const tools=page.locator('.topnav-tools button')
  await expect(tools).toHaveCount(3);await expect(tools.nth(0)).toHaveAccessibleName('メニューを左側に表示');await expect(tools.nth(1)).toHaveAccessibleName('ダッシュボードを詳細表示にする')
  await tools.nth(1).click()
  await expect(page.locator('.dashboard-overview')).toBeVisible();await expect(page.locator('.dash-home')).toHaveCount(0)
  await expect(page.locator('.dashboard-overview .network-overview .connection-card')).toHaveCount(3)
  await expect(tools.nth(1)).toHaveAccessibleName('ダッシュボードを概要表示にする')
  // From another screen the switch also brings the dashboard up.
  await page.locator('.topnav').getByRole('link',{name:'走行履歴',exact:true}).click();await expect(page.getByRole('heading',{name:'日別の詳細',exact:true})).toBeVisible()
  await tools.nth(1).click();await expect(page.locator('.dash-home')).toBeVisible()
  await tools.nth(1).click();await expect(page.locator('.dashboard-overview')).toBeVisible()
  await page.getByRole('button',{name:'メニューを左側に表示',exact:true}).click()
  await expect(page.locator('.sidebar')).toBeVisible();await expect(page.locator('.topnav')).toHaveCount(0)
  await expect(page.locator('.sidebar nav a')).toHaveCount(10)
  const sideTools=page.locator('.sidebar-tools button')
  await expect(sideTools.nth(0)).toHaveAccessibleName('メニューを上部に表示');await expect(sideTools.nth(1)).toHaveAccessibleName('ダッシュボードを概要表示にする')
  expect(await page.evaluate(()=>[localStorage.getItem('ncc.nav'),localStorage.getItem('ncc.dashboard')])).toEqual(['side','detail'])
  await page.reload();await expect(page.locator('.sidebar')).toBeVisible();await expect(page.locator('.dashboard-overview')).toBeVisible()
  // With the sidebar the overview fills the rest of the window.
  await sideTools.nth(1).click();await expect(page.locator('.dash-home')).toBeVisible()
  const side=(await page.locator('.sidebar').boundingBox())!,map=(await page.locator('.dh-map').boundingBox())!
  expect(map.x).toBe(side.width);expect(map.x+map.width).toBe(1440);expect(map.y).toBe(0)
  await sideTools.nth(1).click();await expect(page.locator('.dashboard-overview')).toBeVisible()
  // The same two choices are offered with the display settings and apply at once.
  await page.getByRole('link',{name:'システム',exact:true}).click();await page.getByRole('button',{name:'設定・バックアップ',exact:true}).click()
  await expect(page.getByLabel('PCのメニュー位置')).toHaveValue('side');await expect(page.getByLabel('ダッシュボードの表示')).toHaveValue('detail')
  await page.getByLabel('PCのメニュー位置').selectOption('top');await expect(page.locator('.topnav')).toBeVisible();await expect(page.locator('.sidebar')).toHaveCount(0)
  await page.getByLabel('ダッシュボードの表示').selectOption('overview')
  await page.locator('.topnav').getByRole('link',{name:'ダッシュボード',exact:true}).click();await expect(page.locator('.dash-home')).toBeVisible()
  expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
 })
 for(const width of [390,320])test(`${width}px uses four grouped tabs that open a second menu`,async({page})=>{
  await page.setViewportSize({width,height:780});const state=await open(page)
  const tabs=page.locator('.tabbar');await expect(tabs).toBeVisible();await expect(page.locator('.sidebar,.topnav')).toHaveCount(0)
  await expect(tabs.locator('>*')).toHaveText(['ダッシュボード','ネットワーク','ドラレコ','設定'])
  await expect(page.locator('.mobile-title')).toHaveText('ダッシュボード');await expect(tabs.getByRole('link',{name:'ダッシュボード',exact:true})).toHaveClass(/active/)
  for(const tab of await tabs.locator('>*').all())expect((await tab.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  await expect(page.locator('.dash-home .dh-tile')).toHaveCount(4);expect(await fits(page)).toBe(true)
  // The title bar floats like the PC menu bar, and the map fills the screen behind it down to the bottom menu.
  const title=(await page.locator('.mobile-toolbar').boundingBox())!,menu=(await tabs.boundingBox())!,map=(await page.locator('.dh-map').boundingBox())!
  expect(title.x).toBeGreaterThanOrEqual(8);expect(title.y).toBeGreaterThanOrEqual(6);expect(title.x+title.width).toBeLessThanOrEqual(width-8)
  expect(map.x).toBe(0);expect(map.y).toBe(0);expect(map.width).toBe(width);expect(Math.abs(map.y+map.height-menu.y)).toBeLessThanOrEqual(1)
  expect(await page.evaluate(()=>document.documentElement.scrollHeight<=innerHeight)).toBe(true)
  await expect(page.locator('.page-footer')).toHaveCount(0)
  // Only the front camera is shown, small, at the top right; it opens the live page.
  const thumb=page.locator('.dh-thumb');await expect(thumb.locator('figure')).toHaveCount(1);await expect(thumb.locator('figcaption')).toHaveText('前方')
  const picture=(await thumb.boundingBox())!;expect(picture.x+picture.width).toBeGreaterThanOrEqual(width-14);expect(picture.y).toBeGreaterThanOrEqual(title.y+title.height);expect(picture.width).toBeLessThanOrEqual(width*.45)
  await expect(thumb.getByRole('button',{name:'前方のライブ映像を開く'})).toBeVisible()
  // Everything else is in a sheet pulled up from the bottom: half open at first, full on request, then a peek.
  const panels=page.locator('.dh-sheet'),grip=panels.getByRole('button',{name:/パネルを/}),top=async()=>Math.round((await panels.boundingBox())!.y)
  await expect(panels.getByRole('heading',{name:'走行の記録',exact:true})).toBeVisible()
  const half=Math.round(menu.y)-Math.round(menu.y*.46);await expect.poll(top).toBe(half);expect(half).toBeGreaterThan(picture.y+picture.height)
  await grip.click();await expect(panels).toHaveClass(/full/);await expect.poll(top).toBeLessThan(title.y+title.height+30)
  await panels.locator('.device-status').scrollIntoViewIfNeeded();await expect(panels.locator('.device-status')).toBeInViewport()
  await grip.click();await expect(panels).not.toHaveClass(/full/);await expect.poll(top).toBeGreaterThan(half+100)
  await grip.click();await expect.poll(top).toBe(half)
  // Dragging the sheet works too.
  await page.mouse.move(width/2,half+12);await page.mouse.down();await page.mouse.move(width/2,half-150,{steps:6});await page.mouse.up()
  await expect(panels).toHaveClass(/full/);await grip.click();await grip.click();await expect.poll(top).toBe(half)
  // The dashboard switch is an icon in the title bar, beside the refresh button.
  const bar=page.locator('.mobile-toolbar'),switches=bar.getByRole('button',{name:/ダッシュボードを/})
  await expect(bar.getByRole('button')).toHaveCount(2);await expect(switches).toHaveText('')
  for(const button of await bar.getByRole('button').all()){const hit=(await button.boundingBox())!;expect(hit.width).toBeGreaterThanOrEqual(44);expect(hit.height).toBeGreaterThanOrEqual(44);expect(hit.x+hit.width).toBeLessThanOrEqual(title.x+title.width)}
  await expect(switches).toHaveAccessibleName('ダッシュボードを詳細表示にする');await switches.click();await expect(page.locator('.dashboard-overview')).toBeVisible()
  await expect(switches).toHaveAccessibleName('ダッシュボードを概要表示にする');await switches.click();await expect(page.locator('.dash-home')).toBeVisible()
  await tabs.getByRole('button',{name:'ドラレコ',exact:true}).click()
  const sheet=page.getByRole('dialog',{name:'ドラレコのメニュー'})
  await expect(sheet.getByRole('link')).toHaveText([/ライブ/,/ドラレコ再生/,/走行履歴/,/ナンバー/])
  for(const item of await sheet.getByRole('link').all())expect((await item.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  // Another group can be chosen without closing the first.
  await tabs.getByRole('button',{name:'ネットワーク',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'ネットワークのメニュー'}).getByRole('link')).toHaveText([/4G/,/Wi-Fi \/ AP/,/Tailscale/])
  await page.locator('.sheet-backdrop').click({position:{x:10,y:10}});await expect(page.locator('.tab-sheet')).toHaveCount(0)
  await tabs.getByRole('button',{name:'ネットワーク',exact:true}).click();await page.getByRole('dialog').getByRole('link',{name:/4G/}).click()
  await expect(page.getByRole('heading',{name:'APN接続設定',exact:true})).toBeVisible();await expect(page.locator('.tab-sheet')).toHaveCount(0)
  await expect(page.locator('.mobile-title')).toHaveText('4G');await expect(tabs.getByRole('button',{name:'ネットワーク',exact:true})).toHaveClass(/active/)
  await tabs.getByRole('button',{name:'設定',exact:true}).click();await page.getByRole('dialog').getByRole('link',{name:/システム/}).click()
  const sections=page.getByRole('navigation',{name:'システム設定'});await expect(sections).toBeVisible()
  // The section tabs scroll sideways instead of wrapping onto several rows.
  expect((await sections.boundingBox())!.height).toBeLessThan(70)
  await sections.getByRole('button',{name:'設定・バックアップ',exact:true}).click();await expect(page.getByRole('heading',{name:'表示設定',exact:true})).toBeVisible()
  expect(await fits(page)).toBe(true)
  await tabs.getByRole('link',{name:'ダッシュボード',exact:true}).click();await expect(page.locator('.dash-home')).toBeVisible()
  expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
 })
})
test.describe('zooming the overview map (synthetic fixtures)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to the local Vite UI')
 const middle=async(page:Page,selector:string)=>{const box=(await page.locator(selector).boundingBox())!;return {x:box.x+box.width/2,y:box.y+box.height/2}}
 test('on a PC the wheel zooms the map about the current position',async({page})=>{
  await page.setViewportSize({width:1440,height:800});const state=await open(page,'dashboard',true)
  await expect(page.locator('.current-location-marker')).toHaveCount(1)
  // The position is shown in the middle of the uncovered part of the map, and the credit stays visible.
  const open_=await middle(page,'.dh-window')
  await expect.poll(async()=>Math.abs((await middle(page,'.current-location-marker')).x-open_.x)+Math.abs((await middle(page,'.current-location-marker')).y-open_.y)).toBeLessThanOrEqual(6)
  await expect(page.locator('.current-location-credit')).toContainText('OpenStreetMap');await expect(page.locator('.current-location-credit')).toBeInViewport()
  expect(Math.max(...state.zooms)).toBe(15)
  await page.mouse.move(open_.x,open_.y+50);await page.mouse.wheel(0,-300)
  await expect.poll(()=>Math.max(...state.zooms)).toBeGreaterThan(15)
  await expect.poll(async()=>Math.abs((await middle(page,'.current-location-marker')).x-open_.x)+Math.abs((await middle(page,'.current-location-marker')).y-open_.y)).toBeLessThanOrEqual(6)
  await page.mouse.wheel(0,900);await expect.poll(()=>Math.min(...state.zooms)).toBeLessThan(15)
  // Over a panel the wheel is left alone.
  const seen=state.zooms.length,panel=await middle(page,'.dh-journey')
  await page.mouse.move(panel.x,panel.y);await page.mouse.wheel(0,-300);await page.waitForTimeout(400);expect(state.zooms.length).toBe(seen)
  expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
 })
 test.describe('touch',()=>{
  test.use({hasTouch:true})
  test('on a phone a pinch zooms the map about the current position',async({page})=>{
   await page.setViewportSize({width:390,height:844});const state=await open(page,'dashboard',true)
   await expect(page.locator('.current-location-marker')).toHaveCount(1)
   const open_=await middle(page,'.dh-window')
   await expect.poll(async()=>Math.abs((await middle(page,'.current-location-marker')).x-open_.x)+Math.abs((await middle(page,'.current-location-marker')).y-open_.y)).toBeLessThanOrEqual(6)
   await expect(page.locator('.current-location-credit')).toBeInViewport()
   const session=await page.context().newCDPSession(page),fingers=(spread:number)=>[{x:open_.x-spread,y:open_.y,id:1},{x:open_.x+spread,y:open_.y,id:2}]
   await session.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:fingers(30)})
   for(let step=1;step<=10;step++)await session.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:fingers(30+step*12)})
   await session.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]})
   await expect.poll(()=>Math.max(...state.zooms)).toBeGreaterThan(15)
   await expect.poll(async()=>Math.abs((await middle(page,'.current-location-marker')).x-open_.x)+Math.abs((await middle(page,'.current-location-marker')).y-open_.y)).toBeLessThanOrEqual(6)
   expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
  })
 })
})
