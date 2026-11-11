import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-tailscale-ui',observedAt:new Date().toISOString(),verified:false,refreshSeconds:2})
async function fixture(page:Page,options:{writable?:boolean;mode?:'unconfirmed'|'conflict'|'busy'|'deferred';long?:boolean;theme?:string;matchingName?:boolean}={}){
 const calls:any[]=[]
 let release:()=>void=()=>{},waiting=new Promise<void>(resolve=>release=resolve)
 const account=options.long?'a-very-long-account-name-for-small-mobile-layouts-and-identity-display@example.test':'owner@example.test'
 const data={online:true,backendState:'Running',ips:['100.64.0.2','fd7a:115c:a1e0::2'],accountLogin:account,accountName:'Fixture Owner',deviceName:options.matchingName?'np1-host':'registered-np1',hostName:'np1-host',dnsName:'registered-np1.fixture-tailnet.ts.net',nameWritable:options.writable!==false}
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let value:unknown={}
  if(path.endsWith('/auth/session'))value={csrf:'fixture-csrf'}
  if(path.endsWith('/settings'))value={revision:1,theme:options.theme||'light',locale:'ja',favorites:[],cellularLimitBytes:'0'}
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/system/summary'))value={tailscale:obs(data)}
  if(path.endsWith('/traffic/usage'))value=obs({networks:{}})
  if(path.endsWith('/vpn/tailscale/name')){
   calls.push(route.request().postDataJSON())
   if(options.mode==='deferred')await waiting
   if(options.mode==='conflict'||options.mode==='busy')return route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({code:options.mode==='busy'?'TAILSCALE_BUSY':'TAILSCALE_NAME_CONFLICT',reason:options.mode==='busy'?'Tailscaleの設定を変更中です':'別の操作で機器名が変更されました。最新の状態を取得してください'})})
   if(options.mode!=='unconfirmed')data.hostName=calls.at(-1).hostName
   value=obs(data)
  }
  return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(url!+'#vpn')
 await expect(page.getByRole('heading',{name:'Tailscale / 遠隔アクセス'})).toBeVisible()
 return {data,calls,release,account}
}
async function fits(page:Page){expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
test.describe('Tailscale identities and safe hostname edit',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to the static current UI.')
 test('normal matching name uses simple device-name wording',async({page})=>{
  const f=await fixture(page,{matchingName:true});await expect(page.getByLabel('機器名',{exact:true})).toHaveValue('np1-host');await expect(page.locator('.tailscale-name')).not.toContainText('この機器から通知する名前');await page.getByLabel('機器名',{exact:true}).fill('np1-new');await page.getByRole('button',{name:'機器名を変更',exact:true}).click();await expect(page.getByRole('status')).toContainText('機器名の変更を確認しました');expect(f.calls).toEqual([{expectedHostName:'np1-host',hostName:'np1-new'}])
 })
 test('dashboard and detail show account/login and registered name; edit uses hostname only',async({page})=>{
  const f=await fixture(page)
  await expect(page.getByLabel('ホスト名',{exact:true})).toHaveValue('np1-host')
  await expect(page.locator('.tailscale-identities')).toContainText('registered-np1')
  await expect(page.locator('.tailscale-identities')).toContainText(f.account)
  await page.getByLabel('ホスト名',{exact:true}).fill('np1-new')
  await page.getByRole('button',{name:'機器名を変更',exact:true}).click()
  await expect(page.getByRole('status')).toContainText('ホスト名の変更を確認しました')
  expect(f.calls).toEqual([{expectedHostName:'np1-host',hostName:'np1-new'}])
  await expect(page.locator('.tailscale-identities')).toContainText('registered-np1')
  await page.getByRole('link',{name:'ダッシュボード',exact:true}).click()
  const card=page.locator('.connection-card').filter({has:page.getByRole('heading',{name:'Tailscale',exact:true})})
  await expect(card).toContainText('registered-np1');await expect(card).toContainText(f.account);await expect(card).not.toContainText('Fixture Owner')
 })
 test('invalid names cannot be submitted; pending edit cannot be duplicated',async({page})=>{
  const f=await fixture(page,{mode:'deferred'}),input=page.getByLabel('ホスト名',{exact:true}),save=page.getByRole('button',{name:'機器名を変更',exact:true})
  for(const name of ['UPPER','-np1','np1-','two words','np1.example','日本語','']){await input.fill(name);await expect(save).toBeDisabled()}
  expect(f.calls).toHaveLength(0)
  await input.fill('a'.repeat(63));await expect(save).toBeEnabled();await save.click()
  await expect(page.getByRole('button',{name:'反映を確認中…',exact:true})).toBeDisabled();await expect(input).toBeDisabled();expect(f.calls).toHaveLength(1)
  f.release();await expect(page.getByRole('status')).toContainText('ホスト名の変更を確認しました')
 })
 test('read-only identity remains visible and cannot be edited',async({page})=>{
  const f=await fixture(page,{writable:false});await expect(page.getByLabel('ホスト名',{exact:true})).toBeDisabled();await expect(page.getByRole('button',{name:'機器名を変更',exact:true})).toBeDisabled();await expect(page.locator('.tailscale-identities')).toContainText(f.account);expect(f.calls).toHaveLength(0)
 })
 for(const mode of ['unconfirmed','conflict','busy'] as const)test(`${mode} never shows successful rename`,async({page})=>{
  await fixture(page,{mode});await page.getByLabel('ホスト名',{exact:true}).fill('new-name');await page.getByRole('button',{name:'機器名を変更',exact:true}).click();await expect(page.getByRole('alert')).toBeVisible();await expect(page.getByRole('status')).toHaveCount(0)
 })
 test('external refresh preserves draft and requires fresh baseline',async({page})=>{
  const f=await fixture(page);await page.getByLabel('ホスト名',{exact:true}).fill('my-draft');f.data.hostName='changed-elsewhere';await page.getByRole('button',{name:'Tailscaleの状態を更新'}).click();await expect(page.getByLabel('ホスト名',{exact:true})).toHaveValue('my-draft');await expect(page.getByRole('button',{name:'機器名を変更',exact:true})).toBeDisabled();await page.getByRole('button',{name:'最新の名前を読み込む'}).click();await page.getByLabel('ホスト名',{exact:true}).fill('my-final');await page.getByRole('button',{name:'機器名を変更',exact:true}).click();await expect(page.getByRole('status')).toBeVisible();expect(f.calls.at(-1)).toEqual({expectedHostName:'changed-elsewhere',hostName:'my-final'})
 })
 for(const [width,theme] of [[320,'light'],[390,'dark']] as const)test(`long identities fit ${width}px ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:844});await fixture(page,{long:true,theme});await fits(page);await page.screenshot({path:info.outputPath(`tailscale-${width}-${theme}.png`),fullPage:true});await page.goto(url!+'#dashboard');await fits(page)
 })
})
