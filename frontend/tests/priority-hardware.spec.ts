import {test,expect} from '@playwright/test'
test.skip(process.env.NCC_PRIORITY_HARDWARE!=='1','Explicit read-only device UI verification required')
test('recordings expose the measured track and the five native categories share one page',async({page},info)=>{
 test.setTimeout(90000)
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 await page.goto('/#recordings')
 await expect(page.getByRole('region',{name:'録画と走行履歴の地図'})).toBeVisible()
 await expect(page.locator('.playback-map-empty')).toContainText('この日の位置記録はありません',{timeout:30000})
 await expect(page.locator('.playback-route,.playback-location')).toHaveCount(0)
 await page.screenshot({path:info.outputPath('device-recording-map.png'),fullPage:true})
 await page.getByRole('link',{name:'純正設定',exact:true}).click()
 for(const name of ['運転サポート','ナビ・交通情報','案内・コエ','プライバシー','車両・取付']){
  await expect(page.getByRole('heading',{name,exact:true})).toBeVisible({timeout:30000})
  await expect(page.getByRole('link',{name,exact:true})).toHaveCount(0)
 }
 await page.setViewportSize({width:390,height:844})
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath('device-native-mobile.png'),fullPage:true})
 expect(errors).toEqual([])
})
