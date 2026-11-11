import { test,expect } from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const observe=(items:any[],status='available')=>({status,data:{items},observedAt:'2026-11-11T08:00:00Z',source:'browser-only-fixture',verified:false})
const item={id:'tailgating-detection',label:'Test preference',group:'Test group',description:'Synthetic browser fixture',kind:'info',value:0,valueLabel:'OFF',status:'available',writable:false,reason:'Read only: apply is not verified'}
test.describe('native settings isolated component review',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to Vite to run browser-only fixtures')
 test('read-only and missing values never offer fake toggles, including 320px',async({page},info)=>{
  let missing=false;const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
  await page.route('**/api/v1/**',async route=>{const path=new URL(route.request().url()).pathname;let data:any={};if(path.endsWith('/auth/session'))data={csrf:'test'};if(path.endsWith('/settings'))data={revision:1,theme:'light',locale:'ja',favorites:[]};if(path.endsWith('/capabilities'))data=[];if(path.endsWith('/device/native-settings'))data=observe([{...item,...(missing?{value:null,valueLabel:undefined,status:'unknown'}:{})}],missing?'unknown':'available');if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});await route.fulfill({contentType:'application/json',body:JSON.stringify(data)})})
  await page.goto(url!+'/#native')
  await expect(page.locator('.native-value')).toContainText('OFF')
  await expect(page.locator('.native-row input,.native-row select,.native-row button')).toHaveCount(0)
  await page.screenshot({path:info.outputPath('native-desktop.png'),fullPage:true})
  await page.setViewportSize({width:320,height:780});await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  missing=true;await page.locator('.native-toolbar button').click();await expect(page.locator('.native-value')).not.toContainText('OFF');await expect(page.locator('.native-row input')).toHaveCount(0)
  await page.screenshot({path:info.outputPath('native-mobile-missing.png'),fullPage:true});expect(errors).toEqual([])
 })
})
