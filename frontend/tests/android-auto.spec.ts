import {expect,test} from '@playwright/test'

const base=process.env.NCC_UI_REVIEW_URL
test.skip(!base,'Local Vite fixtures required')

for(const width of [1440,390])test(`Android Autoの状態表示と設定保存 (${width}px)`,async({page})=>{
 await page.setViewportSize({width,height:900})
 await page.addInitScript(()=>{(window as any).EventSource=class extends EventTarget{close(){}}})
 let settings={enabled:false,maxResolution:'800x480',fps:15,bitrateKbps:2500,theme:'auto',mapZoom:16,revision:3}
 let puts:any[]=[]
 const status=()=>settings.enabled?{state:'projecting',since:'2026-11-26T01:00:00Z',listen:'192.168.226.1:5277',certificate:'self-signed',sessions:2,peer:'192.168.226.20:40312',
  headUnit:{make:'Sim',model:'Bench',headUnitMake:'NP1CC',headUnitModel:'SimHeadUnit',displayName:''},video:{width:800,height:480,fps:30,marginWidth:0,marginHeight:0},
  stream:{fps:9.4,kbps:930,frames:1200,dropped:12,renderMs:3.2,encodeMs:58,width:800,height:480,stalls:0,touches:0,maxUnacked:4}}
  :{state:'disabled',since:'2026-11-26T00:00:00Z',certificate:'self-signed',sessions:0}
 await page.route('https://tiles.openfreemap.org/**',route=>route.abort())
 await page.route('**/api/v1/**',async route=>{
  const {pathname}=new URL(route.request().url()),method=route.request().method()
  let json:unknown={}
  switch(pathname){
   case '/api/v1/settings': json={revision:1,theme:'light',locale:'ja',favorites:[]};break
   case '/api/v1/auth/session': json={csrf:'fixture'};break
   case '/api/v1/android-auto': json={settings,service:'running',status:status()};break
   case '/api/v1/android-auto/settings': {
    const body=route.request().postDataJSON()
    puts.push(body)
    if(method!=='PUT'||body.expectedRevision!==settings.revision){await route.fulfill({status:409,json:{code:'REVISION_CONFLICT',reason:'別の画面で設定が変更されました'}});return}
    settings={...body.settings,revision:settings.revision+1};json=settings;break
   }
  }
  await route.fulfill({json})
 })
 await page.goto(base!+'#system/androidauto')
 const tab=page.getByRole('navigation',{name:'システム設定'}).getByRole('button',{name:'Android Auto',exact:true})
 await expect(tab).toHaveAttribute('aria-current','page')
 await expect(page.getByRole('heading',{name:'Android Auto',exact:true})).toBeVisible()
 await expect(page.locator('.android-auto .tag').first()).toHaveText('停止中')
 await expect(page.getByRole('heading',{name:'ヘッドユニットアプリでの接続'})).toBeVisible()
 await expect(page.getByRole('heading',{name:'純正ナビの無線Android Auto'})).toBeVisible()
 const save=page.getByRole('button',{name:'保存',exact:true})
 await expect(save).toBeDisabled()
 await page.getByLabel('Android Autoの投影を有効にする').check()
 await page.getByLabel('最大解像度').selectOption('1280x720')
 await save.click()
 await expect(page.getByRole('status').filter({hasText:'保存しました'})).toBeVisible()
 expect(puts).toHaveLength(1)
 expect(puts[0]).toEqual({expectedRevision:3,settings:{enabled:true,maxResolution:'1280x720',fps:15,bitrateKbps:2500,theme:'auto',mapZoom:16,revision:3}})
 await expect(page.locator('.android-auto .tag').first()).toHaveText('投影中')
 await expect(page.getByText('Sim Bench（NP1CC SimHeadUnit）')).toBeVisible()
 await expect(page.getByText(/9\.4 fps · 930 kbps/)).toBeVisible()
 await expect(page.getByText('rev. 4')).toBeVisible()
 const scrollWidth=await page.evaluate(()=>document.documentElement.scrollWidth)
 expect(scrollWidth).toBeLessThanOrEqual(width)
 if(process.env.AA_UI_SHOT)await page.screenshot({path:`${process.env.AA_UI_SHOT}/android-auto-${width}.png`,fullPage:true})
})
