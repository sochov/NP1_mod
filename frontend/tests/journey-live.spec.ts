import {test,expect,type Page,type Route} from '@playwright/test'
import {calendar,type JourneyHistory} from '../src/services/journey'

const instant='2026-11-11T08:00:00Z'
const observation=(data:unknown)=>({status:'available',data,source:'synthetic-retained-journey',observedAt:instant,refreshSeconds:30,verified:false})
function history(query:URLSearchParams,multiplier:number):JourneyHistory{
 const kind=query.get('period')||'7',to=query.get('to')||'2026-11-11',count=['all','custom'].includes(kind)?7:Number(kind),from=query.get('from')||(kind==='all'?'2026-08-01':new Date(Date.parse(to)-(count-1)*86400000).toISOString().slice(0,10))
 const days=calendar(from,to,[]).map(day=>({...day,observed:day.date!=='2026-10-01',distanceKm:day.date==='2026-10-01'?null:multiplier,drivingSeconds:day.date==='2026-10-01'?null:multiplier*60,observedSeconds:day.date==='2026-10-01'?0:multiplier*60,sampleCount:day.date==='2026-10-01'?0:2}))
 const observed=days.filter(day=>day.observed),distance=observed.reduce((sum,day)=>sum+day.distanceKm!,0),seconds=observed.reduce((sum,day)=>sum+day.drivingSeconds!,0)
 return {range:{period:kind,from,to,sourceFrom:from,sourceTo:to},days,period:{distanceKm:distance,drivingSeconds:seconds,hasData:true,validObservedDays:observed.length,sampleCount:observed.length*2,days},trips:observed.map(day=>({id:'synthetic-'+day.date,start:day.date+'T01:00:00Z',end:day.date+'T01:01:00Z',distanceKm:day.distanceKm!,drivingSeconds:day.drivingSeconds!,observedSeconds:day.drivingSeconds!,sampleCount:2})),totalTrips:observed.length,detailsTruncated:false,sourceTruncated:false,sourceFiles:0,unreadableFiles:0,noFixSentences:0,badChecksums:0,indexedAt:instant,source:'ncc-retained-measured-intervals',status:'available'}
}
async function setup(page:Page){
 await page.clock.install({time:new Date(instant)})
 let multiplier=1,fail=false,hold=false,pending:Route|undefined
 const queries:URLSearchParams[]=[],errors:string[]=[],aborted:string[]=[]
 page.on('pageerror',error=>errors.push(error.message))
 page.on('requestfailed',request=>{if(new URL(request.url()).pathname.endsWith('/trips'))aborted.push(request.url())})
 await page.route('https://tile.openstreetmap.org/**',route=>route.abort())
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': synthetic fixture\n\n'})
  let data:unknown={}
  if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  else if(path.endsWith('/settings'))data={revision:1,theme:'dark',locale:'ja',favorites:[]}
  else if(path.endsWith('/capabilities'))data=[]
  else if(path.endsWith('/system/summary'))data={tripStats:observation({periods:{7:{distanceKm:777,drivingSeconds:777,hasData:true,validObservedDays:7,sampleCount:7,days:[]}}})}
  else if(path.endsWith('/trips')){
   queries.push(url.searchParams)
   if(hold){hold=false;pending=route;return}
   if(fail)return route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({reason:'Synthetic source failure'})})
   data=observation(history(url.searchParams,multiplier))
  }
  else if(path.endsWith('/radio/history'))data={unit:'dBm',points:[],cells:[],total:0,unplaced:0,accuracyExcluded:0,missing:0,outOfService:0,carriers:[],bands:[],truncated:false}
  else if(path.endsWith('/radio/heatmap/legend'))data={unit:'dBm',note:'',thresholds:[]}
  await route.fulfill({contentType:'application/json',body:JSON.stringify(data)})
 })
 return {queries,errors,aborted,setMultiplier(value:number){multiplier=value},setFailure(value:boolean){fail=value},holdNext(){hold=true},hasPending(){return !!pending},async releasePending(value:number){const route=pending;pending=undefined;if(route){try{await route.fulfill({contentType:'application/json',body:JSON.stringify(observation(history(new URL(route.request().url()).searchParams,value)))})}catch{}}}}
}
async function visibility(page:Page,hidden:boolean){await page.evaluate(value=>{Object.defineProperty(document,'hidden',{configurable:true,get:()=>value});Object.defineProperty(document,'visibilityState',{configurable:true,get:()=>value?'hidden':'visible'});document.dispatchEvent(new Event('visibilitychange'))},hidden)}

test('dashboard reads retained API and updates in place without following every summary poll',async({page})=>{
 const state=await setup(page);await page.goto('/#dashboard')
 await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 expect(state.queries).toHaveLength(1)
 await page.locator('.jc-bucket').first().click();const selected=await page.locator('.jc-bucket.selected').getAttribute('aria-label')
 state.setMultiplier(2)
 await page.clock.runFor(10000);expect(state.queries).toHaveLength(1)
 await page.clock.runFor(20000)
 await expect(page.getByTestId('trip-distance')).toHaveText('14km')
 expect(state.queries).toHaveLength(2)
 await expect(page.locator('.jc-bucket.selected')).toHaveAttribute('aria-label',selected!.replace('距離 1 km','距離 2 km').replace('0時間 1分','0時間 2分'))
 await expect(page.locator('.jc-detail')).toContainText('距離 2 km')
 expect(state.errors).toEqual([])
})

