import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-wifi-ui-fixture',observedAt:new Date().toISOString(),verified:false,refreshSeconds:2})
async function open(page:Page,options:{badAP?:boolean}={}){
 const requests:{path:string;body:any}[]=[]
 let transaction:any=null,current='Fixture LAN',apSettings={revision:1,enabled:true,ssid:'NP1 test AP',passphraseConfigured:true}
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;let value:unknown={}
  if(path.endsWith('/events'))return route.fulfill({status:200,contentType:'text/event-stream',body:': fixture\n\n'})
  if(path.endsWith('/auth/session'))value={csrf:'fixture-csrf'}
  if(path.endsWith('/settings'))value={revision:1,theme:'light',locale:'ja',favorites:[],cellularLimitBytes:'0'}
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/system/summary'))value={}
  if(path.endsWith('/traffic/usage'))value=obs({networks:{wifi:{}},stale:true})
  if(route.request().method()!=='GET')requests.push({path,body:route.request().postDataJSON()})
  if(path.endsWith('/network/wifi/networks'))value={status:{state:'COMPLETED',ssid:current,networkID:4,ipv4:'192.0.2.4'},networks:[{id:4,ssid:current,current:true,disabled:false,managed:false}],saved:[{id:'ncc-fixture-id',ssid:'Saved NCC LAN',security:'wpa2',passwordConfigured:true}],transaction}
  if(path.endsWith('/network/wifi/scan'))value={scanRequested:true,resultsAreCached:true,results:[{ssid:'Nearby LAN',bssid:'00:11:22:33:44:55',frequency:2412,signalDBm:-61,security:'wpa2'}]}
  if(path.endsWith('/network/wifi/connect-new')||path.endsWith('/network/wifi/connect-saved')){const body=route.request().postDataJSON();current=body.ssid||'Saved NCC LAN';transaction={id:'fixture-transaction',state:'pending',expiresAt:new Date(Date.now()+90000).toISOString(),targetSSID:current,canConfirm:true};value=transaction}
  if(path.endsWith('/network/wifi/confirm')){transaction={...transaction,state:'confirmed'};value=transaction}
  if(path.endsWith('/network/wifi/rollback')){current='Fixture LAN';transaction={...transaction,state:'rolled-back'};value=transaction}
  if(path.endsWith('/network/hotspot')){if(route.request().method()==='PUT'&&!options.badAP){const body=route.request().postDataJSON();apSettings={...apSettings,...body.settings,passphraseConfigured:true,revision:2}}value=obs({settings:apSettings,enabled:apSettings.enabled,dhcpReady:apSettings.enabled,address:'192.0.2.1/24',channel:6,clientCount:0,clients:[]})}
  return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(url!+'#wifi');await expect(page.getByRole('heading',{name:'Wi-Fi接続',exact:true})).toBeVisible();await expect(page.getByRole('textbox',{name:'管理APのSSID',exact:true})).toHaveValue('NP1 test AP')
 return requests
}
async function fits(page:Page){expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
test.describe('Wi-Fi/AP typed control UI (synthetic fixture)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to local static UI.')
 test('new Wi-Fi requires second-stage confirm, secrets clear, clients share this page',async({page},info)=>{
  const requests=await open(page);await page.getByRole('button',{name:'周辺を検索',exact:true}).click();await page.getByRole('button',{name:/Nearby LAN/}).click();await expect(page.getByRole('textbox',{name:'接続先SSID',exact:true})).toHaveValue('Nearby LAN');await page.getByRole('textbox',{name:'Wi-Fi接続パスワード',exact:true}).fill('fixture-secret');await page.getByRole('button',{name:'このWi-Fiへ接続',exact:true}).click();await expect(page.getByRole('heading',{name:'このWi-Fi接続を使いますか？'})).toBeVisible();await expect(page.getByRole('textbox',{name:'Wi-Fi接続パスワード',exact:true})).toHaveValue('');await expect(page.getByRole('button',{name:'管理APに反映',exact:true})).toBeDisabled();await page.getByRole('button',{name:'この接続を使う',exact:true}).click();await expect(page.getByRole('status')).toContainText('接続を確定しました');expect(requests.find(r=>r.path.endsWith('connect-new'))?.body).toEqual({ssid:'Nearby LAN',security:'wpa2',password:'fixture-secret'});expect(requests.find(r=>r.path.endsWith('confirm'))?.body).toEqual({transactionID:'fixture-transaction'});await expect(page.getByRole('link',{name:'接続端末',exact:true})).toHaveCount(0);await expect(page.locator('.wifi-ap-clients')).toContainText('接続している端末はありません');await fits(page);await page.screenshot({path:info.outputPath('wifi-ap-desktop.png'),fullPage:true})
 })
 test('NCC private saved profile uses its own ID; cancellation restores previous link',async({page})=>{
  const requests=await open(page);await page.getByRole('button',{name:/Saved NCC LAN/}).click();await expect(page.getByRole('heading',{name:'このWi-Fi接続を使いますか？'})).toBeVisible();await page.getByRole('button',{name:'元に戻す',exact:true}).click();await expect(page.getByRole('status')).toContainText('元のWi-Fi接続へ戻しました');expect(requests.find(r=>r.path.endsWith('connect-saved'))?.body).toEqual({profileID:'ncc-fixture-id'});await expect(page.locator('.wifi-current h3')).toHaveText('Fixture LAN')
 })
 test('AP update omits unchanged secret and rejects unverified old-state response',async({page})=>{
  const requests=await open(page,{badAP:true});await page.getByRole('textbox',{name:'管理APのSSID',exact:true}).fill('Changed test AP');await page.getByRole('button',{name:'管理APに反映',exact:true}).click();await expect(page.getByRole('alert')).toContainText('管理APの反映を確認できません');await expect(page.getByRole('status')).toHaveCount(0);expect(requests.find(r=>r.path.endsWith('/hotspot'))?.body).toEqual({expectedRevision:1,settings:{enabled:true,ssid:'Changed test AP'}})
 })
 for(const width of [320,390])test(`mobile ${width}px network and AP forms fit`,async({page},info)=>{await page.setViewportSize({width,height:844});await open(page);await page.getByRole('button',{name:'周辺を検索',exact:true}).click();await page.getByRole('button',{name:/Nearby LAN/}).click();await fits(page);await page.screenshot({path:info.outputPath(`wifi-ap-${width}.png`),fullPage:true})})
})
