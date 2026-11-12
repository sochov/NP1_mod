import {test,expect,type Page} from '@playwright/test'
const base=process.env.NCC_UI_REVIEW_URL,stamp='2026-11-12T03:00:00Z'
const tile='<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256"><rect width="256" height="256" fill="#e7e9ec"/><path d="M0 38H256M0 142H256M63 0V256M190 0V256" stroke="#fff" stroke-width="11"/><path d="M0 220L256 66" stroke="#b5cadc" stroke-width="20"/><rect x="75" y="52" width="100" height="76" rx="7" fill="#cbd8cc"/></svg>'
const observation=(data:unknown,status='available')=>({data,status,source:'synthetic-dashboard-position',observedAt:stamp,refreshSeconds:10,verified:false})
async function fixture(page:Page,theme:string){
 await page.clock.install({time:new Date(stamp)})
 await page.addInitScript(()=>{(window as any).EventSource=class extends EventTarget{onerror=null;close(){}}})
 const state={gps:observation({fix:true,at:stamp,lat:35.68,lon:139.76,satellites:12,accuracyM:8}),recording:observation({writing:true,cameras:{front:true,in:true},writingObservedAt:stamp,finalizedClips:128})}
 await page.route('https://tile.openstreetmap.org/**',route=>route.fulfill({contentType:'image/svg+xml',body:tile}))
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;let data:unknown={}
  if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme,locale:'ja',favorites:[]}
  if(path.endsWith('/system/summary'))data=state
  if(path.endsWith('/traffic/usage'))data=observation({networks:{}})
  if(path.endsWith('/trips'))data=observation({range:{period:'7',from:'2026-11-06',to:'2026-11-12'},days:[],period:{hasData:false},trips:[]})
  await route.fulfill({json:data})
 })
 return state
}

