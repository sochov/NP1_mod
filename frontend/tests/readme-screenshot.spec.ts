import {test,expect} from '@playwright/test'
import {readFileSync} from 'node:fs'
test.skip(process.env.NCC_PUBLIC_SCREENSHOTS!=='1','Explicit synthetic public illustration only')
// The public illustration shows what a first-time browser gets: the floating menu and the overview dashboard.
test.use({storageState:{cookies:[],origins:[]}})
test('public dashboard illustration contains only synthetic data',async({page},info)=>{
 const jpeg=Buffer.from(readFileSync(new URL('./plate-recognition.spec.ts',import.meta.url),'utf8').match(/const jpeg=Buffer.from\('([^']+)'/)![1]!,'base64')
 const base=process.env.NCC_UI_REVIEW_URL
 if(!base||new URL(base).hostname!=='127.0.0.1')throw new Error('Isolated local UI required')
 await page.addInitScript(()=>{(window as any).EventSource=class extends EventTarget{onerror=null;close(){}}});await page.setViewportSize({width:1440,height:1000});await page.clock.install({time:new Date('2026-11-11T08:00:00Z')})
 let sampleTime=Date.parse('2026-11-11T08:00:00Z')
 const obs=(data:unknown)=>({status:'available',data,source:'synthetic-public-illustration',verified:false,observedAt:new Date(sampleTime).toISOString()})
 const days=[28.4,17.2,32.3,0,42,19.1,22.6].map((distanceKm,index)=>({date:`2026-11-${String(5+index).padStart(2,'0')}`,distanceKm,drivingSeconds:Math.round(distanceKm*110),observed:true,sampleCount:1800,observedSeconds:3600}))
 const period={hasData:true,distanceKm:161.6,drivingSeconds:17776,validObservedDays:7,sampleCount:12600,days}
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),method=route.request().method()
  if(url.pathname.endsWith('/camera/sessions')&&method==='POST')return route.fulfill({json:{id:'synthetic-'+route.request().postDataJSON().source}})
  if(url.pathname.includes('/camera/sessions/')&&url.pathname.endsWith('/stream'))return route.fulfill({contentType:'image/jpeg',body:jpeg})
  if(url.pathname.includes('/camera/sessions/')&&method==='DELETE')return route.fulfill({json:{}})
  if(method!=='GET')throw new Error('Public illustration must not write persistent settings')
  const path=new URL(route.request().url()).pathname;let value:unknown={}
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': synthetic\n\n'})
  if(path.endsWith('/auth/session'))value={csrf:'synthetic-only'}
  if(path.endsWith('/settings'))value={revision:1,theme:'dark',locale:'ja',favorites:[],cellularLimitBytes:'30000000000'}
  if(path.endsWith('/system/summary'))value={modem:obs({registered:true,psAttached:true,rat:'LTE',plmn:'44010',band:'B3',bands:['B3'],rsrp:-86,rsrq:-10,snr:15}),apnCurrent:obs({current:true,apn:'sample.example',wanIPv4:'192.0.2.20'}),wifi:obs({ssid:'Home Wi-Fi',wpa_state:'COMPLETED',key_mgmt:'WPA2-PSK',freq:2437}),hotspot:obs({enabled:true,ssid:'NP1 Hotspot',clientCount:2,mode:'always-on'}),tailscale:obs({online:true,backendState:'Running',ips:['100.64.0.10'],deviceName:'np1-control',accountLogin:'owner@example.com',activePeerConnections:['Direct']}),gps:obs({fix:false}),cpu:obs({usedPercent:23}),ram:obs({usedPercent:48,usedBytes:'1288490188',totalBytes:'2684354560'}),sd:obs({freeBytes:'3221225472',totalBytes:'32000000000',usedPercent:90}),internalStorage:obs({freeBytes:'1610612736',totalBytes:'4294967296',usedPercent:62.5}),temperatures:obs([{name:'cpu',celsius:48.2},{name:'gpu-usr',celsius:44.1},{name:'mdm-core-usr',celsius:42.3},{name:'emmc_ufs-therm',celsius:39.4}]),recording:obs({writing:true,cameras:{front:true,in:true},writingObservedAt:new Date(sampleTime).toISOString(),finalizedClips:128}),uptime:obs(90061),load:obs('0.42 0.60 0.71 1/100 999'),tripStats:obs({periods:{7:period}})}
  if(path.endsWith('/traffic/usage'))value=obs({networks:{cellular:{status:'available',active:true,todayRX:'280000000',todayTX:'40000000',monthRX:'21000000000',monthTX:'1600000000',speedStatus:'available',stale:false,rxBytesPerSecond:1400000,txBytesPerSecond:75000},wifi:{status:'available',active:true,todayRX:'3000000000',todayTX:'250000000',monthRX:'12000000000',monthTX:'1500000000',speedStatus:'available',stale:false,rxBytesPerSecond:400000,txBytesPerSecond:90000}},stale:false,sampledAt:new Date(sampleTime).toISOString()})
  if(path.endsWith('/trips'))value=obs({range:{period:'7',from:'2026-11-05',to:'2026-11-11'},days,period,trips:[],totalTrips:0,detailsTruncated:false,sourceTruncated:false,sourceFiles:40,unreadableFiles:0,noFixSentences:0,badChecksums:0,indexedAt:'2026-11-11T08:00:00Z',source:'synthetic',status:'available'})
  await route.fulfill({json:value})
 })
 await page.goto(base+'/#dashboard')
 await expect(page.getByTestId('trip-distance')).toHaveText('161.6km')
 await expect(page.locator('.jc-distance-bar')).toHaveCount(6)
 await expect(page.locator('.dashboard-camera-pair img')).toHaveCount(2)
 for(let step=0;step<30;step++){
  sampleTime+=10000;await page.clock.runFor(10000)
  await Promise.all([page.waitForResponse(response=>new URL(response.url()).pathname.endsWith('/system/summary')),page.getByRole('button',{name:'状態を更新',exact:true}).evaluate(element=>(element as HTMLButtonElement).click())])
 }
 await expect(page.locator('[data-metric=cpu] .resource-line')).toHaveAttribute('d',/L/)
 // Stepping the clock can abort a camera frame that is in flight; wait until both pictures are back.
 await expect(page.locator('.dashboard-camera-pair img')).toHaveCount(2)
 await page.screenshot({path:info.outputPath('dashboard-sample.png'),fullPage:true})
})
