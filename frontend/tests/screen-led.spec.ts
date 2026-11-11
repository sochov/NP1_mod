import {test,expect,type Page,type Route} from '@playwright/test'
type Frame={sequence:number;zones:number[][]}
async function setup(page:Page,{insecure=false,audio=false}={}){
 await page.addInitScript(({insecure,audio})=>{
  const state=(window as any).__screenTest={calls:0,tracks:[] as MediaStreamTrack[],mode:0,options:undefined as DisplayMediaStreamOptions|undefined,worklets:[] as string[]}
  const addModule=AudioWorklet.prototype.addModule;AudioWorklet.prototype.addModule=function(url,options){state.worklets.push(String(url));return addModule.call(this,url,options)}
  if(insecure)Object.defineProperty(window,'isSecureContext',{value:false})
  const canvas=document.createElement('canvas');canvas.width=500;canvas.height=200
  const ctx=canvas.getContext('2d')!,palette=['#ff0000','#00ff00','#0000ff','#ffffff','#ff8000']
  function paint(){for(let i=0;i<5;i++){ctx.fillStyle=state.mode===1?'#000000':palette[i]!;ctx.fillRect(i*100,0,100,200)}}
  paint();setInterval(paint,40)
  Object.defineProperty(navigator.mediaDevices,'getDisplayMedia',{configurable:true,value:async(options:DisplayMediaStreamOptions)=>{state.calls++;state.options=options;const stream=canvas.captureStream(10);if(audio&&options.audio){const context=new AudioContext({sampleRate:48000}),oscillator=context.createOscillator(),destination=context.createMediaStreamDestination();oscillator.frequency.value=220;oscillator.connect(destination);oscillator.start();await context.resume();stream.addTrack(destination.stream.getAudioTracks()[0]!)}state.tracks=stream.getTracks();return stream}})
 },{insecure,audio})
 const frames:Frame[]=[],deletes:string[]=[],errors:string[]=[],audioFrames:{sequence:number;pcm:string}[]=[],audioDeletes:string[]=[],audioStarts:string[]=[];let held:Route|undefined,hold=false,fail=false,creates=0,audioFail=false
 page.on('pageerror',e=>errors.push(e.message))
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname,method=route.request().method();let data:unknown={}
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme:'dark',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))data=[]
  if(path.endsWith('/device/led'))data={status:'available',data:{settings:{revision:1,mode:'off',brightness:180,speed:240,fps:15,rgb:[255,255,255],autostart:false},serviceState:'active'}}
  if(path.endsWith('/screen/sessions')&&method==='POST'){creates++;data={id:'synthetic-screen-session',frameIntervalMs:100,idleTimeoutMs:1500}}
  if(path.endsWith('/screen/sessions/synthetic-screen-session')){
   if(method==='DELETE'){deletes.push(path);data={restored:true}}
   if(method==='PUT'){
    const frame=route.request().postDataJSON();frames.push(frame)
    if(hold){held=route;hold=false;return}
    if(fail)return route.fulfill({status:503,json:{reason:'Synthetic link failure'}})
    data={accepted:true,sequence:frame.sequence}
   }
  }
  if(path.endsWith('/audio/sessions')&&method==='POST'){audioStarts.push(route.request().postData()||'');data={id:'synthetic-audio-session',sampleRate:24000,channels:1,chunkSamples:4800,idleTimeoutMs:2000}}
  if(path.endsWith('/audio/sessions/synthetic-audio-session')){if(method==='DELETE'){audioDeletes.push(path);data={stopped:true}}if(method==='PUT'){const frame=route.request().postDataJSON();audioFrames.push(frame);if(audioFail)return route.fulfill({status:503,json:{code:'AUDIO_UNAVAILABLE'}});data={accepted:true,sequence:frame.sequence}}}
  await route.fulfill({json:data})
 })
 const preview=process.env.NCC_UI_REVIEW_URL
 await page.goto(preview?preview.replace(/\/$/,'')+'/#system/led':'/#system/led');await expect(page.getByRole('region',{name:'画面連動LED'})).toBeVisible()
 return {frames,deletes,errors,audioFrames,audioDeletes,audioStarts,failAudio(){audioFail=true},get creates(){return creates},holdNext(){hold=true},hasHeld(){return !!held},failNext(){fail=true},async release(){const r=held;held=undefined;if(r)await r.fulfill({json:{accepted:true,sequence:r.request().postDataJSON().sequence}})}}
}
const panel=(page:Page)=>page.getByRole('region',{name:'画面連動LED'})
async function start(page:Page){await panel(page).getByRole('button',{name:'画面を選んで開始'}).click();await expect(panel(page).getByText('画面連動中',{exact:true})).toBeVisible()}
async function fullRegion(page:Page){const p=panel(page);await p.getByText('範囲を数値で調整',{exact:true}).click();for(const [name,value] of [['左位置','0'],['上位置','0'],['幅','100'],['高さ','100']]){const field=p.getByLabel('選択範囲の'+name);await field.fill(value!);await field.press('Tab')}for(const name of ['画面連動の彩度','画面連動の明るさ','画面連動のコントラスト','色のなめらかさ'])await p.getByLabel(name,{exact:true}).evaluate((element:HTMLInputElement)=>{element.value=element.getAttribute('aria-label')==='色のなめらかさ'?'0':'1';element.dispatchEvent(new Event('input',{bubbles:true}))})}
async function stopped(page:Page){await expect.poll(()=>page.evaluate(()=>(window as any).__screenTest.tracks.every((track:MediaStreamTrack)=>track.readyState==='ended'))).toBe(true)}

