import {test,expect} from '@playwright/test'
test('device plate tabs remain visible with no configured conditions',async({page},info)=>{
 test.skip(process.env.NCC_PLATE_HARDWARE!=='1','Explicit read-only device UI verification')
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 await page.goto('/#plates')
 await expect(page.getByRole('tab',{name:'記録',exact:true})).toBeVisible()
 await page.getByRole('tab',{name:'条件と設定',exact:true}).click()
 await expect(page.getByRole('region',{name:'条件と設定',exact:true})).toBeVisible()
 await expect(page.getByRole('button',{name:'条件を追加',exact:true})).toBeVisible()
 await page.getByRole('tab',{name:'状態',exact:true}).click()
 await expect(page.getByRole('heading',{name:'認識と保存の状態',exact:true})).toBeVisible()
 await page.getByRole('tab',{name:'記録',exact:true}).click()
 await expect(page.getByRole('region',{name:'認識したナンバーの記録'})).toBeVisible()
 expect(errors).toEqual([])
 await page.screenshot({path:info.outputPath('plate-tabs-device.png'),fullPage:true})
})

test('device plate LED editor and map surfaces follow the selected theme without writes',async({page},info)=>{
 test.skip(process.env.NCC_PLATE_HARDWARE!=='1','Explicit read-only device UI verification')
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 const response=await page.goto('/#plates')
 expect(response?.headers()['referrer-policy']).toBe('strict-origin-when-cross-origin')
 await page.getByRole('tab',{name:'条件と設定',exact:true}).click()
 const rules=page.locator('.plate-rule')
 const count=await rules.count()
 expect(count).toBeGreaterThan(0)
 await expect(page.getByRole('combobox',{name:'LED通知',exact:true})).toHaveCount(count)
 await expect(page.locator('.plate-voice select')).toHaveValue('1')
 const settings=await (await page.request.get('/api/v1/plate/settings')).json()
 expect(settings.rules.every((r:any)=>!r.id.startsWith('led-test-'))).toBe(true)
 for(const theme of ['dark','light'] as const){
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await page.setViewportSize({width:390,height:844})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath(`device-plate-led-${theme}.png`),fullPage:true})
 }
 await page.getByRole('tab',{name:'記録',exact:true}).click()
 for(const theme of ['dark','light'] as const){
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await expect(page.locator('.plate-page .leaflet-control-zoom-in')).toHaveCSS('background-color',theme==='dark'?'rgb(27, 31, 38)':'rgb(255, 255, 255)')
 }
 expect(errors).toEqual([])
})
