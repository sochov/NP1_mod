import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const obs=(data:unknown)=>({status:'available',data,source:'synthetic-cellular-ui-fixture',observedAt:'2026-11-11T08:00:00Z',verified:false,refreshSeconds:2})
async function open(page:Page,options:{available?:boolean;configured?:boolean;current?:boolean;apply?:{verified:boolean;appliedRevision:number};job?:'success'|'failed'}={}){
 const puts:any[]=[],applies:any[]=[];let polls=0
 let settings={revision:1,profiles:[{id:'profile1',enabled:true,apn:'fixture.example',auth:1,usernameConfigured:true,passwordConfigured:true},{id:'profile2',enabled:false,apn:'',auth:0,usernameConfigured:false,passwordConfigured:false}],savedAt:null,appliedAt:null,appliedRevision:null as number|null,applyAvailable:options.available??false,...(options.configured?{configuredProfile1:{status:'available',source:'synthetic-saved-profile',current:false,observedAt:'2026-11-11T08:00:00Z',apn:'configured.example',auth:1,usernameConfigured:true,passwordConfigured:true,profile2Known:true,profile2Absent:true}}:{}),applyReason:'適用経路の確認待ち'}
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;let value:unknown={}
  if(path.endsWith('/events'))return route.fulfill({status:200,contentType:'text/event-stream',body:': fixture\n\n'})
  if(path.endsWith('/auth/session'))value={csrf:'fixture-csrf'}
  if(path.endsWith('/settings'))value={revision:1,theme:'light',locale:'ja',favorites:[],cellularLimitBytes:'20000000000'}
  if(path.endsWith('/system/summary'))value={modem:obs({registered:true,psAttached:true,rat:'LTE',plmn:'44010',band:'B3',bands:['B3'],rsrp:-89,rsrq:-10,snr:0}),apnCurrent:obs({current:options.current??true,apn:'current.example',wanIPv4:'10.1.2.3'})}
  if(path.endsWith('/traffic/usage'))value=obs({networks:{cellular:{todayRX:'1000000',todayTX:'1000000',monthRX:'3000000000',monthTX:'1000000000',speedStatus:'available',stale:false,rxBytesPerSecond:1000,txBytesPerSecond:1000}},stale:false})
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/network/cellular/apn')){if(route.request().method()==='PUT'){const body=route.request().postDataJSON();puts.push(body);settings={...settings,revision:settings.revision+1,profiles:body.profiles.map((p:any)=>({...p,usernameConfigured:p.clearUsername?false:!!p.username||p.id==='profile1',passwordConfigured:p.clearPassword?false:!!p.password||p.id==='profile1'}))}}value=settings}
  if(path.endsWith('/network/cellular/apn/apply')){applies.push(route.request().postDataJSON());value=options.job?{id:'a'.repeat(48),revision:settings.revision,status:'queued',verified:false}:options.apply??{verified:true,appliedRevision:settings.revision};if((value as any).verified)settings.appliedRevision=settings.revision}
  if(path.includes('/network/cellular/apn/jobs/')){polls++;value={id:'a'.repeat(48),revision:settings.revision,status:polls<2?'running':options.job==='failed'?'failed':'succeeded',verified:polls>=2&&options.job!=='failed',appliedRevision:polls>=2&&options.job!=='failed'?settings.revision:undefined,code:options.job==='failed'?'APN_OWNER_COMPLETION_UNVERIFIED':undefined,result:{dataLink:{status:'available',linuxEgressConfigured:false}}};if((value as any).verified)settings.appliedRevision=settings.revision}
  return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(url!+'#internet');await expect(page.getByRole('heading',{name:'APN接続設定'})).toBeVisible();await expect(page.getByRole('textbox',{name:'profile1のAPN'})).toHaveValue('fixture.example')
 return {puts,applies}
}
async function fits(page:Page){expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
test.describe('dedicated cellular/APN page (synthetic API)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to the local UI.')
 test('connection status and signals are distinct from dashboard; credentials stay secret',async({page},info)=>{
  await page.setViewportSize({width:1440,height:1050});const state=await open(page)
  await expect(page.getByRole('heading',{name:'NTT docomo'})).toBeVisible();await expect(page.locator('.cellular-session')).toContainText('Band 3');await expect(page.locator('.cellular-session')).toContainText('未取得');await expect(page.locator('.cellular-signal')).toContainText('0 dB');await expect(page.locator('.cellular-connection')).toContainText('current.example');await expect(page.locator('.cellular-session')).toContainText('10.1.2.3')
  await expect(page.getByRole('textbox',{name:'APNユーザー名',exact:true})).toHaveValue('');await expect(page.getByRole('textbox',{name:'APNパスワード',exact:true})).toHaveAttribute('type','password');await expect(page.getByRole('button',{name:'保存した設定を適用',exact:true})).toBeDisabled();expect(state.applies).toHaveLength(0);await fits(page);await page.screenshot({path:info.outputPath('cellular-desktop.png'),fullPage:true})
 })
 test('save preserves unchanged secrets and remains separate from verified apply',async({page})=>{
  const state=await open(page,{available:true});await page.getByRole('textbox',{name:'profile1のAPN'}).fill('changed.example');await expect(page.getByRole('button',{name:'保存した設定を適用',exact:true})).toBeDisabled();await page.getByRole('button',{name:'接続設定を保存',exact:true}).click();await expect(page.getByRole('status')).toContainText('回線への適用はまだ');expect(state.puts[0]).toEqual({expectedRevision:1,profiles:[{id:'profile1',enabled:true,apn:'changed.example',auth:1},{id:'profile2',enabled:false,apn:'',auth:0}]});expect(state.applies).toHaveLength(0)
  await page.getByRole('button',{name:'保存した設定を適用',exact:true}).click();await expect(page.getByRole('status')).toContainText('本体へ反映しました');expect(state.applies).toEqual([{expectedRevision:2}])
 })
 test('asynchronous apply polls one revision and distinguishes saved APN from Linux egress',async({page})=>{
  const state=await open(page,{available:true,job:'success'});await page.getByRole('button',{name:'保存した設定を適用',exact:true}).click();await expect(page.getByRole('status')).toContainText('本体へ反映しました');await expect(page.getByRole('status')).toContainText('通信経路は未接続');expect(state.applies).toEqual([{expectedRevision:1}]);expect(await page.evaluate(()=>sessionStorage.getItem('np1.apnJob'))).toBeNull()
 })
 test('failed asynchronous apply does not claim success or resend a native operation',async({page})=>{
  const state=await open(page,{available:true,job:'failed'});await page.getByRole('button',{name:'保存した設定を適用',exact:true}).click();await expect(page.getByRole('alert')).toContainText('反映を確認できませんでした');expect(state.applies).toHaveLength(1);await expect(page.getByRole('status')).toHaveCount(0);await expect(page.locator('.cellular-apn-review')).toContainText('未適用')
 })
 test('unverified result stays visible after refresh and cannot claim applied',async({page})=>{
  await open(page,{available:true,apply:{verified:false,appliedRevision:1}});await page.getByRole('button',{name:'保存した設定を適用',exact:true}).click();await expect(page.getByRole('alert')).toContainText('反映を確認できません');await expect(page.getByRole('status')).toHaveCount(0);await expect(page.locator('.cellular-apn-review')).toContainText('未適用')
 })
 test('native saved APN is labeled configured and never inferred as active PDP',async({page})=>{
  await open(page,{configured:true,current:false});await expect(page.getByTestId('cellular-apn-identity')).toContainText('設定APN');await expect(page.getByTestId('cellular-apn-identity')).toContainText('configured.example');await expect(page.getByTestId('cellular-apn-identity')).not.toContainText('現在のAPN');await expect(page.getByTestId('configured-apn-source')).toHaveText('本体の保存プロファイル');await expect(page.locator('.cellular-session')).not.toContainText('10.1.2.3')
 })
 test('verified current APN takes precedence over configured profile',async({page})=>{
  await open(page,{configured:true,current:true});await expect(page.getByTestId('cellular-apn-identity')).toContainText('現在のAPN');await expect(page.getByTestId('cellular-apn-identity')).toContainText('current.example');await expect(page.getByTestId('configured-apn-source')).toHaveCount(0)
 })
 for(const width of [320,390])test(`mobile ${width}px profile editing and supplemental usage fit`,async({page},info)=>{
  await page.setViewportSize({width,height:844});await open(page);await page.getByRole('tab',{name:/プロファイル 2/}).click();await expect(page.getByRole('textbox',{name:'profile2のAPN'})).toHaveValue('');await page.getByRole('textbox',{name:'profile2のAPN'}).fill('mobile.example');await expect(page.getByRole('heading',{name:'通信量・月間上限',exact:true})).toBeVisible();await expect(page.locator('.cellular-usage-body')).toBeVisible();await fits(page);await page.screenshot({path:info.outputPath(`cellular-${width}.png`),fullPage:true})
 })
})
