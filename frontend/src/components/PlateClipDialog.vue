<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref} from 'vue'
import {ExternalLink,Film,RotateCcw,X} from '@lucide/vue'
import {api,type Observation} from '../services/api'
import {plateDisplay,type PlateRecord} from '../services/plate'

// Plays only the front-camera recording around one plate record, without leaving the plate page.
type Clip={id:string;camera:'front'|'in';startAt:string;endAt:string;durationSeconds:number}
type Catalog={clips:Clip[];pendingFiles:number}
const props=defineProps<{record:PlateRecord}>()
const emit=defineEmits<{close:[]}>()
// Lead-in before the first reading and run-out after the last one; long sightings are capped.
const LEAD=10_000,TAIL=10_000,LONGEST=180_000
const first=computed(()=>Date.parse(props.record.firstSeen)),last=computed(()=>Math.max(first.value,Date.parse(props.record.lastSeen)||first.value))
const from=computed(()=>first.value-LEAD),until=computed(()=>Math.min(last.value+TAIL,from.value+LONGEST))
const clips=ref<Clip[]>([]),index=ref(0),loading=ref(true),error=ref(''),notice=ref(''),position=ref(0),finished=ref(false),pending=ref(0)
const video=ref<HTMLVideoElement>(),dialog=ref<HTMLElement>()
const clip=computed(()=>clips.value[index.value])
const percent=(time:number)=>Math.max(0,Math.min(100,(time-from.value)/(until.value-from.value)*100))
const clock=(time:number)=>new Intl.DateTimeFormat('ja-JP',{timeZone:'Asia/Tokyo',hour:'2-digit',minute:'2-digit',second:'2-digit',hourCycle:'h23'}).format(new Date(time))
const day=(time:number)=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date(time))
const playbackLink=computed(()=>'#recordings?at='+encodeURIComponent(new Date(from.value).toISOString()))
const covered=computed(()=>clips.value.length>0&&Date.parse(clips.value[0]!.startAt)<=first.value&&clips.value.some(c=>Date.parse(c.endAt)>first.value))

async function load(){
 loading.value=true;error.value='';clips.value=[];pending.value=0
 try{
  const found=new Map<string,Clip>()
  for(const date of new Set([day(from.value),day(until.value)])){
   const catalog=await api<Observation<Catalog>>('/recordings?date='+date)
   pending.value+=catalog.data?.pendingFiles||0
   for(const c of catalog.data?.clips||[])if(c.camera==='front'&&Date.parse(c.startAt)<until.value&&Date.parse(c.endAt)>from.value)found.set(c.id,c)
  }
  clips.value=[...found.values()].sort((a,b)=>Date.parse(a.startAt)-Date.parse(b.startAt))
  index.value=0;position.value=from.value;finished.value=false
 }catch(e){error.value=e instanceof Error?e.message:String(e)}
 finally{loading.value=false}
}
const offset=(c:Clip,time:number)=>Math.max(0,Math.min(c.durationSeconds-.05,(time-Date.parse(c.startAt))/1000))
function ready(){
 const element=video.value,c=clip.value;if(!element||!c)return
 element.currentTime=offset(c,Math.max(from.value,position.value))
 element.play().catch(()=>{notice.value='再生ボタンを押して開始してください。'})
}
function tick(){
 const element=video.value,c=clip.value;if(!element||!c)return
 position.value=Date.parse(c.startAt)+element.currentTime*1000
 if(position.value>=until.value){element.pause();finished.value=true}
}
// Clips are a minute or so long; a sighting near a boundary continues into the next file.
async function advance(){
 const c=clip.value;if(!c)return
 const end=Date.parse(c.endAt),next=clips.value.findIndex((n,i)=>i>index.value&&Date.parse(n.startAt)<=end+2000&&Date.parse(n.endAt)>end)
 if(next<0||end>=until.value){finished.value=true;return}
 position.value=Math.max(end,Date.parse(clips.value[next]!.startAt));index.value=next
}
async function replay(){finished.value=false;notice.value='';position.value=from.value;if(index.value!==0){index.value=0}else{await nextTick();ready()}}
function key(event:KeyboardEvent){if(event.key==='Escape')emit('close')}
onMounted(async()=>{document.addEventListener('keydown',key);dialog.value?.focus();await load()})
onUnmounted(()=>{document.removeEventListener('keydown',key);video.value?.pause()})
</script>