test('screen selection starts only from a button; sends five area colors and restores normal mode',async({page},info)=>{
 const state=await setup(page);expect(await page.evaluate(()=>(window as any).__screenTest.calls)).toBe(0);expect(state.creates).toBe(0)
 await expect(panel(page).getByLabel('画面連動の彩度',{exact:true})).toHaveValue('1.25');await expect(panel(page).getByLabel('画面連動の明るさ',{exact:true})).toHaveValue('1.1');await expect(panel(page).getByLabel('画面連動のコントラスト',{exact:true})).toHaveValue('1.15')
 await start(page);await fullRegion(page)
 await expect.poll(()=>state.frames.at(-1)?.zones).toEqual([[255,0,0],[0,255,0],[0,0,255],[255,255,255],[255,128,0]])
 expect(state.frames.every(frame=>Object.keys(frame).sort().join(',')==='sequence,zones')).toBe(true)
 expect(state.frames.every((frame,i)=>frame.sequence===i+1&&frame.zones.length===5&&frame.zones.every(rgb=>rgb.length===3&&rgb.every(n=>Number.isInteger(n)&&n>=0&&n<=255)))).toBe(true)
 await panel(page).getByLabel('左右を反転',{exact:true}).check();await expect.poll(()=>state.frames.at(-1)?.zones[0]).toEqual([255,128,0])
 await page.screenshot({path:info.outputPath('screen-led-desktop-dark.png'),fullPage:true})
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page);await expect.poll(()=>state.deletes.length).toBe(1)
 const count=state.frames.length;await page.waitForTimeout(250);expect(state.frames.length).toBe(count);expect(state.errors).toEqual([])
})
test('single in-flight update keeps only the latest frame while a request is delayed',async({page})=>{
 const state=await setup(page);await start(page);await fullRegion(page);await expect.poll(()=>state.frames.length).toBeGreaterThan(1)
 state.holdNext();await expect.poll(()=>state.hasHeld()).toBe(true);const count=state.frames.length
 await page.evaluate(()=>(window as any).__screenTest.mode=1);await page.waitForTimeout(350);expect(state.frames.length).toBe(count)
 await state.release();await expect.poll(()=>state.frames.at(-1)?.zones).toEqual(Array(5).fill([0,0,0]));expect(state.frames.length).toBeLessThan(count+5)
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page)
})
test('network error stops tracks and releases the device session',async({page})=>{
 const state=await setup(page);await start(page);state.failNext();await expect(panel(page).getByRole('alert')).toContainText('画面連動を停止しました')
 await stopped(page);await expect.poll(()=>state.deletes.length).toBe(1);await expect(panel(page).getByRole('button',{name:'画面を選んで開始'})).toBeEnabled()
})
test('native chooser cancellation never creates a device session and can be retried',async({page})=>{
 const state=await setup(page)
 await page.evaluate(()=>{const original=navigator.mediaDevices.getDisplayMedia.bind(navigator.mediaDevices);let cancelled=false;Object.defineProperty(navigator.mediaDevices,'getDisplayMedia',{value:async(...args:any[])=>{if(!cancelled){cancelled=true;throw new DOMException('Synthetic cancellation','NotAllowedError')}return original(...args as [DisplayMediaStreamOptions])}})})
 await panel(page).getByRole('button',{name:'画面を選んで開始'}).click();await expect(panel(page).getByRole('alert')).toContainText('キャンセル');expect(state.creates).toBe(0)
 await start(page);await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page);expect(state.errors).toEqual([])
})
test('a stalled update times out and releases capture rather than leaving stale LEDs active',async({page})=>{
 const state=await setup(page);state.holdNext();await start(page);await expect.poll(()=>state.hasHeld()).toBe(true)
 await expect(panel(page).getByRole('alert')).toContainText('画面連動を停止しました');await stopped(page);await expect.poll(()=>state.deletes.length).toBe(1)
 await state.release().catch(()=>{});const count=state.frames.length;await page.waitForTimeout(200);expect(state.frames.length).toBe(count)
})
test('hidden document keeps screen capture and RGB updates running',async({page})=>{
 const state=await setup(page);await start(page);const count=state.frames.length
 await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,get:()=>true});Object.defineProperty(document,'visibilityState',{configurable:true,get:()=>'hidden'});document.dispatchEvent(new Event('visibilitychange'));(window as any).__screenTest.mode=1})
 await expect.poll(()=>state.frames.length).toBeGreaterThan(count+2);expect(state.deletes.length).toBe(0)
 expect(await page.evaluate(()=>(window as any).__screenTest.tracks[0].readyState)).toBe('live')
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page)
})
test('share ended, pagehide and component navigation release all tracks and sessions',async({page})=>{
 const state=await setup(page);await start(page)
 await page.evaluate(()=>(window as any).__screenTest.tracks[0].dispatchEvent(new Event('ended')));await stopped(page);await expect.poll(()=>state.deletes.length).toBe(1)
 await start(page);await page.evaluate(()=>window.dispatchEvent(new Event('pagehide')));await stopped(page);await expect.poll(()=>state.deletes.length).toBe(2)
 await start(page);await page.getByRole('button',{name:'本体',exact:true}).click();await stopped(page);await expect.poll(()=>state.deletes.length).toBe(3)
})
test('mobile touch selection moves/resizes and controls remain readable in both themes',async({page},info)=>{
 await page.setViewportSize({width:390,height:844});const state=await setup(page);await start(page)
 const selection=panel(page).locator('.screen-led-selection'),before=await selection.getAttribute('style')
 // Real browser input exercises pointer capture; screen content remains a synthetic canvas.
 const box=await panel(page).locator('.screen-led-preview').boundingBox();expect(box).not.toBeNull()
 await page.mouse.move(box!.x+box!.width*.1,box!.y+box!.height*.2);await page.mouse.down();await page.mouse.move(box!.x+box!.width*.9,box!.y+box!.height*.5);await page.mouse.up()
 expect(await selection.getAttribute('style')).not.toBe(before)
 const cdp=await page.context().newCDPSession(page),rect=await panel(page).locator('.screen-led-preview').boundingBox()
 await cdp.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:rect!.x+rect!.width*.15,y:rect!.y+rect!.height*.2}]})
 await cdp.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:[{x:rect!.x+rect!.width*.85,y:rect!.y+rect!.height*.4}]})
 await cdp.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});await cdp.detach()
 await panel(page).scrollIntoViewIfNeeded();await page.screenshot({path:info.outputPath('screen-led-mobile-dark.png'),fullPage:true})
 expect((await panel(page).locator('.screen-led-swatches>span').first().boundingBox())!.width).toBeGreaterThan(45)
 expect((await panel(page).getByLabel('画面連動の彩度').boundingBox())!.width).toBeGreaterThan(250)
 expect(await panel(page).locator('.screen-led-audio-choice').evaluate(element=>getComputedStyle(element).flexDirection)).toBe('row')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:info.outputPath('screen-led-mobile-light.png'),fullPage:true})
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();expect(state.errors).toEqual([])
})
test('insecure LAN origin explains actual browser requirement without starting capture',async({page})=>{
 const state=await setup(page,{insecure:true});await expect(panel(page).getByText('画面共有にはHTTPSまたはlocalhostでの接続が必要です。')).toBeVisible()
 await expect(panel(page).getByRole('link',{name:'HTTPSで開く'})).toHaveAttribute('href',/^https:/)
 await expect(panel(page).getByRole('button',{name:'画面を選んで開始'})).toBeDisabled();expect(state.creates).toBe(0)
})
for(const width of [1440,390])for(const theme of ['light','dark'])test(`active screen layout ${width}px ${theme}`,async({page},info)=>{
 await page.setViewportSize({width,height:1000});const state=await setup(page);await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
 const audio=panel(page).getByLabel('音声もNP1で再生',{exact:true});await expect(audio).toBeEnabled();await start(page);await expect(audio).toBeDisabled()
 expect(await panel(page).locator('.screen-led-audio-choice').evaluate(element=>getComputedStyle(element).flexDirection)).toBe('row')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await panel(page).scrollIntoViewIfNeeded();await page.screenshot({path:info.outputPath(`active-screen-${width}-${theme}.png`),fullPage:true})
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page);expect(state.errors).toEqual([])
})
test('LED snapshot identifies another browser screen session while editing saved normal mode',async({page})=>{
 const state=await setup(page);let writes=0
 await page.route('**/api/v1/device/led',route=>{if(route.request().method()!=='GET')writes++;return route.fulfill({json:{status:'available',data:{settings:{revision:1,mode:'rainbow',brightness:180,speed:240,fps:15,rgb:[255,255,255],autostart:false},serviceState:'active',lastCommand:{mode:'screen',running:true,screenSequence:5}}}})})
 await page.locator('.led-panel').getByRole('button',{name:'状態を更新',exact:true}).click()
 await expect(page.locator('.led-state')).toContainText('取得時の表示設定');await expect(page.locator('.led-state')).toContainText('画面連動')
 await expect(page.getByLabel('通常の表示モード',{exact:true})).toHaveValue('rainbow');expect(writes).toBe(0);expect(state.frames).toHaveLength(0)
})
test('audio opt-in uses same-origin worklet and bounded PCM; stopping releases video, audio and output',async({page})=>{
 await page.route('http://127.0.0.1:5174/',async route=>{const response=await route.fetch();await route.fulfill({response,headers:{...response.headers(),'Content-Security-Policy':"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; media-src 'self'"}})})
 const state=await setup(page,{audio:true});await panel(page).getByLabel('音声もNP1で再生',{exact:true}).check();await start(page)
 await expect(panel(page).getByText('共有音声をNP1で再生中',{exact:true})).toBeVisible();await expect.poll(()=>state.audioFrames.length).toBeGreaterThan(2)
 const worklets=await page.evaluate(()=>(window as any).__screenTest.worklets as string[])
 expect(state.audioStarts).toEqual(['']);expect(worklets).toHaveLength(1);expect(new URL(worklets[0]!,page.url()).origin).toBe('http://127.0.0.1:5174');expect(worklets[0]).toMatch(/\/assets\/screen-audio-worklet-.+\.js$/)
 for(const [i,frame] of state.audioFrames.entries()){expect(frame.sequence).toBe(i+1);const pcm=Buffer.from(frame.pcm,'base64');expect(pcm.length).toBe(9600);expect(pcm.some(v=>v!==0)).toBe(true);expect(Object.keys(frame).sort()).toEqual(['pcm','sequence'])}
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page);await expect.poll(()=>state.audioDeletes.length).toBe(1);expect(state.errors).toEqual([])
})
test('missing shared audio explains availability while LED continues',async({page})=>{
 const state=await setup(page);await panel(page).getByLabel('音声もNP1で再生',{exact:true}).check();await start(page);await expect(panel(page).locator('.screen-led-audio-status')).toContainText('共有音声を取得できません');expect(state.audioStarts).toHaveLength(0);expect(state.frames.length).toBeGreaterThan(0)
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page)
})
test('audio failure stops only shared audio and keeps the screen LED running',async({page})=>{
 const state=await setup(page,{audio:true});await panel(page).getByLabel('音声もNP1で再生',{exact:true}).check();await start(page);await expect.poll(()=>state.audioFrames.length).toBeGreaterThan(0);state.failAudio()
 await expect(panel(page).locator('.screen-led-audio-status')).toContainText('音声の接続が途切れた');await expect.poll(()=>state.audioDeletes.length).toBe(1)
 expect(await page.evaluate(()=>(window as any).__screenTest.tracks.find((t:MediaStreamTrack)=>t.kind==='video').readyState)).toBe('live');await expect(panel(page).getByText('画面連動中',{exact:true})).toBeVisible()
 await panel(page).getByRole('button',{name:'停止',exact:true}).click();await stopped(page)
})
