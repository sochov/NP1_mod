<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {Camera,Maximize} from '@lucide/vue'
import {api} from '../services/api'

const props=defineProps<{source:'front'|'in';format:'h264'|'jpeg';color:string;playing:boolean}>(),emit=defineEmits<{fallback:[];busy:[value:boolean]}>()
const source=computed(()=>props.source),format=computed(()=>props.format),color=computed(()=>props.color),active=ref(false),busy=ref(false),status=ref('idle'),error=ref(''),imageURL=ref(''),videoURL=ref(''),video=ref<HTMLVideoElement>(),width=ref(1920),height=ref(1080),fps=ref(0)
let generation=0,session='',controller:AbortController|undefined,timer:ReturnType<typeof setTimeout>|undefined,finishPause:(()=>void)|undefined,mediaSource:MediaSource|undefined,objectURL=''
const caption=computed(()=>format.value==='h264'?`${width.value} × ${height.value}${fps.value?' · '+Number(fps.value.toFixed(1))+' fps':''} · H.264`:'640 × 360 · 最大 1 fps · JPEG')
const waiting=computed(()=>active.value&&status.value!=='live')
function clearMedia(){imageURL.value='';videoURL.value='';if(objectURL)URL.revokeObjectURL(objectURL);objectURL='';if(mediaSource?.readyState==='open'){try{mediaSource.endOfStream()}catch{}}mediaSource=undefined}
async function release(id:string){if(id){try{await api('/camera/sessions/'+encodeURIComponent(id),'DELETE')}catch{}}}
async function stop(reset=true){active.value=false;generation++;busy.value=false;controller?.abort();clearTimeout(timer);finishPause?.();finishPause=undefined;const id=session;session='';clearMedia();if(reset){status.value='idle';error.value=''}await release(id)}
function pause(ms:number,g:number){return new Promise<void>(resolve=>{if(!active.value||g!==generation)return resolve();finishPause=resolve;timer=setTimeout(()=>{finishPause=undefined;resolve()},ms)})}
async function createSession(g:number){const value=await api<{id:string}>('/camera/sessions','POST',{source:source.value,color:format.value==='jpeg'?color.value:'source'});if(g!==generation||!active.value){await release(value.id);return false}session=value.id;return true}
async function start(){
 if(active.value||busy.value)return
 error.value=''
 if(format.value==='h264'&&typeof MediaSource==='undefined'){status.value='unsupported';error.value='このブラウザーはH.264ライブ表示に対応していません。JPEG表示を利用できます。';return}
 busy.value=true;active.value=true;status.value='loading';const g=++generation
 try{if(await createSession(g)){busy.value=false;if(format.value==='h264')await h264Loop(g);else await jpegLoop(g)}}catch{if(g===generation){await stop(false);status.value='error';error.value='映像を開けません。もう一度お試しください。'}}finally{if(g===generation)busy.value=false}
}
async function change(){const wasActive=active.value||props.playing;await stop();if(wasActive)await start()}
async function jpegLoop(g:number){
 while(active.value&&g===generation){
  controller=new AbortController()
  try{
   const response=await fetch('/api/v1/camera/sessions/'+encodeURIComponent(session)+'/stream',{signal:controller.signal})
   if(g!==generation)return
   if(response.status===403){await release(session);if(!await createSession(g))return;continue}
   if(!response.ok){status.value='waiting';imageURL.value='';await pause(2000,g);continue}
   const blob=await response.blob();if(g!==generation)return
   if(objectURL)URL.revokeObjectURL(objectURL);objectURL=URL.createObjectURL(blob);imageURL.value=objectURL;status.value='live';error.value=''
   await pause(1000,g)
  }catch(e){if(g!==generation||(e instanceof DOMException&&e.name==='AbortError'))return;status.value='waiting';imageURL.value='';await pause(2000,g)}
 }
}
function event(target:EventTarget,type:string,signal:AbortSignal){return new Promise<void>((resolve,reject)=>{
 const cleanup=()=>{target.removeEventListener(type,done);target.removeEventListener('error',failed);signal.removeEventListener('abort',aborted)}
 const done=()=>{cleanup();resolve()},failed=()=>{cleanup();reject(new Error('media unavailable'))},aborted=()=>{cleanup();reject(new DOMException('Aborted','AbortError'))}
 if(signal.aborted)return aborted();target.addEventListener(type,done,{once:true});target.addEventListener('error',failed,{once:true});signal.addEventListener('abort',aborted,{once:true})
})}
async function append(buffer:SourceBuffer,chunk:Uint8Array,signal:AbortSignal){if(buffer.updating)await event(buffer,'updateend',signal);const ended=event(buffer,'updateend',signal);try{buffer.appendBuffer(new Uint8Array(chunk))}catch(e){ended.catch(()=>{});throw e}await ended}
async function h264Loop(g:number){
 while(active.value&&g===generation){
  const abort=new AbortController();controller=abort;let reader:ReadableStreamDefaultReader<Uint8Array>|undefined
  try{
   status.value='loading'
   const response=await fetch('/api/v1/camera/sessions/'+encodeURIComponent(session)+'/h264',{signal:abort.signal})
   if(g!==generation)return
   if(response.status===403){await release(session);if(!await createSession(g))return;continue}
   if(!response.ok||!response.body){status.value='waiting';await pause(2000,g);continue}
   const codec=response.headers.get('X-Camera-Codec')||'',mime=`video/mp4; codecs="${codec}"`
   if(!/^avc1\.[0-9a-f]{6}$/i.test(codec)||!MediaSource.isTypeSupported(mime)){await stop(false);status.value='unsupported';error.value='このブラウザーでは映像を再生できません。JPEG表示を利用できます。';return}
   width.value=Number(response.headers.get('X-Camera-Width'))||1920;height.value=Number(response.headers.get('X-Camera-Height'))||1080;fps.value=Number(response.headers.get('X-Camera-FPS'))||0
   clearMedia();const mse=new MediaSource();mediaSource=mse
   const opened=event(mse,'sourceopen',abort.signal);objectURL=URL.createObjectURL(mse);videoURL.value=objectURL;await opened
   const buffer=mse.addSourceBuffer(mime);buffer.mode='segments';reader=response.body.getReader();let played=false
   while(active.value&&g===generation){
    const item=await reader.read();if(item.done)break
    await append(buffer,item.value,abort.signal)
    if(buffer.buffered.length){
     const end=buffer.buffered.end(buffer.buffered.length-1),player=video.value
     if(player&&end>.3){if(!played||end-player.currentTime>2.5){player.currentTime=Math.max(0,end-.25)}if(!played){played=true;void player.play().catch(()=>{})}status.value='live';error.value=''}
     if(end>12&&buffer.buffered.start(0)<end-12){const removed=event(buffer,'updateend',abort.signal);try{buffer.remove(0,end-12)}catch(e){removed.catch(()=>{});throw e}await removed}
    }
   }
  }catch(e){if(g!==generation||(e instanceof DOMException&&e.name==='AbortError'))return}
  finally{try{await reader?.cancel()}catch{};abort.abort();if(g===generation)clearMedia()}
  if(active.value&&g===generation){status.value='waiting';await pause(1500,g)}
 }
}
function fallback(){emit('fallback')}
function fullscreen(){video.value?.requestFullscreen?.().catch(()=>{})}
function videoWaiting(){if(active.value)status.value='waiting'}
function videoPlaying(){if(active.value)status.value='live'}
watch(()=>props.playing,value=>{if(value)start();else stop()},{immediate:true})
watch([format,color],()=>{change()})
watch(busy,value=>emit('busy',value),{immediate:true})
onUnmounted(()=>{stop()})
defineExpose({stop})
</script>