<template>
 <div class="modal-backdrop" @click.self="emit('close')">
  <section ref="dialog" class="modal plate-clip-modal" role="dialog" aria-modal="true" tabindex="-1" :aria-label="`${plateDisplay(record)} の録画`">
   <div class="modal-title"><div><h2>{{plateDisplay(record)}}</h2><p class="plate-small muted">前方カメラ · {{clock(from)}} — {{clock(until)}}</p></div><button class="icon-button" aria-label="閉じる" @click="emit('close')"><X :size="16"/></button></div>
   <div class="plate-clip-frame">
    <video v-if="clip" :key="clip.id" ref="video" :src="`/api/v1/recordings/${encodeURIComponent(clip.id)}/stream`" controls playsinline preload="metadata" @loadedmetadata="ready" @timeupdate="tick" @ended="advance" @error="error='この録画をブラウザーで再生できません。ドラレコ再生画面から原本を確認してください。'"></video>
    <div v-else class="plate-clip-empty"><Film :size="32" stroke-width="1.3"/><strong>{{loading?'録画を探しています…':error?'録画を取得できません':'この時刻の前方カメラの録画はありません'}}</strong><span v-if="!loading&&!error">{{pending?'録画がまだ確定していない可能性があります。少し待ってから開き直してください。':'保存期間を過ぎて上書きされたか、録画していなかった時間です。'}}</span></div>
   </div>
   <div class="plate-clip-bar" aria-hidden="true"><i class="plate-clip-seen" :style="{left:percent(first)+'%',width:Math.max(.6,percent(last)-percent(first))+'%'}"></i><i class="plate-clip-head" :style="{left:percent(position)+'%'}"></i></div>
   <div class="plate-clip-times"><span>{{clock(from)}}</span><span class="plate-clip-seen-label">検出 {{clock(first)}}<template v-if="last>first+1000"> — {{clock(last)}}</template></span><span>{{clock(until)}}</span></div>
   <p v-if="error" class="plate-clip-message error" role="alert">{{error}}</p>
   <p v-else-if="notice" class="plate-clip-message" role="status">{{notice}}</p>
   <p v-else-if="clip&&!covered" class="plate-clip-message" role="status">検出時刻の一部は録画がありません。録画のある範囲だけを再生します。</p>
   <div class="plate-clip-actions"><button class="secondary compact" type="button" :disabled="!clip" @click="replay"><RotateCcw :size="14"/>{{finished?'もう一度再生':'最初から'}}</button><a class="text-button" :href="playbackLink" @click="emit('close')"><ExternalLink :size="14"/>ドラレコ再生で開く</a></div>
  </section>
 </div>
</template>

<style scoped>
.plate-clip-modal{max-width:880px;outline:none}
.plate-clip-modal .modal-title{align-items:flex-start;gap:12px;margin-bottom:14px}
.plate-clip-modal .modal-title p{margin:4px 0 0}
.plate-clip-frame{position:relative;aspect-ratio:16/9;border-radius:10px;overflow:hidden;background:#101318}
.plate-clip-frame video{display:block;width:100%;height:100%;object-fit:contain;background:#101318}
.plate-clip-empty{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:8px;padding:20px;text-align:center;color:#a9b1bd;font-size:12px;line-height:1.7}
.plate-clip-empty strong{color:#e6e9ee;font-size:14px;font-weight:600}
.plate-clip-bar{position:relative;height:8px;margin:14px 0 6px;border-radius:4px;background:var(--bg);overflow:hidden}
.plate-clip-seen{position:absolute;top:0;bottom:0;background:color-mix(in srgb,#e8590c 70%,transparent);border-radius:4px}
.plate-clip-head{position:absolute;top:-2px;bottom:-2px;width:3px;margin-left:-1px;background:var(--accent);border-radius:2px}
.plate-clip-times{display:flex;justify-content:space-between;gap:10px;font-size:11px;color:var(--muted);font-variant-numeric:tabular-nums}
.plate-clip-seen-label{color:var(--text);font-weight:600}
.plate-clip-message{margin:12px 0 0;font-size:12px;color:var(--muted)}
.plate-clip-message.error{color:var(--danger)}
.plate-clip-actions{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px;margin-top:14px}
@media(max-width:600px){.modal-backdrop:has(.plate-clip-modal){padding:10px}.plate-clip-modal{padding:16px}}
</style>
