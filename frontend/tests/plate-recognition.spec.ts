import {test,expect,type Page,type Route} from '@playwright/test'
import {vectorTiles,vectorState,overlayCount,clickVectorFeature} from './fixtures/vector-tiles'
// All plates, places and images below are synthetic fixtures, not device data.
const reviewURL=process.env.NCC_UI_REVIEW_URL
const now=Date.now()
const at=(minutes:number)=>new Date(now-minutes*60000).toISOString()
const record=(id:string,minutes:number,reading:{region:string;class:string;kana:string;number:string},opts:{lat?:number;lon?:number;estimated?:boolean;rule?:string;image?:boolean;speech?:string}={})=>{
 const n=reading.number,display=[reading.region,reading.class,reading.kana,n.length===4?n.slice(0,2)+'-'+n.slice(2):'・'.repeat(4-n.length)+n].join(' ')
 return {record:{id,firstSeen:at(minutes),lastSeen:at(minutes),sightings:2,reading:{...reading,confidence:.91},display,location:opts.lat===undefined?undefined:{at:at(minutes),lat:opts.lat,lon:opts.lon,...(opts.estimated?{estimated:true,accuracyM:400}:{})},box:[800,600,240,120],matches:opts.rule?[{ruleId:'r1',ruleName:opts.rule,saveImage:true,speak:true}]:[],imageId:opts.image?id:undefined,speech:opts.speech},imageAvailable:!!opts.image}
}
const records=[
 record('pfix-0001',2,{region:'品川',class:'300',kana:'あ',number:'1234'},{lat:35.6200,lon:139.7400,rule:'家族の車',image:true,speech:'unsupported'}),
 record('pfix-0002',5,{region:'練馬',class:'500',kana:'さ',number:'8'},{lat:35.6300,lon:139.7100}),
 record('pfix-0003',9,{region:'多摩',class:'330',kana:'わ',number:'4567'},{lat:35.6400,lon:139.7250,estimated:true}),
 record('pfix-0004',12,{region:'横浜',class:'301',kana:'ほ',number:'77'}),
]
const settings={revision:3,enabled:true,rules:[{id:'r1',name:'家族の車',enabled:true,region:'品川',class:'',kana:'',number:'1234',saveImage:true,speak:true,message:'やりますねぇ！',messageAudio:''}],retentionDays:365,imageQuotaMB:256,mergeSeconds:30,minConfidence:.6,minSightings:2,voice:{style:1,volume:70,phrase:'番号を検知しました。'}}
const status=(state='model-missing')=>({enabled:true,revision:3,sensor:{state,reason:state==='running'?'':'認識モデルが未導入です'},collector:{lastPollAt:at(0),error:''},speech:{available:false,reason:'VOICEVOX APIキーが未設定です'},usage:{recordBytes:20480,recordDays:3,imageBytes:1048576,imageCount:1,openRecords:0},imageQuotaBytes:268435456})
const speechFixture=(key=false)=>({available:key,reason:key?'':'VOICEVOX APIキーが未設定です',styles:key?[{id:1,speaker:'ずんだもん',name:'あまあま'},{id:3,speaker:'ずんだもん',name:'ノーマル'}]:[],status:{keyConfigured:key,job:key?{state:'ready',total:302,ready:302}:{state:'no-key',total:302,ready:0,error:'VOICEVOX APIキーが未設定です'},rules:{r1:{ready:key}},credit:'VOICEVOX:ずんだもん'}})
// A tiny synthetic 8 kHz WAV (sine tone) for the upload conversion test.
function toneWav(seconds=0.25,rate=8000){const n=Math.round(seconds*rate),b=Buffer.alloc(44+n*2);b.write('RIFF',0);b.writeUInt32LE(36+n*2,4);b.write('WAVEfmt ',8);b.writeUInt32LE(16,16);b.writeUInt16LE(1,20);b.writeUInt16LE(1,22);b.writeUInt32LE(rate,24);b.writeUInt32LE(rate*2,28);b.writeUInt16LE(2,32);b.writeUInt16LE(16,34);b.write('data',36);b.writeUInt32LE(n*2,40);for(let i=0;i<n;i++)b.writeInt16LE(Math.round(Math.sin(i/rate*2*Math.PI*440)*8000),44+i*2);return b}
const jpeg=Buffer.from('/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAA0JCgsKCA0LCgsODg0PEyAVExISEyccHhcgLikxMC4pLSwzOko+MzZGNywtQFdBRkxOUlNSMj5aYVpQYEpRUk//2wBDAQ4ODhMREyYVFSZPNS01T09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT0//wAARCABaAKADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDDooopGQUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFABRRRQAUUUUAFFFFAH//Z','base64')

