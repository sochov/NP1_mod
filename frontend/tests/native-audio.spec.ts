import {test,expect,type Page,type Route} from '@playwright/test'
const reviewURL=process.env.NCC_UI_REVIEW_URL
const state=(mute=0,correction=2,writable=true)=>({status:'available',observedAt:'2026-11-12T00:00:00Z',source:'synthetic-audio-fixture',verified:false,refreshSeconds:10,data:{masterVolume:17,mute:{value:mute,enabled:mute===0},correction:{value:correction,label:['OFF','弱','強'][correction]},writable,reason:writable?'':'保存状態を確認できません'}})
async function open(page:Page,handler:(route:Route)=>Promise<void>){
 await page.route('**/api/v1/**',handler)
 await page.route('**/__native-audio-review',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/native-audio-review.ts"></script></body></html>'}))
 await page.goto(reviewURL!+'/__native-audio-review');await expect(page.getByRole('heading',{name:'本体の音声設定',exact:true})).toBeVisible()
}

test.describe('native audio isolated user review (synthetic only)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to an isolated Vite server.')
 for(const width of [320,390])for(const theme of ['light','dark'] as const){test(`${width}px ${theme}: current values, clear controls and no master-volume write`,async({page},info)=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message));await page.setViewportSize({width,height:844});await page.emulateMedia({colorScheme:theme});let puts=0
  await open(page,async route=>{if(route.request().method()!=='GET')puts++;await route.fulfill({json:state()})})
  const panel=page.locator('.native-audio-settings');await expect(panel.getByTestId('native-master-volume')).toHaveText('17');await expect(panel.getByRole('radio',{name:'消音する',exact:true})).toBeChecked();await expect(panel.getByLabel('補正の強さ')).toHaveValue('2');await expect(panel.getByRole('button',{name:'消音設定を保存',exact:true})).toBeDisabled();await expect(panel.getByRole('button',{name:'補正設定を保存',exact:true})).toBeDisabled();await expect(panel.getByRole('slider')).toHaveCount(0);await expect(panel.getByRole('spinbutton')).toHaveCount(0);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([]);expect(puts).toBe(0);await page.screenshot({path:info.outputPath(`native-audio-${width}-${theme}.png`),fullPage:true})
 })}
 test('mute uses storage inversion and blocks all duplicate writes until confirmed',async({page})=>{
  await page.setViewportSize({width:390,height:844});let puts=0,release:(()=>void)|undefined
  await open(page,async route=>{if(route.request().method()==='PUT'){puts++;expect(new URL(route.request().url()).pathname).toBe('/api/v1/device/native-audio/mute');expect(route.request().postDataJSON()).toEqual({expectedValue:0,value:1});await new Promise<void>(resolve=>release=resolve);await route.fulfill({json:state(1)});return}await route.fulfill({json:state(0)})})
  const panel=page.locator('.native-audio-settings');await panel.getByRole('radio',{name:'音を出す',exact:true}).check();await panel.getByRole('button',{name:'消音設定を保存',exact:true}).click();await expect(panel.getByRole('button',{name:'保存中…',exact:true})).toBeDisabled();await expect(panel.getByLabel('補正の強さ')).toBeDisabled();await expect(panel.getByRole('button',{name:'状態を更新',exact:true})).toBeDisabled();await expect.poll(()=>Boolean(release)).toBe(true);expect(puts).toBe(1);release!();await expect(panel.getByRole('status')).toContainText('消音設定を保存しました');await expect(panel.getByRole('radio',{name:'音を出す',exact:true})).toBeChecked();await expect(panel.getByRole('button',{name:'消音設定を保存',exact:true})).toBeDisabled()
 })
 test('correction is saved separately and readonly storage disables both controls',async({page})=>{
  let writable=true,puts=0;await open(page,async route=>{if(route.request().method()==='PUT'){puts++;expect(route.request().url()).toContain('/volume-correction');expect(route.request().postDataJSON()).toEqual({expectedValue:2,value:1});await route.fulfill({json:state(0,1)});return}await route.fulfill({json:state(0,2,writable)})})
  const panel=page.locator('.native-audio-settings');await panel.getByLabel('補正の強さ').selectOption('1');await panel.getByRole('button',{name:'補正設定を保存',exact:true}).click();await expect(panel.getByRole('status')).toContainText('音量補正を保存しました');expect(puts).toBe(1);writable=false;await panel.getByRole('button',{name:'状態を更新',exact:true}).click();await expect(panel.getByLabel('補正の強さ')).toBeDisabled();await expect(panel.getByRole('radio').first()).toBeDisabled();await expect(panel).toContainText('保存状態を確認できません')
 })
 test('missing state and failed apply cannot produce a successful default or retry',async({page})=>{
  let available=false,puts=0;await open(page,async route=>{if(route.request().method()==='PUT'){puts++;await route.fulfill({status:409,json:{code:'NATIVE_AUDIO_CONFLICT'}});return}await route.fulfill(available?{json:state()}:{status:503,json:{code:'NATIVE_AUDIO_UNAVAILABLE'}})})
  const panel=page.locator('.native-audio-settings');await expect(panel.getByRole('radio')).toHaveCount(0);await expect(panel.getByTestId('native-master-volume')).toHaveCount(0);available=true;await panel.getByRole('button',{name:'状態を更新',exact:true}).click();await panel.getByRole('radio',{name:'音を出す',exact:true}).check();await panel.getByRole('button',{name:'消音設定を保存',exact:true}).click();await expect(panel.getByRole('alert')).toContainText('変更後の設定を確認できません');await expect(panel.getByRole('radio')).toHaveCount(0);expect(puts).toBe(1)
 })
 test('a mismatched success response remains unconfirmed',async({page})=>{
  await open(page,async route=>route.fulfill({json:state(0)}));const panel=page.locator('.native-audio-settings');await panel.getByRole('radio',{name:'音を出す',exact:true}).check();await panel.getByRole('button',{name:'消音設定を保存',exact:true}).click();await expect(panel.getByRole('alert')).toContainText('変更後の設定を確認できません');await expect(panel.getByRole('status')).toHaveCount(0)
 })
})
