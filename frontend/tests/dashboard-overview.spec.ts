import {test,expect,type Page} from '@playwright/test'
import {readFileSync} from 'node:fs'
import {remember,chartPath,type MetricHistory} from '../src/services/resource-history'
const base=process.env.NCC_UI_REVIEW_URL,stamp=Date.parse('2026-11-12T03:00:00Z')
// Reuse the existing flat synthetic JPEG, never device footage.
const jpeg=Buffer.from(readFileSync(new URL('./plate-recognition.spec.ts',import.meta.url),'utf8').match(/const jpeg=Buffer.from\('([^']+)'/)![1]!,'base64')
const observation=(data:unknown,at=stamp)=>({status:'available',data,observedAt:new Date(at).toISOString(),refreshSeconds:10,verified:false,source:'synthetic-dashboard-overview'})
async function fixture(page:Page,theme:string){
 await page.clock.install({time:new Date(stamp)});await page.clock.pauseAt(new Date(stamp))
 await page.addInitScript(()=>{(window as any).EventSource=class extends EventTarget{onerror=null;close(){}}})
 const calls={frames:[] as string[],created:[] as string[],released:[] as string[],traffic:0,summary:0}
 await page.route('https://tile.openstreetmap.org/**',route=>route.abort())
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname,method=route.request().method();let data:unknown={}
  if(path.endsWith('/auth/session'))data={csrf:'synthetic'}
  if(path.endsWith('/settings'))data={revision:1,theme,locale:'ja',favorites:[],cellularLimitBytes:'30000000000'}
  if(path.endsWith('/system/summary')){const at=stamp+calls.summary++*10000;data={modem:observation({registered:true,plmn:'44010',band:'B3',rsrp:-86},at),wifi:observation({ssid:'Home Wi-Fi',wpa_state:'COMPLETED',key_mgmt:'WPA2-PSK',freq:2437},at),hotspot:observation({enabled:true,ssid:'NP1 Hotspot',clientCount:2,mode:'hotspot-auto-uplink'},at),tailscale:observation({online:true,deviceName:'np1-control',accountLogin:'owner@example.com',ips:['100.64.0.10'],activePeerConnections:['Direct']},at),gps:observation({fix:false},at),recording:observation({writing:true,cameras:{front:true,in:true},writingObservedAt:new Date(at).toISOString(),finalizedClips:128},at),cpu:observation({usedPercent:23+calls.summary},at),ram:observation({usedPercent:52,usedBytes:'1395864371',totalBytes:'2684354560'},at),temperatures:observation([{name:'cpu',celsius:61.5}],at),load:observation('0.42 0.60 0.71 1/100 999',at),uptime:observation(90061,at),sd:observation({usedPercent:90,freeBytes:'3221225472',totalBytes:'32000000000'},at),internalStorage:observation({freeBytes:'1181116006',totalBytes:'4294967296',usedPercent:72},at)}}
  if(path.endsWith('/traffic/usage')){const at=stamp+calls.traffic++*2000;const network=(factor:number)=>({status:'available',speedStatus:'available',stale:false,active:true,todayRX:'280000000',todayTX:'40000000',monthRX:'21000000000',monthTX:'1600000000',rxBytesPerSecond:factor*(100000+calls.traffic*10000),txBytesPerSecond:factor*10000,sampledAt:new Date(at).toISOString()});data=observation({networks:{cellular:network(1),wifi:network(4)},sampledAt:new Date(at).toISOString(),stale:false},at)}
  if(path.endsWith('/trips'))data=observation({range:{period:'7',from:'2026-11-06',to:'2026-11-12'},days:[],period:{hasData:false},trips:[]})
  if(path.endsWith('/camera/sessions')&&method==='POST'){const source=route.request().postDataJSON().source;calls.created.push(source);data={id:'fixture-'+source}}
  if(/\/camera\/sessions\/[^/]+\/stream$/.test(path)){calls.frames.push(path);return route.fulfill({contentType:'image/jpeg',body:jpeg})}
  if(/\/camera\/sessions\/[^/]+$/.test(path)&&method==='DELETE')calls.released.push(path)
  await route.fulfill({json:data})
 })
 return calls
}

