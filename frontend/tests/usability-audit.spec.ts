import {test,expect,type Page} from '@playwright/test'
import {readFileSync} from 'node:fs'
import {nativePages} from '../src/services/native'
const enabled=process.env.NCC_UI_AUDIT==='1',stamp='2026-11-11T08:00:00Z'
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-whole-ui-review',observedAt:stamp,refreshSeconds:2,verified:false})
const pages=['dashboard','internet','wifi','vpn','camera','recordings','trips',...nativePages.map(p=>p.id),'alexa','system','system/routing','system/storage','system/led','system/usb','system/logs','system/settings']
const labels:Record<string,string>={'driving-support':'運転サポート','tailgating-detection':'後方車両の接近検知','tailgating-sensitivity':'接近検知の感度','front-car-start':'先行車発進のお知らせ','signal-change':'信号の変化','traffic-check':'交通情報','beginner-driver':'初心者向け案内','traffic-congestion':'渋滞を回避','traffic-regulation':'交通規制を回避','ferry':'フェリーを利用','toll-road':'有料道路を利用','smart-ic':'スマートICを利用','route-search':'ルート探索','guidance-frequency':'案内の頻度','daily-voice':'日常の案内','support-voice':'運転支援の案内','privacy-mode':'プライバシーモード','service-cabin-camera':'車内カメラ'}
async function fixture(page:Page,theme:string){
 let writes=0;const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;if(route.request().method()!=='GET'){writes++;return route.fulfill({status:409,json:{reason:'Review never changes device settings'}})}
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': synthetic fixture\n\n'})
  let data:unknown={};if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme,locale:'ja',favorites:[],cellularLimitBytes:'30000000000'}
  if(path.endsWith('/capabilities'))data=[]
  if(path.endsWith('/system/summary'))data={modem:obs({registered:true,psAttached:true,rat:'LTE',plmn:'44010',band:'B3',bands:['B3'],rsrp:-89,rsrq:-10,snr:12}),apnCurrent:obs({current:true,apn:'fixture.example',wanIPv4:'192.0.2.20'}),wifi:obs({ssid:'Review network with a long display name',wpa_state:'COMPLETED',key_mgmt:'WPA2-PSK',freq:2412}),hotspot:obs({enabled:true,ssid:'NP1 Review AP',clientCount:1,mode:'always-on'}),tailscale:obs({online:true,backendState:'Running',ips:['100.64.0.10'],activePeerConnections:[]}),gps:obs({fix:false,satellites:0}),cpu:obs({usedPercent:21}),ram:obs({usedPercent:50,usedBytes:'1073741824',totalBytes:'2147483648'}),sd:obs({freeBytes:'12884901888',totalBytes:'128849018880',usedPercent:90}),internalStorage:obs({freeBytes:'2147483648'}),temperatures:obs([{name:'cpu',celsius:48.2}]),uptime:obs(86400),kernel:obs('synthetic-kernel'),load:obs('.2 .3 .4'),memory:obs('MemTotal: 2097152 kB\nMemAvailable: 1048576 kB'),services:obs(['Communication','DriveRec','ImageRecog','Voice','QMMF'].map(name=>({name,state:'active'}))),routes:obs('default via 192.0.2.1 dev wlan0'),usb:obs('Peripheral / ADB')}
  if(path.endsWith('/traffic/usage'))data=obs({networks:{cellular:{todayRX:'100000000',todayTX:'10000000',monthRX:'3000000000',monthTX:'1000000000',speedStatus:'available',stale:false,rxBytesPerSecond:1000000,txBytesPerSecond:50000},wifi:{todayRX:'900000000',todayTX:'50000000',monthRX:'4000000000',monthTX:'500000000',speedStatus:'available',stale:false,rxBytesPerSecond:200000,txBytesPerSecond:30000}},stale:false,sampledAt:stamp})
  if(path.endsWith('/network/cellular/apn'))data={revision:1,profiles:[{id:'profile1',enabled:true,apn:'fixture.example',auth:1,usernameConfigured:true,passwordConfigured:true},{id:'profile2',enabled:false,apn:'',auth:0,usernameConfigured:false,passwordConfigured:false}],savedAt:null,appliedAt:null,appliedRevision:null,applyAvailable:false,applyReason:'本体の適用経路を確認しています'}
  if(path.endsWith('/network/wifi/networks'))data={status:{state:'COMPLETED',ssid:'Review network with a long display name',networkID:4,ipv4:'192.0.2.4'},networks:[{id:4,ssid:'Review network with a long display name',current:true,disabled:false,managed:false}],saved:[{id:'fixture',ssid:'Saved review network',security:'wpa2',passwordConfigured:true}],transaction:null}
  if(path.endsWith('/network/hotspot'))data=obs({settings:{revision:1,enabled:true,ssid:'NP1 Review AP',passphraseConfigured:true},enabled:true,dhcpReady:true,address:'192.0.2.1/24',channel:6,clientCount:1,clients:[{mac:'00:11:22:33:44:55',hostname:'Review phone',ipv4:'192.0.2.9',signalDBm:-55}]})
  if(path.endsWith('/recordings'))data=obs({clips:[],days:[],pendingFiles:0,unreadableFiles:0,truncated:false,timezone:'Asia/Tokyo',indexedAt:stamp})
  if(path.endsWith('/trips'))data=obs({range:{period:'7',from:'2026-11-05',to:'2026-11-11'},days:[],period:{hasData:false,days:[],distanceKm:null,drivingSeconds:null,validObservedDays:0,sampleCount:0},trips:[],totalTrips:0,detailsTruncated:false,sourceTruncated:false,sourceFiles:1,unreadableFiles:0,noFixSentences:10,badChecksums:0,indexedAt:stamp,source:'ncc-retained-measured-intervals',status:'unknown'})
  if(path.endsWith('/radio/history'))data={unit:'dBm',points:[],cells:[],total:0,unplaced:0,accuracyExcluded:0,missing:0,outOfService:0,carriers:[],bands:[],truncated:false}
  if(path.endsWith('/radio/heatmap/legend'))data={unit:'dBm',note:'',thresholds:[]}
  if(path.endsWith('/device/native-settings'))data=obs({items:nativePages.filter(p=>p.id!=='installation').flatMap(p=>p.items.map(id=>({id,label:labels[id]||id,group:p.label,kind:'boolean',value:true,status:'available',writable:true})))})
  if(path.endsWith('/device/alexa'))data=obs({wakeWord:{value:0,enabled:true,fixed:false},writable:true,linkage:{status:'unknown',source:'fixture'}})
  if(path.endsWith('/device/ota-policy'))data=obs({disabled:true,managed:true,writable:true,token:'a'.repeat(64),scope:'本体OTAの新しい更新データ取得'})
  if(path.endsWith('/device/led'))data=obs({settings:{revision:1,mode:'solid',brightness:180,speed:240,fps:15,rgb:[255,255,255],autostart:false},serviceState:'active'})
  if(path.endsWith('/storage/files'))data=[{name:'review-recordings',path:'review-recordings',directory:true,size:'0',modified:stamp,media:''}]
  if(path.endsWith('/logs'))data='合成レビュー記録: 端末への操作は行っていません。'
  await route.fulfill({json:data})
 })
 return {errors,get writes(){return writes}}
}
for(const width of [1440,390])for(const theme of ['light','dark'])test(`whole UI read-only synthetic review ${width} ${theme}`,async({page},info)=>{
 test.skip(!enabled,'NCC_UI_AUDIT=1 enables the whole-screen visual audit');test.setTimeout(120000)
 await page.setViewportSize({width,height:1000});await page.emulateMedia({colorScheme:theme as 'light'|'dark'});const state=await fixture(page,theme)
 for(const route of pages){const base=process.env.NCC_UI_REVIEW_URL?.replace(/\/$/,'')||'';await page.goto(base+'/#'+route);await expect(page.locator('main')).toBeVisible();await expect(page.locator('.boot')).toHaveCount(0);await page.waitForTimeout(160);await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:info.outputPath(route.replace('/','-')+'.png'),fullPage:true});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),route).toBe(true)
  if(width===390&&nativePages.some(p=>p.id===route)){const bar=await page.locator('.mobile-toolbar').boundingBox(),intro=await page.locator('.native-toolbar p').boundingBox();expect(intro!.y,route+' title bar overlap').toBeGreaterThanOrEqual(bar!.y+bar!.height)}
 }
 expect(state.errors).toEqual([]);expect(state.writes).toBe(0)
})
test('deployed device UI read-only GET-only review',async({page},info)=>{
 test.skip(process.env.NCC_DEVICE_READONLY_REVIEW!=='1','Explicit read-only device review only');test.setTimeout(120000)
 const {url}=JSON.parse(readFileSync(new URL('../../tmp/device-access.json',import.meta.url),'utf8'));const writes:string[]=[],errors:string[]=[]
 page.on('pageerror',e=>errors.push(e.message));await page.route('**/api/v1/**',route=>{if(route.request().method()!=='GET'){writes.push(new URL(route.request().url()).pathname);return route.abort('blockedbyclient')}return route.continue()})
 for(const width of [1440,390]){await page.setViewportSize({width,height:1000});for(const route of ['dashboard','internet','vpn','system','system/led']){await page.goto(url+'/#'+route);await expect(page.locator('main')).toBeVisible();await page.waitForTimeout(300);await page.screenshot({path:info.outputPath(`private-device-${width}-${route.replace('/','-')}.png`),fullPage:true});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),route).toBe(true)}}
 expect(writes).toEqual([]);expect(errors).toEqual([])
})
test('Tailscale connection and authentication states remain distinct without prompting an online device',async({page})=>{
 test.skip(!process.env.NCC_UI_REVIEW_URL,'Local UI fixtures only')
 const state=await fixture(page,'light');let backend='Running',online=true
 await page.route('**/api/v1/system/summary',route=>route.fulfill({json:{tailscale:obs({online,backendState:backend,ips:['100.64.0.10']})}}))
 await page.goto(process.env.NCC_UI_REVIEW_URL!.replace(/\/$/,'')+'/#vpn');await expect(page.getByText('接続済み',{exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Tailscaleの認証を開始',exact:true})).toHaveCount(0)
 online=false;await page.locator('.sidebar-bottom').getByRole('button',{name:'状態を更新'}).click();await expect(page.getByText('未接続',{exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Tailscaleの認証を開始',exact:true})).toHaveCount(0)
 backend='NeedsLogin';await page.locator('.sidebar-bottom').getByRole('button',{name:'状態を更新'}).click();await expect(page.getByRole('button',{name:'Tailscaleの認証を開始',exact:true})).toBeVisible();expect(state.writes).toBe(0)
})
