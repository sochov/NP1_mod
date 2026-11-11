import { test, expect, type Page } from '@playwright/test'
import { existsSync, readFileSync } from 'node:fs'

const reviewURL=process.env.NCC_UI_REVIEW_URL
const mediaPath='../tmp/drive-test-fixture.mp4'
const observation=(data:unknown)=>({status:'available',data,source:'browser-test-fixture',observedAt:'2026-11-11T08:00:00Z',refreshSeconds:15,verified:false})
const clip=(id:string,camera:string,startAt:string,endAt:string)=>({id,camera,name:`synthetic-${id}.mp4`,kind:'normal',startAt,endAt,durationSeconds:20,sizeBytes:'120000',videoCodec:'avc1',audioCodec:'mp4a',width:320,height:180})
const clips=[clip('front-a','front','2026-11-11T03:00:00Z','2026-11-11T03:00:20Z'),clip('cabin-a','in','2026-11-11T03:00:00Z','2026-11-11T03:00:20Z'),clip('front-b','front','2026-11-11T03:00:19Z','2026-11-11T03:00:39Z'),clip('cabin-later','in','2026-11-11T04:00:00Z','2026-11-11T04:00:20Z')]
async function open(page:Page){
 await page.clock.install({time:new Date('2026-11-11T08:00:00Z')})
 const media=existsSync(mediaPath)?readFileSync(mediaPath):null
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({status:200,contentType:'text/event-stream',body:': synthetic test fixture\n\n'})
  if(path.endsWith('/stream')){
   if(!media)return route.fulfill({status:404,body:'Generate the optional synthetic MP4 with tools/create-playback-test-media.ps1'})
   const range=route.request().headers()['range']?.match(/^bytes=(\d+)-(\d*)$/)
   if(range){const start=Number(range[1]),end=Math.min(range[2]?Number(range[2]):media.length-1,media.length-1);return route.fulfill({status:206,contentType:'video/mp4',headers:{'accept-ranges':'bytes','content-range':`bytes ${start}-${end}/${media.length}`},body:media.subarray(start,end+1)})}
   return route.fulfill({status:200,contentType:'video/mp4',headers:{'accept-ranges':'bytes'},body:media})
  }
  let value:unknown={}
  if(path.endsWith('/auth/session'))value={csrf:'test-fixture'}
  if(path.endsWith('/settings'))value={revision:1,theme:'system',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/recordings'))value=observation({clips,days:[{date:'2026-11-11',count:4,frontCount:2,cabinCount:2,recordedSeconds:60}],pendingFiles:1,unreadableFiles:0,truncated:false,timezone:'Asia/Tokyo',indexedAt:'2026-11-11T08:00:00Z'})
  return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(reviewURL!+'/#recordings')
 await expect(page.locator('.drive-toolbar h2')).toHaveText('11月11日(水)')
 await expect(page.locator('.drive-recorded-segment')).toHaveCount(3)
}
async function noOverflow(page:Page){expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
test.describe('dashcam playback user review (synthetic browser fixtures)',()=>{
 test.use({hasTouch:true})
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to the local Vite UI for isolated playback review.')
 test('desktop calendar, recorded intervals, empty gaps and original audio',async({page})=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  await page.setViewportSize({width:1440,height:1100});await open(page)
  await expect(page.locator('.drive-calendar')).toBeVisible()
  await expect(page.locator('.drive-calendar-grid button.recorded')).toHaveCount(1)
  await expect(page.locator('.recorded-range-list button')).toHaveCount(2)
  await page.getByRole('button',{name:'並べて表示',exact:true}).click()
  await expect(page.locator('.drive-videos video')).toHaveCount(2)
  expect(await page.locator('video').evaluateAll(elements=>elements.map(element=>(element as HTMLVideoElement).muted))).toEqual([false,true])
  await noOverflow(page)
  await page.screenshot({path:'../tmp/review-playback-desktop.png',fullPage:true})
  await page.getByRole('button',{name:'前方',exact:true}).click()
  await page.getByLabel('再生する時刻を指定').fill('12:30:00')
  await expect(page.locator('.drive-video-empty h3')).toHaveText('この時刻の録画がありません')
  await expect(page.locator('.drive-videos video')).toHaveCount(0)
  await page.locator('.recorded-range-list button').last().click()
  await expect(page.locator('.drive-video-label').first()).toContainText('車内カメラ')
  expect(errors).toEqual([])
 })
 test('390px tap timeline, calendar and date with no recordings',async({page})=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  await page.setViewportSize({width:390,height:844});await open(page)
  const lane=page.getByRole('slider',{name:'前方カメラの録画時間バー'})
  await lane.locator('.drive-recorded-segment').first().tap()
  await expect(page.locator('.drive-videos video')).toHaveCount(1)
  await expect(page.locator('.drive-time-ruler')).toContainText('12:00')
  await noOverflow(page)
  await page.screenshot({path:'../tmp/review-playback-mobile.png',fullPage:true})
  await page.getByRole('button',{name:/カレンダーで日付を選ぶ/}).click()
  await expect(page.locator('.drive-calendar')).toBeVisible()
  await page.getByRole('button',{name:'2026-11-10、録画なし',exact:true}).click()
  await expect(page.locator('.drive-toolbar')).toContainText('この日の確定録画はありません')
  await expect(page.locator('.drive-recorded-segment')).toHaveCount(0)
  await expect(page.locator('.drive-videos video')).toHaveCount(0)
  await noOverflow(page);expect(errors).toEqual([])
 })
 test('320px dual view preserves controls and keyboard seek',async({page})=>{
  await page.setViewportSize({width:320,height:780});await open(page)
  await page.getByRole('button',{name:'並べて表示',exact:true}).click()
  await expect(page.locator('.drive-videos video')).toHaveCount(2)
  await noOverflow(page)
  await page.screenshot({path:'../tmp/review-playback-narrow.png',fullPage:true})
  const lane=page.getByRole('slider',{name:'前方カメラの録画時間バー'})
  await lane.focus();await page.keyboard.press('ArrowRight')
  await expect(lane).toHaveAttribute('aria-valuetext','12:00:10')
 })
 test('native MP4 seek starts at requested second',async({page})=>{
  test.skip(!existsSync(mediaPath),'Generate optional synthetic MP4 with tools/create-playback-test-media.ps1')
  await open(page)
  await page.getByLabel('再生する時刻を指定').fill('12:00:07')
  const video=page.locator('.drive-videos video')
  await expect.poll(()=>video.evaluate(element=>(element as HTMLVideoElement).readyState)).toBeGreaterThanOrEqual(2)
  await expect.poll(()=>video.evaluate(element=>(element as HTMLVideoElement).currentTime)).toBeGreaterThanOrEqual(7)
  await expect(video).toHaveJSProperty('muted',false)
 })
 test('overlapping file boundary continues without replaying the overlap',async({page})=>{
  test.skip(!existsSync(mediaPath),'Generate optional synthetic MP4 with tools/create-playback-test-media.ps1')
  await open(page)
  let video=page.locator('.drive-videos video')
  await expect.poll(()=>video.evaluate(element=>(element as HTMLVideoElement).readyState)).toBeGreaterThanOrEqual(2)
  await video.evaluate(element=>{const media=element as HTMLVideoElement;media.currentTime=19.9;return media.play()})
  await expect(video).toHaveAttribute('src',/front-b\/stream$/)
  await expect.poll(()=>video.evaluate(element=>(element as HTMLVideoElement).currentTime)).toBeGreaterThanOrEqual(1)
  await page.getByLabel('再生する時刻を指定').fill('12:00:19')
  video=page.locator('.drive-videos video')
  await expect(video).toHaveAttribute('src',/front-b\/stream$/)
 })
})
