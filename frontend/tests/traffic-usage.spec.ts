import { test, expect, type Page } from '@playwright/test'

const reviewURL=process.env.NCC_UI_REVIEW_URL
const observation=(data:unknown,status='available')=>({status,data,source:'synthetic-traffic-ui-fixture',observedAt:'2026-11-11T08:00:00Z',refreshSeconds:2,verified:false})
function network(values:Record<string,unknown>={}){return {interface:'synthetic-cellular',todayRX:'1000000000',todayTX:'200000000',monthRX:'3000000000',monthTX:'1000000000',rxBytesPerSecond:1250000,txBytesPerSecond:125000,sampledAt:'2026-11-11T08:00:00Z',observedSince:'2026-11-11T00:00:00Z',status:'available',speedStatus:'available',active:true,stale:false,partial:true,...values}}
function fixture(){return {networks:{cellular:network(),wifi:network({interface:'synthetic-wifi',todayRX:'2000000000',todayTX:'1000000000',monthRX:'10000000000',monthTX:'2000000000',rxBytesPerSecond:6250000,txBytesPerSecond:0})},sampledAt:'2026-11-11T08:00:00Z',startedAt:'2026-11-11T00:00:00Z',stale:false}}
async function open(page:Page,options:{usage?:any;limit?:string;put?:(body:any)=>Promise<{status:number;data:any}>}={}){
 await page.clock.install({time:new Date('2026-11-11T08:00:00Z')})
 let current=options.usage??fixture(),settings={revision:1,theme:'light',locale:'ja',favorites:['system','storage'],cellularLimitBytes:options.limit??'20000000000'}
 const requests:any[]=[]
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({status:200,contentType:'text/event-stream',body:': synthetic fixture\n\n'})
  let value:unknown={}
  if(path.endsWith('/auth/session'))value={csrf:'synthetic-csrf'}
  if(path.endsWith('/settings')){
   if(route.request().method()==='PUT'){
    const body=route.request().postDataJSON();requests.push(body)
    if(options.put){const result=await options.put(body);if(result.status===409)settings={...settings,revision:2,theme:'dark'};if(result.status===200)settings=result.data;return route.fulfill({status:result.status,contentType:'application/json',body:JSON.stringify(result.data)})}
    settings={...body.settings,revision:settings.revision+1}
   }
   value=settings
  }
  if(path.endsWith('/system/summary'))value={usage:observation(current),modem:observation({registered:true,carrier:'Fixture Carrier',apn:'fixture.invalid',band:'B3',rsrp:-82}),interfaces:observation([{ifname:'synthetic-cellular',flags:['UP'],addr_info:[{family:'inet',scope:'global',local:'10.42.88.19'}]}])}
  if(path.endsWith('/traffic/usage'))value=observation(current)
	if(path.endsWith('/trips'))value=observation({source:'retained-measured-intervals',status:'no-fix',range:{period:'7',from:'2026-11-05',to:'2026-11-11'},period:{distanceKm:null,drivingSeconds:null,hasData:false,validObservedDays:0,sampleCount:0,days:[]},days:[],trips:[],totalTrips:0,sourceTruncated:false,detailsTruncated:false,sourceFiles:0,unreadableFiles:0,noFixSentences:0,badChecksums:0,indexedAt:'2026-11-11T08:00:00Z'})
  if(path.endsWith('/capabilities'))value=[]
  return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(reviewURL!)
 await expect(page.getByRole('heading',{name:'4G データ通信',exact:true})).toBeVisible()
 return {requests,setUsage(value:any){current=value}}
}
async function noOverflow(page:Page){expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
test.describe('separate cellular/Wi-Fi traffic and allowance (synthetic fixtures)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to the local Vite UI for isolated UI tests.')
 test('4G allowance is decimal GB, speeds convert bytes/sec to bits/sec and Wi-Fi stays separate',async({page},testInfo)=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  await page.setViewportSize({width:1440,height:1050});const state=await open(page)
  await expect(page.getByTestId('cellular-remaining')).toHaveText('16.0GB')
  await expect(page.getByTestId('cellular-month')).toHaveText('4 GB')
  await expect(page.getByTestId('traffic-total')).toHaveText('1.2 GB')
  await expect(page.getByTestId('cellular-download')).toHaveText('10 Mbps')
  await expect(page.getByTestId('cellular-upload')).toHaveText('1.0 Mbps')
  await expect(page.getByTestId('wifi-month')).toHaveText('12 GB')
  await expect(page.getByTestId('wifi-today')).toHaveText('3 GB')
  await expect(page.getByRole('button',{name:'通信量を開く',exact:true})).toBeVisible()
  await expect(page.locator('.usage-note')).toHaveCount(0)
  await expect(page.locator('.traffic-usage .wifi-traffic')).toHaveCount(0)
  await expect(page.getByTestId('cellular-carrier')).toContainText('Fixture Carrier')
  await expect(page.locator('.cellular-details')).toContainText('LTE Band 3')
  await expect(page.locator('.cellular-details')).toContainText('10.42.88.19')
  await expect(page.getByRole('heading',{name:'接続状況',exact:true})).toHaveCount(0)
  await noOverflow(page);await page.screenshot({path:testInfo.outputPath('traffic-desktop.png'),fullPage:true})
  const next=fixture();next.networks.cellular.rxBytesPerSecond=2500000;next.sampledAt=next.networks.cellular.sampledAt='2026-11-11T08:00:02Z';state.setUsage(next)
  await page.clock.runFor(2000);await expect(page.getByTestId('cellular-download')).toHaveText('20 Mbps')
  await page.getByRole('link',{name:'4G',exact:true}).click()
  await expect(page.locator('.traffic-usage .hero-head button')).toHaveCount(0)
  await expect(page.getByTestId('wifi-month')).toHaveCount(0)
  await noOverflow(page)
  expect(errors).toEqual([])
 })
 test('390px unset plan can be configured without replacing other settings',async({page},testInfo)=>{
  await page.setViewportSize({width:390,height:844});const state=await open(page,{limit:'0'})
  await expect(page.locator('.traffic-donut-placeholder')).toHaveText('未設定')
  await page.getByRole('button',{name:'月間上限を設定',exact:true}).click()
  await page.getByRole('textbox',{name:/4Gの月間上限/}).fill('20.5')
  await page.locator('.traffic-limit-form').getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.getByTestId('cellular-remaining')).toHaveText('16.5GB')
  expect(state.requests).toHaveLength(1)
  expect(state.requests[0]).toEqual({expectedRevision:1,settings:{revision:1,theme:'light',locale:'ja',favorites:['system','storage'],cellularLimitBytes:'20500000000'}})
  await noOverflow(page);await page.screenshot({path:testInfo.outputPath('traffic-mobile.png'),fullPage:true})
 })
 test('320px missing counters and speeds stay unknown; an exceeded plan stays at zero',async({page},testInfo)=>{
  await page.setViewportSize({width:320,height:780});const usage=fixture();usage.networks.cellular=network({todayRX:null,monthRX:null,rxBytesPerSecond:0,speedStatus:'baseline'}) as any
  const state=await open(page,{usage})
  await expect(page.getByTestId('cellular-remaining')).toHaveText('—GB')
  await expect(page.getByTestId('traffic-total')).toHaveText('—')
  await expect(page.getByTestId('cellular-download')).toHaveText('—')
  await expect(page.getByTestId('wifi-month')).toHaveText('12 GB')
  await noOverflow(page);await page.screenshot({path:testInfo.outputPath('traffic-missing-narrow.png'),fullPage:true})
  state.setUsage({...fixture(),networks:{...fixture().networks,cellular:network({monthRX:'25000000000',monthTX:'1000000000'})}})
  await page.clock.runFor(2000)
  await expect(page.getByTestId('cellular-remaining')).toHaveText('0.0GB')
  await expect(page.getByText('設定上限を6 GB超過')).toBeVisible()
  await noOverflow(page)
 })
 test('stale speeds are hidden and revision conflict reloads settings before retry',async({page})=>{
  let attempts=0
  const state=await open(page,{usage:{...fixture(),stale:true},put:async body=>{attempts++;return attempts===1?{status:409,data:{reason:'REVISION_CONFLICT'}}:{status:200,data:{...body.settings,revision:3}}}})
  await expect(page.getByTestId('cellular-download')).toHaveText('—')
  await expect(page.getByText('更新が止まっています')).toBeVisible()
  await page.getByRole('button',{name:'月間上限を変更',exact:true}).click()
  await page.getByRole('textbox',{name:/4Gの月間上限/}).fill('101000')
  await page.locator('.traffic-limit-form').getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.getByRole('alert')).toContainText('0〜100,000 GB')
  expect(state.requests).toHaveLength(0)
  await page.getByRole('textbox',{name:/4Gの月間上限/}).fill('50')
  await page.locator('.traffic-limit-form').getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.getByRole('alert')).toContainText('別の操作で設定が変わりました')
  await page.locator('.traffic-limit-form').getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.getByRole('status')).toContainText('月間上限を保存しました')
  expect(state.requests[1].expectedRevision).toBe(2)
  expect(state.requests[1].settings.theme).toBe('dark')
 })
 test('dark mode, measured zero and a failed live poll preserve totals without stale speeds',async({page},testInfo)=>{
  await page.setViewportSize({width:390,height:844});const usage=fixture();usage.networks.cellular=network({rxBytesPerSecond:0,txBytesPerSecond:0}) as any
  await open(page,{usage})
  await page.evaluate(()=>document.documentElement.setAttribute('data-theme','dark'))
  await expect(page.getByTestId('cellular-download')).toHaveText('0 bps')
  await page.route('**/api/v1/traffic/usage',route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({reason:'Synthetic offline fixture'})}))
  await page.clock.runFor(2000)
  await expect(page.getByText('更新が止まっています')).toBeVisible()
  await expect(page.getByTestId('cellular-download')).toHaveText('—')
  await expect(page.getByTestId('cellular-month')).toHaveText('4 GB')
  await noOverflow(page);await page.screenshot({path:testInfo.outputPath('traffic-dark-offline.png'),fullPage:true})
 })
})