<template>
 <section class="panel camera-panel" :aria-busy="busy">
  <div class="panel-head"><h2>{{source==='front'?'前方カメラ':'車内カメラ'}}</h2><span class="tag">{{caption}}</span><button v-if="format==='h264'&&status==='live'" class="icon-button" :aria-label="(source==='front'?'前方':'車内')+'映像を全画面表示'" @click="fullscreen"><Maximize :size="18"/></button></div>
  <div class="camera-preview live-camera">
   <video v-if="format==='h264'" ref="video" :src="videoURL||undefined" aria-label="純正H.264ライブ映像" muted playsinline autoplay @waiting="videoWaiting" @playing="videoPlaying"></video>
   <img v-else-if="imageURL" :src="imageURL" alt="ライブカメラ映像">
   <div v-if="(!active||waiting)&&!imageURL" class="camera-placeholder"><Camera :size="40" stroke-width="1"/><h3>{{waiting?'映像の再開待ち':'カメラは停止しています'}}</h3><p>{{waiting?'カメラ出力が再開すると自動で表示します。':'表示ボタンでライブ映像を開きます。'}}</p></div>
  </div>
  <p v-if="waiting" class="hint pad" role="status">カメラ出力の再開を待っています。</p>
  <p v-if="error" class="error pad" role="alert">{{error}} <button v-if="status==='unsupported'" class="text-button" @click="fallback">JPEG表示に切り替える</button></p>
 </section>
</template>

<style scoped>
.live-camera{position:relative;aspect-ratio:16/9;min-height:180px;background:#090b0e;overflow:hidden}.live-camera video,.live-camera img{display:block;width:100%;height:100%;object-fit:contain}.live-camera video:not([src]){visibility:hidden}.camera-placeholder{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;flex-direction:column;padding:20px;text-align:center;color:#b6c0cf;background:#11151c}.camera-placeholder h3{font-size:18px;margin:16px 0 8px}.camera-placeholder p{font-size:14px;margin:0;line-height:1.7}.camera-controls{flex-wrap:wrap}.camera-controls select,.camera-controls button{min-height:44px}.pad{margin:0;padding:0 20px 18px}@media(max-width:600px){.camera-controls select{flex:1;min-width:120px}.camera-controls .primary,.camera-controls .secondary{flex:1}.live-camera{min-height:170px}.camera-placeholder{padding:14px}.camera-placeholder h3{font-size:16px}.camera-placeholder p{font-size:13px}}
</style>
