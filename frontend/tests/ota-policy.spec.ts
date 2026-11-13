import { test,expect,type Page,type Route } from '@playwright/test'
const reviewURL=process.env.NCC_UI_REVIEW_URL
const observation=(data:any,status='available')=>({status,data,source:'synthetic-ota-browser-fixture',observedAt:'2026-11-11T08:00:00Z',refreshSeconds:10,verified:false})
// Synthetic guard rounds only; nothing here comes from a device.
const guard=(over:any={})=>({checkedAt:new Date().toISOString(),flag:{present:true,managed:true,protected:true,heals:0},network:{wanted:true,active:true,uid:12109,rejectedPackets:42,ipv6Exit:false},staging:{workFiles:0,workBytes:0,offeredItems:0,listReadable:true,sdTrigger:false,firmwareKnown:true,updateRunning:true},alerts:[],...over})
const policy=(disabled=true,extra:any={})=>observation({disabled,managed:true,writable:true,token:'a'.repeat(64),scope:'本体OTAの新しい更新データ取得。進行中の更新は中断しません',protected:disabled,heals:0,guard:guard(),...extra})
async function open(page:Page,handler:(route:Route)=>Promise<void>){
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/device/ota-policy'))return handler(route)
  if(path.endsWith('/events'))return route.fulfill({status:200,contentType:'text/event-stream',body:': fixture\n\n'})
  let data:any={}
  if(path.endsWith('/auth/session'))data={csrf:'test-fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme:'system',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))data=[]
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(data)})
 })
 await page.goto(`${reviewURL!}/#system/settings`)
 await expect(page.getByRole('heading',{name:'本体の自動更新',exact:true})).toBeVisible()
}
const layer=(page:Page,name:string)=>page.locator('.ota-layers li').filter({hasText:name})
test.describe('OTA settings user review (synthetic browser fixtures only)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to an isolated static UI server.')
 for(const width of [320,390,1440])for(const theme of ['light','dark'] as const){
  test(`${width}px ${theme} shows every protection layer and stays readable`,async({page},info)=>{
   const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));await page.setViewportSize({width,height:844});await page.emulateMedia({colorScheme:theme})
   await open(page,async route=>{expect(route.request().method()).toBe('GET');await route.fulfill({json:policy()})})
   const panel=page.locator('.ota-settings');await expect(panel.locator('.panel-head .tag')).toHaveText('更新を防止中')
   await expect(panel.locator('.ota-layers li')).toHaveCount(5);await expect(panel.locator('.ota-layers li.on')).toHaveCount(5)
   await expect(layer(page,'更新処理の通信遮断')).toContainText('遮断 42 件')
   await expect(layer(page,'更新データ・ファームウェアの監視')).toContainText('取得済みの更新データ なし')
   await expect(panel.getByRole('alert')).toHaveCount(0)
   await expect(panel).toContainText('開始済みの更新処理は中断しません')
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([])
   await panel.scrollIntoViewIfNeeded();await page.screenshot({path:info.outputPath(`ota-${width}-${theme}.png`),fullPage:true})
  })
 }
 test('warnings from the guard are spelled out and a stale guard is not shown as working',async({page})=>{
  await page.setViewportSize({width:390,height:844});let value:any=policy(true,{protected:false,guard:guard({flag:{present:true,managed:true,protected:false,heals:2,lastHeal:'2026-11-13T03:00:00Z'},staging:{workFiles:3,workBytes:1,offeredItems:1,listReadable:true,sdTrigger:true,firmwareKnown:false,updateRunning:true},alerts:['flag-unprotected','staged-package','update-offered','sd-trigger','firmware-changed']})})
  await open(page,async route=>route.fulfill({json:value}))
  const panel=page.locator('.ota-settings');await expect(panel.locator('.panel-head .tag')).toHaveText('要確認')
  const alerts=panel.locator('.ota-alerts p');await expect(alerts).toHaveCount(5)
  await expect(panel.getByRole('alert')).toContainText('SDカードに更新用のファイル');await expect(panel.getByRole('alert')).toContainText('ファームウェアが更新された可能性')
  await expect(layer(page,'停止設定の削除防止')).toHaveClass(/off/);await expect(layer(page,'停止設定の自動復旧')).toContainText('2 回作り直しました')
  value=policy(true,{guard:null});await panel.getByRole('button',{name:'状態を更新',exact:true}).click()
  await expect(layer(page,'更新処理の通信遮断')).toContainText('不明');await expect(panel).toContainText('見張り（np1-control-ota-guard）の結果が2分以上更新されていません')
 })
 test('allowing updates needs a confirmation; missing state cannot write; conflicts require refresh',async({page})=>{
  await page.setViewportSize({width:390,height:844});let available=false,puts=0,release:(()=>void)|undefined
  await open(page,async route=>{
   if(route.request().method()==='PUT') {puts++;expect(route.request().postDataJSON()).toEqual({expectedToken:'a'.repeat(64),disabled:false});await new Promise<void>(resolve=>release=resolve);await route.fulfill({status:409,json:{reason:'OTA_POLICY_CONFLICT'}});return}
   await route.fulfill({json:available?policy():observation(null,'unknown')})
  })
  const panel=page.locator('.ota-settings');const allow=panel.getByRole('button',{name:'更新を許可する（非推奨）',exact:true});await expect(allow).toHaveCount(0)
  available=true;await panel.getByRole('button',{name:'状態を更新',exact:true}).click()
  // Cancel, Escape and the backdrop all leave the policy untouched.
  await allow.click();const dialog=page.getByRole('dialog',{name:'本体の更新を許可'});await expect(dialog).toContainText('NP1modが動かなくなる可能性');await dialog.getByRole('button',{name:'やめる',exact:true}).click();await expect(dialog).toHaveCount(0)
  await allow.click();await page.keyboard.press('Escape');await expect(dialog).toHaveCount(0)
  await allow.click();await page.locator('.modal-backdrop').click({position:{x:5,y:5}});await expect(dialog).toHaveCount(0);expect(puts).toBe(0)
  await allow.click();await dialog.getByRole('button',{name:'許可する',exact:true}).click()
  await expect(panel.getByRole('button',{name:'変更中…',exact:true})).toBeDisabled();expect(puts).toBe(1);await expect.poll(()=>Boolean(release)).toBe(true);release!()
  await expect(panel.getByRole('alert')).toContainText('変更後の設定を確認できません');await expect(allow).toHaveCount(0)
  await panel.getByRole('button',{name:'状態を更新',exact:true}).click();await expect(allow).toBeEnabled();expect(puts).toBe(1)
 })
 test('stopping again needs no confirmation',async({page})=>{
  await page.setViewportSize({width:1440,height:900});let puts=0
  await open(page,async route=>{if(route.request().method()==='PUT'){puts++;expect(route.request().postDataJSON()).toEqual({expectedToken:'a'.repeat(64),disabled:true});return route.fulfill({json:policy(true)})}await route.fulfill({json:policy(false,{protected:false,guard:guard({network:{wanted:false,active:false,uid:12109,rejectedPackets:0,ipv6Exit:false}})})})})
  const panel=page.locator('.ota-settings');await expect(panel.locator('.panel-head .tag')).toHaveText('更新を許可')
  await expect(layer(page,'更新処理の通信遮断')).toHaveClass(/off/)
  await panel.getByRole('button',{name:'更新を防止する',exact:true}).click();await expect(panel.locator('.panel-head .tag')).toHaveText('更新を防止中');expect(puts).toBe(1)
  await expect(panel.getByRole('status')).toContainText('通信遮断は30秒以内に戻ります')
 })
})
