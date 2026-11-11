<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue'
import {api} from '../services/api'
import {defaultScreenRegion,normalizeRegion,screenColors,smoothColors,type RGB,type ScreenRegion} from '../services/screen-colors'
import {prepareScreenAudio,screenAudioSupported,startScreenAudio,type ScreenAudioHandle} from '../services/audio-capture'
import '../screen-led.css'

type ScreenSession={id:string;frameIntervalMs:number;idleTimeoutMs:number}
const video=ref<HTMLVideoElement>(),surface=ref<HTMLElement>(),active=ref(false),starting=ref(false),error=ref(''),message=ref(''),region=ref({...defaultScreenRegion}),colors=ref<RGB[]>([])
const saturation=ref(1.25),brightness=ref(1.10),contrast=ref(1.15),mirror=ref(false),smoothing=ref(200)
const withAudio=ref(false),audioMessage=ref(''),audioAvailable=screenAudioSupported()
let audioHandle:ScreenAudioHandle|undefined,audioContext:AudioContext|undefined
const secure=window.isSecureContext,supported=secure&&typeof navigator.mediaDevices?.getDisplayMedia==='function'
const httpsURL=computed(()=>{const url=new URL(location.href);url.protocol='https:';url.port=url.port==='80'?'':url.port;return url.href})
const selectionStyle=computed(()=>({left:`${region.value.x*100}%`,top:`${region.value.y*100}%`,width:`${region.value.width*100}%`,height:`${region.value.height*100}%`}))
const regionDraft=ref({x:'10',y:'65',width:'80',height:'20'});let editingRegion=''
function syncRegionDraft(){for(const key of ['x','y','width','height'] as const)if(editingRegion!==key)regionDraft.value[key]=String(Math.round(region.value[key]*100))}
watch(region,syncRegionDraft,{deep:true})
function commitRegion(key:keyof ScreenRegion){const raw=String(regionDraft.value[key]),value=Number(raw);if(raw.trim()&&Number.isFinite(value))region.value=normalizeRegion({...region.value,[key]:value/100});editingRegion='';syncRegionDraft();smoothed=undefined}
let stream:MediaStream|undefined,session:ScreenSession|undefined,timer:ReturnType<typeof setInterval>|undefined,generation=0,sequence=0,sending=false,pending:RGB[]|undefined,request:AbortController|undefined,lastSample=0,smoothed:RGB[]|undefined
const canvas=document.createElement('canvas'),context=canvas.getContext('2d',{willReadFrequently:true})
let drag:{id:number;kind:'move'|'draw'|'resize';x:number;y:number;region:ScreenRegion}|undefined
function reset(){region.value={...defaultScreenRegion};smoothed=undefined}
function point(event:PointerEvent){const box=surface.value!.getBoundingClientRect();return {x:Math.min(1,Math.max(0,(event.clientX-box.left)/box.width)),y:Math.min(1,Math.max(0,(event.clientY-box.top)/box.height))}}
function pointerDown(event:PointerEvent){if(!active.value||event.button!==0)return;const p=point(event),target=event.target as HTMLElement;drag={id:event.pointerId,kind:target.closest('.screen-led-handle')?'resize':target.closest('.screen-led-selection')?'move':'draw',...p,region:{...region.value}};surface.value!.setPointerCapture(event.pointerId);event.preventDefault()}
function pointerMove(event:PointerEvent){if(!drag||drag.id!==event.pointerId)return;const p=point(event),r=drag.region,dx=p.x-drag.x,dy=p.y-drag.y
 if(drag.kind==='move')region.value={...r,x:Math.min(1-r.width,Math.max(0,r.x+dx)),y:Math.min(1-r.height,Math.max(0,r.y+dy))}
 else {const x=drag.kind==='resize'?r.x:Math.min(drag.x,p.x),y=drag.kind==='resize'?r.y:Math.min(drag.y,p.y),width=Math.max(.05,drag.kind==='resize'?p.x-r.x:Math.abs(dx)),ratio=(video.value?.videoWidth||16)/(video.value?.videoHeight||9),height=Math.min(Math.max(.025,drag.kind==='resize'?p.y-r.y:Math.abs(dy)),width*ratio/2);region.value=normalizeRegion({x,y,width,height})}
 smoothed=undefined
}
function pointerUp(event:PointerEvent){if(drag?.id===event.pointerId){drag=undefined;if(surface.value?.hasPointerCapture(event.pointerId))surface.value.releasePointerCapture(event.pointerId)}}
function reason(e:unknown){if(e instanceof DOMException&&e.name==='NotAllowedError')return '画面の共有がキャンセルされたか、ブラウザで許可されませんでした。';if(e instanceof DOMException&&e.name==='NotReadableError')return '選択した画面を取得できません。別の画面やウィンドウを選んでください。';return e instanceof Error?e.message:String(e)}
async function removeSession(id:string){const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),1200);try{await api(`/device/led/screen/sessions/${encodeURIComponent(id)}`,'DELETE',undefined,true,controller.signal,true)}catch{/* The device lease also expires when updates stop. */}finally{clearTimeout(timeout)}}
async function stop(note='画面連動を停止しました。'){
 generation++;active.value=false;starting.value=false;clearInterval(timer);timer=undefined;pending=undefined;request?.abort();request=undefined
 const oldAudio=audioHandle;audioHandle=undefined;const oldContext=audioContext;audioContext=undefined;audioMessage.value='';void oldAudio?.stop();if(!oldAudio)void oldContext?.close().catch(()=>{})
 const old=session;session=undefined;const captured=stream;stream=undefined;captured?.getTracks().forEach(track=>track.stop());if(video.value)video.value.srcObject=null
 sending=false;smoothed=undefined;colors.value=[];message.value=note
 if(old)await removeSession(old.id)
}
async function send(){
 if(sending||!session||!pending||!active.value)return
 sending=true;const current=generation,id=session.id;const zones=pending.map(rgb=>rgb.map(Math.round));pending=undefined;const next=++sequence;const controller=new AbortController();request=controller;const timeout=setTimeout(()=>controller.abort(),1000)
 try{const response=await api<{accepted:boolean;sequence:number}>(`/device/led/screen/sessions/${encodeURIComponent(id)}`,'PUT',{sequence:next,zones},true,controller.signal);if(current===generation&&(!response.accepted||response.sequence!==next))throw new Error('LEDの更新を確認できません。')}
 catch(e){if(current===generation){error.value='接続が途切れたため、画面連動を停止しました。'+reason(e);void stop('')}}
 finally{clearTimeout(timeout);if(current===generation){sending=false;request=undefined;if(pending)void send()}}
}
function sample(){
 const element=video.value;if(!active.value||!element||element.readyState<2||!element.videoWidth||!context)return
 try{const scale=Math.min(1,640/element.videoWidth,360/element.videoHeight);canvas.width=Math.max(1,Math.round(element.videoWidth*scale));canvas.height=Math.max(1,Math.round(element.videoHeight*scale));context.drawImage(element,0,0,canvas.width,canvas.height)
 const image=context.getImageData(0,0,canvas.width,canvas.height),next=screenColors(image.data,image.width,image.height,region.value,{saturation:saturation.value,brightness:brightness.value,contrast:contrast.value,mirror:mirror.value}),now=performance.now();smoothed=smoothColors(smoothed,next,lastSample?now-lastSample:100,smoothing.value);lastSample=now;colors.value=smoothed.map(rgb=>rgb.map(Math.round) as RGB);pending=colors.value;void send()
 }catch(e){error.value='共有画面の色を取得できません。'+reason(e);void stop('')}
}
async function start(){
 if(starting.value||active.value||!supported)return
 const current=++generation;starting.value=true;error.value='';message.value='';audioMessage.value='';let captured:MediaStream|undefined
 try{
  if(withAudio.value&&audioAvailable)audioContext=prepareScreenAudio()
  // Must remain in the user button's activation chain: the native chooser is the browser's control.
  captured=await navigator.mediaDevices.getDisplayMedia({video:{frameRate:{ideal:10,max:15}},audio:withAudio.value})
  if(current!==generation){captured.getTracks().forEach(track=>track.stop());return}
  stream=captured;const track=captured.getVideoTracks()[0];if(!track||track.readyState==='ended')throw new Error('共有画面が終了しました。')
  track.addEventListener('ended',()=>{if(current===generation)void stop('画面の共有が終了しました。')},{once:true})
  await nextTick();video.value!.srcObject=captured;await video.value!.play()
  const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),5000);let created:ScreenSession
  try{created=await api<ScreenSession>('/device/led/screen/sessions','POST',{},true,controller.signal)}finally{clearTimeout(timeout)}
  if(!created?.id||!Number.isFinite(created.frameIntervalMs)||created.frameIntervalMs<50||created.frameIntervalMs>1000||!Number.isFinite(created.idleTimeoutMs)||created.idleTimeoutMs<created.frameIntervalMs)throw new Error('LEDセッションを確認できません。')
  if(current!==generation){void removeSession(created.id);return}
  session=created;sequence=0;lastSample=0;smoothed=undefined;active.value=true;starting.value=false;message.value='画面連動中';sample();timer=setInterval(sample,Math.max(100,created.frameIntervalMs))
  if(withAudio.value&&audioContext){const context=audioContext;if(!captured.getAudioTracks().length){audioMessage.value='共有音声を取得できませんでした。音声も再生する場合は、対応するタブや画面を選び、「音声を共有」を有効にしてください。';audioContext=undefined;void context.close().catch(()=>{})}else{audioMessage.value='NP1の音声を準備中…';try{const handle=await startScreenAudio(captured,context,note=>{if(current===generation)audioMessage.value=note});if(current!==generation){void handle.stop()}else{audioHandle=handle;audioMessage.value='共有音声をNP1で再生中'}}catch(e){if(current===generation)audioMessage.value='NP1で音声を再生できません。'+reason(e)}}}
 }catch(e){captured?.getTracks().forEach(track=>track.stop());if(current===generation){error.value=reason(e);await stop('')}}
}
function pageHide(){void stop('')}
onMounted(()=>window.addEventListener('pagehide',pageHide))
onUnmounted(()=>{window.removeEventListener('pagehide',pageHide);void stop('')})
</script>
<template>
 <section class="panel screen-led" aria-label="画面連動LED">
  <div class="panel-head"><h2>画面連動</h2><span class="tag">前面LED · 5エリア</span></div>
  <div class="settings-body">
   <p class="screen-led-intro">PCの画面やウィンドウの色に合わせて、5か所のLEDを点灯します。共有する画面は開始時に選べます。</p>
   <div v-if="!secure" class="screen-led-unavailable"><p>画面共有にはHTTPSまたはlocalhostでの接続が必要です。</p><a :href="httpsURL">HTTPSで開く ↗</a><p class="hint">証明書の確認が表示された場合は、ブラウザで接続を確認してください。</p></div>
   <p v-else-if="!supported" class="hint">このブラウザは画面共有に対応していません。対応するPCブラウザで開いてください。</p>
   <label class="screen-led-check screen-led-audio-choice"><input type="checkbox" v-model="withAudio" :disabled="active||starting||!audioAvailable||!supported">音声もNP1で再生</label><p v-if="withAudio" class="hint screen-led-audio-hint">画面の選択時に「音声を共有」も有効にしてください。対応する音声はブラウザ・共有対象によって異なります。</p><p v-else-if="!audioAvailable" class="hint">このブラウザは共有音声の変換に対応していません。</p>
   <div class="screen-led-actions"><button class="primary" @click="start" :disabled="!supported||active||starting">{{starting?'画面を準備中…':'画面を選んで開始'}}</button><button class="secondary" @click="stop()" :disabled="!active&&!starting">停止</button><span v-if="message" role="status" class="hint">{{message}}</span></div>
   <p v-if="error" class="error" role="alert">{{error}}</p>
   <p v-if="audioMessage" class="hint screen-led-audio-status" role="status">{{audioMessage}}</p>
   <div class="screen-led-preview" ref="surface" :class="{active}" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerUp">
    <video ref="video" autoplay muted playsinline :class="{'screen-led-video-hidden':!active&&!starting}" aria-label="共有画面のプレビュー"></video>
    <div v-if="!active&&!starting" class="screen-led-placeholder">画面を選ぶと、ここにプレビューが表示されます</div>
    <div v-if="active" class="screen-led-selection" :style="selectionStyle"><span v-for="n in 5" :key="n" class="screen-led-zone">{{n}}</span><span class="screen-led-handle" aria-hidden="true"></span></div>
   </div>
   <p class="hint screen-led-help">範囲をドラッグして選択。選択枠を動かすと移動、右下を動かすとサイズを変更できます。</p>
   <div class="screen-led-swatches" aria-label="LEDに送信する色"><span v-for="n in 5" :key="n" :style="{background:colors[n-1]?`rgb(${colors[n-1]!.join(',')})`:undefined}" :aria-label="`LED ${n}${colors[n-1]?': '+colors[n-1]!.join(', '):': 未送信'}`"><b>{{n}}</b></span></div>
   <div class="screen-led-options">
    <label>彩度 <output>{{Math.round(saturation*100)}}%</output><input type="range" v-model.number="saturation" min="0" max="2" step=".05" aria-label="画面連動の彩度"></label>
    <label>明るさ <output>{{Math.round(brightness*100)}}%</output><input type="range" v-model.number="brightness" min="0" max="2" step=".05" aria-label="画面連動の明るさ"></label>
    <label>コントラスト <output>{{Math.round(contrast*100)}}%</output><input type="range" v-model.number="contrast" min="0" max="2" step=".05" aria-label="画面連動のコントラスト"></label>
    <label>色のなめらかさ <output>{{smoothing===0?'なし':smoothing+' ms'}}</output><input type="range" v-model.number="smoothing" min="0" max="1000" step="50" aria-label="色のなめらかさ"></label>
   </div>
   <div class="screen-led-actions screen-led-secondary"><label class="screen-led-check"><input type="checkbox" v-model="mirror">左右を反転</label><button class="text-button" @click="reset">範囲をリセット</button></div>
   <details class="screen-led-region"><summary>範囲を数値で調整</summary><div><label v-for="(label,key) in {x:'左位置',y:'上位置',width:'幅',height:'高さ'}" :key="key">{{label}} %<input type="number" v-model="regionDraft[key]" min="0" max="100" @focus="editingRegion=key" @blur="commitRegion(key)" @keydown.enter="($event.target as HTMLInputElement).blur()" :aria-label="`選択範囲の${label}`"></label></div></details>
   <p class="hint screen-led-note">画面の色はこのブラウザ内で計算し、5色のRGB値だけを本体へ送ります。停止すると、通常のLED設定に戻ります。</p>
  </div>
 </section>
</template>
