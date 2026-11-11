import { test,expect,type Page,type Route } from '@playwright/test'
const reviewURL=process.env.NCC_UI_REVIEW_URL
const observation=(data:any,status='available')=>({status,data,source:'synthetic-ota-browser-fixture',observedAt:'2026-11-11T08:00:00Z',refreshSeconds:10,verified:false})
const policy=(disabled=true)=>observation({disabled,managed:true,writable:true,token:'a'.repeat(64),scope:'本体OTAの新しい更新データ取得。進行中の更新は中断しません'})
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
 await page.waitForFunction(()=>!document.querySelector('.sidebar')||document.querySelector('.sidebar')!.getBoundingClientRect().right<=0)
}
test.describe('OTA settings user review (synthetic browser fixtures only)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to an isolated static UI server.')
 for(const width of [320,390])for(const theme of ['light','dark'] as const){
  test(`${width}px ${theme} keeps the policy and its scope readable`,async({page},info)=>{
   const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));await page.setViewportSize({width,height:844});await page.emulateMedia({colorScheme:theme})
   await open(page,async route=>{expect(route.request().method()).toBe('GET');await route.fulfill({json:policy()})})
   const panel=page.locator('.ota-settings');await expect(panel.getByText('新規取得を停止中',{exact:true})).toBeVisible();await expect(panel.getByRole('button',{name:'取得を有効にする（非推奨）',exact:true})).toBeEnabled()
   await expect(panel).toContainText('開始済みのダウンロードや更新処理は中断しません')
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([])
   await panel.scrollIntoViewIfNeeded();await page.screenshot({path:info.outputPath(`ota-${width}-${theme}.png`),fullPage:true})
  })
 }
 test('missing state cannot write; confirmed state prevents double submission and conflicts require refresh',async({page})=>{
  await page.setViewportSize({width:390,height:844});let available=false,puts=0,release:(()=>void)|undefined
  await open(page,async route=>{
   if(route.request().method()==='PUT') {puts++;expect(route.request().postDataJSON()).toEqual({expectedToken:'a'.repeat(64),disabled:false});await new Promise<void>(resolve=>release=resolve);await route.fulfill({status:409,json:{reason:'OTA_POLICY_CONFLICT'}});return}
   await route.fulfill({json:available?policy():observation(null,'unknown')})
  })
  const panel=page.locator('.ota-settings');await expect(panel.getByRole('button',{name:'取得を有効にする（非推奨）',exact:true})).toHaveCount(0)
  available=true;await panel.getByRole('button',{name:'状態を更新',exact:true}).click();const action=panel.getByRole('button',{name:'取得を有効にする（非推奨）',exact:true});await action.click()
  await expect(panel.getByRole('button',{name:'変更中…',exact:true})).toBeDisabled();expect(puts).toBe(1);await expect.poll(()=>Boolean(release)).toBe(true);release!()
  await expect(panel.getByRole('alert')).toContainText('変更後の設定を確認できません');await expect(action).toHaveCount(0)
  await panel.getByRole('button',{name:'状態を更新',exact:true}).click();await expect(action).toBeEnabled();expect(puts).toBe(1)
 })
})
