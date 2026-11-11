import {test,expect} from '@playwright/test'

// Synthetic input only; the unmodified production UI/API and real LED/audio
// workers run on the NP1. This test never captures a desktop or microphone.
test('real device accepts browser-generated RGB and audio and restores saved settings',async({page,request})=>{
 test.skip(process.env.NCC_SCREEN_HARDWARE!=='1','Set NCC_SCREEN_HARDWARE=1 for the attached device.')
 await page.addInitScript(()=>{
  const canvas=document.createElement('canvas');canvas.width=500;canvas.height=100
  const context=canvas.getContext('2d')!,palette=['#d00000','#00d000','#0000d0','#b0b0b0','#d07000']
  let frame=0;const paint=()=>{palette.forEach((color,index)=>{context.fillStyle=color;context.fillRect(index*100,0,100,100)});frame++}
  paint();const timer=setInterval(paint,100)
  Object.defineProperty(navigator.mediaDevices,'getDisplayMedia',{value:async(options:DisplayMediaStreamOptions)=>{
   const stream=canvas.captureStream(10)
   let audio:AudioContext|undefined
   if(options.audio){audio=new AudioContext({sampleRate:48000});const source=audio.createOscillator(),gain=audio.createGain(),destination=audio.createMediaStreamDestination();source.frequency.value=330;gain.gain.value=.04;source.connect(gain);gain.connect(destination);source.start();await audio.resume();stream.addTrack(destination.stream.getAudioTracks()[0]!)}
   ;(window as any).__hardwareCapture=stream
   ;(window as any).__hardwareCleanup=()=>{clearInterval(timer);stream.getTracks().forEach(t=>t.stop());void audio?.close()}
   return stream
  }})
 })
 const getLED=async()=>{const response=await request.get('/api/v1/device/led');expect(response.ok()).toBe(true);return (await response.json()).data}
 const before=await getLED(),errors:string[]=[],frames:{sequence:number;zones:number[][]}[]=[],audioChunks:any[]=[]
 page.on('pageerror',error=>errors.push(error.message))
 page.on('request',r=>{if(r.method()!=='PUT')return;const path=new URL(r.url()).pathname;if(path.includes('/led/screen/sessions/'))frames.push(r.postDataJSON());if(path.includes('/audio/sessions/'))audioChunks.push(r.postDataJSON())})
 await page.goto('/#system/led')
 const panel=page.getByRole('region',{name:'画面連動LED'})
 await expect(panel).toBeVisible()
 await panel.getByLabel('音声もNP1で再生',{exact:true}).check()
 try{
  await panel.getByRole('button',{name:'画面を選んで開始',exact:true}).click()
  await expect(panel.getByText('共有音声をNP1で再生中',{exact:true})).toBeVisible()
  await expect.poll(()=>frames.length).toBeGreaterThan(20)
  await expect.poll(()=>audioChunks.length).toBeGreaterThan(5)
  await expect.poll(async()=> (await getLED()).lastCommand.mode).toBe('screen')
  expect((await getLED()).settings).toEqual(before.settings)
  expect(frames.every(f=>f.zones.length===5&&f.zones.every(z=>z.length===3&&z.every(v=>Number.isInteger(v)&&v>=0&&v<=255)))).toBe(true)
  expect(errors).toEqual([])
  await expect(panel.locator('[role=alert]')).toHaveCount(0)
 }finally{
  await panel.getByRole('button',{name:'停止',exact:true}).click()
  await page.evaluate(()=>(window as any).__hardwareCleanup?.())
 }
 await expect.poll(async()=> (await getLED()).lastCommand.mode).toBe(before.settings.mode)
 expect((await getLED()).settings).toEqual(before.settings)
 expect(await page.evaluate(()=>(window as any).__hardwareCapture.getTracks().every((t:MediaStreamTrack)=>t.readyState==='ended'))).toBe(true)
})