test('resource history retains real zero, bounds memory and breaks missing intervals',()=>{
 const history:MetricHistory={};remember(history,'cpu',1000,0);remember(history,'cpu',2000,50);remember(history,'cpu',3000,null);remember(history,'cpu',4000,20);remember(history,'cpu',3000,90)
 expect(history.cpu?.map(p=>p.value)).toEqual([0,50,null,20])
 const path=chartPath(history.cpu!,0,5000,100);expect(path.match(/M/g)).toHaveLength(2);expect(path).toContain('L')
 remember(history,'cpu',4000000,42);expect(history.cpu).toHaveLength(1)
})
test.describe('compact dashboard review',()=>{
 test.skip(!base,'Local Vite fixtures required')
 for(const [width,theme] of [[1920,'dark'],[1440,'light'],[390,'dark'],[320,'light']] as const)test(`${width}px ${theme}: layout, capacities, uptime and load`,async({page},info)=>{
  await page.setViewportSize({width,height:1100});const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message));await fixture(page,theme);await page.goto(base!)
  await page.locator('.dashboard-cameras').scrollIntoViewIfNeeded();await expect(page.locator('.dashboard-camera-pair img')).toHaveCount(2)
  expect(await page.locator('.dashboard-camera-pair img').first().evaluate(image=>(image as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
  const network=page.getByRole('region',{name:'ネットワーク接続'}),resource=page.getByRole('region',{name:'リソースモニター'}),body=page.getByRole('region',{name:'本体の状態'})
  await expect(network.getByTestId('cellular-remaining')).toHaveText('7.4GB')
  await expect(body).toContainText('1日 1時間 1分');await expect(body).toContainText('SDカード');await expect(body).toContainText('本体空き容量')
  await expect(resource).toContainText('Load average');await expect(resource).toContainText('0.42');await expect(resource).not.toContainText('SDカード')
  await expect(resource.locator('[data-metric]')).toHaveCount(4)
  expect(new Set(await resource.locator('.resource-load .resource-line').evaluateAll(nodes=>nodes.map(node=>getComputedStyle(node).stroke))).size).toBe(3)
  expect((await resource.getByRole('heading',{name:'リソースモニター',exact:true}).boundingBox())!.height).toBeLessThan(40)
  const boxes=await page.locator('.dashboard-overview').evaluate(element=>{
   const rect=(selector:string)=>{const b=element.querySelector(selector)!.getBoundingClientRect();return {x:b.x,y:b.y,width:b.width,height:b.height,bottom:b.bottom}}
   return {root:element.getBoundingClientRect().width,journey:rect('.jc-panel'),gps:rect('.gps-panel'),network:rect('.network-overview'),resources:rect('.resource-monitor'),body:rect('.device-status'),recording:rect('.dashboard-recording'),map:rect('.current-location-frame'),sections:[...element.querySelectorAll('.network-overview>section')].map(e=>e.getBoundingClientRect().y)}
  })
  expect(boxes.recording.bottom).toBeLessThanOrEqual(boxes.map.y+1)
  expect(Math.abs(boxes.gps.bottom-boxes.map.bottom)).toBeLessThanOrEqual(1)
  if(width>=1440){expect(boxes.gps.x).toBeGreaterThan(boxes.journey.x);expect(Math.abs(boxes.gps.y-boxes.journey.y)).toBeLessThan(1);expect(boxes.network.width).toBeCloseTo(boxes.root,0);expect(Math.max(...boxes.sections)-Math.min(...boxes.sections)).toBeLessThan(1);expect(boxes.network.height).toBeLessThan(300);expect(boxes.resources.x).toBeLessThan(boxes.body.x)}
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([])
  await page.screenshot({path:info.outputPath(`dashboard-${width}-${theme}.png`),fullPage:true})
 })
 test('JPEG polling stays at 0.5fps and stops when paused or leaving the dashboard',async({page})=>{
  const calls=await fixture(page,'light');await page.goto(base!);await page.locator('.dashboard-cameras').scrollIntoViewIfNeeded();await expect(page.locator('.dashboard-camera-pair img')).toHaveCount(2)
  expect(calls.created.sort()).toEqual(['front','in']);expect(calls.frames).toHaveLength(2)
  await page.clock.runFor(1999);expect(calls.frames).toHaveLength(2)
  await page.clock.runFor(1);await expect.poll(()=>calls.frames.length).toBe(4)
  await page.getByRole('button',{name:'ライブ映像を停止',exact:true}).click();await expect.poll(()=>calls.released.length).toBe(2)
  await page.clock.runFor(5000);expect(calls.frames).toHaveLength(4)
  await page.getByRole('button',{name:'ライブ映像を再開',exact:true}).click();await expect.poll(()=>calls.frames.length).toBe(6)
  await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'))})
  await expect.poll(()=>calls.released.length).toBe(4);await page.clock.runFor(5000);expect(calls.frames).toHaveLength(6)
  await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});document.dispatchEvent(new Event('visibilitychange'))})
  await expect.poll(()=>calls.frames.length).toBe(8)
  await page.getByRole('link',{name:'システム',exact:true}).evaluate(element=>(element as HTMLAnchorElement).click())
  await expect.poll(()=>calls.released.length).toBe(6);await page.clock.runFor(5000);expect(calls.frames).toHaveLength(8)
 })
 test('real samples produce traffic, CPU and load curves without invented capacity series',async({page})=>{
  await fixture(page,'light');await page.goto(base!);await expect(page.locator('.resource-monitor')).toBeVisible()
  await page.clock.runFor(2000);await expect(page.locator('.resource-network .resource-line').first()).toHaveAttribute('d',/L/)
  await page.clock.runFor(8000);await page.getByRole('button',{name:'状態を更新',exact:true}).evaluate(element=>(element as HTMLButtonElement).click())
  await expect(page.locator('[data-metric=cpu] .resource-line')).toHaveAttribute('d',/L/)
  await expect(page.locator('.resource-load .resource-line')).toHaveCount(3)
  await expect(page.locator('.resource-load .resource-line').first()).toHaveAttribute('d',/L/)
  await page.getByLabel('リソースモニターの表示期間').selectOption('15')
  await expect(page.locator('.resource-footer')).toContainText('11:45')
 })
})
