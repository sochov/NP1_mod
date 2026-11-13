import {test,expect,type Page} from '@playwright/test'
import {existsSync,readFileSync} from 'node:fs'
// Synthetic plates, recordings and video only (tools/create-playback-test-media.ps1); nothing comes from a device.
const reviewURL=process.env.NCC_UI_REVIEW_URL
const mediaPath='../tmp/drive-test-fixture.mp4'
const seen=Date.parse('2026-11-11T03:00:15Z'),iso=(offsetSeconds:number)=>new Date(seen+offsetSeconds*1000).toISOString()
const plate=(id:string,firstSeen:string,lastSeen=firstSeen)=>({record:{id,firstSeen,lastSeen,sightings:2,reading:{region:'品川',class:'300',kana:'あ',number:'1234',confidence:.9},display:'品川 300 あ 12-34',box:[800,600,240,120],matches:[]},imageAvailable:false})
// The fixture video is 20 seconds long, so each synthetic clip lasts 20 seconds.
const clip=(id:string,camera:string,start:number)=>({id,camera,name:`synthetic-${id}.mp4`,kind:'normal',startAt:iso(start),endAt:iso(start+20),durationSeconds:20,sizeBytes:'120000',width:320,height:180})
const clips=[clip('front-a','front',-15),clip('cabin-a','in',-15),clip('front-b','front',5),clip('front-later','front',600)]
async function open(page:Page,records:unknown[],options:{clips?:unknown[];pending?:number}={}){
 const errors:string[]=[],dates:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 const media=existsSync(mediaPath)?readFileSync(mediaPath):null
 await page.route('https://tile.openstreetmap.org/**',route=>route.abort())
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname
  if(route.request().method()!=='GET')return route.fulfill({status:409,json:{reason:'Review never changes the device'}})
  if(path==='/api/v1/plate/status')return route.fulfill({json:{enabled:true,revision:1,sensor:{state:'running'},collector:{error:''},speech:{available:false,reason:''},usage:{recordBytes:0,recordDays:1,imageBytes:0,imageCount:0,openRecords:0},imageQuotaBytes:268435456}})
  if(path==='/api/v1/plate/settings')return route.fulfill({json:{revision:1,enabled:true,rules:[],retentionDays:365,imageQuotaMB:256,mergeSeconds:30,minConfidence:.6,minSightings:2,voice:{style:1,volume:70,phrase:''}}})
  if(path==='/api/v1/plate/speech')return route.fulfill({json:{available:false,reason:'',styles:[],status:{keyConfigured:false,job:{state:'no-key',total:0,ready:0},rules:{},credit:''}}})
  if(path==='/api/v1/plate/records')return route.fulfill({json:{records,truncated:false,reason:''}})
  if(path==='/api/v1/recordings'){dates.push(url.searchParams.get('date')||'');return route.fulfill({json:{status:'available',source:'synthetic',observedAt:iso(0),verified:false,data:{clips:options.clips??clips,days:[],pendingFiles:options.pending??0,unreadableFiles:0,truncated:false,timezone:'Asia/Tokyo',indexedAt:iso(0)}}})}
  if(/\/api\/v1\/recordings\/[^/]+\/stream$/.test(path)){
   if(!media)return route.fulfill({status:404,body:''})
   const range=route.request().headers()['range']?.match(/^bytes=(\d+)-(\d*)$/)
   if(range){const start=Number(range[1]),end=Math.min(range[2]?Number(range[2]):media.length-1,media.length-1);return route.fulfill({status:206,contentType:'video/mp4',headers:{'accept-ranges':'bytes','content-range':`bytes ${start}-${end}/${media.length}`},body:media.subarray(start,end+1)})}
   return route.fulfill({status:200,contentType:'video/mp4',headers:{'accept-ranges':'bytes'},body:media})
  }
  return route.fulfill({status:404,json:{reason:'fixture'}})
 })
 await page.route('**/__plate-review',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/plate-review.ts"></script></body></html>'}))
 await page.goto(reviewURL!+'/__plate-review')
 await expect(page.locator('.plate-item')).toHaveCount(records.length)
 return {errors,dates}
}
const video=(page:Page)=>page.getByRole('dialog').locator('video')
test.describe('a plate record plays its own stretch of recording (synthetic fixtures)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to an isolated Vite server.')
 test.skip(!existsSync(mediaPath),'Run tools/create-playback-test-media.ps1 to create the synthetic video.')
 for(const width of [1440,390])test(`opens in a dialog, starts before the reading and continues into the next file at ${width}px`,async({page})=>{
  await page.setViewportSize({width,height:width>800?900:844})
  const state=await open(page,[plate('p1',iso(0))])
  await page.getByRole('button',{name:'品川 300 あ 12-34 の録画を再生',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'品川 300 あ 12-34 の録画'});await expect(dialog).toBeVisible()
  // Only the front camera, from 10 seconds before the reading: offset 5 s into the clip that started 15 s before.
  await expect(video(page)).toHaveAttribute('src','/api/v1/recordings/front-a/stream')
  await expect.poll(()=>video(page).evaluate((v:HTMLVideoElement)=>v.readyState)).toBeGreaterThanOrEqual(1)
  await expect.poll(()=>video(page).evaluate((v:HTMLVideoElement)=>v.currentTime)).toBeGreaterThanOrEqual(4.5)
  expect(await video(page).evaluate((v:HTMLVideoElement)=>v.currentTime)).toBeLessThan(7)
  await expect(dialog.locator('.plate-clip-times')).toContainText('検出 12:00:15')
  await expect(dialog.locator('.plate-clip-times')).toContainText('12:00:05');await expect(dialog.locator('.plate-clip-times')).toContainText('12:00:25')
  expect(state.dates).toEqual(['2026-11-11'])
  // The end of the first file hands over to the next one; playback stops 10 seconds after the reading.
  await video(page).evaluate(async(v:HTMLVideoElement)=>{v.muted=true;v.currentTime=v.duration-.05;await v.play().catch(()=>{})})
  await expect(video(page)).toHaveAttribute('src','/api/v1/recordings/front-b/stream')
  await expect.poll(()=>video(page).evaluate((v:HTMLVideoElement)=>v.readyState)).toBeGreaterThanOrEqual(1)
  await video(page).evaluate((v:HTMLVideoElement)=>{v.currentTime=5.5;v.dispatchEvent(new Event('timeupdate'))})
  await expect(dialog.getByRole('button',{name:'もう一度再生',exact:true})).toBeVisible()
  expect(await video(page).evaluate((v:HTMLVideoElement)=>v.paused)).toBe(true)
  await dialog.getByRole('button',{name:'もう一度再生',exact:true}).click()
  await expect(video(page)).toHaveAttribute('src','/api/v1/recordings/front-a/stream')
  await expect.poll(()=>video(page).evaluate((v:HTMLVideoElement)=>v.currentTime)).toBeGreaterThanOrEqual(4.5)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  // The full playback page is one click away, at the start of the same stretch.
  const link=dialog.getByRole('link',{name:'ドラレコ再生で開く',exact:true})
  await expect(link).toHaveAttribute('href','#recordings?at='+encodeURIComponent(iso(-10)))
  await link.click();await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(page.url()).toContain('#recordings?at=')
  expect(state.errors).toEqual([])
 })
 test('Escape and the backdrop close the dialog',async({page})=>{
  await page.setViewportSize({width:1440,height:900});const state=await open(page,[plate('p1',iso(0))])
  const play=page.getByRole('button',{name:'品川 300 あ 12-34 の録画を再生',exact:true})
  await play.click();await expect(page.getByRole('dialog')).toBeVisible();await page.keyboard.press('Escape');await expect(page.getByRole('dialog')).toHaveCount(0)
  await play.click();await page.locator('.modal-backdrop').click({position:{x:8,y:8}});await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.errors).toEqual([])
 })
 test('a reading with no recording says so instead of playing something else',async({page})=>{
  await page.setViewportSize({width:1440,height:900})
  const state=await open(page,[plate('p1',iso(300))])
  await page.getByRole('button',{name:'品川 300 あ 12-34 の録画を再生',exact:true}).click()
  const dialog=page.getByRole('dialog')
  await expect(dialog.getByText('この時刻の前方カメラの録画はありません',{exact:true})).toBeVisible()
  await expect(dialog.getByText(/保存期間を過ぎて上書きされたか/)).toBeVisible()
  await expect(dialog.locator('video')).toHaveCount(0);await expect(dialog.getByRole('button',{name:'最初から',exact:true})).toBeDisabled()
  await expect(dialog.getByRole('link',{name:'ドラレコ再生で開く',exact:true})).toBeVisible()
  expect(state.errors).toEqual([])
 })
 test('an unfinished recording is reported as pending',async({page})=>{
  await page.setViewportSize({width:390,height:844})
  const state=await open(page,[plate('p1',iso(0))],{clips:[],pending:2})
  await page.getByRole('button',{name:'品川 300 あ 12-34 の録画を再生',exact:true}).click()
  await expect(page.getByRole('dialog').getByText(/録画がまだ確定していない可能性があります/)).toBeVisible()
  expect(state.errors).toEqual([])
 })
 test('a reading just after midnight also looks at the previous day',async({page})=>{
  await page.setViewportSize({width:1440,height:900})
  const state=await open(page,[plate('p1','2026-11-11T15:00:04Z')],{clips:[]})
  await page.getByRole('button',{name:'品川 300 あ 12-34 の録画を再生',exact:true}).click()
  await expect(page.getByRole('dialog').getByText('この時刻の前方カメラの録画はありません',{exact:true})).toBeVisible()
  expect(state.dates.sort()).toEqual(['2026-11-11','2026-11-12'])
  expect(state.errors).toEqual([])
 })
})
