import {test,expect,type Page,type Route} from '@playwright/test'
// All plates, places and images below are synthetic fixtures, not device data.
const reviewURL=process.env.NCC_UI_REVIEW_URL
const now=Date.now()
const at=(minutes:number)=>new Date(now-minutes*60000).toISOString()
const record=(id:string,minutes:number,reading:{region:string;class:string;kana:string;number:string},opts:{lat?:number;lon?:number;rule?:string;image?:boolean;speech?:string}={})=>{
 const n=reading.number,display=[reading.region,reading.class,reading.kana,n.length===4?n.slice(0,2)+'-'+n.slice(2):'・'.repeat(4-n.length)+n].join(' ')
 return {record:{id,firstSeen:at(minutes),lastSeen:at(minutes),sightings:2,reading:{...reading,confidence:.91},display,location:opts.lat===undefined?undefined:{at:at(minutes),lat:opts.lat,lon:opts.lon},box:[800,600,240,120],matches:opts.rule?[{ruleId:'r1',ruleName:opts.rule,saveImage:true,speak:true}]:[],imageId:opts.image?id:undefined,speech:opts.speech},imageAvailable:!!opts.image}
}
const records=[
 record('pfix-0001',2,{region:'品川',class:'300',kana:'あ',number:'1234'},{lat:35.6200,lon:139.7400,rule:'家族の車',image:true,speech:'unsupported'}),
 record('pfix-0002',5,{region:'練馬',class:'500',kana:'さ',number:'8'},{lat:35.6300,lon:139.7100}),
 record('pfix-0003',9,{region:'多摩',class:'330',kana:'わ',number:'4567'},{lat:35.6400,lon:139.7250}),
 record('pfix-0004',12,{region:'横浜',class:'301',kana:'ほ',number:'77'}),
]
const settings={revision:3,enabled:true,rules:[{id:'r1',name:'家族の車',enabled:true,region:'品川',class:'',kana:'',number:'1234',saveImage:true,speak:true}],retentionDays:365,imageQuotaMB:256,mergeSeconds:30,minConfidence:.6}
const status=(state='model-missing')=>({enabled:true,revision:3,sensor:{state,reason:state==='running'?'':'認識モデルが未導入です'},collector:{lastPollAt:at(0),error:''},speech:{available:false,reason:'本体スピーカーからの発話経路は未検証のため、まだ発話しません'},usage:{recordBytes:20480,recordDays:3,imageBytes:1048576,imageCount:1,openRecords:0},imageQuotaBytes:268435456})
const jpeg=Buffer.from('/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAA0JCgsKCA0LCgsODg0PEyAVExISEyccHhcgLikxMC4pLSwzOko+MzZGNywtQFdBRkxOUlNSMj5aYVpQYEpRUk//2wBDAQ4ODhMREyYVFSZPNS01T09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT0//wAARCABaAKADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDDooopGQUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFAH//Z','base64')

