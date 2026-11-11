import {test,expect,type Page} from '@playwright/test'
import {readFileSync} from 'node:fs'
import {pointAt,nearestPoint,type TrackPoint} from '../src/services/playbackTrack'
const url=process.env.NCC_UI_REVIEW_URL,at=Date.parse('2026-11-11T03:00:00Z'),stamp=(second:number)=>new Date(at+second*1000).toISOString()
const points:TrackPoint[]=[0,1,2,10,11,12].map((second,index)=>({at:stamp(second),lat:35+(index<3?0:.0001),lon:139+index*.0001,segment:index<3?0:1,speedMps:10}))
const segments=[{id:0,start:stamp(0),end:stamp(2),pointCount:3,distanceKm:.02,drivingSeconds:2},{id:1,start:stamp(10),end:stamp(12),pointCount:3,distanceKm:.02,drivingSeconds:2}]
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-playback-track',verified:false})
async function open(page:Page,empty=false,theme='light',linked=false){
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 const media=readFileSync('../tmp/drive-test-fixture.mp4'),png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=','base64')
 await page.route('https://tile.openstreetmap.org/**',route=>route.fulfill({contentType:'image/png',body:png}))
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/stream'))return route.fulfill({contentType:'video/mp4',headers:{'accept-ranges':'bytes'},body:media})
  if(path.endsWith('/recordings/track'))return route.fulfill({json:obs({date:'2026-11-11',points:empty?[]:points,segments:empty?[]:segments,source:'synthetic',sourceTruncated:false,indexedAt:stamp(20)})})
  if(path.endsWith('/recordings'))return route.fulfill({json:obs({clips:[{id:'synthetic-front',name:'synthetic.mp4',camera:'front',startAt:stamp(0),endAt:stamp(20),durationSeconds:20,sizeBytes:String(media.length),width:320,height:180}],days:[{date:'2026-11-11',count:1,frontCount:1,cabinCount:0,recordedSeconds:20}],timezone:'Asia/Tokyo',indexedAt:stamp(20),pendingFiles:0,unreadableFiles:0,truncated:false})})
  return route.fulfill({json:{}})
 })
 await page.route('**/__playback-track-review',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/playback-track-review.ts"></script></body></html>'}))
 await page.goto(url!+'/__playback-track-review#recordings'+(linked?'?at='+encodeURIComponent(stamp(11)):''))
 await page.evaluate(value=>document.documentElement.dataset.theme=value,theme)
 await expect(page.locator('.drive-toolbar')).toContainText('11月11日')
 return errors
}
async function setTime(page:Page,second:number){const video=page.locator('.drive-videos video');await expect.poll(()=>video.evaluate(element=>(element as HTMLVideoElement).readyState)).toBeGreaterThanOrEqual(2);await video.evaluate((element,value)=>{const video=element as HTMLVideoElement;video.pause();video.currentTime=value;video.dispatchEvent(new Event('timeupdate'))},second)}

test('track lookup rejects missing time and chooses the closest actual sample',()=>{
 expect(pointAt(points,at+11000)?.at).toBe(stamp(11));expect(pointAt(points,at+6000)).toBeUndefined()
 expect(nearestPoint(points,points[1].lat,points[1].lon,at,0)?.at).toBe(stamp(1))
})
test.describe('recordings, journey and map synchronization',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to isolated Vite')
 for(const width of [390,1440])test(`${width}px video position, map seek and GPS gaps`,async({page},info)=>{
  await page.setViewportSize({width,height:900});const errors=await open(page,false,width===390?'dark':'light')
  await expect(page.locator('.playback-route')).toHaveCount(2)
  if(width===1440){const video=await page.locator('.drive-player-panel').boundingBox(),map=await page.locator('.playback-track').boundingBox();expect(map!.y).toBeLessThan(video!.y+video!.height);expect(map!.x).toBeGreaterThan(video!.x)}
  await setTime(page,11);await expect(page.getByTestId('playback-position-time')).toHaveText('12:00:11')
  await expect(page.locator('.playback-location')).toHaveCount(1)
  await page.locator('.playback-route').first().click({force:true})
  await expect.poll(()=>page.locator('.drive-videos video').evaluate(element=>(element as HTMLVideoElement).currentTime)).toBeLessThan(4)
  await setTime(page,6);await expect(page.locator('.playback-location')).toHaveCount(0)
  await expect(page.getByText('この時刻の位置記録はありません',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'12:00:10の走行区間から再生',exact:true}).click()
  await expect.poll(()=>page.locator('.drive-videos video').evaluate(element=>(element as HTMLVideoElement).currentTime)).toBeGreaterThanOrEqual(10)
  await page.locator('.drive-videos video').evaluate(element=>(element as HTMLVideoElement).pause())
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('recording-map-'+width+'.png'),fullPage:true});expect(errors).toEqual([])
 })
 test('no fix stays empty and an initial journey link selects its exact recording time',async({page})=>{
  await open(page,true,'dark',true)
  await expect(page.locator('.playback-map-empty')).toContainText('この日の位置記録はありません')
  await expect(page.locator('.playback-location,.playback-route')).toHaveCount(0)
  await expect.poll(()=>page.locator('.drive-videos video').evaluate(element=>(element as HTMLVideoElement).currentTime)).toBeCloseTo(11,0)
 })
})
