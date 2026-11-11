import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-hotspot-preference',observedAt:new Date().toISOString(),refreshSeconds:2,verified:false})
async function fixture(page:Page,options:{mode?:'conflict'|'unconfirmed'|'deferred';missing?:boolean;theme?:string;preference?:'cellular'|'wifi';actual?:'cellular'|'wifi'}={}){
 const calls:any[]=[];let release:()=>void=()=>{};const held=new Promise<void>(resolve=>release=resolve)
 let preference=options.preference||'cellular',revision=1
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let value:unknown={}
  if(path.endsWith('/auth/session'))value={csrf:'fixture'}
  if(path.endsWith('/settings'))value={revision:1,theme:options.theme||'light',locale:'ja',favorites:[]}
  if(path.endsWith('/system/summary'))value={}
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/traffic/usage'))value=obs({networks:{wifi:{}}})
  if(path.endsWith('/network/wifi/networks'))value={status:{state:'COMPLETED',ssid:'Fixture STA',networkID:1,ipv4:'192.0.2.2'},networks:[],saved:[],transaction:null}
  if(path.endsWith('/network/hotspot')){
   if(route.request().method()==='PUT'){
    const body=route.request().postDataJSON();calls.push(body)
    if(options.mode==='deferred')await held
    if(options.mode==='conflict')return route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({reason:'別の操作で設定が変更されました。最新の状態を取得してください。'})})
    if(options.mode!=='unconfirmed'){preference=body.settings.preference;revision++}
   }
   value=obs({settings:{revision,enabled:true,ssid:'Original Hotspot',passphraseConfigured:true,...(!options.missing?{preference}:{})},enabled:true,dhcpReady:true,address:'192.0.2.1/24',channel:6,clientCount:0,clients:[],sharing:{ready:true,state:'ready',...(!options.missing?{preference}:{}),upstream:{kind:options.actual||'wifi'}}})
  }
  return route.fulfill({contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(url!+'#wifi');await expect(page.getByRole('heading',{name:'ホットスポット',exact:true})).toBeVisible();await expect(page.getByRole('textbox',{name:'ホットスポットのSSID',exact:true})).toHaveValue('Original Hotspot')
 return {calls,release}
}
test.describe('hotspot preferred uplink',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to static current UI.')
 test('preference-only update preserves AP and unsaved credential drafts',async({page})=>{
  const f=await fixture(page);const region=page.getByRole('region',{name:'ホットスポットの優先回線'})
  await expect(region).toContainText('4G優先の設定です。現在はWi-Fi経由')
  await page.getByRole('textbox',{name:'ホットスポットのSSID',exact:true}).fill('Unsaved Hotspot')
  await page.getByLabel('ホットスポットのパスワード',{exact:true}).fill('unsaved-test-secret')
  await page.getByRole('radio',{name:'Wi-Fi STAを優先',exact:true}).check();await page.getByRole('button',{name:'優先回線を保存',exact:true}).click()
  await expect(region.getByRole('status')).toContainText('優先回線を保存しました')
  expect(f.calls).toEqual([{expectedRevision:1,settings:{preference:'wifi'}}])
  await expect(page.getByRole('textbox',{name:'ホットスポットのSSID',exact:true})).toHaveValue('Unsaved Hotspot');await expect(page.getByLabel('ホットスポットのパスワード',{exact:true})).toHaveValue('unsaved-test-secret')
  await expect(region).toContainText('現在の共有 Wi-Fi経由')
 })
 test('actual fallback remains separate from preferred Wi-Fi',async({page})=>{
  await fixture(page,{preference:'wifi',actual:'cellular'});const region=page.getByRole('region',{name:'ホットスポットの優先回線'});await expect(page.getByRole('radio',{name:'Wi-Fi STAを優先',exact:true})).toBeChecked();await expect(region).toContainText('現在の共有 4G経由');await expect(region).toContainText('Wi-Fi優先の設定です。現在は4G経由')
 })
 test('in-flight preference blocks AP settings and duplicate preference write',async({page})=>{
  const f=await fixture(page,{mode:'deferred'});await page.getByRole('radio',{name:'Wi-Fi STAを優先',exact:true}).check();await page.getByRole('button',{name:'優先回線を保存',exact:true}).click();await expect(page.getByRole('button',{name:'反映を確認中…',exact:true})).toBeDisabled();await expect(page.getByRole('textbox',{name:'ホットスポットのSSID',exact:true})).toBeDisabled();expect(f.calls).toHaveLength(1);f.release();await expect(page.getByRole('region',{name:'ホットスポットの優先回線'}).getByRole('status')).toBeVisible()
 })
 for(const mode of ['unconfirmed','conflict'] as const)test(`${mode} is not presented as applied`,async({page})=>{
  await fixture(page,{mode});await page.getByRole('radio',{name:'Wi-Fi STAを優先',exact:true}).check();await page.getByRole('button',{name:'優先回線を保存',exact:true}).click();const region=page.getByRole('region',{name:'ホットスポットの優先回線'});await expect(region.getByRole('alert')).toBeVisible();await expect(region.getByRole('status')).toHaveCount(0)
 })
 test('unknown preference is not assumed to be 4G priority',async({page})=>{
  await fixture(page,{missing:true});await expect(page.getByRole('radio',{name:'4Gを優先',exact:true})).not.toBeChecked();await expect(page.getByRole('radio',{name:'Wi-Fi STAを優先',exact:true})).toBeDisabled();await expect(page.getByRole('region',{name:'ホットスポットの優先回線'})).toContainText('設定を取得できていません')
 })
 for(const [width,theme] of [[320,'light'],[390,'dark']] as const)test(`preference fits ${width}px ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:844});await fixture(page,{theme});await page.getByRole('region',{name:'ホットスポットの優先回線'}).scrollIntoViewIfNeeded();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath(`preference-${width}-${theme}.png`),fullPage:true})
 })
})