type Handler=(route:Route)=>Promise<boolean|void>
async function open(page:Page,extra?:Handler,key=false){
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 await vectorTiles(page)
 await page.route('**/api/v1/**',async route=>{
  if(extra&&await extra(route))return
  const url=new URL(route.request().url())
  if(url.pathname==='/api/v1/plate/status')return route.fulfill({json:status()})
  if(url.pathname==='/api/v1/plate/settings')return route.fulfill({json:settings})
  if(url.pathname==='/api/v1/plate/speech')return route.fulfill({json:speechFixture(key)})
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

 test('date dividers use Japan time, newest first, and follow the current filter',async({page})=>{
  const dates=['2026-11-14T15:01:00Z','2026-11-14T14:59:00Z','2026-11-14T15:00:00Z','2026-11-13T14:59:00Z']
  const dated=records.map((item,i)=>({...item,record:{...item.record,firstSeen:dates[i]!,lastSeen:dates[i]!}}))
  dated[0]!.record.lastSeen='2026-11-14T15:01:02Z'
  dated[1]!.record.lastSeen='2026-11-14T15:00:02Z'
  await open(page,async route=>{
   const url=new URL(route.request().url());if(!url.pathname.endsWith('/plate/records'))return false
   await route.fulfill({json:{records:url.searchParams.get('matched')==='true'?dated.filter(item=>item.record.matches.length):dated,truncated:false,reason:''}});return true
  })
  await expect(page.locator('.plate-day-heading time')).toHaveText(['2026年11月15日(日)','2026年11月14日(土)','2026年11月13日(金)'])
  expect(await page.locator('.plate-list > *').evaluateAll(nodes=>nodes.map(node=>node.getAttribute('data-record')||node.querySelector('time')?.getAttribute('datetime')))).toEqual(['2026-11-15','pfix-0001','pfix-0003','2026-11-14','pfix-0002','2026-11-13','pfix-0004'])
  await expect(page.locator('.plate-day-heading span')).toHaveText(['2 件','1 件','1 件'])
  await expect(page.locator('[data-record=pfix-0001] .plate-meta time')).toHaveText(['11/15 00:01:00','00:01:02'])
  await expect(page.locator('[data-record=pfix-0002] .plate-meta time')).toHaveText(['11/14 23:59:00','11/15 00:00:02'])
  await expect(page.locator('[data-record=pfix-0003] .plate-meta time')).toHaveCount(1)
  await page.getByLabel('条件に合ったものだけ').check()
  await expect(page.locator('.plate-day-heading')).toHaveCount(1)
  await expect(page.locator('.plate-day-heading time')).toHaveAttribute('datetime','2026-11-15')
  await expect(page.locator('.plate-item')).toHaveCount(1)
 })

 test('selecting a plate highlights only its recorded driving interval',async({page})=>{
  const start=Date.parse(at(2)),end=start+14500
  const iso=(ms:number)=>new Date(ms).toISOString()
  const trackQueries:URLSearchParams[]=[]
  const point=(offset:number,lon:number,segment=0)=>({at:iso(start+offset),lat:35.62,lon,segment})
  await open(page,async route=>{
   const url=new URL(route.request().url())
   if(url.pathname.endsWith('/plate/records')){
    const spanning={...records[0]!,record:{...records[0]!.record,lastSeen:iso(end)}}
    await route.fulfill({json:{records:[spanning,...records.slice(1)],truncated:false,reason:''}})
    return true
   }
   if(!url.pathname.endsWith('/recordings/track'))return false
   trackQueries.push(url.searchParams)
   await route.fulfill({json:{data:{points:[point(-500,139.738),point(500,139.742),point(2500,139.746),point(12000,139.768,1),point(14000,139.772,1),point(15000,139.776,1)]}}})
   return true
  })
  const map=page.locator('.plate-map')
  await page.locator('[data-record=pfix-0001] .plate-item-main').click()
  await expect(page.getByText('選択した記録の検出区間')).toBeVisible()
  await expect(map).toHaveAttribute('data-map-ready','true')
  await expect(map).toHaveAttribute('data-features-plate-trail','2')
  await expect(map).toHaveAttribute('data-features-plate-trail-ends','2')
  expect(trackQueries.map(q=>[q.get('from'),q.get('to')])).toEqual([[iso(start-5000),iso(end+5000)]])
  const layers=await map.evaluate(async el=>{
   const {inspectVectorMap}=await import('/src/services/vector-map.ts' as string)
   const instance=inspectVectorMap(el as HTMLElement)!
   const data=(id:string)=>(instance.getStyle().sources[id] as any).data.features
   return {lines:data('plate-trail').map((f:any)=>({geometry:f.geometry.type,from:f.properties.from,to:f.properties.to})),ends:data('plate-trail-ends').map((f:any)=>f.properties.start),center:instance.getCenter().lng}
  })
  expect(layers.lines).toEqual([{geometry:'LineString',from:iso(start),to:iso(start+2500)},{geometry:'LineString',from:iso(start+12000),to:iso(end)}])
  expect(layers.ends).toEqual([true,false])
  expect(layers.center).toBeCloseTo(139.757,5)
  await expect(map.locator('.maplibregl-popup')).toHaveCount(0)
  const style=(await vectorState(page,'.plate-map'))!
  expect(style.sources.plates.features.map((f:any)=>f.properties.id)).not.toContain('pfix-0001')
  const ids=style.layers.map((layer:{id:string})=>layer.id)
  expect(ids.indexOf('plate-trail-line')).toBeGreaterThan(ids.indexOf('plate-trail-endpoints'))
  await page.locator('[data-record=pfix-0002] .plate-item-main').click()
  await expect(map).toHaveAttribute('data-features-plate-trail','0')
  await expect(page.getByText('選択した記録の検出区間')).toHaveCount(0)
  expect(trackQueries).toHaveLength(1)
 })

 test('short driving intervals replace a pin when the measured route becomes available',async({page})=>{
  const start=Date.parse(at(12)),end=start+625
  const iso=(ms:number)=>new Date(ms).toISOString()
  const point=(ms:number,lon:number)=>({at:iso(start+ms),lat:35.62,lon,segment:0})
  let trackQueries=0
  const short={...records[3]!,record:{...records[3]!.record,lastSeen:iso(end),sightings:4}}
  const instant={...records[0]!,record:{...records[0]!.record,lastSeen:at(2-1/60),sightings:1}}
  await open(page,async route=>{
   const {pathname}=new URL(route.request().url())
   if(pathname.endsWith('/plate/records')){
    await route.fulfill({json:{records:[instant,...records.slice(1,3),short],truncated:false,reason:''}})
    return true
   }
   if(!pathname.endsWith('/recordings/track'))return false
   trackQueries++
   // A subsecond detection can fall entirely between two one-second GPS fixes.
   const points=trackQueries===1?[point(500,139.74)]:[point(-250,139.73999),point(750,139.74001)]
   await route.fulfill({json:{data:{points}}})
   return true
  })
  const map=page.locator('.plate-map')
  const item=page.locator('[data-record=pfix-0004] .plate-item-main')
  await item.click()
  await expect(page.getByText('検出地点を表示しています')).toBeVisible()
  await expect(map).toHaveAttribute('data-features-plate-trail','0')
  expect((await vectorState(page,'.plate-map'))!.sources.plates.features.map((f:any)=>f.properties.id)).toContain('pfix-0004')
  await item.click()
  await expect(map).toHaveAttribute('data-features-plate-trail','1')
  await expect.poll(async()=>(await vectorState(page,'.plate-map'))?.rendered.some((f:{layer:string})=>f.layer==='plate-trail-line')).toBe(true)
  const visible=(await vectorState(page,'.plate-map'))!
  expect(visible.zoom).toBeGreaterThan(18)
  expect(visible.sources.plates.features.map((f:any)=>f.properties.id)).not.toContain('pfix-0004')
  expect(visible.sources['plate-trail'].features[0].properties).toMatchObject({from:iso(start),to:iso(end)})
  await expect(map.locator('.maplibregl-popup')).toHaveCount(0)
  await page.locator('[data-record=pfix-0001] .plate-item-main').click()
  await expect(map).toHaveAttribute('data-features-plate-trail','0')
  await expect(map.locator('.maplibregl-popup')).toContainText('12-34')
  expect(trackQueries).toBe(2)
 })

 test('first-run null rules do not break tabs, editing or reset',async({page})=>{
  let savedLED=false
  const errors=await open(page,async route=>{
   if(new URL(route.request().url()).pathname!=='/api/v1/plate/settings')return false
   if(route.request().method()==='PUT'){
    const saved=route.request().postDataJSON().settings
    savedLED=saved.detectionLED
    await route.fulfill({json:{...saved,revision:saved.revision+1}})
   }else await route.fulfill({json:{...settings,rules:null}})
   return true
  })
  await page.getByRole('tab',{name:'条件と設定',exact:true}).click()
  await expect(page.getByText('条件はまだありません。',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'条件を追加',exact:true}).click()
  await expect(page.locator('.plate-rule')).toHaveCount(1)
  await page.getByRole('button',{name:'変更を取り消す',exact:true}).click()
  await expect(page.locator('.plate-rule')).toHaveCount(0)
  const detectionLED=page.getByRole('checkbox',{name:/条件に関係なく、ナンバー検出時にLEDを光らせる/})
  await expect(detectionLED).not.toBeChecked()
  await detectionLED.check()
  await page.getByRole('button',{name:'設定を保存',exact:true}).click()
  await expect(page.getByText('設定を保存しました',{exact:true})).toBeVisible()
  expect(savedLED).toBe(true)
  await page.getByRole('tab',{name:'状態',exact:true}).click()
  await expect(page.getByRole('heading',{name:'認識と保存の状態',exact:true})).toBeVisible()
  await page.getByRole('tab',{name:'記録',exact:true}).click()
  await expect(page.locator('.plate-item')).toHaveCount(records.length)
  expect(errors).toEqual([])
 })

 test('NumberShare receives all sightings and masks the saved key until explicitly shown',async({page})=>{
  await page.setViewportSize({width:390,height:900})
  const configWrites:any[]=[],managementWrites:any[]=[],uploads:any[]=[],receiveMethods:string[]=[]
  const shared=[
   {id:1,event_id:'11111111-1111-4111-8111-111111111111',observed_at:at(3),received_at:at(2),match_status:'exact',recognition_revision:0,plate:{serial_number:'12-34',region:'品川',classification_number:'300',hiragana:'あ'},location:{latitude:35.62,longitude:139.74},mine:true},
   {id:2,event_id:'22222222-2222-4222-8222-222222222222',observed_at:at(8),received_at:at(7),match_status:'none',recognition_revision:0,plate:{serial_number:'56-78',region:'練馬'},location:{latitude:35.64,longitude:139.71},mine:false},
  ]
  const shareStatus={config:{revision:2,enabled:true,endpoint:'https://share.example/api/',keyConfigured:true,apiKey:'saved-fixture-key',managementUrl:'https://share.example/',adminKeyConfigured:true,adminApiKey:'saved-admin-key',userHandle:'fixture_user'},pending:1,sent:4,blocked:1,items:[{eventId:'33333333-3333-4333-8333-333333333333',recordId:'pfix-0004',payload:{event_id:'33333333-3333-4333-8333-333333333333',observed_at:at(12),plate:{serial_number:'77',region:'横浜'}},state:'blocked',attempts:1,queuedAt:at(12),httpStatus:422,lastError:'API 422: bad plate'}]}
  const errors=await open(page,async route=>{
   const url=new URL(route.request().url()),method=route.request().method()
   if(url.pathname==='/api/v1/plate/share/status'){await route.fulfill({json:shareStatus});return true}
   if(url.pathname==='/api/v1/plate/share/sightings'){receiveMethods.push(method);const scope=url.searchParams.get('scope');await route.fulfill({json:{items:scope==='others'?shared.filter(x=>!x.mine):shared,truncated:false,syncedAt:at(0)}});return true}
   if(url.pathname==='/api/v1/plate/share/vehicles'){await route.fulfill({json:{items:[{id:10,serial_number:'12-34',region:'品川',classification_number:'300',hiragana:'あ',category:'危険',comment:'合成テスト車両',sightings:2,last_seen:at(2),last_location:{latitude:35.62,longitude:139.74}}],total:1,categories:['未分類','危険']}});return true}
   if(url.pathname==='/api/v1/plate/share/config'&&method==='PUT'){configWrites.push(route.request().postDataJSON());await route.fulfill({json:{...shareStatus.config,revision:3}});return true}
   if(url.pathname==='/api/v1/plate/share/management'&&method==='PUT'){managementWrites.push(route.request().postDataJSON());await route.fulfill({json:{...shareStatus.config,revision:4}});return true}
   if(url.pathname==='/api/v1/plate/share/check'){await route.fulfill({json:{ok:true,identity:{user_handle:'fixture_user'}}});return true}
   if(url.pathname==='/api/v1/plate/share/upload'&&method==='POST'){uploads.push(route.request().postDataJSON());await route.fulfill({status:202,json:{total:4,queued:3,requeued:0,existing:1,skipped:0}});return true}
   return false
  })
  await page.getByRole('tab',{name:'共有',exact:true}).click()
  await expect(page.locator('.plate-share .panel')).toHaveCount(5)
  await expect(page.locator('.plate-share .panel-head h2')).toHaveText(['NumberShare API','APIの全検知履歴','送信状況','期間を指定してアップロード','登録車両を検索'])
  const configToggle=page.getByRole('button',{name:'API設定を開く'})
  await expect(configToggle).toHaveAttribute('aria-expanded','false')
  await expect(page.locator('.share-config-body')).toBeHidden()
  await configToggle.click()
  await expect(page.locator('.share-config-body')).toBeVisible()
  expect(await page.locator('.plate-share .panel').evaluateAll(panels=>panels.map(panel=>({
   panelTop:getComputedStyle(panel).paddingTop,
   headingTop:getComputedStyle(panel.querySelector('.panel-head')!).paddingTop,
   bodyTop:getComputedStyle(panel.querySelector('.share-panel-body')!).paddingTop,
  })))).toEqual(Array(5).fill({panelTop:'0px',headingTop:'16px',bodyTop:'18px'}))
  await expect(page.getByText('リアルタイム送信中')).toBeVisible()
  await expect(page.getByText('API 422: bad plate')).toBeVisible()
  await expect(page.locator('.share-item.mine')).toContainText('自分')
  await expect(page.locator('.share-item:not(.mine)')).toContainText('他の利用者')
  await expect.poll(async()=>(await vectorState(page,'.share-map'))?.sources['number-share'].features.filter((f:any)=>f.properties.mine).length??0).toBe(1)
  await expect.poll(async()=>(await vectorState(page,'.share-map'))?.sources['number-share'].features.filter((f:any)=>!f.properties.mine).length??0).toBe(1)
  await expect(page.locator('.share-vehicle-grid')).toContainText('合成テスト車両')
  await page.locator('.share-upload').getByLabel('開始日').fill('2026-11-01')
  await page.locator('.share-upload').getByLabel('終了日').fill('2026-11-16')
  await page.getByRole('button',{name:'この期間をアップロード'}).click()
  await expect(page.getByText('3件を追加、0件を再送待ちへ戻しました（登録済み 1件）')).toBeVisible()
  expect(uploads).toEqual([{from:'2026-11-01',to:'2026-11-16'}])
  await page.getByRole('combobox',{name:'表示',exact:true}).selectOption('others')
  await page.locator('.share-history').getByRole('button',{name:'APIから全件受信'}).click()
  expect(receiveMethods.length).toBeGreaterThan(0)
  expect(receiveMethods.every(method=>method==='GET')).toBe(true)
  await expect(page.locator('.share-item')).toHaveCount(1)
  await expect(page.locator('.share-item')).toContainText('他の利用者')
  await expect(page.getByLabel('認証キー',{exact:true})).toHaveAttribute('type','password')
  await expect(page.getByLabel('認証キー',{exact:true})).toHaveValue('saved-fixture-key')
  await expect(page.getByLabel('管理画面用キー',{exact:true})).toHaveAttribute('type','password')
  await expect(page.getByLabel('管理画面用キー',{exact:true})).toHaveValue('saved-admin-key')
  await expect(page.getByRole('link',{name:'キーをコピーして管理画面を開く'})).toHaveAttribute('href','https://share.example/')
  await page.getByLabel('認証キーを表示する').check()
  await expect(page.getByLabel('認証キー',{exact:true})).toHaveAttribute('type','text')
  await page.getByLabel('認証キー',{exact:true}).fill('replacement-fixture-key')
  await page.getByRole('button',{name:'設定を保存'}).click()
  await expect(page.getByText('NumberShare API設定を保存しました')).toBeVisible()
  expect(configWrites).toEqual([{expectedRevision:2,enabled:true,endpoint:'https://share.example/api/',key:'replacement-fixture-key'}])
  expect(managementWrites).toEqual([{expectedRevision:3,managementUrl:'https://share.example/',key:'saved-admin-key'}])
  await expect(page.getByLabel('認証キー',{exact:true})).toHaveValue('saved-fixture-key')
  await page.getByRole('button',{name:'接続を確認'}).click()
  await expect(page.getByText('接続しました（匿名ID: fixture_user）')).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.setViewportSize({width:1440,height:900})
  expect(await page.locator('.plate-share .panel').evaluateAll(panels=>panels.map(panel=>({
   panelTop:getComputedStyle(panel).paddingTop,
   headingTop:getComputedStyle(panel.querySelector('.panel-head')!).paddingTop,
   bodyTop:getComputedStyle(panel.querySelector('.share-panel-body')!).paddingTop,
  })))).toEqual(Array(5).fill({panelTop:'0px',headingTop:'18px',bodyTop:'18px'}))
  expect(errors).toEqual([])
 })

 test('NumberShare API settings stay expanded by default until a key is configured',async({page})=>{
  const registrations:any[]=[]
  const unconfigured={config:{revision:1,enabled:false,endpoint:'',keyConfigured:false,apiKey:'',adminKeyConfigured:false},pending:0,sent:0,blocked:0,items:[]}
  const errors=await open(page,async route=>{
   const path=new URL(route.request().url()).pathname
   if(path==='/api/v1/plate/share/status'){await route.fulfill({json:unconfigured});return true}
   if(path==='/api/v1/plate/share/register'){registrations.push(route.request().postDataJSON());await route.fulfill({status:201,json:{userHandle:'new_owner',deviceId:'device-1'}});return true}
   return false
  })
  await page.getByRole('tab',{name:'共有',exact:true}).click()
  await expect(page.getByRole('button',{name:'API設定を閉じる'})).toHaveAttribute('aria-expanded','true')
  await expect(page.locator('.share-config-body')).toBeVisible()
  await expect(page.getByRole('heading',{name:'初回登録'})).toBeVisible()
  const registerButton=page.getByRole('button',{name:'APIキーを取得して保存'})
  await expect(registerButton).toBeDisabled()
  await page.getByLabel('匿名ユーザーID').fill('new_owner')
  await page.getByLabel('接続用キー（DEVICE_API_KEY）').fill('bootstrap-fixture-key-0123456789')
  await expect(registerButton).toBeEnabled()
  await expect(page.getByLabel('接続用キー（DEVICE_API_KEY）')).toHaveAttribute('type','password')
  await page.getByLabel('接続用キーを表示する').check()
  await expect(page.getByLabel('接続用キー（DEVICE_API_KEY）')).toHaveAttribute('type','text')
  await registerButton.click()
  await expect(page.getByText('new_owner として初回登録しました。発行された機器用認証キーを保存しました')).toBeVisible()
  expect(registrations).toEqual([{expectedRevision:1,endpoint:'https://nmch.jp/app/numbershare/api/',userHandle:'new_owner',bootstrapKey:'bootstrap-fixture-key-0123456789'}])
  expect(errors).toEqual([])
 })

 for(const width of [320,390,1440])for(const theme of ['light','dark'] as const){test(`${width}px ${theme}: list, map and highlight without horizontal overflow`,async({page},info)=>{
  await page.setViewportSize({width,height:900});await page.emulateMedia({colorScheme:theme})
  const errors=await open(page)
  await expect(page.locator('.plate-item.hit')).toHaveCount(1)
  await expect(page.locator('.plate-item.hit .plate-rule-tag')).toHaveText('家族の車')
  await expect.poll(async()=>(await vectorState(page,'.plate-map'))?.sources.plates.features.filter((f:any)=>f.properties.hit).length??0).toBe(1)
  await expect.poll(async()=>(await vectorState(page,'.plate-map'))?.sources.plates.features.filter((f:any)=>!f.properties.hit).length??0).toBe(2)
  await expect(page.getByText('位置なし',{exact:true})).toHaveCount(1)
  await expect(page.locator('[data-record="pfix-0003"] .plate-estimated')).toHaveText('位置推定')
  await expect(page.locator('.plate-speech.unsupported')).toHaveText('発話できず（未設定）')
  await expect(page.getByRole('note').getByRole('link',{name:'システム › LED / 音声 で設定する'})).toHaveAttribute('href','#system/led')
  await expect(page.getByText('認識モデルが未導入です',{exact:false})).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  const shortSerial=page.locator('[data-record="pfix-0002"] .plate-serial')
  await expect(shortSerial).toHaveText('・・ ・8')
  expect(await shortSerial.evaluate(node=>node.scrollWidth<=node.clientWidth)).toBe(true)
  expect(errors).toEqual([])
  await page.screenshot({path:info.outputPath(`plate-${width}-${theme}.png`),fullPage:true})
 })}

 test('short serials use the same font and positions as four digits',async({page})=>{
  const numbers=['1234','234','34','4']
  const formats=['12-34','・2 34','・・ 34','・・ ・4']
  const aligned=records.map(({record,...rest},index)=>({...rest,record:{...record,reading:{...record.reading,number:numbers[index]!,kana:index%2?'':record.reading.kana}}}))
  await open(page,async route=>{
   if(!new URL(route.request().url()).pathname.endsWith('/plate/records'))return false
   await route.fulfill({json:{records:aligned,truncated:false,reason:''}})
   return true
  })
  for(const width of [320,1440]){
   await page.setViewportSize({width,height:900})
   const rows=page.locator('.plate-serial')
   await expect(rows).toHaveText(formats)
   const geometry=await rows.evaluateAll(nodes=>nodes.map(node=>{
    const badge=node.closest('.plate-badge')!.getBoundingClientRect()
    const positions=Array.from(node.children).map(slot=>{
     const rect=slot.getBoundingClientRect(),range=document.createRange()
     range.selectNodeContents(slot)
     const glyph=range.getBoundingClientRect()
     return {x:rect.x-badge.x,y:rect.y-badge.y,width:rect.width,glyphLeft:glyph.left-badge.left,glyphRight:glyph.right-badge.left}
    })
    return {font:getComputedStyle(node).fontSize,width:badge.width,positions}
   }))
   const full=geometry[0]!
   for(const row of geometry){
    expect(row.font).toBe(full.font)
    expect(row.positions.map(({x,y,width})=>({x,y,width}))).toEqual(full.positions.map(({x,y,width})=>({x,y,width})))
    expect(row.positions[4]!.glyphLeft).toBeCloseTo(full.positions[4]!.glyphLeft,1)
    expect(row.positions[4]!.glyphRight).toBeLessThan(row.width)
   }
  }
 })

 test('list selection opens the matching map popup and marker selection highlights the list item',async({page})=>{
  await open(page)
  await page.locator('[data-record="pfix-0003"] .plate-item-main').click()
  await expect(page.locator('[data-record="pfix-0003"]')).toHaveClass(/selected/)
  await expect(page.locator('.maplibregl-popup-content')).toContainText('多摩 330 わ 45-67')
  await expect(page.locator('.maplibregl-popup-content')).toContainText('推定した位置です（誤差 約400m）')
  await clickVectorFeature(page,'.plate-map','plates',{key:'hit',value:true})
  await expect(page.locator('[data-record="pfix-0001"]')).toHaveClass(/selected/)
  await expect(page.locator('[data-record="pfix-0003"]')).not.toHaveClass(/selected/)
  await expect(page.locator('.maplibregl-popup')).toHaveCount(1)
  await expect(page.locator('.maplibregl-popup-content')).toContainText('条件: 家族の車')
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

 test('rules save with the current revision, prompt for the API, preview the spoken text, and surface conflicts',async({page})=>{
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
  await expect(page.getByRole('link',{name:'システム › LED / 音声 で設定する'})).toBeVisible()
  await rule.getByLabel('発話メッセージ').fill('やりますねぇ！')
  await expect(rule.getByText('読み上げ：「やりますねぇ！番号を検知しました。 わ 12-11」')).toBeVisible()
  await expect(rule.getByText('保存すると音声を生成します')).toBeVisible()
  await expect(rule.getByRole('button',{name:'本体で試聴'})).toBeDisabled()
  await save.click()
  await expect(page.getByText('設定を保存しました')).toBeVisible()
  expect(puts).toHaveLength(1)
  expect(puts[0].expectedRevision).toBe(3)
  expect(puts[0].settings.rules[1]).toMatchObject({name:'レンタカー',kana:'わ',number:'12**',speak:true,saveImage:true,enabled:true,message:'やりますねぇ！',messageAudio:''})
  expect(puts[0].settings.voice).toEqual({style:1,volume:70,phrase:'番号を検知しました。'})
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

 test('an over-long date range is explained in the list and clears once valid',async({page})=>{
  const queries:string[]=[]
  await open(page,async route=>{const url=new URL(route.request().url());if(url.pathname==='/api/v1/plate/records')queries.push(url.search);return false})
  const before=queries.length
  await page.getByLabel('開始日').fill('2020-01-01')
  await expect(page.locator('.plate-list-error')).toContainText('366日以内')
  expect(queries.length).toBe(before)
  await expect(page.getByRole('alert').filter({hasText:'366日'})).toHaveCount(1)
  const d=new Date(Date.now()-3*86400000).toISOString().slice(0,10)
  await page.getByLabel('開始日').fill(d)
  await expect(page.locator('.plate-list-error')).toHaveCount(0)
  await expect(page.locator('.plate-item')).toHaveCount(4)
 })

 test('status tab explains the sensor and speech state honestly',async({page})=>{
  await open(page)
  await page.getByRole('tab',{name:'状態'}).click()
  const panel=page.locator('.plate-status')
  await expect(panel).toContainText('認識モデル未導入')
  await expect(panel).toContainText('VOICEVOX APIキーが未設定です')
  await expect(panel).toContainText('VOICEVOX:ずんだもん')
  await expect(panel).toContainText('1 枚')
 })

 test('shows a waiting camera and the last processed frame',async({page})=>{
  await open(page,async route=>{
   if(!route.request().url().endsWith('/api/v1/plate/status'))return
   const state='camera-waiting'
   await route.fulfill({json:{...status(state),sensor:{state,reason:'認識用のカメラ画像が更新されていません',lastFrameAt:at(5)}}})
   return true
  })
  await expect(page.locator('.plate-sensor-note')).toContainText('認識用のカメラ画像が更新されていません')
  await page.getByRole('tab',{name:'状態'}).click()
  await expect(page.locator('.plate-status')).toContainText('カメラ画像待ち')
  await expect(page.locator('.plate-status')).toContainText('最後に処理したフレーム')
 })

 test('an uploaded audio file is converted to 24 kHz mono WAV and used instead of the message',async({page})=>{
  const uploads:Buffer[]=[],puts:any[]=[]
  await open(page,async route=>{
   const url=new URL(route.request().url())
   if(url.pathname==='/api/v1/plate/speech/uploads'&&route.request().method()==='POST'){uploads.push(route.request().postDataBuffer()!);await route.fulfill({json:{id:'0123456789abcdef0123',durationMs:250}});return true}
   if(url.pathname.startsWith('/api/v1/plate/speech/uploads/')){await route.fulfill({contentType:'audio/wav',body:toneWav()});return true}
   if(route.request().method()==='PUT'){const body=route.request().postDataJSON();puts.push(body);await route.fulfill({json:{...body.settings,revision:body.expectedRevision+1}});return true}
   return false
  },true)
  await page.getByRole('tab',{name:'条件と設定'}).click()
  const rule=page.locator('.plate-rule').first()
  await expect(rule.getByText('音声を準備済み')).toBeVisible()
  await rule.locator('input[type=file]').setInputFiles({name:'okaeri.wav',mimeType:'audio/wav',buffer:toneWav()})
  await expect(page.getByText('音声を読み込みました（0.3秒）',{exact:false})).toBeVisible()
  expect(uploads).toHaveLength(1)
  const wav=uploads[0]
  expect(wav.toString('ascii',0,4)).toBe('RIFF');expect(wav.readUInt16LE(22)).toBe(1);expect(wav.readUInt32LE(24)).toBe(24000);expect(wav.readUInt16LE(34)).toBe(16)
  expect(Math.abs(wav.readUInt32LE(40)-6000*2)).toBeLessThanOrEqual(4)
  await expect(rule.getByLabel('発話メッセージ')).toBeDisabled()
  await expect(rule.locator('audio')).toHaveAttribute('src','/api/v1/plate/speech/uploads/0123456789abcdef0123')
  await expect(rule.getByText('読み上げ：「（録音した音声）番号を検知しました。 品川 12-34」')).toBeVisible()
  await page.getByRole('button',{name:'設定を保存'}).click()
  await expect(page.getByText('設定を保存しました')).toBeVisible()
  expect(puts[0].settings.rules[0].messageAudio).toBe('0123456789abcdef0123')
 })

 for(const width of [320,1440])for(const theme of ['light','dark'] as const){test(`${width}px ${theme}: speech settings in the rules tab fit without horizontal overflow`,async({page},info)=>{
  await page.setViewportSize({width,height:900});await page.emulateMedia({colorScheme:theme})
  const errors=await open(page)
  await page.getByRole('tab',{name:'条件と設定'}).click()
  await expect(page.locator('.plate-speech-edit')).toHaveCount(1)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  expect(errors).toEqual([])
  await page.screenshot({path:info.outputPath(`plate-rules-${width}-${theme}.png`),fullPage:true})
 })}

 test('a saved speaking rule can be previewed on the device speaker',async({page})=>{
  const previews:any[]=[]
  await open(page,async route=>{
   const url=new URL(route.request().url())
   if(url.pathname==='/api/v1/plate/speech/preview'){previews.push(route.request().postDataJSON());await route.fulfill({json:{spoken:true}});return true}
   return false
  },true)
  await expect(page.locator('.plate-api-note')).toHaveCount(0)
  await page.getByRole('tab',{name:'条件と設定'}).click()
  await expect(page.locator('.plate-voice select')).toHaveValue('1')
  await expect(page.locator('.plate-voice select option')).toHaveText(['ずんだもん（あまあま）','ずんだもん（ノーマル）'])
  const rule=page.locator('.plate-rule').first()
  await rule.getByRole('button',{name:'本体で試聴'}).click()
  await expect(page.getByText('本体のスピーカーで再生しました')).toBeVisible()
  expect(previews).toEqual([{ruleId:'r1'}])
  // An unsaved edit disables the preview until it is saved.
  await rule.getByLabel('発話メッセージ').fill('変更中')
  await expect(rule.getByRole('button',{name:'本体で試聴'})).toBeDisabled()
 })

 test('rule LED settings save effects and colors and participate in device preview',async({page})=>{
  await page.setViewportSize({width:320,height:900})
  let stored:any=structuredClone(settings);const previews:any[]=[]
  await open(page,async route=>{
   const p=new URL(route.request().url()).pathname
   if(p==='/api/v1/plate/settings'){
    if(route.request().method()==='PUT')stored={...route.request().postDataJSON().settings,revision:4}
    await route.fulfill({json:stored});return true
   }
   if(p==='/api/v1/plate/speech/preview'){previews.push(route.request().postDataJSON());await route.fulfill({json:{spoken:true,led:true}});return true}
  },true)
  await page.getByRole('tab',{name:'条件と設定'}).click()
  const rule=page.locator('.plate-rule').first()
  await rule.getByRole('combobox',{name:'LED通知',exact:true}).selectOption('solid')
  await rule.getByRole('combobox',{name:'光り方',exact:true}).selectOption('flow')
  await rule.getByLabel('色',{exact:true}).fill('#ff6600')
  await expect(rule.getByRole('button',{name:'本体で試聴'})).toBeDisabled()
  await page.getByRole('button',{name:'設定を保存',exact:true}).click()
  expect(stored.rules[0].led).toEqual({mode:'solid',effect:'flow',rgb:[255,102,0],brightness:180})
  await rule.getByRole('button',{name:'本体で試聴'}).click()
  expect(previews).toEqual([{ruleId:'r1'}])
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await rule.getByRole('combobox',{name:'LED通知',exact:true}).selectOption('rainbow')
  await expect(rule.getByRole('combobox',{name:'光り方',exact:true})).toHaveCount(0)
  await rule.getByLabel('本体で発話する',{exact:true}).uncheck()
  await page.getByRole('button',{name:'設定を保存',exact:true}).click()
  await rule.getByRole('button',{name:'LEDを試す',exact:true}).click()
  await expect(page.getByText('LED通知を開始しました',{exact:true})).toBeVisible()
  expect(stored.rules[0].led.mode).toBe('rainbow')
 })

 test('plate map follows explicit and system themes including popup and controls',async({page})=>{
  await open(page)
  await page.locator('.plate-item.hit .plate-item-main').click()
  const wrapper=page.locator('.plate-page .maplibregl-popup-content')
  for(const theme of ['dark','light'] as const){
   await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
   await expect(wrapper).toHaveCSS('background-color',theme==='dark'?'rgb(27, 31, 38)':'rgb(255, 255, 255)')
   await expect(page.locator('.plate-page .maplibregl-ctrl-zoom-in')).toHaveCSS('color',theme==='dark'?'rgb(230, 233, 238)':'rgb(32, 38, 48)')
   await expect(page.locator('.plate-map')).toHaveAttribute('data-map-theme',theme==='light'?'mist':'night')
  }
  await page.evaluate(()=>delete document.documentElement.dataset.theme)
  await page.emulateMedia({colorScheme:'dark'})
  await expect(wrapper).toHaveCSS('background-color','rgb(27, 31, 38)')
  await page.emulateMedia({colorScheme:'light'})
  await expect(wrapper).toHaveCSS('background-color','rgb(255, 255, 255)')
 })

 test('an empty plate map requests no tiles and shows a readable themed placeholder',async({page})=>{
  let tileRequests=0;page.on('request',r=>{if(r.url().includes('tiles.openfreemap.org'))tileRequests++})
  await open(page)
  await page.route('**/api/v1/plate/records?*',route=>route.fulfill({json:{records:[],truncated:false,reason:''}}))
  await page.getByRole('button',{name:'更新',exact:true}).click()
  await expect(page.locator('.plate-map-placeholder')).toBeVisible()
  await expect(page.locator('.plate-map')).toHaveAttribute('data-map-background','off')
  const count=tileRequests
  await page.getByLabel('地図の背景を表示').uncheck();await page.getByLabel('地図の背景を表示').check()
  expect(tileRequests).toBe(count)
 })

 test('system VOICEVOX settings mask the saved key until explicitly shown',async({page})=>{
  let cfg={endpoint:'https://deprecatedapis.tts.quest',defaultEndpoint:'https://deprecatedapis.tts.quest',keyConfigured:false,apiKey:''}
  const puts:any[]=[]
  await page.route('**/api/v1/**',async route=>{
   const url=new URL(route.request().url()),method=route.request().method()
   if(url.pathname==='/api/v1/voicevox'&&method==='GET')return route.fulfill({json:cfg})
   if(url.pathname==='/api/v1/voicevox'&&method==='PUT'){const b=route.request().postDataJSON();puts.push(b);cfg={...cfg,endpoint:b.endpoint.replace(/\/$/,''),keyConfigured:'key' in b?b.key!=='':cfg.keyConfigured,apiKey:'key' in b?b.key:cfg.apiKey};return route.fulfill({json:cfg})}
   if(url.pathname==='/api/v1/voicevox/check')return route.fulfill({json:{points:9991900,resetInHours:23.9}})
   return route.fulfill({status:404,json:{reason:'fixture'}})
  })
  await page.route('**/__voicevox-review',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/voicevox-review.ts"></script></body></html>'}))
  await page.setViewportSize({width:390,height:800})
  await page.goto(reviewURL!+'/__voicevox-review')
  await expect(page.getByLabel('エンドポイント')).toHaveValue('https://deprecatedapis.tts.quest')
  await expect(page.getByText('APIキー未設定')).toBeVisible()
  await expect(page.getByRole('button',{name:'接続を確認'})).toBeDisabled()
  await expect(page.getByLabel('APIキー',{exact:true})).toHaveAttribute('type','password')
  await page.getByLabel('APIキー',{exact:true}).fill('fictional-key')
  await page.getByRole('button',{name:'保存'}).click()
  await expect(page.getByText('APIキー設定済み')).toBeVisible()
  expect(puts[0]).toEqual({endpoint:'https://deprecatedapis.tts.quest',key:'fictional-key'})
  await expect(page.getByLabel('APIキー',{exact:true})).toHaveValue('fictional-key')
  await page.getByLabel('APIキーを表示する').check()
  await expect(page.getByLabel('APIキー',{exact:true})).toHaveAttribute('type','text')
  // Changing only the endpoint does not send (or clear) the key.
  await page.getByLabel('エンドポイント').fill('https://voice.example.invalid/')
  await page.getByRole('button',{name:'保存'}).click()
  await expect(page.getByText('既定のエンドポイントは',{exact:false})).toBeVisible()
  expect(puts[1]).toEqual({endpoint:'https://voice.example.invalid/'})
  await page.getByRole('button',{name:'接続を確認'}).click()
  await expect(page.getByText('残り 9,991,900 ポイント',{exact:false})).toBeVisible()
  await page.getByRole('button',{name:'キーを削除'}).click()
  await expect(page.getByText('APIキー未設定')).toBeVisible()
  expect(puts[2]).toEqual({endpoint:'https://voice.example.invalid',key:''})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 })
})