type Handler=(route:Route)=>Promise<boolean|void>
async function open(page:Page,extra?:Handler){
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 await page.route('https://tile.openstreetmap.org/**',route=>route.abort())
 await page.route('**/api/v1/**',async route=>{
  if(extra&&await extra(route))return
  const url=new URL(route.request().url())
  if(url.pathname==='/api/v1/plate/status')return route.fulfill({json:status()})
  if(url.pathname==='/api/v1/plate/settings')return route.fulfill({json:settings})
  if(url.pathname==='/api/v1/plate/records'){const matched=url.searchParams.get('matched')==='true';return route.fulfill({json:{records:matched?records.filter(r=>r.record.matches.length):records,truncated:false,reason:''}})}
  if(/\/api\/v1\/plate\/records\/[^/]+\/image$/.test(url.pathname))return route.fulfill({contentType:'image/jpeg',body:jpeg})
  return route.fulfill({status:404,json:{reason:'fixture'}})
 })
 await page.route('**/__plate-review',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/plate-review.ts"></script></body></html>'}))
 await page.goto(reviewURL!+'/__plate-review')
 await expect(page.getByRole('heading',{name:'ナンバー',exact:true})).toBeVisible()
 await expect(page.locator('.plate-item')).toHaveCount(records.length)
 return errors
}

test.describe('plate recognition isolated review (synthetic only)',()=>{
 test.skip(!reviewURL,'Set NCC_UI_REVIEW_URL to an isolated Vite server.')

 for(const width of [320,390,1440])for(const theme of ['light','dark'] as const){test(`${width}px ${theme}: list, map and highlight without horizontal overflow`,async({page},info)=>{
  await page.setViewportSize({width,height:900});await page.emulateMedia({colorScheme:theme})
  const errors=await open(page)
  await expect(page.locator('.plate-item.hit')).toHaveCount(1)
  await expect(page.locator('.plate-item.hit .plate-rule-tag')).toHaveText('家族の車')
  await expect(page.locator('path.plate-marker-hit')).toHaveCount(1)
  await expect(page.locator('path.plate-marker')).toHaveCount(2)
  await expect(page.getByText('位置なし',{exact:true})).toHaveCount(1)
  await expect(page.locator('.plate-speech.unsupported')).toHaveText('発話未対応')
  await expect(page.getByText('認識モデルが未導入です',{exact:false})).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  expect(errors).toEqual([])
  await page.screenshot({path:info.outputPath(`plate-${width}-${theme}.png`),fullPage:true})
 })}

 test('list selection opens the matching map popup and marker selection highlights the list item',async({page})=>{
  await open(page)
  await page.locator('[data-record="pfix-0003"] .plate-item-main').click()
  await expect(page.locator('[data-record="pfix-0003"]')).toHaveClass(/selected/)
  await expect(page.locator('.leaflet-popup-content')).toContainText('多摩 330 わ 45-67')
  await page.locator('path.plate-marker-hit').click({force:true})
  await expect(page.locator('[data-record="pfix-0001"]')).toHaveClass(/selected/)
  await expect(page.locator('[data-record="pfix-0003"]')).not.toHaveClass(/selected/)
  await expect(page.locator('.leaflet-popup')).toHaveCount(1)
  await expect(page.locator('.leaflet-popup-content')).toContainText('条件: 家族の車')
 })

 test('matched-only filter requests matched records and the image dialog shows the stored frame',async({page})=>{
  const queries:string[]=[]
  await open(page,async route=>{const url=new URL(route.request().url());if(url.pathname==='/api/v1/plate/records')queries.push(url.search);return false})
  await page.getByLabel('条件に合ったものだけ').check()
  await expect(page.locator('.plate-item')).toHaveCount(1)
  expect(queries.at(-1)).toContain('matched=true')
  await page.getByRole('button',{name:'画像を見る'}).click()
  const dialog=page.getByRole('dialog',{name:'品川 300 あ 12-34 の画像'})
  await expect(dialog.locator('img')).toHaveAttribute('src','/api/v1/plate/records/pfix-0001/image')
  await expect(dialog.locator('.plate-box')).toBeVisible()
  await dialog.getByRole('button',{name:'閉じる'}).click()
  await expect(dialog).toHaveCount(0)
 })

 test('rules save with the current revision, warn about unsupported speech, and surface conflicts',async({page})=>{
  const puts:any[]=[];let conflict=false
  await open(page,async route=>{
   if(route.request().method()!=='PUT')return false
   puts.push(route.request().postDataJSON())
   if(conflict){await route.fulfill({status:409,json:{code:'REVISION_CONFLICT',reason:'別の画面で設定が変更されました。再読み込みしてください'}});return true}
   const body=route.request().postDataJSON();await route.fulfill({json:{...body.settings,revision:body.expectedRevision+1}});return true
  })
  await page.getByRole('tab',{name:'条件と設定'}).click()
  const save=page.getByRole('button',{name:'設定を保存'})
  await expect(save).toBeDisabled()
  await page.getByRole('button',{name:'条件を追加'}).click()
  const rule=page.locator('.plate-rule').nth(1)
  await rule.getByLabel('名前').fill('レンタカー')
  await rule.getByLabel('ひらがな').fill('わ')
  await rule.getByLabel('番号',{exact:true}).fill('12**')
  await rule.getByLabel('本体で発話する').check()
  await expect(rule.getByText('記録には「発話未対応」と残ります',{exact:false})).toBeVisible()
  await save.click()
  await expect(page.getByText('設定を保存しました')).toBeVisible()
  expect(puts).toHaveLength(1)
  expect(puts[0].expectedRevision).toBe(3)
  expect(puts[0].settings.rules[1]).toMatchObject({name:'レンタカー',kana:'わ',number:'12**',speak:true,saveImage:true,enabled:true})
  expect(puts[0].settings.rules[1].id).toMatch(/^r[0-9a-f]{10}$/)
  await expect(page.getByText('rev. 4')).toBeVisible()
  conflict=true
  await page.locator('.plate-rule').first().getByLabel('有効').uncheck()
  await save.click()
  await expect(page.getByRole('alert')).toContainText('別の画面で設定が変更されました')
  expect(puts[1].expectedRevision).toBe(4)
  await page.getByRole('button',{name:'変更を取り消す'}).click()
  await expect(page.locator('.plate-rule').first().getByLabel('有効')).toBeChecked()
 })

 test('status tab explains the sensor and speech state honestly',async({page})=>{
  await open(page)
  await page.getByRole('tab',{name:'状態'}).click()
  const panel=page.locator('.plate-status')
  await expect(panel).toContainText('認識モデル未導入')
  await expect(panel).toContainText('本体スピーカーからの発話経路は未検証')
  await expect(panel).toContainText('1 枚')
 })
})
