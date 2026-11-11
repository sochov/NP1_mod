import {test,expect,type Page} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=','base64')

async function open(page:Page,stream:(id:string)=>Promise<{status:number;body?:Buffer}>){
 let created=0,deleted=0
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname,method=route.request().method()
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  if(path.endsWith('/camera/sessions')&&method==='POST'){created++;return route.fulfill({status:201,json:{id:'fixture-'+created}})}
  if(path.includes('/camera/sessions/')&&path.endsWith('/stream')){const value=await stream(path.split('/').at(-2)!);return route.fulfill({status:value.status,contentType:value.status===200?'image/png':'text/plain',body:value.body||'unavailable'})}
  if(path.includes('/camera/sessions/')&&method==='DELETE'){deleted++;return route.fulfill({json:{ok:true}})}
  const value=path.endsWith('/auth/session')?{csrf:'fixture-token'}:path.endsWith('/settings')?{revision:1,theme:'dark',locale:'ja',favorites:[]}:path.endsWith('/capabilities')?[]:{}
  await route.fulfill({json:value})
 })
 await page.goto(url!+'#camera')
 await page.getByRole('combobox',{name:'映像形式'}).selectOption('jpeg')
 await page.getByRole('button',{name:'映像を表示',exact:true}).click()
 return {created:()=>created,deleted:()=>deleted}
}

test.describe('camera recovery without service actions (synthetic)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to isolated Vite')
 test('waits for a restarted producer and resumes in the same session',async({page})=>{
  let requests=0
  const state=await open(page,async()=>++requests<3?{status:503}:{status:200,body:png})
  await expect(page.getByText('カメラ出力の再開を待っています。',{exact:true})).toBeVisible()
  await expect(page.getByRole('img',{name:'ライブカメラ映像'})).toBeVisible({timeout:10000})
  await expect(page.getByText('カメラ出力の再開を待っています。',{exact:true})).toHaveCount(0)
  expect(state.created()).toBe(1);expect(state.deleted()).toBe(0)
 })
 test('stopping during producer wait cancels retries and releases the session',async({page})=>{
  let requests=0
  const state=await open(page,async()=>{requests++;return {status:503}})
  await expect(page.getByText('カメラ出力の再開を待っています。',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'表示を停止',exact:true}).click()
  const before=requests
  await page.waitForTimeout(2200)
  expect(requests).toBe(before);expect(state.deleted()).toBe(1)
 })
 for(const width of [320,390,1440])test(`${width}px dual view shares the playback switch and preserves the surviving stream`,async({page})=>{
  await page.setViewportSize({width,height:900})
  const state=await open(page,async()=>({status:200,body:png}))
  await expect(page.getByRole('img',{name:'ライブカメラ映像'})).toHaveCount(1)
  await page.getByRole('button',{name:'並べて表示',exact:true}).click()
  await expect(page.getByRole('img',{name:'ライブカメラ映像'})).toHaveCount(2)
  expect(state.created()).toBe(2)
  await expect(page.getByRole('button',{name:'並べて表示',exact:true})).toHaveAttribute('aria-pressed','true')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.getByRole('button',{name:'車内',exact:true}).click()
  await expect(page.getByRole('img',{name:'ライブカメラ映像'})).toHaveCount(1)
  await expect(page.getByRole('heading',{name:'車内カメラ',exact:true})).toBeVisible()
  expect(state.created()).toBe(2)
  await expect.poll(()=>state.deleted()).toBe(1)
  await page.getByRole('button',{name:'表示を停止',exact:true}).click()
  await expect.poll(()=>state.deleted()).toBe(2)
 })
})
