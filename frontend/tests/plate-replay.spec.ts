import {test,expect} from '@playwright/test'
// Runs against a real NCC server fed by `ncc --mode plate-replay` (recorded
// dash-camera clips through the real sensor and engine). No API mocks.
const url=process.env.NCC_PLATE_REPLAY_URL

test.describe('plate page with replayed dash-camera footage',()=>{
 test.skip(!url,'Set NCC_PLATE_REPLAY_URL to a server fed by plate-replay.')
 test.setTimeout(180000)
 for(const [width,theme] of [[1440,'light'],[390,'dark']] as const)test(`${width}px ${theme}: real records in list and map, highlighted hits, stored image`,async({page},info)=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
  await page.setViewportSize({width,height:width>800?1000:900});await page.emulateMedia({colorScheme:theme})
  await page.goto(url!+'/#plates')
  await expect(page.getByRole('heading',{name:'ナンバー',exact:true})).toBeVisible({timeout:30000})
  // The page defaults to the last 7 days; replayed footage keeps its recording date.
  const from=process.env.NCC_PLATE_REPLAY_FROM,to=process.env.NCC_PLATE_REPLAY_TO||from
  if(from&&to){await page.getByLabel('開始日').fill(from);await page.getByLabel('終了日').fill(to);await page.getByLabel('開始日').fill(from)}
  await expect(page.locator('.plate-item').first()).toBeVisible({timeout:60000})
  const items=await page.locator('.plate-item').count()
  const hits=await page.locator('.plate-item.hit').count()
  const markers=await page.locator('path.plate-marker, path.plate-marker-hit').count()
  info.annotations.push({type:'records',description:JSON.stringify({items,hits,markers})})
  expect(items).toBeGreaterThan(0)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath(`plate-replay-${from||'default'}-${width}-${theme}.png`),fullPage:true})
  if(hits){
   await page.locator('.plate-item.hit .plate-item-main').first().click()
   if(markers) await expect(page.locator('.leaflet-popup')).toHaveCount(1)
   const image=page.locator('.plate-item.hit',{has:page.getByRole('button',{name:'画像を見る'})}).first().getByRole('button',{name:'画像を見る'})
   if(await image.count()){await image.click();await expect(page.getByRole('dialog').locator('img')).toBeVisible();await expect.poll(()=>page.getByRole('dialog').locator('img').evaluate((i:HTMLImageElement)=>i.naturalWidth)).toBeGreaterThan(0);await page.screenshot({path:info.outputPath(`plate-replay-image-${width}-${theme}.png`)});await page.getByRole('dialog').getByRole('button',{name:'閉じる'}).click()}
  }
  await page.getByRole('tab',{name:'状態'}).click()
  await page.screenshot({path:info.outputPath(`plate-replay-status-${width}-${theme}.png`),fullPage:true})
  expect(errors).toEqual([])
 })
})
