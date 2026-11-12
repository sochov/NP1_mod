import {test,expect,type Page,type Route} from '@playwright/test'
// All plates, places and images below are synthetic fixtures, not device data.
const reviewURL=process.env.NCC_UI_REVIEW_URL
const now=Date.now()
const at=(minutes:number)=>new Date(now-minutes*60000).toISOString()
const record=(id:string,minutes:number,reading:{region:string;class:string;kana:string;number:string},opts:{lat?:number;lon?:number;rule?:string;image?:boolean;speech?:string}={})=>{
 const n=reading.number,display=[reading.region,reading.class,reading.kana,n.length===4?n.slice(0,2)+'-'+n.slice(2):'・'.repeat(4-n.length)+n].join(' ')
 return {record:{id,firstSeen:at(minutes),lastSeen:at(minutes),sightings:2,reading:{...reading,confidence:.91},display,location:opts.lat===undefined?undefined:{at:at(minutes),lat:opts.lat,lon:opts.lon},box:[800,600,240,120],matches:opts.rule?[{ruleId:'r1',ruleName:opts.rule,saveImage:true,speak:true}]:[],imageId:opts.image?id:undefined,speech:opts.speech},imageAvailable:!!opts.image}
}
const records=[
 record('pfix-0001',2,{region:'品川',class:'300',kana:'あ',number:'1234'},{lat:35.6200,lon:139.7400,rule:'家族の車',image:true,speech:'unsupported'}),
 record('pfix-0002',5,{region:'練馬',class:'500',kana:'さ',number:'8'},{lat:35.6300,lon:139.7100}),
 record('pfix-0003',9,{region:'多摩',class:'330',kana:'わ',number:'4567'},{lat:35.6400,lon:139.7250}),
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
 await page.route('https://tile.openstreetmap.org/**',route=>route.abort())
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

 test('first-run null rules do not break tabs, editing or reset',async({page})=>{
  const errors=await open(page,async route=>{if(new URL(route.request().url()).pathname==='/api/v1/plate/settings') {await route.fulfill({json:{...settings,rules:null}});return true}})
  await page.getByRole('tab',{name:'条件と設定',exact:true}).click()
  await expect(page.getByText('条件はまだありません。',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'条件を追加',exact:true}).click()
  await expect(page.locator('.plate-rule')).toHaveCount(1)
  await page.getByRole('button',{name:'変更を取り消す',exact:true}).click()
  await expect(page.locator('.plate-rule')).toHaveCount(0)
  await page.getByRole('tab',{name:'状態',exact:true}).click()
  await expect(page.getByRole('heading',{name:'認識と保存の状態',exact:true})).toBeVisible()
  await page.getByRole('tab',{name:'記録',exact:true}).click()
  await expect(page.locator('.plate-item')).toHaveCount(records.length)
  expect(errors).toEqual([])
 })

 for(const width of [320,390,1440])for(const theme of ['light','dark'] as const){test(`${width}px ${theme}: list, map and highlight without horizontal overflow`,async({page},info)=>{
  await page.setViewportSize({width,height:900});await page.emulateMedia({colorScheme:theme})
  const errors=await open(page)
  await expect(page.locator('.plate-item.hit')).toHaveCount(1)
  await expect(page.locator('.plate-item.hit .plate-rule-tag')).toHaveText('家族の車')
  await expect(page.locator('path.plate-marker-hit')).toHaveCount(1)
  await expect(page.locator('path.plate-marker')).toHaveCount(2)
  await expect(page.getByText('位置なし',{exact:true})).toHaveCount(1)
  await expect(page.locator('.plate-speech.unsupported')).toHaveText('発話できず（未設定）')
  await expect(page.getByRole('note').getByRole('link',{name:'システム › LED / 音声 で設定する'})).toHaveAttribute('href','#system/led')
  await expect(page.getByText('認識モデルが未導入です',{exact:false})).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  expect(errors).toEqual([])
  await page.screenshot({path:info.outputPath(`plate-${width}-${theme}.png`),fullPage:true})
 })}

 test('list selection opens the matching map popup and marker selection highlights the list item',async({page})=>{
  await open(page)
  await page.locator('[data-record="pfix-0003"] .plate-item-main').click()
  await expect(page.locator('[data-record="pfix-0003"]')).toHaveClass(/selected/)
  await expect(page.locator('.leaflet-popup-content')).toContainText('多摩 330 わ 45-67')
  await page.locator('path.plate-marker-hit').click({force:true})
  await expect(page.locator('[data-record="pfix-0001"]')).toHaveClass(/selected/)
  await expect(page.locator('[data-record="pfix-0003"]')).not.toHaveClass(/selected/)
  await expect(page.locator('.leaflet-popup')).toHaveCount(1)
  await expect(page.locator('.leaflet-popup-content')).toContainText('条件: 家族の車')
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

 test('system VOICEVOX settings keep the key write-only and check the endpoint',async({page})=>{
  let cfg={endpoint:'https://deprecatedapis.tts.quest',defaultEndpoint:'https://deprecatedapis.tts.quest',keyConfigured:false}
  const puts:any[]=[]
  await page.route('**/api/v1/**',async route=>{
   const url=new URL(route.request().url()),method=route.request().method()
   if(url.pathname==='/api/v1/voicevox'&&method==='GET')return route.fulfill({json:cfg})
   if(url.pathname==='/api/v1/voicevox'&&method==='PUT'){const b=route.request().postDataJSON();puts.push(b);cfg={...cfg,endpoint:b.endpoint.replace(/\/$/,''),keyConfigured:'key' in b?b.key!=='':cfg.keyConfigured};return route.fulfill({json:cfg})}
   if(url.pathname==='/api/v1/voicevox/check')return route.fulfill({json:{points:9991900,resetInHours:23.9}})
   return route.fulfill({status:404,json:{reason:'fixture'}})
  })
  await page.route('**/__voicevox-review',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/voicevox-review.ts"></script></body></html>'}))
  await page.setViewportSize({width:390,height:800})
  await page.goto(reviewURL!+'/__voicevox-review')
  await expect(page.getByLabel('エンドポイント')).toHaveValue('https://deprecatedapis.tts.quest')
  await expect(page.getByText('APIキー未設定')).toBeVisible()
  await expect(page.getByRole('button',{name:'接続を確認'})).toBeDisabled()
  await page.getByLabel('APIキー').fill('fictional-key')
  await page.getByRole('button',{name:'保存'}).click()
  await expect(page.getByText('APIキー設定済み')).toBeVisible()
  expect(puts[0]).toEqual({endpoint:'https://deprecatedapis.tts.quest',key:'fictional-key'})
  await expect(page.getByLabel('APIキー')).toHaveValue('')
  await expect(page.getByLabel('APIキー')).toHaveAttribute('placeholder','設定済み（変更する場合のみ入力）')
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