test.describe('dashboard current location and recording',()=>{
 test.skip(!base,'Set NCC_UI_REVIEW_URL to isolated Vite')
 for(const [width,theme] of [[1440,'light'],[390,'dark'],[320,'light']] as const)test(`${width}px ${theme}: current location, recording, no-fix and stopped`,async({page},info)=>{
  await page.setViewportSize({width,height:1000});const state=await fixture(page,theme)
  await page.goto(base!);const panel=page.locator('.gps-panel');await panel.scrollIntoViewIfNeeded()
  await expect(panel.getByRole('region',{name:'現在地の地図'})).toBeVisible()
  await expect(panel.locator('.current-location-marker')).toHaveCount(1)
  await expect(panel.locator('.leaflet-tile-loaded').first()).toBeVisible()
  await expect(panel.getByLabel('現在地の緯度経度')).toContainText('緯度 35.680000')
  await expect(panel.getByLabel('現在地の緯度経度')).toContainText('経度 139.760000')
  const geometry=await panel.evaluate(element=>{
   const panel=element.getBoundingClientRect(),frame=element.querySelector('.current-location-frame')!.getBoundingClientRect(),details=element.querySelector('.current-location-details')!.getBoundingClientRect(),canvas=element.querySelector('.current-location-canvas')!.getBoundingClientRect(),recording=element.querySelector('.dashboard-recording')!.getBoundingClientRect()
   return {left:frame.left-panel.left,right:panel.right-frame.right,top:Math.abs(frame.top-recording.bottom),bottom:Math.abs(panel.bottom-frame.bottom),overlayInside:details.top>=canvas.top&&details.bottom<=canvas.bottom&&details.left>=canvas.left&&details.right<=canvas.right}
  })
  expect(geometry.left).toBeLessThanOrEqual(1);expect(geometry.right).toBeLessThanOrEqual(1)
  expect(geometry.top).toBeLessThanOrEqual(1);expect(geometry.bottom).toBeLessThanOrEqual(1);expect(geometry.overlayInside).toBe(true)
  await expect(panel.getByRole('status',{name:'録画状態'})).toContainText('録画中')
  await expect(panel.locator('.recording-copy')).toContainText('前方 録画中車内 録画中')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await panel.screenshot({path:info.outputPath(`dashboard-map-${theme}-${width}.png`)})
  state.gps=observation({fix:false} as any,'unknown');state.recording=observation({writing:false,cameras:{front:false,in:false},writingObservedAt:stamp,finalizedClips:128})
  await page.getByRole('button',{name:'状態を更新',exact:true}).evaluate(element=>(element as HTMLButtonElement).click());await panel.scrollIntoViewIfNeeded()
  await expect(panel.locator('.current-location-empty')).toContainText('測位待ち')
  await expect(panel.locator('.current-location-marker,.leaflet-tile')).toHaveCount(0)
  await expect(panel.getByLabel('現在地の緯度経度')).not.toContainText('35.680000')
  await expect(panel.getByRole('status',{name:'録画状態'})).toContainText('録画停止中')
  await page.getByRole('button',{name:'ドラレコ再生を開く'}).click();await expect(page).toHaveURL(/#recordings$/)
 })
 test('old positions, old recording evidence and missing SD never remain current',async({page})=>{
  const state=await fixture(page,'dark');await page.goto(base!);const panel=page.locator('.gps-panel');await panel.scrollIntoViewIfNeeded()
  await expect(panel.locator('.current-location-marker')).toHaveCount(1)
  await page.clock.fastForward(18000)
  await expect(panel.locator('.current-location-marker')).toHaveCount(0)
  await expect(panel.getByLabel('現在地の緯度経度')).not.toContainText('35.680000')
  await expect(panel.locator('.current-location-empty')).toContainText('現在地を更新できません')
  await page.clock.fastForward(13000)
  await expect(panel.getByRole('status',{name:'録画状態'})).toContainText('録画状態を取得できません')
  state.recording=observation(null as any,'unknown');state.gps=observation({fix:true,at:stamp,lat:900,lon:139.76} as any)
  await page.getByRole('button',{name:'状態を更新',exact:true}).evaluate(element=>(element as HTMLButtonElement).click())
  await expect(panel.locator('.current-location-marker')).toHaveCount(0)
  await expect(panel.getByRole('status',{name:'録画状態'})).not.toContainText('録画停止中')
  const recent=new Date(Date.parse(stamp)+31000).toISOString()
  state.gps=observation({fix:true,at:recent,lat:35.69,lon:139.77,satellites:10,accuracyM:12})
  state.recording=observation({writing:true,cameras:{front:true,in:false},writingObservedAt:recent,finalizedClips:128})
  await page.getByRole('button',{name:'状態を更新',exact:true}).evaluate(element=>(element as HTMLButtonElement).click())
  await expect(panel.locator('.current-location-marker')).toHaveCount(1)
  await expect(panel.getByRole('status',{name:'録画状態'})).toContainText('車内 更新なし')
 })
})

test('device dashboard shows measured recording activity and current GPS availability',async({page},info)=>{
 test.skip(process.env.NCC_DASHBOARD_HARDWARE!=='1','Explicit read-only device check')
 const errors:string[]=[],frames:{url:string;at:number;type:string}[]=[]
 page.on('pageerror',error=>errors.push(error.message))
 page.on('response',response=>{if(/\/camera\/sessions\/[^/]+\/stream$/.test(new URL(response.url()).pathname)&&response.ok())frames.push({url:response.url(),at:Date.now(),type:response.headers()['content-type']||''})})
 await page.setViewportSize({width:1920,height:1100})
 await page.goto('/#dashboard')
 const panel=page.locator('.gps-panel');await panel.scrollIntoViewIfNeeded()
 await expect(panel.getByRole('status',{name:'録画状態'})).toContainText('録画中',{timeout:30000})
 await expect(panel.locator('.dashboard-camera-pair img')).toHaveCount(2,{timeout:30000})
 await expect.poll(()=>panel.locator('.dashboard-camera-pair img').evaluateAll(images=>images.every(image=>(image as HTMLImageElement).naturalWidth>0))).toBe(true)
 const response=await page.request.get('/api/v1/system/summary');expect(response.ok()).toBe(true)
 const summary=await response.json()
 expect(summary.recording.data.writing).toBe(true)
 expect(summary.recording.data.cameras).toEqual({front:true,in:true})
 if(summary.gps.status==='available'&&summary.gps.data?.fix===true)await expect(panel.locator('.current-location-marker')).toHaveCount(1)
 else {await expect(panel.locator('.current-location-marker')).toHaveCount(0);await expect(panel.locator('.current-location-empty')).toBeVisible()}
 const resource=page.getByRole('region',{name:'リソースモニター'})
 await expect(resource.locator('[data-metric=cpu] .resource-line')).toHaveAttribute('d',/L/,{timeout:30000})
 await expect(resource.locator('.resource-load .resource-line').first()).toHaveAttribute('d',/L/,{timeout:30000})
 await expect(page.getByRole('region',{name:'本体の状態'}).locator('.device-uptime strong')).not.toHaveText('—')
 expect(new Set(frames.map(frame=>frame.url)).size).toBe(2)
 expect(frames.every(frame=>frame.type.includes('image/jpeg'))).toBe(true)
 for(const url of new Set(frames.map(frame=>frame.url))){const times=frames.filter(frame=>frame.url===url).map(frame=>frame.at);expect(times.length).toBeGreaterThanOrEqual(2);for(let i=1;i<times.length;i++)expect(times[i]!-times[i-1]!).toBeGreaterThanOrEqual(1800)}
 await page.screenshot({path:info.outputPath('device-dashboard-desktop.png'),fullPage:true})
 await page.setViewportSize({width:390,height:844});await panel.scrollIntoViewIfNeeded()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 const card=await panel.boundingBox(),frame=await panel.locator('.current-location-frame').boundingBox(),details=await panel.locator('.current-location-details').boundingBox()
 expect(Math.abs(frame!.x-card!.x)).toBeLessThanOrEqual(1)
 expect(Math.abs(frame!.x+frame!.width-card!.x-card!.width)).toBeLessThanOrEqual(1)
 expect(frame!.height).toBeGreaterThanOrEqual(250)
 expect(details!.y+details!.height).toBeLessThanOrEqual(frame!.y+frame!.height)
 expect(errors).toEqual([])
 await panel.screenshot({path:info.outputPath('device-dashboard-location.png')})
})
