import {test,expect,type Page} from '@playwright/test'

type Theme='light'|'dark'|'system'
const tile=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=','base64')
async function fixture(page:Page,theme:Theme,populated=false){
 const requests:string[]=[]
 page.on('request',request=>{if(request.url().includes('tile.openstreetmap.org'))requests.push(request.url())})
 await page.route('https://tile.openstreetmap.org/**',route=>route.fulfill({contentType:'image/png',body:tile}))
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url())
  if(url.pathname.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let body:unknown={}
  if(url.pathname.endsWith('/auth/session'))body={csrf:'synthetic-test'}
  else if(url.pathname.endsWith('/settings'))body={revision:1,theme,locale:'ja',favorites:[]}
  else if(url.pathname.endsWith('/capabilities'))body=[]
  else if(url.pathname.endsWith('/radio/history'))body={unit:'dBm',points:populated?[{at:'2026-11-11T08:00:00Z',gps:{at:'2026-11-11T08:00:00Z',lat:35,lon:135,accuracyM:4},value:-85,state:'measured',color:'#247976',carrier:'Fixture carrier',band:'B19',source:'synthetic-test',accuracyUnknown:false}]:[],cells:[],total:12,unplaced:populated?11:12,accuracyExcluded:0,missing:0,outOfService:0,carriers:['Fixture carrier'],bands:['B19'],truncated:false,reason:'同時刻の有効な測位がありません'}
  else if(url.pathname.endsWith('/radio/heatmap/legend'))body={unit:url.searchParams.get('metric')==='rsrp'?'dBm':'dB',note:'参考の目安です',thresholds:[{minimum:-80,label:'良好',color:'#247976'}]}
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(body)})
 })
 return requests
}
async function colors(page:Page){
 return page.evaluate(()=>{
  const style=(selector:string)=>getComputedStyle(document.querySelector(selector)!)
  const contrast=(foreground:string,background:string)=>{
   const luminance=(value:string)=>{const rgb=value.match(/[\d.]+/g)!.slice(0,3).map(Number).map(n=>{n/=255;return n<=.04045?n/12.92:((n+.055)/1.055)**2.4});return rgb[0]!*.2126+rgb[1]!*.7152+rgb[2]!*.0722}
   const a=luminance(foreground),b=luminance(background);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05)
  }
  const select=style('.radio-survey select'),canvas=style('.radio-map-canvas'),empty=document.querySelector('.radio-empty'),paper=style('.radio-survey')
  const control=style('.leaflet-control-zoom-in')
  return {select:contrast(select.color,select.backgroundColor),empty:empty?contrast(style('.radio-empty').color,style('.radio-empty').backgroundColor):null,mapBackground:canvas.backgroundColor,text:paper.color,control:contrast(control.color,control.backgroundColor),tileFilter:style('.leaflet-tile-pane').filter,overflow:document.documentElement.scrollWidth>innerWidth}
 })
}
for(const width of [1440,320])for(const theme of ['light','dark','system'] as const){
 test(`radio map uses readable ${theme} surfaces at ${width}px and follows OS changes`,async({page},testInfo)=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  await page.setViewportSize({width,height:width===320?780:1100})
  await page.emulateMedia({colorScheme:'light'})
  const requests=await fixture(page,theme)
  await page.goto('/#trips')
  await expect(page.getByText('地図に表示できる測位がありません')).toBeVisible()
  await expect.poll(()=>requests.length).toBeGreaterThan(0)
  let c=await colors(page)
  expect(c.select).toBeGreaterThan(4.5);expect(c.empty).toBeGreaterThan(4.5);expect(c.control).toBeGreaterThan(4.5);expect(c.overflow).toBe(false)
  expect(c.mapBackground).toBe(theme==='dark'?'rgb(17, 19, 24)':'rgb(244, 245, 247)')
  await page.getByLabel('電波の指標').selectOption('sinr');await expect(page.getByText('SINR / dB')).toBeVisible()
  await page.getByText('絞り込み・地図設定').click()
  const background=page.getByLabel('背景地図を表示')
  await expect(background).toBeChecked();await background.uncheck();await expect(page.locator('.radio-survey .leaflet-tile-pane img')).toHaveCount(0)
  await expect(page.getByText('表示範囲がOpenStreetMapへ送信されます')).toHaveCount(0)
  await expect(page.getByRole('button',{name:'CSV',exact:true})).toBeDisabled()
  await background.check()
  await page.emulateMedia({colorScheme:'dark'})
  await expect.poll(async()=>(await colors(page)).mapBackground).toBe(theme==='light'?'rgb(244, 245, 247)':'rgb(17, 19, 24)')
  c=await colors(page)
  expect(c.select).toBeGreaterThan(4.5);expect(c.empty).toBeGreaterThan(4.5);expect(c.control).toBeGreaterThan(4.5);expect(c.overflow).toBe(false)
  expect(c.tileFilter==='none').toBe(theme==='light')
  await page.screenshot({path:testInfo.outputPath(`radio-${theme}-${width}.png`),fullPage:true})
  await page.emulateMedia({colorScheme:'light'})
  await expect.poll(async()=>(await colors(page)).mapBackground).toBe(theme==='dark'?'rgb(17, 19, 24)':'rgb(244, 245, 247)')
  expect(errors).toEqual([])
 })
}
for(const theme of ['light','dark'] as const)test(`radio map popup and attribution use ${theme} theme`,async({page},testInfo)=>{
 await page.setViewportSize({width:320,height:780});await fixture(page,theme,true);await page.goto('/#trips')
 await page.locator('.radio-survey .leaflet-interactive').first().click()
 await expect(page.locator('.radio-survey .leaflet-popup')).toBeVisible()
 await expect(page.locator('.radio-survey .leaflet-popup')).toHaveCSS('opacity','1')
 await expect(page.locator('.radio-survey .leaflet-popup')).toContainText('Fixture carrier / B19')
 const styles=await page.evaluate(()=>{
  const popup=getComputedStyle(document.querySelector('.leaflet-popup-content-wrapper')!),text=getComputedStyle(document.querySelector('.leaflet-popup-content')!),attribution=getComputedStyle(document.querySelector('.leaflet-control-attribution')!)
  return {background:popup.backgroundColor,text:text.color,attribution:attribution.backgroundColor,overflow:document.documentElement.scrollWidth>innerWidth}
 })
 expect(styles.background).toBe(theme==='dark'?'rgb(27, 31, 38)':'rgb(255, 255, 255)')
 expect(styles.text).toBe(theme==='dark'?'rgb(230, 233, 238)':'rgb(32, 38, 48)')
 expect(styles.attribution).toBe(styles.background);expect(styles.overflow).toBe(false)
 await expect(page.getByRole('button',{name:'CSV',exact:true})).toBeEnabled()
 await page.screenshot({path:testInfo.outputPath(`radio-popup-${theme}-320.png`),fullPage:true})
})
