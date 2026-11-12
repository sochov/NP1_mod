import {test,expect} from '@playwright/test'

for(const width of [1440,320])test(`front LED defaults to a temporary light at ${width}px`,async({page},info)=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 const calls:{path:string;body:any}[]=[]
 await page.setViewportSize({width,height:900})
 await page.clock.install()
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let data:any={}
  if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme:'dark',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))data=[]
  if(path.endsWith('/voicevox'))data={endpoint:'https://voice.example.invalid',defaultEndpoint:'https://voice.example.invalid',keyConfigured:false}
  if(path.endsWith('/device/led'))data={status:'available',data:{settings:{revision:7,mode:'rainbow',brightness:180,speed:240,fps:15,rgb:[255,255,255],autostart:false},serviceState:'active',lastCommand:{mode:'off',notificationIdle:true}}}
  if(route.request().method()!=='GET'){calls.push({path,body:route.request().postDataJSON()});data={accepted:true}}
  await route.fulfill({json:data})
 })
 await page.goto('/#system/led')
 const panel=page.locator('.led-panel')
 await expect(panel.getByRole('button',{name:'点灯',exact:true})).toBeEnabled()
 await expect(panel.getByRole('button',{name:'設定を適用',exact:true})).toHaveCount(0)
 await expect(panel.getByLabel('点灯し続ける',{exact:true})).not.toBeChecked()
 await expect(panel.getByRole('combobox',{name:'点灯時間',exact:true})).toHaveValue('5')
 await panel.getByRole('button',{name:'点灯',exact:true}).click()
 expect(calls).toHaveLength(1);expect(calls[0]?.path).toBe('/api/v1/device/led/test');expect(calls[0]?.body.durationMs).toBe(5000)
 expect(calls[0]?.body.settings.mode).toBe('rainbow')
 await panel.getByRole('button',{name:'消灯',exact:true}).click()
 expect(calls.at(-1)?.path).toBe('/api/v1/device/led/test/stop')
 expect(calls.some(c=>c.path==='/api/v1/device/led')).toBe(false)
 await page.clock.runFor(600)
 await expect(panel.locator('.led-state')).toContainText('消灯')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 expect(errors).toEqual([])
 await panel.screenshot({path:info.outputPath(`front-led-test-${width}.png`)})
})
