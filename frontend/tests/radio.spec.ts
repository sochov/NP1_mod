import {test,expect} from '@playwright/test'

test('radio map explains missing positions and fits mobile without contacting tile services',async({page})=>{
 const external:string[]=[];page.on('request',request=>{if(request.url().includes('tile.openstreetmap.org'))external.push(request.url())})
 await page.route('**/api/v1/**',async route=>{const url=new URL(route.request().url());let body:unknown={};if(url.pathname.endsWith('/auth/session'))body={csrf:'synthetic-test'};else if(url.pathname.endsWith('/settings'))body={revision:1,theme:'light',locale:'ja',favorites:[]};else if(url.pathname.endsWith('/capabilities'))body=[];else if(url.pathname.endsWith('/radio/history'))body={unit:'dBm',points:[],cells:[],total:12,unplaced:12,accuracyExcluded:0,missing:0,outOfService:0,carriers:[],bands:[],truncated:false,reason:'同時刻の有効な測位がありません'};else if(url.pathname.endsWith('/radio/heatmap/legend'))body={unit:url.searchParams.get('metric')==='rsrp'?'dBm':'dB',note:'参考の目安です',thresholds:[{minimum:-80,label:'良好',color:'#247976'}]};await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(body)})})
 await page.setViewportSize({width:390,height:844});await page.goto('/#trips')
 await expect(page.getByText('地図に表示できる測位がありません')).toBeVisible()
 await expect(page.getByText('測位不足 12')).toBeVisible()
 await page.getByLabel('電波の指標').selectOption('sinr');await expect(page.getByText('SINR / dB')).toBeVisible()
 await page.getByText('絞り込み・地図設定').click();await expect(page.getByText('表示範囲がOpenStreetMapへ送信されます')).toBeVisible()
 await expect(page.getByRole('button',{name:'CSV',exact:true})).toBeDisabled()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 expect(external).toEqual([])
 await page.screenshot({path:'../tmp/ui-radio-mobile-reviewed.png',fullPage:true})
})
