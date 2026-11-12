<script setup lang="ts">
import {onMounted,onUnmounted,ref} from 'vue'
import {ExternalLink,Pause,Play} from '@lucide/vue'
import {api} from '../services/api'
const emit=defineEmits<{open:[]}>(),container=ref<HTMLElement>(),paused=ref(false),running=ref(false)
const images=ref([{source:'front',label:'前方',url:'',id:'',waiting:true},{source:'in',label:'車内',url:'',id:'',waiting:true}])
let generation=0,visible=false,disposed=false,observer:IntersectionObserver|undefined
const timers=new Set<ReturnType<typeof setTimeout>>(),controllers=new Set<AbortController>()
async function release(id:string){try{await api('/camera/sessions/'+encodeURIComponent(id),'DELETE',undefined,true,undefined,true)}catch{}}
function stop(){running.value=false;generation++;for(const timer of timers)clearTimeout(timer);timers.clear();for(const controller of controllers)controller.abort();controllers.clear();for(const item of images.value){if(item.id)void release(item.id);item.id='';if(item.url)URL.revokeObjectURL(item.url);item.url='';item.waiting=true}}
async function frame(index:number,g:number){
 const item=images.value[index]!
 if(g!==generation||!running.value)return
 let controller:AbortController|undefined,deadline:ReturnType<typeof setTimeout>|undefined
 try{
  if(!item.id){const session=await api<{id:string}>('/camera/sessions','POST',{source:item.source,color:'auto'});if(typeof session.id!=='string'||!session.id)throw new Error('No session');if(g!==generation||!running.value){void release(session.id);return}item.id=session.id}
  controller=new AbortController();controllers.add(controller)
  deadline=setTimeout(()=>controller?.abort(),8000);timers.add(deadline)
  const response=await fetch('/api/v1/camera/sessions/'+encodeURIComponent(item.id)+'/stream',{signal:controller.signal})
  if(g!==generation)return
  if(response.status===403){void release(item.id);item.id='';throw new Error('Session expired')}
  if(!response.ok||!response.headers.get('content-type')?.includes('image/jpeg'))throw new Error('Frame unavailable')
  const blob=await response.blob();if(g!==generation)return
  const old=item.url;item.url=URL.createObjectURL(blob);item.waiting=false;if(old)URL.revokeObjectURL(old)
 }catch{if(g===generation){item.waiting=true;if(item.url)URL.revokeObjectURL(item.url);item.url=''}}
 finally{if(controller)controllers.delete(controller);if(deadline){clearTimeout(deadline);timers.delete(deadline)}}
}
async function cycle(g:number){
 // The broker serializes JPEG conversion across both camera sources.
 await frame(0,g);if(g!==generation||!running.value)return
 await frame(1,g);if(g!==generation||!running.value)return
 const timer=setTimeout(()=>{timers.delete(timer);void cycle(g)},2000);timers.add(timer)
}
function update(){const shouldRun=!disposed&&visible&&!document.hidden&&!paused.value;if(shouldRun===running.value)return;if(!shouldRun){stop();return}running.value=true;void cycle(++generation)}
function toggle(){paused.value=!paused.value;update()}
onMounted(()=>{document.addEventListener('visibilitychange',update);if(container.value){observer=new IntersectionObserver(entries=>{visible=!!entries[0]?.isIntersecting;update()},{rootMargin:'60px'});observer.observe(container.value)}})
onUnmounted(()=>{disposed=true;observer?.disconnect();document.removeEventListener('visibilitychange',update);stop()})
</script>
<template>
 <div ref="container" class="dashboard-cameras" aria-label="低負荷のカメラライブ表示"><div class="dashboard-camera-tools"><div class="dashboard-live-tools"><span>ライブ</span><button class="icon-button" :aria-label="paused?'ライブ映像を再開':'ライブ映像を停止'" @click="toggle"><Play v-if="paused" :size="14"/><Pause v-else :size="14"/></button><button class="icon-button" aria-label="ライブを開く" title="ライブを開く" @click="emit('open')"><ExternalLink :size="14"/></button></div><slot/></div><div class="dashboard-camera-pair"><figure v-for="image in images" :key="image.source"><img v-if="image.url" :src="image.url" :alt="image.label+'のライブ映像'"><span v-else class="dashboard-camera-wait">{{paused?'停止中':image.waiting?'映像待ち':'読込中'}}</span><figcaption>{{image.label}}</figcaption></figure></div></div>
</template>
<style scoped>
.dashboard-cameras{min-width:0}.dashboard-camera-tools{display:flex;align-items:center;gap:4px;min-height:44px;padding:7px 14px;color:var(--muted)}.dashboard-camera-tools .icon-button{width:28px;height:28px;min-height:28px;color:var(--muted);flex-shrink:0}.dashboard-camera-pair{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0}.dashboard-camera-pair figure{position:relative;aspect-ratio:16/9;min-width:0;background:#101318;overflow:hidden;margin:0}.dashboard-camera-pair figure+figure{border-left:1px solid var(--line)}.dashboard-camera-pair img{display:block;width:100%;height:100%;object-fit:contain}.dashboard-camera-pair figcaption{position:absolute;top:5px;left:6px;padding:2px 5px;background:#101318bb;border-radius:3px;color:#fff;font-size:10px}.dashboard-camera-wait{position:absolute;inset:0;display:grid;place-items:center;color:#a9b1bd;font-size:11px}@media(max-width:600px){.dashboard-camera-tools{padding:7px 10px;gap:2px}}
</style>
<style scoped>
.dashboard-camera-tools{justify-content:space-between;flex-wrap:wrap;gap:6px 14px}.dashboard-live-tools{display:flex;align-items:center;gap:3px;color:var(--text);font-size:12px;white-space:nowrap}.dashboard-live-tools>span{margin-right:4px}
</style>
