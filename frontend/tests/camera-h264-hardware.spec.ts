import {test,expect} from '@playwright/test'
import {writeFile} from 'node:fs/promises'
test.skip(process.env.NCC_H264_HARDWARE!=='1','Explicit live-camera field flag required')

test('both original H264 camera streams decode and survive shared view switching',async({page},info)=>{
 const soakSeconds=Number(process.env.NCC_H264_SECONDS||0)
 if(!Number.isInteger(soakSeconds)||soakSeconds<0||soakSeconds>240)throw new Error('NCC_H264_SECONDS must be 0..240')
 test.setTimeout(90000+soakSeconds*1000)
 const journal:unknown[]=[],note=(value:unknown)=>{if(journal.length<200)journal.push({at:Date.now(),value})}
 page.on('console',message=>{if(message.text().startsWith('NCC_MEDIA '))note(message.text().slice(10))})
 page.on('requestfailed',request=>{if(request.url().endsWith('/h264'))note({requestFailed:request.failure()?.errorText})})
 const network=await page.context().newCDPSession(page)
 await network.send('Network.enable')
 network.on('Network.responseReceived',event=>{if(event.response.url.endsWith('/h264'))note({protocol:event.response.protocol})})
 await page.addInitScript(()=>{
  const report=(value:unknown)=>console.debug('NCC_MEDIA '+JSON.stringify(value))
  const read=ReadableStreamDefaultReader.prototype.read
  ReadableStreamDefaultReader.prototype.read=async function(){try{const value=await read.call(this);if(value.done)report({streamEnded:true});return value}catch(e){report({readError:e instanceof Error?e.message:String(e)});throw e}}
  const original=MediaSource.prototype.addSourceBuffer
  MediaSource.prototype.addSourceBuffer=function(type:string){const buffer=original.call(this,type)
   for(const method of ['appendBuffer','remove'] as const){const action=buffer[method].bind(buffer) as (...args:any[])=>void;(buffer as any)[method]=(...args:any[])=>{try{action(...args)}catch(e){report({method,error:e instanceof Error?e.message:String(e)});throw e}}}
   buffer.addEventListener('error',()=>report({bufferError:true}));return buffer
  }
  document.addEventListener('error',event=>{if(event.target instanceof HTMLVideoElement)report({videoError:event.target.error?.code,message:event.target.error?.message})},true)
 })
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 let responses=0;page.on('response',response=>{if(/\/camera\/sessions\/[^/]+\/h264$/.test(new URL(response.url()).pathname)){note({response:response.status()});if(response.status()===200)responses++}})
 try{
 await page.goto('/#camera')
 await page.getByRole('button',{name:'並べて表示',exact:true}).click()
 await page.getByRole('button',{name:'映像を表示',exact:true}).click()
 const videos=page.locator('.live-camera-grid video')
 await expect(videos).toHaveCount(2)
 await expect.poll(()=>videos.evaluateAll(items=>items.every(item=>(item as HTMLVideoElement).readyState>=2&&(item as HTMLVideoElement).videoWidth===1920&&(item as HTMLVideoElement).videoHeight===1080)),{timeout:25000}).toBe(true)
 const frames=await videos.evaluateAll(items=>items.map(item=>(item as HTMLVideoElement).getVideoPlaybackQuality().totalVideoFrames))
 await expect.poll(()=>videos.evaluateAll((items,before)=>items.every((item,index)=>(item as HTMLVideoElement).getVideoPlaybackQuality().totalVideoFrames>before[index]+8),frames),{timeout:35000}).toBe(true)
 const next=await videos.evaluateAll(items=>items.map(item=>(item as HTMLVideoElement).getVideoPlaybackQuality().totalVideoFrames))
 note({initialFrames:frames,advancedFrames:next})
 expect(next.every((value,index)=>value>frames[index]+8)).toBe(true)
 if(soakSeconds){
  const connections=responses,started=Date.now()
  while(Date.now()-started<soakSeconds*1000){
   await page.waitForTimeout(Math.max(1,Math.min(10000,soakSeconds*1000-(Date.now()-started))))
   expect(await videos.evaluateAll(items=>items.every(item=>!(item as HTMLVideoElement).error))).toBe(true)
  }
  await expect.poll(()=>videos.evaluateAll(items=>items.every(item=>(item as HTMLVideoElement).readyState>=2)),{timeout:20000}).toBe(true)
  const final=await videos.evaluateAll(items=>items.map(item=>(item as HTMLVideoElement).getVideoPlaybackQuality().totalVideoFrames))
  note({finalFrames:final,connections,responses})
  expect(final.every((value,index)=>value>next[index]+soakSeconds*8)).toBe(true)
  expect(responses).toBe(connections)
 }
 await page.screenshot({path:info.outputPath('h264-live-dual-desktop.png'),fullPage:true})
 await page.getByRole('button',{name:'車内',exact:true}).click()
 await expect(videos).toHaveCount(1)
 await expect(page.getByRole('heading',{name:'車内カメラ',exact:true})).toBeVisible()
 await page.getByRole('button',{name:'並べて表示',exact:true}).click()
 await expect(videos).toHaveCount(2)
 await expect.poll(()=>videos.evaluateAll(items=>items.every(item=>(item as HTMLVideoElement).readyState>=2)),{timeout:20000}).toBe(true)
 await page.setViewportSize({width:390,height:844})
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath('h264-live-dual-mobile.png'),fullPage:true})
 await page.getByRole('button',{name:'表示を停止',exact:true}).click()
 await expect.poll(()=>videos.evaluateAll(items=>items.every(item=>!(item as HTMLVideoElement).getAttribute('src')))).toBe(true)
 expect(errors).toEqual([])
 }finally{await writeFile(info.outputPath('media-diagnostics.private.json'),JSON.stringify(journal,null,2))}
})
