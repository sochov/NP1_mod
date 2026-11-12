import {test,expect} from '@playwright/test'

test('device front LED uses timed on/off and deferred hotspot preference is hidden',async({page},info)=>{
 test.skip(process.env.NCC_LED_HARDWARE!=='1','Explicit temporary LED test on the device')
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto('/#system/led')
 const panel=page.locator('.led-panel')
 await expect(panel.getByRole('button',{name:'点灯',exact:true})).toBeEnabled()
 await expect(panel.getByRole('button',{name:'設定を適用',exact:true})).toHaveCount(0)
 await expect(panel.getByRole('combobox',{name:'点灯時間',exact:true})).toHaveValue('5')
 const saved=(await (await page.request.get('/api/v1/device/led')).json()).data.settings
 const mode=async()=>(await (await page.request.get('/api/v1/device/led')).json()).data.lastCommand.mode
 await panel.getByRole('button',{name:'点灯',exact:true}).click()
 await expect.poll(mode).toBe('screen')
 await expect.poll(mode,{timeout:8000}).toBe('off')
 await panel.getByRole('button',{name:'点灯',exact:true}).click()
 await expect.poll(mode).toBe('screen')
 await panel.getByRole('button',{name:'消灯',exact:true}).click()
 await expect.poll(mode).toBe('off')
 expect((await (await page.request.get('/api/v1/device/led')).json()).data.settings).toEqual(saved)
 await panel.screenshot({path:info.outputPath('front-led-device.png')})
 await page.goto('/#wifi')
 await expect(page.getByRole('textbox',{name:'ホットスポットのSSID',exact:true})).toBeVisible()
 await expect(page.locator('.hotspot-upstream')).toHaveCount(0)
 await expect(page.getByRole('region',{name:'ホットスポットの優先回線'})).toHaveCount(0)
 expect(errors).toEqual([])
})
