import {test,expect} from '@playwright/test'

test('installed service controls and hotspot priorities reflect the actual device',async({page,request})=>{
 test.skip(process.env.NCC_STOCK_HARDWARE!=='1','Set NCC_STOCK_HARDWARE=1 for GET-only installed UI review.')
 const response=await request.get('/api/v1/system/services'),value=await response.json()
 expect(value.status).toBe('available')
 expect(value.data.length).toBeGreaterThan(10)
 await page.goto('/#system')
 await expect(page.locator('.stock-service-item')).toHaveCount(value.data.length)
 for(const id of ['opening','voice','drive-rec']){
  const service=value.data.find((item:{id:string})=>item.id===id)
  expect(service).toBeTruthy()
  await expect(page.getByRole('button',{name:service.label+'を停止',exact:true})).toBeEnabled()
  await expect(page.getByRole('button',{name:service.label+'を再起動',exact:true})).toBeEnabled()
 }
 await page.setViewportSize({width:390,height:844})
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.goto('/#wifi')
 await expect(page.getByRole('radio',{name:'4Gを優先',exact:true})).toBeVisible()
 await expect(page.getByRole('radio',{name:'Wi-Fi STAを優先',exact:true})).toBeVisible()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})