test('background refresh preserves custom drafts, aggregation, missing-day selection and expanded day list',async({page},info)=>{
 const state=await setup(page);await page.goto('/#trips')
 await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 await page.getByRole('button',{name:'カスタム',exact:true}).click()
 await page.getByLabel('開始日',{exact:true}).fill('2026-08-20');await page.getByLabel('終了日',{exact:true}).fill('2026-11-10');await page.getByRole('button',{name:'表示',exact:true}).click()
 await expect(page.getByTestId('trip-distance')).toHaveText('82km')
 await page.getByRole('button',{name:'さらに表示',exact:true}).click()
 await expect(page.locator('.dh-days>button:not(.dh-more)')).toHaveCount(80)
 await page.getByLabel('詳細を表示する日付').fill('2026-10-01')
 await expect(page.getByText('この日に有効な連続測位の記録はありません。')).toBeVisible()
 await page.getByLabel('走行グラフの集約').selectOption('month');await page.locator('.jc-bucket').first().click()
 // Editing an unapplied range must not change the active query during polling.
 await page.getByLabel('開始日',{exact:true}).fill('2026-09-01')
 state.setMultiplier(2);await page.clock.runFor(30000)
 await expect(page.getByTestId('trip-distance')).toHaveText('164km')
 await expect(page.getByLabel('走行グラフの集約')).toHaveValue('month')
 await expect(page.getByLabel('開始日',{exact:true})).toHaveValue('2026-09-01')
 await expect(page.getByLabel('終了日',{exact:true})).toHaveValue('2026-11-10')
 await expect(page.getByLabel('詳細を表示する日付')).toHaveValue('2026-10-01')
 await expect(page.locator('.dh-days>button:not(.dh-more)')).toHaveCount(80)
 await expect(page.locator('.jc-bucket.selected')).toHaveCount(1)
 expect(state.queries.at(-1)!.get('from')).toBe('2026-08-20')
 await page.getByLabel('詳細を表示する日付').scrollIntoViewIfNeeded();await page.screenshot({path:info.outputPath('journey-live-preserved.png')})
 expect(state.errors).toEqual([])
})

test('one request stays in flight, hidden tabs abort and pause, visible resumes with stale responses ignored',async({page})=>{
 const state=await setup(page);await page.goto('/#trips');await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 state.holdNext();await page.clock.runFor(30000);await expect.poll(()=>state.hasPending()).toBe(true)
 await page.clock.runFor(60000);expect(state.queries).toHaveLength(2)
 await visibility(page,true);await page.clock.runFor(90000);expect(state.queries).toHaveLength(2)
 await expect.poll(()=>state.aborted.length).toBeGreaterThan(0)
 await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 state.setMultiplier(2);await visibility(page,false)
 await expect(page.getByTestId('trip-distance')).toHaveText('14km')
 expect(state.queries).toHaveLength(3)
 await state.releasePending(99)
 await expect(page.getByTestId('trip-distance')).toHaveText('14km')
 await expect(page.getByText('更新できませんでした。前回の走行記録を表示しています。')).toHaveCount(0)
 expect(state.errors).toEqual([])
})

test('changing period cancels the old request and cannot replace the new period with a late response',async({page})=>{
 const state=await setup(page);await page.goto('/#trips');await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 state.holdNext();await page.getByRole('button',{name:'90日',exact:true}).click();await expect.poll(()=>state.hasPending()).toBe(true)
 await page.getByRole('button',{name:'30日',exact:true}).click()
 await expect.poll(()=>state.aborted.some(value=>new URL(value).searchParams.get('period')==='90')).toBe(true)
 await expect(page.getByTestId('trip-distance')).toHaveText('30km')
 await state.releasePending(99)
 await expect(page.getByTestId('trip-distance')).toHaveText('30km')
 await expect(page.getByRole('button',{name:'30日',exact:true})).toHaveAttribute('aria-pressed','true')
 expect(state.errors).toEqual([])
})

test('failed background refresh keeps last measured values and selection until a confirmed retry',async({page},info)=>{
 await page.setViewportSize({width:390,height:844})
 const state=await setup(page);await page.goto('/#trips');await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 await page.getByLabel('詳細を表示する日付').fill('2026-11-07')
 state.setFailure(true);await page.clock.runFor(30000)
 await expect(page.getByText('更新できませんでした。前回の走行記録を表示しています。')).toBeVisible()
 await expect(page.getByTestId('trip-distance')).toHaveText('7km')
 await expect(page.getByLabel('詳細を表示する日付')).toHaveValue('2026-11-07')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.evaluate(()=>window.scrollTo(0,0))
 await page.screenshot({path:info.outputPath('journey-live-failed-refresh-mobile.png')})
 state.setFailure(false);state.setMultiplier(3);await page.getByRole('button',{name:'再試行',exact:true}).click()
 await expect(page.getByTestId('trip-distance')).toHaveText('21km')
 await expect(page.getByText('更新できませんでした。前回の走行記録を表示しています。')).toHaveCount(0)
 await expect(page.getByLabel('詳細を表示する日付')).toHaveValue('2026-11-07')
 expect(state.errors).toEqual([])
})
