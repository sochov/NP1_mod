import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-service-fixture',observedAt:new Date().toISOString(),verified:false,refreshSeconds:5})
async function fixture(page:Page,options:{mode?:'busy'|'failed'|'unconfirmed'|'wrong-service'|'deferred';theme?:string;unavailable?:boolean}={}){
 const calls:{id:string;body:any}[]=[];let release:()=>void=()=>{};const held=new Promise<void>(resolve=>release=resolve)
 const services=[
  {id:'communication',name:'app-@Communication.service',label:'通信・ナビ',description:'NP1の通信と案内を処理します。',impact:'停止すると純正の通信・案内を利用できません。',state:'active',subState:'running',mainPID:101,invocationID:'comm-1',loadState:'loaded',unitFileState:'static',canStart:false,canStop:true,canRestart:true},
  {id:'recording',name:'app-@DriveRec.service',label:'録画',description:'ドライブレコーダーの録画を処理します。',impact:'停止中は録画されません。',state:'active',subState:'running',mainPID:102,invocationID:'rec-1',loadState:'loaded',unitFileState:'static',canStart:false,canStop:true,canRestart:true},
  {id:'voice',name:'app-@Voice.service',label:'音声',description:'音声案内を処理します。',impact:'停止中は純正音声案内を利用できません。',state:'inactive',subState:'dead',mainPID:0,invocationID:'voice-old',loadState:'loaded',unitFileState:'static',canStart:true,canStop:false,canRestart:true},
  {id:'absent',name:'fixture-not-installed.service',label:'未導入サービス',description:'',impact:'',state:'inactive',subState:'dead',mainPID:0,invocationID:'',loadState:'not-found',unitFileState:'',canStart:false,canStop:false,canRestart:false,controlReason:'この本体ではサービスが導入されていません。'},
  {id:'unknown',name:'fixture-unreadable.service',label:'状態未取得サービス',description:'',impact:'',state:'unknown',subState:'unknown',mainPID:0,invocationID:'',loadState:'unknown',unitFileState:'',canStart:true,canStop:true,canRestart:true,controlReason:'現在の動作状態を確認できません。'},
 ]
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let value:unknown={}
  if(path.endsWith('/auth/session'))value={csrf:'fixture'}
  if(path.endsWith('/settings'))value={revision:1,theme:options.theme||'light',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/system/summary'))value={services:obs(services),uptime:obs(123)}
  if(path.endsWith('/system/services'))value=options.unavailable?{...obs(null),status:'unknown',reason:'サービス管理へ接続できません'}:obs(services)
  const action=path.match(/\/system\/services\/([^/]+)\/actions$/)
  if(action){
   const id=decodeURIComponent(action[1]!),body=route.request().postDataJSON();calls.push({id,body})
   if(options.mode==='deferred')await held
   if(options.mode==='busy'||options.mode==='failed')return route.fulfill({status:options.mode==='busy'?409:503,contentType:'application/json',body:JSON.stringify({reason:options.mode==='busy'?'別のサービスを操作中です':'サービスの実動作を確認できません'})})
   const service=services.find(item=>item.id===id)!
   if(options.mode!=='unconfirmed'&&options.mode!=='wrong-service'){service.state=body.action==='stop'?'inactive':'active';service.subState=body.action==='stop'?'dead':'running';service.mainPID=body.action==='stop'?0:service.mainPID+200;service.invocationID+='-next';service.canStart=body.action==='stop';service.canStop=body.action!=='stop'}
   value={status:'applied',action:body.action,service:options.mode==='wrong-service'?services[0]:service,observedAt:new Date().toISOString()}
  }
  return route.fulfill({contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(url!+'#system');await expect(page.getByRole('heading',{name:'純正サービス',exact:true})).toBeVisible()
 return {services,calls,release}
}
function row(page:Page,label:string){return page.locator('.stock-service-item').filter({has:page.getByRole('heading',{name:label,exact:true})})}
test.describe('individual stock service controls (synthetic)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to static current UI.')
 test('stop, start and restart use fixed id and snapshot and show read-back states',async({page})=>{
  const f=await fixture(page)
  await page.getByRole('button',{name:'録画を停止',exact:true}).click();await expect(row(page,'録画').getByRole('status')).toContainText('録画を停止しました');await expect(row(page,'録画').locator('.stock-service-state')).toHaveText('停止')
  expect(f.calls[0]).toEqual({id:'recording',body:{action:'stop',expectedInvocationID:'rec-1',expectedState:'active'}})
  await page.getByRole('button',{name:'録画を起動',exact:true}).click();await expect(row(page,'録画').getByRole('status')).toContainText('録画を起動しました');await expect(row(page,'録画').locator('.stock-service-state')).toHaveText('稼働中')
  await page.getByRole('button',{name:'録画を再起動',exact:true}).click();await expect(row(page,'録画').getByRole('status')).toContainText('録画を再起動しました');expect(f.calls).toHaveLength(3)
 })
 test('pending operation blocks every action and reports progress',async({page})=>{
  const f=await fixture(page,{mode:'deferred'});await page.getByRole('button',{name:'録画を再起動',exact:true}).click();await expect(row(page,'録画').getByRole('status')).toContainText('確認しています');await expect(page.getByRole('button',{name:'音声を起動',exact:true})).toBeDisabled();await expect(page.getByRole('button',{name:'録画を再起動',exact:true})).toBeDisabled();expect(f.calls).toHaveLength(1);f.release();await expect(row(page,'録画').getByRole('status')).toContainText('録画を再起動しました')
 })
 for(const mode of ['busy','failed','unconfirmed','wrong-service'] as const)test(`${mode} never reports applied service`,async({page})=>{
  await fixture(page,{mode});await page.getByRole('button',{name:'録画を再起動',exact:true}).click();await expect(row(page,'録画').getByRole('alert')).toBeVisible();await expect(page.locator('.stock-service-success')).toHaveCount(0)
 })
 test('not-installed and unknown services stay disabled with explanation',async({page})=>{
  await fixture(page);const absent=row(page,'未導入サービス');await expect(absent).toContainText('この本体ではサービスが導入されていません');for(const button of await absent.getByRole('button').all())await expect(button).toBeDisabled();const unknown=row(page,'状態未取得サービス');await expect(unknown).toContainText('プロセス情報未取得');await expect(unknown).not.toContainText('プロセス停止');for(const button of await unknown.getByRole('button').all())await expect(button).toBeDisabled()
 })
 test('observation unavailable never enables actions',async({page})=>{
  await fixture(page,{unavailable:true});await expect(page.locator('.stock-services').getByRole('alert')).toContainText('サービス管理へ接続できません');await expect(page.locator('.stock-service-actions button')).toHaveCount(0)
 })
 for(const [width,theme] of [[320,'light'],[390,'dark']] as const)test(`service controls fit ${width}px ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:844});await fixture(page,{theme});await expect(page.getByRole('button',{name:'録画を停止',exact:true})).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath(`services-${width}-${theme}.png`),fullPage:true})
 })
})
